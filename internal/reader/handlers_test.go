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
}
