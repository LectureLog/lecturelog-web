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

// agentAuthRepo подставляет заданного пользователя для проверки матрицы
// доступа: владелец, посторонний читатель или аноним (без куки).
type agentAuthRepo struct{ userID string }

func (agentAuthRepo) FindUserByEmail(context.Context, string) (*auth.User, error) {
	return nil, nil
}

func (r agentAuthRepo) FindUserByID(context.Context, string) (*auth.User, error) {
	return &auth.User{ID: r.userID}, nil
}

func (agentAuthRepo) CreateUser(context.Context, auth.Profile) (*auth.User, error) { return nil, nil }

func (agentAuthRepo) UpsertIdentity(context.Context, string, string, string) error { return nil }

func (agentAuthRepo) CreateSession(context.Context, string, time.Time) (*auth.Session, error) {
	return nil, nil
}

func (r agentAuthRepo) GetSession(context.Context, string) (*auth.Session, error) {
	return &auth.Session{ID: "session", UserID: r.userID, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (agentAuthRepo) DeleteSession(context.Context, string) error { return nil }

const structureKey = "results/task/output/structure.json"

const agentStructure = `{
	"source":{"title":"Курс по Go","kind":"audio","duration":"00:42"},
	"sections":[
		{"title":"Введение","subtopics":[{
			"title":"Первый шаг",
			"media":{"kind":"audio","start":"00:05","end":"00:35","key":"results/task/media-1.m4a"},
			"slide_keys":["results/task/slide-1.png","results/task/slide-2.png"],
			"slide_nums":[1,2],
			"content_md":"Абзац первый\n\n<!-- slide:1 -->\n\nТекст **жирный**\n\n> [!tangent]- Подробнее\n> Тело отступления\n>\n> Вторая строка"
		}]},
		{"title":"Практика","subtopics":[{"title":"Разбор","content_md":"<!-- slide:99 -->\n\nФинальный абзац"}]}
	]
}`

const legacyStructure = `{
	"source":{"title":"Старая запись","kind":"video","duration":"00:10"},
	"sections":[
		{"title":"С1","subtopics":[{"title":"П1","slide_keys":["results/task/a.png","results/task/b.png"],"content_md":"Текст 1"}]},
		{"title":"С2","subtopics":[{"title":"П2","slide_keys":["results/task/c.png"],"content_md":"Текст 2"}]}
	]
}`

const longStructure = `{
	"source":{"title":"Длинный курс","kind":"audio","duration":"01:02:03"},
	"sections":[{"title":"Часть","subtopics":[{
		"title":"Кусок",
		"media":{"kind":"video","start":"01:00:00","end":"01:02:03","key":"results/task/media-1.mp4"},
		"content_md":"Текст"
	}]}]
}`

// agentLecture — готовая Публичная лекция владельца «owner».
func agentLecture() *LectureMeta {
	return &LectureMeta{
		ID:         "lecture",
		OwnerID:    "owner",
		Status:     "ready",
		Visibility: "public",
		CoreTaskID: "task",
		Title:      "Лекция по Go",
		SourceKind: "audio",
	}
}

func agentLectureWith(visibility string) *LectureMeta {
	lecture := agentLecture()
	lecture.Visibility = visibility
	return lecture
}

// newAgentHandler собирает HTTP-хендлер читального зала поверх фейковых
// портов и заданного пользователя ("" — аноним).
func newAgentHandler(t *testing.T, lecture *LectureMeta, viewerID, structure string, opts ...HandlerOption) (http.Handler, *objectStore) {
	t.Helper()
	store := &objectStore{objects: map[string][]byte{structureKey: []byte(structure)}}
	return newAgentHandlerWithStore(t, lecture, viewerID, store, opts...), store
}

func newAgentHandlerWithStore(t *testing.T, lecture *LectureMeta, viewerID string, store *objectStore, opts ...HandlerOption) http.Handler {
	t.Helper()
	svc := NewService(handlerRepo{lecture: lecture}, store, handlerPresigner{}, NewMarkdownRenderer(), time.Hour)
	r := chi.NewRouter()
	authSvc := auth.NewService(agentAuthRepo{userID: viewerID}, nil, time.Hour, false)
	r.Use(authSvc.LoadSession)
	NewHandlers(svc, nil, opts...).Mount(r)
	return r
}

// agentGet выполняет запрос от имени viewerID ("owner", "stranger" или "").
func agentGet(handler http.Handler, path, viewerID string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if viewerID != "" {
		req.AddCookie(&http.Cookie{Name: "ll_session", Value: "session"})
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// wantAgentStatus повторяет правила accessLecture для ожидаемого статуса.
func wantAgentStatus(lecture *LectureMeta, viewerID string) int {
	if lecture == nil {
		return http.StatusNotFound
	}
	if lecture.Status != "ready" {
		if viewerID == "owner" {
			return http.StatusAccepted
		}
		return http.StatusNotFound
	}
	if lecture.Visibility == "private" && viewerID != "owner" {
		return http.StatusNotFound
	}
	return http.StatusOK
}

// TestAgentMarkdownAccessMatrix проверяет права доступа на `.md`-маршруте и на
// основном URL с Accept: text/markdown для всех уровней видимости, статусов и
// зрителей, а также отсутствие чтения хранилища при запрещённом доступе.
func TestAgentMarkdownAccessMatrix(t *testing.T) {
	viewers := []string{"", "owner", "stranger"}
	visibilities := []string{"private", "unlisted", "public"}
	statuses := []string{"processing", "ready"}
	paths := []struct {
		name    string
		path    string
		headers map[string]string
	}{
		{name: "md route", path: "/read/lecture.md"},
		{name: "negotiated", path: "/read/lecture", headers: map[string]string{"Accept": "text/markdown"}},
	}

	for _, path := range paths {
		for _, visibility := range visibilities {
			for _, status := range statuses {
				for _, viewerID := range viewers {
					name := path.name + "/" + visibility + "/" + status + "/" + viewerName(viewerID)
					t.Run(name, func(t *testing.T) {
						lecture := agentLectureWith(visibility)
						lecture.Status = status
						handler, store := newAgentHandler(t, lecture, viewerID, agentStructure)

						rec := agentGet(handler, path.path, viewerID, path.headers)
						want := wantAgentStatus(lecture, viewerID)
						if rec.Code != want {
							t.Fatalf("GET %s = %d, want %d: %s", path.path, rec.Code, want, rec.Body.String())
						}
						if want == http.StatusOK {
							if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
								t.Fatalf("Content-Type = %q, want text/markdown", got)
							}
							if len(store.calls) != 1 {
								t.Fatalf("GetObject вызван %d раз, want 1: %v", len(store.calls), store.calls)
							}
							return
						}
						if len(store.calls) != 0 {
							t.Fatalf("GetObject вызван при запрещённом доступе: %v", store.calls)
						}
						if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
							t.Fatalf("Content-Type ошибки = %q, want text/plain", got)
						}
						assertSoftError(t, rec.Body.String())
					})
				}
			}
		}
	}
}

func viewerName(viewerID string) string {
	if viewerID == "" {
		return "anonymous"
	}
	return viewerID
}

// TestAgentMarkdownMissingLecture проверяет 404 на `.md` для несуществующей лекции.
func TestAgentMarkdownMissingLecture(t *testing.T) {
	handler, store := newAgentHandler(t, nil, "", agentStructure)
	rec := agentGet(handler, "/read/lecture.md", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing .md = %d, want 404", rec.Code)
	}
	if len(store.calls) != 0 {
		t.Fatalf("GetObject вызван для несуществующей лекции: %v", store.calls)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
}

// TestAgentMarkdownRouteIsDeterministic проверяет, что явный `.md`-маршрут
// отдаёт Markdown даже браузерным клиентам с Accept: text/html.
func TestAgentMarkdownRouteIsDeterministic(t *testing.T) {
	handler, _ := newAgentHandler(t, agentLecture(), "", agentStructure)
	headers := map[string]string{
		"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36",
	}
	rec := agentGet(handler, "/read/lecture.md", "", headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET .md = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/markdown", got)
	}
	if !strings.HasPrefix(rec.Body.String(), "# Лекция по Go\n") {
		t.Fatalf("тело не начинается с H1 конспекта: %q", rec.Body.String())
	}
}

// TestHandlersReadContentNegotiation проверяет выбор Markdown/HTML на основном
// URL по Accept и User-Agent, а также обязательный Vary.
func TestHandlersReadContentNegotiation(t *testing.T) {
	tests := []struct {
		name         string
		headers      map[string]string
		wantMarkdown bool
	}{
		{
			name:    "browser html",
			headers: map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8", "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Gecko/20100101 Firefox/121.0"},
		},
		{
			name:         "accept markdown",
			headers:      map[string]string{"Accept": "text/markdown"},
			wantMarkdown: true,
		},
		{
			name:         "accept x-markdown",
			headers:      map[string]string{"Accept": "text/x-markdown"},
			wantMarkdown: true,
		},
		{
			name:         "chatgpt user agent",
			headers:      map[string]string{"User-Agent": "ChatGPT-User/2.0"},
			wantMarkdown: true,
		},
		{
			name:         "claudebot user agent",
			headers:      map[string]string{"User-Agent": "ClaudeBot/1.0"},
			wantMarkdown: true,
		},
		{
			name:         "curl user agent",
			headers:      map[string]string{"User-Agent": "curl/8.5.0"},
			wantMarkdown: true,
		},
		{
			name:         "python httpx user agent",
			headers:      map[string]string{"User-Agent": "python-httpx/0.27.0"},
			wantMarkdown: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := newAgentHandler(t, agentLecture(), "", agentStructure)
			rec := agentGet(handler, "/read/lecture", "", tt.headers)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET /read/lecture = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Vary"); got != "Accept, User-Agent" {
				t.Fatalf("Vary = %q, want Accept, User-Agent", got)
			}
			contentType := rec.Header().Get("Content-Type")
			if tt.wantMarkdown {
				if contentType != "text/markdown; charset=utf-8" {
					t.Fatalf("Content-Type = %q, want text/markdown", contentType)
				}
				return
			}
			if contentType != "text/html; charset=utf-8" {
				t.Fatalf("Content-Type = %q, want text/html", contentType)
			}
			if !strings.Contains(rec.Body.String(), `class="ll-reader"`) {
				t.Fatal("браузеру должен отдаваться HTML Читального зала")
			}
		})
	}
}

// TestAgentMarkdownCacheAndRobots проверяет заголовки кеширования и индексации
// для public, unlisted и private, включая не-200 ответы.
func TestAgentMarkdownCacheAndRobots(t *testing.T) {
	tests := []struct {
		name       string
		lecture    *LectureMeta
		status     string
		viewerID   string
		path       string
		headers    map[string]string
		storeErr   error
		wantStatus int
		wantCache  string
		wantRobots string
	}{
		{
			name:       "public md",
			lecture:    agentLecture(),
			path:       "/read/lecture.md",
			wantStatus: http.StatusOK,
			wantCache:  "public, max-age=60",
		},
		{
			name:       "public negotiated",
			lecture:    agentLecture(),
			path:       "/read/lecture",
			headers:    map[string]string{"Accept": "text/markdown"},
			wantStatus: http.StatusOK,
			wantCache:  "public, max-age=60",
		},
		{
			name:       "unlisted md",
			lecture:    agentLectureWith("unlisted"),
			path:       "/read/lecture.md",
			wantStatus: http.StatusOK,
			wantCache:  "private, no-store",
			wantRobots: "noindex, nofollow",
		},
		{
			name:       "private owner md",
			lecture:    agentLectureWith("private"),
			viewerID:   "owner",
			path:       "/read/lecture.md",
			wantStatus: http.StatusOK,
			wantCache:  "private, no-store",
			wantRobots: "noindex, nofollow",
		},
		{
			name:       "private owner processing",
			lecture:    agentLectureWith("private"),
			status:     "processing",
			viewerID:   "owner",
			path:       "/read/lecture.md",
			wantStatus: http.StatusAccepted,
			wantCache:  "private, no-store",
			wantRobots: "noindex, nofollow",
		},
		{
			name:       "unlisted owner processing",
			lecture:    agentLectureWith("unlisted"),
			status:     "processing",
			viewerID:   "owner",
			path:       "/read/lecture.md",
			wantStatus: http.StatusAccepted,
			wantCache:  "private, no-store",
			wantRobots: "noindex, nofollow",
		},
		{
			name:       "public owner processing",
			lecture:    agentLectureWith("public"),
			status:     "processing",
			viewerID:   "owner",
			path:       "/read/lecture.md",
			wantStatus: http.StatusAccepted,
		},
		{
			name:       "unlisted core unavailable",
			lecture:    agentLectureWith("unlisted"),
			path:       "/read/lecture.md",
			storeErr:   errors.New("ошибка хранилища"),
			wantStatus: http.StatusBadGateway,
			wantCache:  "private, no-store",
			wantRobots: "noindex, nofollow",
		},
		{
			name:       "public core unavailable",
			lecture:    agentLecture(),
			path:       "/read/lecture.md",
			storeErr:   errors.New("ошибка хранилища"),
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "missing lecture",
			path:       "/read/lecture.md",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &objectStore{objects: map[string][]byte{structureKey: []byte(agentStructure)}}
			if tt.storeErr != nil {
				store.errs = map[string]error{structureKey: tt.storeErr}
			}
			lecture := tt.lecture
			if lecture != nil && tt.status != "" {
				lecture.Status = tt.status
			}
			handler := newAgentHandlerWithStore(t, lecture, tt.viewerID, store)
			rec := agentGet(handler, tt.path, tt.viewerID, tt.headers)

			if rec.Code != tt.wantStatus {
				t.Fatalf("GET %s = %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if got := rec.Header().Get("X-Robots-Tag"); got != tt.wantRobots {
				t.Fatalf("X-Robots-Tag = %q, want %q", got, tt.wantRobots)
			}
			if tt.wantStatus >= http.StatusBadRequest {
				assertSoftError(t, rec.Body.String())
			}
		})
	}
}

// TestAgentMarkdownContent проверяет точность Агентского представления:
// метаданные, оглавление, заголовки, таймкод, инлайн-Слайд, галерею и
// преобразование callout'ов без медиа и HTML-обвязки.
func TestAgentMarkdownContent(t *testing.T) {
	handler, _ := newAgentHandler(t, agentLecture(), "", agentStructure, WithBaseOrigin("https://lecture.example/"))
	rec := agentGet(handler, "/read/lecture.md", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET .md = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	inlineSlide := "![Слайд 1: Первый шаг](https://lecture.example/read/lecture/slides/1)"
	gallerySlide := "![Слайд 2: Первый шаг](https://lecture.example/read/lecture/slides/2)"
	for _, want := range []string{
		"# Лекция по Go",
		"- Источник: Курс по Go",
		"- Тип источника: Аудио",
		"- Длительность: 00:42",
		"- Канонический URL: https://lecture.example/read/lecture",
		"## Содержание",
		"- 01. Введение",
		"  - 1.1 Первый шаг",
		"- 02. Практика",
		"  - 2.1 Разбор",
		"## 01. Введение",
		"### 1.1 Первый шаг",
		"[00:05 – 00:35]",
		inlineSlide,
		gallerySlide,
		"> **Отступление от темы: Подробнее**",
		"> Тело отступления",
		"> Вторая строка",
		"Финальный абзац",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в Агентском представлении отсутствует %q\n%s", want, body)
		}
	}

	// Аудио- и видеофрагменты, ключи хранилища и сырой HTML не попадают в ответ.
	for _, forbidden := range []string{"<details", "<svg", "<audio", "<video", "media-1.m4a", "results/task/", "slide:99", "/slides/99"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("в Агентском представлении не должно быть %q", forbidden)
		}
	}

	// Инлайн-Слайд стоит на месте маркера внутри текста, а галерея — в конце Подтемы.
	firstParagraph := strings.Index(body, "Абзац первый")
	inlineIndex := strings.Index(body, inlineSlide)
	textAfter := strings.Index(body, "Текст **жирный**")
	galleryIndex := strings.Index(body, gallerySlide)
	nextSection := strings.Index(body, "## 02. Практика")
	if firstParagraph < 0 || inlineIndex < 0 || textAfter < 0 || galleryIndex < 0 || nextSection < 0 {
		t.Fatalf("не найдены ожидаемые фрагменты для проверки порядка\n%s", body)
	}
	if !(firstParagraph < inlineIndex && inlineIndex < textAfter && textAfter < galleryIndex && galleryIndex < nextSection) {
		t.Errorf("нарушен порядок текста, инлайн-Слайда и галереи\n%s", body)
	}
}

