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
