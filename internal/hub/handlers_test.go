package hub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func mountHubTestRouter(svc *Service) http.Handler {
	r := chi.NewRouter()
	svc.Mount(r)
	return r
}

func TestHandleHub_RendersCards(t *testing.T) {
	repo := &mockRepository{
		listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) {
			return []PublicLecture{
				{ID: "lecture-1", Title: "Алгебра", SourceKind: "audio", AuthorName: "Анна", PublishedAt: time.Now()},
				{ID: "lecture-2", Title: "Физика", SourceKind: "video", AuthorName: "Борис", PublishedAt: time.Now()},
			}, nil
		},
	}

	rec := httptest.NewRecorder()
	mountHubTestRouter(NewService(repo, 0)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hub", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /hub = %d, ожидается 200", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, ожидается text/html", rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"Алгебра", "Физика", "Анна", "Борис"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("страница не содержит %q", want)
		}
	}
}

func TestHandleHub_Empty(t *testing.T) {
	repo := &mockRepository{listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) { return []PublicLecture{}, nil }}

	rec := httptest.NewRecorder()
	mountHubTestRouter(NewService(repo, 0)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hub", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /hub = %d, ожидается 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Пока нет публичных лекций") {
		t.Error("пустая страница должна содержать пустое состояние")
	}
}

func TestHandleHub_Anonymous(t *testing.T) {
	repo := &mockRepository{listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) { return []PublicLecture{}, nil }}

	rec := httptest.NewRecorder()
	mountHubTestRouter(NewService(repo, 0)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hub", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /hub = %d, ожидается 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `href="/auth/login"`) || !strings.Contains(rec.Body.String(), "Войти") {
		t.Error("анонимный хаб должен содержать ссылку «Войти»")
	}
}

func TestHandleHub_RepoError(t *testing.T) {
	repo := &mockRepository{listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) { return nil, errors.New("ошибка БД") }}

	rec := httptest.NewRecorder()
	mountHubTestRouter(NewService(repo, 0)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hub", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GET /hub = %d, ожидается 500", rec.Code)
	}
}

const testBaseOrigin = "https://lecturelog.example"

// testRepository — репозиторий витрины с фиксированным набором публичных лекций.
func testRepository(lectures []PublicLecture) *mockRepository {
	return &mockRepository{
		listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) {
			return lectures, nil
		},
	}
}

// testLectures — две публичные лекции с детерминированными метаданными.
func testLectures() []PublicLecture {
	return []PublicLecture{
		{
			ID:          "lecture-1",
			Title:       "Алгебра",
			SourceKind:  "audio",
			AuthorName:  "Анна",
			PublishedAt: time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:          "lecture-2",
			Title:       "Физика",
			SourceKind:  "video_url",
			AuthorName:  "Борис",
			PublishedAt: time.Date(2026, 3, 14, 18, 30, 0, 0, time.UTC),
		},
	}
}

func serveHub(svc *Service, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	mountHubTestRouter(svc).ServeHTTP(rec, req)
	return rec
}

// TestHandleHubMarkdown_Catalog проверяет Markdown-каталог Витрины с абсолютными ссылками.
func TestHandleHubMarkdown_Catalog(t *testing.T) {
	rec := serveHub(NewService(testRepository(testLectures()), 0, WithBaseOrigin(testBaseOrigin)), "/hub.md", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /hub.md = %d, ожидается 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q, ожидается text/markdown; charset=utf-8", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"# Витрина LectureLog",
		"- [Алгебра](https://lecturelog.example/read/lecture-1.md) — Аудио, автор: Анна, опубликовано: 2026-02-03",
		"- [Физика](https://lecturelog.example/read/lecture-2.md) — Ссылка, автор: Борис, опубликовано: 2026-03-14",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("каталог не содержит %q:\n%s", want, body)
		}
	}
}

// TestHandleHubMarkdown_Empty проверяет явное сообщение при пустой Витрине.
func TestHandleHubMarkdown_Empty(t *testing.T) {
	rec := serveHub(NewService(testRepository([]PublicLecture{}), 0, WithBaseOrigin(testBaseOrigin)), "/hub.md", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /hub.md = %d, ожидается 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Пока нет публичных лекций.") {
		t.Errorf("пустой каталог должен сообщать об отсутствии лекций:\n%s", rec.Body.String())
	}
}