// TestAgentMarkdownSourceMetadata проверяет fallback названия источника и
// приоритет типа источника из метаданных Лекции.
func TestAgentMarkdownSourceMetadata(t *testing.T) {
	const noSourceTitle = `{
		"source":{"title":"","kind":"audio","duration":"00:30"},
		"sections":[{"title":"Раздел","subtopics":[{"title":"Подтема","content_md":"Текст"}]}]
	}`

	t.Run("fallback to lecture title and structure kind", func(t *testing.T) {
		lecture := agentLecture()
		lecture.SourceKind = ""
		handler, _ := newAgentHandler(t, lecture, "", noSourceTitle)
		body := agentGet(handler, "/read/lecture.md", "", nil).Body.String()
		if !strings.Contains(body, "- Источник: Лекция по Go") {
			t.Errorf("нет fallback названия источника:\n%s", body)
		}
		if !strings.Contains(body, "- Тип источника: Аудио") {
			t.Errorf("тип источника должен браться из structure.json:\n%s", body)
		}
	})

	t.Run("lecture source kind wins", func(t *testing.T) {
		lecture := agentLecture()
		lecture.SourceKind = "video_url"
		handler, _ := newAgentHandler(t, lecture, "", noSourceTitle)
		body := agentGet(handler, "/read/lecture.md", "", nil).Body.String()
		if !strings.Contains(body, "- Тип источника: Ссылка") {
			t.Errorf("ожидался тип источника «Ссылка»:\n%s", body)
		}
	})
}

