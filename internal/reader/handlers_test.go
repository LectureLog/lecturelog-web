package reader

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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

const slideStructure = `{
	"source":{"title":"Аудиолекция","kind":"audio","duration":42},
	"sections":[{"title":"Тема","subtopics":[{
		"title":"Подтема",
		"slide_keys":["results/task/slide-1.png","results/task/slide-2.jpg","results/task/slide-3.webp"],
		"slide_nums":[1,2,3],
		"content_md":"Текст"
	}]}]
}`

var (
	pngSlideBytes  = []byte("\x89PNG\r\n\x1a\nslide-1")
	jpegSlideBytes = []byte("\xff\xd8\xff\xe0slide-2")
	webpSlideBytes = []byte("RIFF\x00\x00\x00\x00WEBPslide-3")
)

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

// objectStore отдаёт объекты по ключам и запоминает обращения.
type objectStore struct {
	objects map[string][]byte
	errs    map[string]error
	calls   []string
}

func (s *objectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	s.calls = append(s.calls, key)
	if err, ok := s.errs[key]; ok {
		return nil, err
	}
	return s.objects[key], nil
}

func newSlideHandler(t *testing.T, lecture *LectureMeta, store *objectStore) http.Handler {
	t.Helper()
	svc := NewService(handlerRepo{lecture: lecture}, store, handlerPresigner{}, NewMarkdownRenderer(), time.Hour)
	r := chi.NewRouter()
	authSvc := auth.NewService(handlerAuthRepo{}, nil, time.Hour, false)
	r.Use(authSvc.LoadSession)
	NewHandlers(svc, nil).Mount(r)
	return r
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

func TestHandlersSlideServesImage(t *testing.T) {
	objects := map[string][]byte{
		"results/task/output/structure.json": []byte(slideStructure),
		"results/task/slide-1.png":           pngSlideBytes,
		"results/task/slide-2.jpg":           jpegSlideBytes,
		"results/task/slide-3.webp":          webpSlideBytes,
	}

	tests := []struct {
		name       string
		visibility string
		owner      bool
		path       string
		wantBody   []byte
		wantType   string
		wantCache  string
		wantRobots string
	}{
		{
			name:       "owner private png",
			visibility: "private",
			owner:      true,
			path:       "/read/lecture/slides/1",
			wantBody:   pngSlideBytes,
			wantType:   "image/png",
			wantCache:  "private, no-store",
			wantRobots: "noindex, nofollow",
		},
		{
			name:       "anonymous unlisted jpeg",
			visibility: "unlisted",
			path:       "/read/lecture/slides/2",
			wantBody:   jpegSlideBytes,
			wantType:   "image/jpeg",
			wantCache:  "private, no-store",
			wantRobots: "noindex, nofollow",
		},
		{
			name:       "anonymous public webp",
			visibility: "public",
			path:       "/read/lecture/slides/3",
			wantBody:   webpSlideBytes,
			wantType:   "image/webp",
			wantCache:  "public, max-age=3600",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &objectStore{objects: objects}
			handler := newSlideHandler(t, readerLecture("ready", tt.visibility), store)
			rec := requestReader(handler, tt.path, tt.owner)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200: %s", tt.path, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if got := rec.Header().Get("X-Robots-Tag"); got != tt.wantRobots {
				t.Fatalf("X-Robots-Tag = %q, want %q", got, tt.wantRobots)
			}
			if !bytes.Equal(rec.Body.Bytes(), tt.wantBody) {
				t.Fatalf("тело ответа = %q, want %q", rec.Body.Bytes(), tt.wantBody)
			}
		})
	}
}

func TestHandlersSlideDetectsTypeByBytes(t *testing.T) {
	structure := `{
		"source":{"title":"Лекция","kind":"audio","duration":60},
		"sections":[{"title":"Тема","subtopics":[{
			"title":"Подтема",
			"slide_keys":["results/task/frame"],
			"slide_nums":[1],
			"content_md":"Текст"
		}]}]
	}`
	store := &objectStore{objects: map[string][]byte{
		"results/task/output/structure.json": []byte(structure),
		"results/task/frame":                 pngSlideBytes,
	}}
	handler := newSlideHandler(t, readerLecture("ready", "public"), store)
	rec := requestReader(handler, "/read/lecture/slides/1", false)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET slide = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
}