// TestHandleHubMarkdown_OriginFallback проверяет fallback origin из запроса.
func TestHandleHubMarkdown_OriginFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://hub.local/hub.md", nil)
	rec := httptest.NewRecorder()
	mountHubTestRouter(NewService(testRepository(testLectures()), 0)).ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "http://hub.local/read/lecture-1.md") {
		t.Errorf("ссылка должна строиться от origin запроса:\n%s", rec.Body.String())
	}
}

// TestHandleHubMarkdown_RepoError проверяет лаконичную ошибку без утечки деталей.
func TestHandleHubMarkdown_RepoError(t *testing.T) {
	repo := &mockRepository{listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) {
		return nil, errors.New("ошибка БД")
	}}

	rec := serveHub(NewService(repo, 0), "/hub.md", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /hub.md = %d, ожидается 500", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, ожидается text/plain; charset=utf-8", got)
	}
	if strings.Contains(rec.Body.String(), "ошибка БД") {
		t.Errorf("ответ не должен раскрывать детали хранилища:\n%s", rec.Body.String())
	}
}

// TestHandleLLMsTxt проверяет индекс платформы для агентов.
func TestHandleLLMsTxt(t *testing.T) {
	rec := serveHub(NewService(testRepository(testLectures()), 0, WithBaseOrigin(testBaseOrigin)), "/llms.txt", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /llms.txt = %d, ожидается 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q, ожидается text/markdown; charset=utf-8", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"# LectureLog",
		"## Как читать лекции агентом",
		"https://lecturelog.example/read/{id}.md",
		"https://lecturelog.example/read/{id}/slides/{n}",
		"https://lecturelog.example/hub.md",
		"## Публичные лекции",
		"- [Алгебра](https://lecturelog.example/read/lecture-1.md) — Аудио, автор: Анна, опубликовано: 2026-02-03",
		"- [Физика](https://lecturelog.example/read/lecture-2.md) — Ссылка, автор: Борис, опубликовано: 2026-03-14",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("llms.txt не содержит %q:\n%s", want, body)
		}
	}
}

// TestHandleLLMsTxt_Empty проверяет пустую Витрину в llms.txt.
func TestHandleLLMsTxt_Empty(t *testing.T) {
	rec := serveHub(NewService(testRepository([]PublicLecture{}), 0, WithBaseOrigin(testBaseOrigin)), "/llms.txt", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /llms.txt = %d, ожидается 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Пока нет публичных лекций.") {
		t.Errorf("llms.txt без лекций должен сообщать об их отсутствии:\n%s", rec.Body.String())
	}
}

// TestHandleLLMsTxt_RepoError проверяет лаконичную ошибку без утечки деталей.
func TestHandleLLMsTxt_RepoError(t *testing.T) {
	repo := &mockRepository{listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) {
		return nil, errors.New("ошибка БД")
	}}

	rec := serveHub(NewService(repo, 0), "/llms.txt", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /llms.txt = %d, ожидается 500", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, ожидается text/plain; charset=utf-8", got)
	}
}

// TestHandleHub_ContentNegotiation проверяет выбор Markdown/HTML и заголовок Vary.
func TestHandleHub_ContentNegotiation(t *testing.T) {
	tests := []struct {
		name         string
		headers      map[string]string
		wantType     string
		wantMarkdown bool
	}{
		{
			name:     "обычный браузер",
			headers:  map[string]string{"Accept": "text/html,application/xhtml+xml,*/*;q=0.8", "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) Chrome/126.0"},
			wantType: "text/html; charset=utf-8",
		},
		{
			name:         "Accept text/markdown",
			headers:      map[string]string{"Accept": "text/markdown"},
			wantType:     "text/markdown; charset=utf-8",
			wantMarkdown: true,
		},
		{
			name:         "curl",
			headers:      map[string]string{"Accept": "*/*", "User-Agent": "curl/8.5.0"},
			wantType:     "text/markdown; charset=utf-8",
			wantMarkdown: true,
		},
		{
			name:         "ChatGPT-User",
			headers:      map[string]string{"Accept": "text/html", "User-Agent": "Mozilla/5.0 (compatible; ChatGPT-User/1.0)"},
			wantType:     "text/markdown; charset=utf-8",
			wantMarkdown: true,
		},
		{
			name:         "ClaudeBot",
			headers:      map[string]string{"Accept": "*/*", "User-Agent": "ClaudeBot/1.0"},
			wantType:     "text/markdown; charset=utf-8",
			wantMarkdown: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveHub(NewService(testRepository(testLectures()), 0, WithBaseOrigin(testBaseOrigin)), "/hub", tt.headers)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET /hub = %d, ожидается 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantType {
				t.Errorf("Content-Type = %q, ожидается %q", got, tt.wantType)
			}
			if got := rec.Header().Get("Vary"); got != "Accept, User-Agent" {
				t.Errorf("Vary = %q, ожидается \"Accept, User-Agent\"", got)
			}
			body := rec.Body.String()
			if tt.wantMarkdown {
				if !strings.Contains(body, "# Витрина LectureLog") || strings.Contains(body, "<html") {
					t.Errorf("ожидается Markdown-каталог:\n%s", body)
				}
				if !strings.Contains(body, "https://lecturelog.example/read/lecture-1.md") {
					t.Errorf("Markdown-каталог должен содержать абсолютные ссылки:\n%s", body)
				}
			} else if !strings.Contains(body, "<html") {
				t.Errorf("ожидается HTML-страница витрины:\n%s", body)
			}
		})
	}
}