// TestAgentMarkdownLongLecture проверяет таймкоды и длительность в HH:MM:SS
// для записей от часа и без медиа-ссылок.
func TestAgentMarkdownLongLecture(t *testing.T) {
	lecture := agentLecture()
	lecture.SourceKind = "video_url"
	handler, _ := newAgentHandler(t, lecture, "", longStructure, WithBaseOrigin("https://lecture.example"))

	rec := agentGet(handler, "/read/lecture.md", "", nil)
	body := rec.Body.String()
	for _, want := range []string{
		"- Длительность: 01:02:03",
		"[01:00:00 – 01:02:03]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в Агентском представлении отсутствует %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "media-1.mp4") || strings.Contains(body, "<video") {
		t.Errorf("медиафрагмент не должен попадать в Агентское представление:\n%s", body)
	}
}

// TestAgentMarkdownLegacySlideNumbering проверяет 1-based нумерацию Слайдов
// для старых Конспектов без slide_nums.
func TestAgentMarkdownLegacySlideNumbering(t *testing.T) {
	handler, _ := newAgentHandler(t, agentLecture(), "", legacyStructure, WithBaseOrigin("https://lecture.example"))
	body := agentGet(handler, "/read/lecture.md", "", nil).Body.String()

	for _, want := range []string{
		"![Слайд 1: П1](https://lecture.example/read/lecture/slides/1)",
		"![Слайд 2: П1](https://lecture.example/read/lecture/slides/2)",
		"![Слайд 3: П2](https://lecture.example/read/lecture/slides/3)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в галерее отсутствует %q\n%s", want, body)
		}
	}

	first := strings.Index(body, "![Слайд 1: П1]")
	second := strings.Index(body, "![Слайд 2: П1]")
	third := strings.Index(body, "![Слайд 3: П2]")
	nextSection := strings.Index(body, "## 02. С2")
	if !(first >= 0 && first < second && second < nextSection && nextSection < third) {
		t.Errorf("нарушен порядок нумерации Слайдов по Разделам и Подтемам\n%s", body)
	}
}