func TestHandlersSlideNumbering(t *testing.T) {
	explicitStructure := `{
		"source":{"title":"Л","kind":"audio","duration":60},
		"sections":[{"title":"С","subtopics":[{
			"title":"П",
			"slide_keys":["results/task/a.png","results/task/b.png"],
			"slide_nums":[3,4],
			"content_md":"Текст"
		}]}]
	}`
	legacyStructure := `{
		"source":{"title":"Л","kind":"audio","duration":60},
		"sections":[
			{"title":"С1","subtopics":[{"title":"П1","slide_keys":["results/task/a.png","results/task/b.png"],"content_md":"Текст"}]},
			{"title":"С2","subtopics":[{"title":"П2","slide_keys":["results/task/c.png"],"content_md":"Текст"}]}
		]
	}`
	mixedStructure := `{
		"source":{"title":"Л","kind":"audio","duration":60},
		"sections":[
			{"title":"С1","subtopics":[{"title":"П1","slide_keys":["results/task/a.png"],"slide_nums":[2],"content_md":"Текст"}]},
			{"title":"С2","subtopics":[{"title":"П2","slide_keys":["results/task/b.png","results/task/c.png"],"content_md":"Текст"}]}
		]
	}`

	tests := []struct {
		name      string
		structure string
		keys      []string
		want      map[int]string
		missing   int
	}{
		{
			name:      "explicit slide nums",
			structure: explicitStructure,
			keys:      []string{"results/task/a.png", "results/task/b.png"},
			want:      map[int]string{3: "results/task/a.png", 4: "results/task/b.png"},
			missing:   1,
		},
		{
			name:      "legacy without slide nums",
			structure: legacyStructure,
			keys:      []string{"results/task/a.png", "results/task/b.png", "results/task/c.png"},
			want:      map[int]string{1: "results/task/a.png", 2: "results/task/b.png", 3: "results/task/c.png"},
			missing:   4,
		},
		{
			name:      "mixed numbers without collisions",
			structure: mixedStructure,
			keys:      []string{"results/task/a.png", "results/task/b.png", "results/task/c.png"},
			want:      map[int]string{2: "results/task/a.png", 3: "results/task/b.png", 4: "results/task/c.png"},
			missing:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := map[string][]byte{"results/task/output/structure.json": []byte(tt.structure)}
			for _, key := range tt.keys {
				objects[key] = []byte(key)
			}
			for num, key := range tt.want {
				store := &objectStore{objects: objects}
				handler := newSlideHandler(t, readerLecture("ready", "public"), store)
				rec := requestReader(handler, "/read/lecture/slides/"+strconv.Itoa(num), false)
				if rec.Code != http.StatusOK {
					t.Fatalf("GET slide %d = %d, want 200: %s", num, rec.Code, rec.Body.String())
				}
				if !bytes.Equal(rec.Body.Bytes(), []byte(key)) {
					t.Fatalf("slide %d = %q, want %q", num, rec.Body.Bytes(), key)
				}
			}

			store := &objectStore{objects: objects}
			handler := newSlideHandler(t, readerLecture("ready", "public"), store)
			rec := requestReader(handler, "/read/lecture/slides/"+strconv.Itoa(tt.missing), false)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET missing slide %d = %d, want 404", tt.missing, rec.Code)
			}
		})
	}
}

