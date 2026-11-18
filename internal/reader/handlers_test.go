package reader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/go-chi/chi/v5"
)

const handlerStructure = `{
	"source":{"title":"Аудиолекция","kind":"audio","duration":42},
	"sections":[{"title":"Тема","subtopics":[{"title":"Подтема","content_md":"Текст"}]}]
}`

type handlerRepo struct {
	lecture *LectureMeta
}

func (r handlerRepo) FindByID(context.Context, string) (*LectureMeta, error) {
	return r.lecture, nil
}

type handlerStore struct {
	data []byte
	err  error
}

func (s handlerStore) GetObject(context.Context, string) ([]byte, error) {
	return s.data, s.err
}

type handlerPresigner struct{}

func (handlerPresigner) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "https://storage.example/object", nil
}

type handlerExport struct {
	url string
	err error
}

func (e handlerExport) GetResultURL(context.Context, string, string) (string, error) {
	return e.url, e.err
}

// forbiddenExport фиксирует ошибочный вызов провайдера при запрещённом доступе.
type forbiddenExport struct {
	t     *testing.T
	calls int
}

func (e *forbiddenExport) GetResultURL(context.Context, string, string) (string, error) {
	e.calls++
	e.t.Error("GetResultURL вызван при запрещённом доступе")
	return "", errors.New("вызов провайдера не должен происходить")
}

type handlerAuthRepo struct{}

func (handlerAuthRepo) FindUserByEmail(context.Context, string) (*auth.User, error) { return nil, nil }
func (handlerAuthRepo) FindUserByID(context.Context, string) (*auth.User, error) {
	return &auth.User{ID: "owner"}, nil
}
func (handlerAuthRepo) CreateUser(context.Context, auth.Profile) (*auth.User, error) { return nil, nil }
func (handlerAuthRepo) UpsertIdentity(context.Context, string, string, string) error { return nil }
func (handlerAuthRepo) CreateSession(context.Context, string, time.Time) (*auth.Session, error) {
	return nil, nil
}
func (handlerAuthRepo) GetSession(context.Context, string) (*auth.Session, error) {
	return &auth.Session{ID: "session", UserID: "owner", ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (handlerAuthRepo) DeleteSession(context.Context, string) error { return nil }

func newReaderHandler(t *testing.T, lecture *LectureMeta, storeErr error, export ExportURLProvider) http.Handler {
	t.Helper()
	svc := NewService(handlerRepo{lecture: lecture}, handlerStore{data: []byte(handlerStructure), err: storeErr}, handlerPresigner{}, NewMarkdownRenderer(), time.Hour)
	r := chi.NewRouter()
	authSvc := auth.NewService(handlerAuthRepo{}, nil, time.Hour, false)
	r.Use(authSvc.LoadSession)
	NewHandlers(svc, export).Mount(r)
	return r
}

func readerLecture(status, visibility string) *LectureMeta {
	return &LectureMeta{ID: "lecture", OwnerID: "owner", Status: status, Visibility: visibility, CoreTaskID: "task", Title: "Заголовок", SourceKind: "audio"}
}

func requestReader(handler http.Handler, path string, owner bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if owner {
		req.AddCookie(&http.Cookie{Name: "ll_session", Value: "session"})
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertSoftError(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"panic", "boom", "stack", "ошибка хранилища"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("тело ошибки раскрывает внутренние детали %q: %s", forbidden, body)
		}
	}
}

func TestHandlersReadPublicReadyAnonymous(t *testing.T) {
	rec := requestReader(newReaderHandler(t, readerLecture("ready", "public"), nil, nil), "/read/lecture", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /read/lecture = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Подтема") {
		t.Fatalf("страница не содержит раздел: %s", rec.Body.String())
	}
	// Публичная лекция индексируется и не запрещает кеширование.
	if got := rec.Header().Get("X-Robots-Tag"); got != "" {
		t.Errorf("X-Robots-Tag публичной лекции = %q, want пусто", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control публичной лекции = %q, want пусто", got)
	}
	if strings.Contains(rec.Body.String(), `name="robots"`) {
		t.Error("публичная лекция не должна содержать meta robots")
	}
}

// TestHandlersReadUnlistedAnonymous проверяет чтение unlisted-лекции анонимом:
// 200, доступ по прямой ссылке и запрет индексации/кеширования.
func TestHandlersReadUnlistedAnonymous(t *testing.T) {
	rec := requestReader(newReaderHandler(t, readerLecture("ready", "unlisted"), nil, nil), "/read/lecture", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET unlisted /read/lecture = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Подтема") {
		t.Fatalf("страница не содержит раздел: %s", rec.Body.String())
	}
	assertNoIndexHeaders(t, rec)
}

// TestHandlersReadUnlistedProcessingHidden проверяет, что неготовая unlisted-лекция
// не открывается анониму и отдаёт 202 владельцу.
func TestHandlersReadUnlistedProcessingHidden(t *testing.T) {
	handler := newReaderHandler(t, readerLecture("processing", "unlisted"), nil, nil)

	anonymous := requestReader(handler, "/read/lecture", false)
	if anonymous.Code != http.StatusNotFound {
		t.Fatalf("GET unlisted processing анонимом = %d, want 404", anonymous.Code)
	}
	assertSoftError(t, anonymous.Body.String())

	owner := requestReader(handler, "/read/lecture", true)
	if owner.Code != http.StatusAccepted {
		t.Fatalf("GET unlisted processing владельцем = %d, want 202", owner.Code)
	}
	assertSoftError(t, owner.Body.String())
}

// TestHandlersReadPrivateOwnerNoIndex проверяет, что владелец читает свою
// личную лекцию, но страница закрыта от индексации и кеша.
func TestHandlersReadPrivateOwnerNoIndex(t *testing.T) {
	rec := requestReader(newReaderHandler(t, readerLecture("ready", "private"), nil, nil), "/read/lecture", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET private /read/lecture владельцем = %d, want 200", rec.Code)
	}
	assertNoIndexHeaders(t, rec)
}

// assertNoIndexHeaders проверяет пару заголовков запрета индексации и метатег в <head>.
func assertNoIndexHeaders(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Errorf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", got)
	}
	if !strings.Contains(rec.Body.String(), `<meta name="robots" content="noindex, nofollow">`) {
		t.Error("в <head> ожидается meta robots noindex, nofollow")
	}
}

func TestHandlersReadNotFound(t *testing.T) {
	rec := requestReader(newReaderHandler(t, nil, nil, nil), "/read/missing", false)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /read/missing = %d, want 404", rec.Code)
	}
	assertSoftError(t, rec.Body.String())
}

func TestHandlersReadPrivateAnonymousHidden(t *testing.T) {
	rec := requestReader(newReaderHandler(t, readerLecture("ready", "private"), nil, nil), "/read/lecture", false)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET private /read/lecture = %d, want 404", rec.Code)
	}
	assertSoftError(t, rec.Body.String())
}