// TestAgentMarkdownOriginFallback проверяет fallback origin из
// X-Forwarded-Proto + Host, когда базовая конфигурация отсутствует.
func TestAgentMarkdownOriginFallback(t *testing.T) {
	handler, _ := newAgentHandler(t, agentLecture(), "", agentStructure)
	req := httptest.NewRequest(http.MethodGet, "/read/lecture.md", nil)
	req.Host = "cdn.example.test"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET .md = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"- Канонический URL: https://cdn.example.test/read/lecture",
		"![Слайд 1: Первый шаг](https://cdn.example.test/read/lecture/slides/1)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в Агентском представлении отсутствует %q\n%s", want, body)
		}
	}
}

// TestAgentMarkdownCalloutWithoutTitle проверяет цитату callout'а без
// пользовательского заголовка: остаётся только локализованный тип.
func TestAgentMarkdownCalloutWithoutTitle(t *testing.T) {
	const structure = `{
		"source":{"title":"Источник","kind":"audio","duration":"00:10"},
		"sections":[{"title":"Раздел","subtopics":[
			{
				"title":"Подтема",
				"slide_keys":["results/task/s1.png"],
				"slide_nums":[1],
				"content_md":"Текст\n\n<!-- slide:2 -->\n\n> [!note]\n> Тело заметки\n\n> [!warning]+ Важно\n> Осторожно\n\n> [!quote] Только заголовок"
			},
			{
				"title":"Вторая подтема",
				"slide_keys":["results/task/s2.png"],
				"slide_nums":[2],
				"content_md":"Второй текст"
			}
		]}]
	}`
	handler, _ := newAgentHandler(t, agentLecture(), "", structure)
	body := agentGet(handler, "/read/lecture.md", "", nil).Body.String()

	for _, want := range []string{
		"> **Заметка**",
		"> Тело заметки",
		"> **Внимание: Важно**",
		"> Осторожно",
		"> **Цитата: Только заголовок**\n\n",
		"![Слайд 2: Вторая подтема]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в Агентском представлении отсутствует %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "![Слайд 2: Подтема]") {
		t.Errorf("чужой инлайн-маркер слайда не должен вставляться в первую подтему:\n%s", body)
	}
	if strings.Contains(body, "> **Цитата: Только заголовок**\n>") {
		t.Errorf("callout без тела не должен порождать пустую строку цитаты:\n%s", body)
	}
	if strings.Contains(body, "<details") || strings.Contains(body, "<svg") {
		t.Errorf("callout не должен рендериться HTML-обвязкой:\n%s", body)
	}
}