func TestHandlersSlideDeniedDoesNotReadStore(t *testing.T) {
	tests := []struct {
		name       string
		lecture    *LectureMeta
		owner      bool
		wantStatus int
		wantCache  string
		wantRobots string
	}{
		{name: "private anonymous", lecture: readerLecture("ready", "private"), wantStatus: http.StatusNotFound},
		{name: "private other user", lecture: &LectureMeta{ID: "lecture", OwnerID: "another-owner", Status: "ready", Visibility: "private", CoreTaskID: "task"}, owner: true, wantStatus: http.StatusNotFound},
		{name: "processing anonymous", lecture: readerLecture("processing", "private"), wantStatus: http.StatusNotFound},
		{name: "unlisted processing anonymous", lecture: readerLecture("processing", "unlisted"), wantStatus: http.StatusNotFound},
		{name: "public processing anonymous", lecture: readerLecture("processing", "public"), wantStatus: http.StatusNotFound},
		{name: "private processing owner", lecture: readerLecture("processing", "private"), owner: true, wantStatus: http.StatusAccepted, wantCache: "private, no-store", wantRobots: "noindex, nofollow"},
		{name: "unlisted processing owner", lecture: readerLecture("processing", "unlisted"), owner: true, wantStatus: http.StatusAccepted, wantCache: "private, no-store", wantRobots: "noindex, nofollow"},
		{name: "public processing owner", lecture: readerLecture("processing", "public"), owner: true, wantStatus: http.StatusAccepted},
		{name: "missing lecture", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &objectStore{objects: map[string][]byte{
				"results/task/output/structure.json": []byte(slideStructure),
				"results/task/slide-1.png":           pngSlideBytes,
			}}
			rec := requestReader(newSlideHandler(t, tt.lecture, store), "/read/lecture/slides/1", tt.owner)
			if rec.Code != tt.wantStatus {
				t.Fatalf("GET denied slide = %d, want %d", rec.Code, tt.wantStatus)
			}
			if len(store.calls) != 0 {
				t.Fatalf("GetObject вызван при запрещённом доступе: %v", store.calls)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if got := rec.Header().Get("X-Robots-Tag"); got != tt.wantRobots {
				t.Fatalf("X-Robots-Tag = %q, want %q", got, tt.wantRobots)
			}
			assertSoftError(t, rec.Body.String())
		})
	}
}

func TestHandlersSlideInvalidNumber(t *testing.T) {
	for _, n := range []string{"0", "-1", "abc"} {
		t.Run(n, func(t *testing.T) {
			store := &objectStore{objects: map[string][]byte{
				"results/task/output/structure.json": []byte(slideStructure),
				"results/task/slide-1.png":           pngSlideBytes,
			}}
			handler := newSlideHandler(t, readerLecture("ready", "public"), store)
			rec := requestReader(handler, "/read/lecture/slides/"+n, false)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET slide %q = %d, want 404", n, rec.Code)
			}
			if len(store.calls) != 0 {
				t.Fatalf("GetObject вызван для некорректного номера: %v", store.calls)
			}
			if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
				t.Fatalf("Content-Type = %q, want text/plain", got)
			}
			assertSoftError(t, rec.Body.String())
		})
	}
}

func TestHandlersSlideUnknownNumberDoesNotReadSlideObject(t *testing.T) {
	store := &objectStore{objects: map[string][]byte{
		"results/task/output/structure.json": []byte(slideStructure),
		"results/task/slide-1.png":           pngSlideBytes,
	}}
	handler := newSlideHandler(t, readerLecture("ready", "public"), store)
	rec := requestReader(handler, "/read/lecture/slides/999", false)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET unknown slide = %d, want 404", rec.Code)
	}
	for _, call := range store.calls {
		if strings.HasPrefix(call, "results/task/slide-") {
			t.Fatalf("GetObject прочитал объект слайда для отсутствующего номера: %v", store.calls)
		}
	}
}

func TestHandlersSlideCoreUnavailable(t *testing.T) {
	t.Run("structure", func(t *testing.T) {
		store := &objectStore{errs: map[string]error{
			"results/task/output/structure.json": errors.New("ошибка хранилища"),
		}}
		handler := newSlideHandler(t, readerLecture("ready", "public"), store)
		rec := requestReader(handler, "/read/lecture/slides/1", false)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("GET slide without structure = %d, want 502", rec.Code)
		}
		assertSoftError(t, rec.Body.String())
	})

	t.Run("slide object", func(t *testing.T) {
		store := &objectStore{
			objects: map[string][]byte{"results/task/output/structure.json": []byte(slideStructure)},
			errs:    map[string]error{"results/task/slide-1.png": errors.New("ошибка хранилища")},
		}
		handler := newSlideHandler(t, readerLecture("ready", "public"), store)
		rec := requestReader(handler, "/read/lecture/slides/1", false)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("GET slide with failed object = %d, want 502", rec.Code)
		}
		assertSoftError(t, rec.Body.String())
	})
}