func TestHandlersReadOwnerNotReady(t *testing.T) {
	rec := requestReader(newReaderHandler(t, readerLecture("processing", "private"), nil, nil), "/read/lecture", true)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "обрабатывается") {
		t.Fatalf("GET owner processing /read/lecture = %d, body: %s", rec.Code, rec.Body.String())
	}
	assertSoftError(t, rec.Body.String())
}

func TestHandlersReadCoreUnavailable(t *testing.T) {
	rec := requestReader(newReaderHandler(t, readerLecture("ready", "public"), errors.New("ошибка хранилища"), nil), "/read/lecture", false)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("GET unavailable /read/lecture = %d, want 502", rec.Code)
	}
	assertSoftError(t, rec.Body.String())
}

func TestHandlersExport(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		rec := requestReader(newReaderHandler(t, readerLecture("ready", "public"), nil, handlerExport{url: "https://signed"}), "/read/lecture/export", false)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://signed" {
			t.Fatalf("GET /read/lecture/export = %d Location=%q", rec.Code, rec.Header().Get("Location"))
		}
	})
	t.Run("core error", func(t *testing.T) {
		rec := requestReader(newReaderHandler(t, readerLecture("ready", "public"), nil, handlerExport{err: errors.New("boom")}), "/read/lecture/export", false)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("GET unavailable export = %d, want 502", rec.Code)
		}
		assertSoftError(t, rec.Body.String())
	})
	t.Run("unlisted anonymous redirects to signed archive", func(t *testing.T) {
		rec := requestReader(newReaderHandler(t, readerLecture("ready", "unlisted"), nil, handlerExport{url: "https://signed"}), "/read/lecture/export", false)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://signed" {
			t.Fatalf("GET unlisted /read/lecture/export = %d Location=%q, want 302", rec.Code, rec.Header().Get("Location"))
		}
	})
	t.Run("private anonymous is hidden before provider", func(t *testing.T) {
		export := &forbiddenExport{t: t}
		rec := requestReader(newReaderHandler(t, readerLecture("ready", "private"), nil, export), "/read/lecture/export", false)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET private /read/lecture/export = %d, want 404", rec.Code)
		}
		if export.calls != 0 {
			t.Fatalf("GetResultURL вызван %d раз", export.calls)
		}
	})
	t.Run("processing anonymous is hidden before provider", func(t *testing.T) {
		export := &forbiddenExport{t: t}
		rec := requestReader(newReaderHandler(t, readerLecture("processing", "private"), nil, export), "/read/lecture/export", false)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET processing /read/lecture/export = %d, want 404", rec.Code)
		}
		if export.calls != 0 {
			t.Fatalf("GetResultURL вызван %d раз", export.calls)
		}
	})
}