// TestReaderHTMLAlternateMarkdownLink проверяет HTML Читального зала:
// ссылка rel=alternate в <head> и действие MD в шапке.
func TestReaderHTMLAlternateMarkdownLink(t *testing.T) {
	handler, _ := newAgentHandler(t, agentLecture(), "", agentStructure)
	headers := map[string]string{
		"Accept":     "text/html,application/xhtml+xml,*/*;q=0.8",
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15",
	}
	rec := agentGet(handler, "/read/lecture", "", headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /read/lecture = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<link rel="alternate" type="text/markdown" href="/read/lecture.md">`,
		`href="/read/lecture.md"`,
		`title="Агентское представление (Markdown)"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в HTML Читального зала отсутствует %q", want)
		}
	}
}

// TestAgentMarkdownEscapesBracketsAndNewlines проверяет экранирование скобок в
// названиях подтем для ссылок на слайды и нормализацию переводов строк.
func TestAgentMarkdownEscapesBracketsAndNewlines(t *testing.T) {
	const structure = `{
		"source":{"title":"Пост\nсо второй строкой","kind":"video","duration":"00:15"},
		"sections":[{"title":"Раздел\n1","subtopics":[{
			"title":"Полуинтервалы [l, r)",
			"slide_keys":["results/task/s1.png"],
			"slide_nums":[1],
			"content_md":"Текст\n\n<!-- slide:1 -->"
		}]}]
	}`
	lecture := agentLecture()
	lecture.Title = "Алгоритмы\nбаза"
	handler, _ := newAgentHandler(t, lecture, "", structure, WithBaseOrigin("https://lecture.example"))
	body := agentGet(handler, "/read/lecture.md", "", nil).Body.String()

	for _, want := range []string{
		"# Алгоритмы база\n",
		"- Источник: Пост со второй строкой\n",
		"- 01. Раздел 1\n",
		"  - 1.1 Полуинтервалы [l, r)\n",
		`![Слайд 1: Полуинтервалы \[l, r)](https://lecture.example/read/lecture/slides/1)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в Агентском представлении отсутствует %q:\n%s", want, body)
		}
	}
}