// visibilityRecord моделирует запись лекции с уровнем видимости для проверки фильтрации Витрины.
type visibilityRecord struct {
	PublicLecture
	Visibility string
}

// TestHubCatalog_ExcludesPrivateAndUnlistedAndEscapesTitles проверяет, что ни /llms.txt,
// ни /hub.md, ни /hub (HTML и Markdown) не включают private и unlisted лекции,
// что /hub.md и /llms.txt всегда отдают Markdown даже с браузерными заголовками,
// и что скобки в заголовке не ломают Markdown-ссылку.
func TestHubCatalog_ExcludesPrivateAndUnlistedAndEscapesTitles(t *testing.T) {
	records := []visibilityRecord{
		{
			Visibility: "public",
			PublicLecture: PublicLecture{
				ID:          "pub-1",
				Title:       "Лекция [Часть 1]",
				SourceKind:  "video",
				AuthorName:  "Иван",
				PublishedAt: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			Visibility: "unlisted",
			PublicLecture: PublicLecture{
				ID:          "unlisted-secret",
				Title:       "Скрытая по ссылке",
				SourceKind:  "audio",
				AuthorName:  "Пётр",
				PublishedAt: time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			Visibility: "private",
			PublicLecture: PublicLecture{
				ID:          "private-secret",
				Title:       "Личная запись",
				SourceKind:  "audio",
				AuthorName:  "Мария",
				PublishedAt: time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	repo := &mockRepository{
		listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) {
			out := make([]PublicLecture, 0)
			for _, rec := range records {
				if rec.Visibility == "public" {
					out = append(out, rec.PublicLecture)
				}
			}
			return out, nil
		},
	}
	svc := NewService(repo, 0, WithBaseOrigin(testBaseOrigin))
	browserHeaders := map[string]string{
		"Accept":     "text/html,application/xhtml+xml,*/*;q=0.8",
		"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) Chrome/126.0",
	}

	for _, path := range []string{"/llms.txt", "/hub.md"} {
		rec := serveHub(svc, path, browserHeaders)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, ожидается 200", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/markdown; charset=utf-8" {
			t.Errorf("%s Content-Type = %q, ожидается text/markdown; charset=utf-8", path, got)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `- [Лекция \[Часть 1\]](https://lecturelog.example/read/pub-1.md)`) {
			t.Errorf("%s должен содержать экранированную ссылку публичной лекции:\n%s", path, body)
		}
		for _, forbidden := range []string{"unlisted-secret", "Скрытая по ссылке", "private-secret", "Личная запись"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s не должен содержать не-публичную лекцию %q:\n%s", path, forbidden, body)
			}
		}
	}

	for _, headers := range []map[string]string{browserHeaders, {"Accept": "text/markdown"}} {
		rec := serveHub(svc, "/hub", headers)
		body := rec.Body.String()
		for _, forbidden := range []string{"unlisted-secret", "Скрытая по ссылке", "private-secret", "Личная запись"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("/hub не должен содержать не-публичную лекцию %q:\n%s", forbidden, body)
			}
		}
	}

	// Проверяем, что /hub не затирает Vary: Cookie, выставленный внешним CSRF-middleware.
	req := httptest.NewRequest(http.MethodGet, "/hub", nil)
	rec := httptest.NewRecorder()
	rec.Header().Set("Vary", "Cookie")
	mountHubTestRouter(svc).ServeHTTP(rec, req)
	if got := rec.Header().Get("Vary"); got != "Accept, User-Agent, Cookie" {
		t.Errorf("Vary с предварительным Cookie = %q, ожидается \"Accept, User-Agent, Cookie\"", got)
	}
}
