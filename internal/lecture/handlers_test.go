package lecture_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/lecture"
	"github.com/go-chi/chi/v5"
)

// newTestService создаёт lecture.Service с заданными моками для HTTP-тестов.
func newTestService(repo lecture.Repository, core lecture.CoreTasks) *lecture.Service {
	return lecture.NewService(repo, core)
}

// testUser — фиксированный тестовый пользователь.
var testUser = &auth.User{
	ID:    "user-test-uuid",
	Email: "test@example.com",
	Name:  "Тест",
}

// testLecture — вспомогательная лекция для тестов.
func testLecture() lecture.Lecture {
	return lecture.Lecture{
		ID:         "lec-test-uuid",
		OwnerID:    "user-test-uuid",
		CoreTaskID: "",
		Status:     lecture.StatusReady,
		Visibility: lecture.VisibilityPrivate,
		SourceKind: "audio",
		S3Key:      "test/lecture.mp3",
		Title:      "Тестовая лекция",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func TestMapErrorCode_CookiesInvalid(t *testing.T) {
	lec := testLecture()
	lec.Status = lecture.StatusFailed
	lec.ErrorCode = "cookies_invalid"
	repo := &mockRepo{
		listByOwner: func(_ context.Context, _ string) ([]lecture.Lecture, error) {
			return []lecture.Lecture{lec}, nil
		},
	}
	svc := newTestService(repo, &mockCore{})
	handler := mountTestRouter(svc)

	req := addSessionCookie(httptest.NewRequest(http.MethodGet, "/lectures", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /lectures = %d, ожидается 200", rec.Code)
	}
	want := "Cookies YouTube устарели — обратитесь к администратору"
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("карточка должна содержать %q, тело: %s", want, rec.Body.String())
	}
}

// testSessionCookie — имя куки сессии (должно совпадать с auth.sessionCookieName="ll_session").
const testSessionCookie = "ll_session"

// mountTestRouter монтирует lecture.Service + auth.LoadSession(мок) в chi.Router.
// Использует реальный auth.Service с мок-репозиторием, чтобы положить testUser в контекст.
func mountTestRouter(svc *lecture.Service) http.Handler {
	// Создаём мок auth.Repository для LoadSession
	authRepo := &testAuthRepository{user: testUser}
	authSvc := auth.NewService(authRepo, nil, time.Hour, false)

	r := chi.NewRouter()
	// LoadSession с мок-репозиторием инжектирует testUser при наличии сессионной куки
	r.Use(authSvc.LoadSession)
	svc.Mount(r)
	return r
}

// addSessionCookie добавляет тестовую сессионную куку в запрос.
// Без этой куки auth.LoadSession не инжектирует user в контекст.
func addSessionCookie(req *http.Request) *http.Request {
	req.AddCookie(&http.Cookie{Name: testSessionCookie, Value: "test-session-id"})
	return req
}

// testAuthRepository — минимальный мок auth.Repository для тестов хендлеров.
// GetSession всегда возвращает тестовую сессию, FindUserByID возвращает testUser.
type testAuthRepository struct {
	user *auth.User
}

func (r *testAuthRepository) FindUserByEmail(_ context.Context, _ string) (*auth.User, error) {
	return nil, nil
}
func (r *testAuthRepository) FindUserByID(_ context.Context, _ string) (*auth.User, error) {
	return r.user, nil
}
func (r *testAuthRepository) CreateUser(_ context.Context, _ auth.Profile) (*auth.User, error) {
	return nil, nil
}
func (r *testAuthRepository) UpsertIdentity(_ context.Context, _, _, _ string) error {
	return nil
}
func (r *testAuthRepository) CreateSession(_ context.Context, _ string, _ time.Time) (*auth.Session, error) {
	return nil, nil
}
func (r *testAuthRepository) GetSession(_ context.Context, _ string) (*auth.Session, error) {
	return &auth.Session{ID: "test-session", UserID: r.user.ID, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (r *testAuthRepository) DeleteSession(_ context.Context, _ string) error {
	return nil
}

// TestHandlers_GetLectures_OK проверяет GET /lectures → 200 + карточки.
func TestHandlers_GetLectures_OK(t *testing.T) {
	lec := testLecture()
	repo := &mockRepo{
		listByOwner: func(_ context.Context, ownerID string) ([]lecture.Lecture, error) {
			return []lecture.Lecture{lec}, nil
		},
	}
	svc := newTestService(repo, &mockCore{})
	handler := mountTestRouter(svc)

	req := addSessionCookie(httptest.NewRequest(http.MethodGet, "/lectures", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /lectures = %d, ожидается 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Тестовая лекция") {
		t.Error("GET /lectures должен содержать заголовок лекции")
	}
}

// TestHandlers_GetLectures_Empty проверяет GET /lectures без лекций → пустое состояние.
func TestHandlers_GetLectures_Empty(t *testing.T) {
	repo := &mockRepo{
		listByOwner: func(_ context.Context, _ string) ([]lecture.Lecture, error) {
			return []lecture.Lecture{}, nil
		},
	}
	svc := newTestService(repo, &mockCore{})
	handler := mountTestRouter(svc)

	req := addSessionCookie(httptest.NewRequest(http.MethodGet, "/lectures", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /lectures (пусто) = %d, ожидается 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ll-empty") {
		t.Error("пустой список: ожидается пустое состояние ll-empty")
	}
}

// TestHandlers_Rename_OK проверяет POST /lectures/{id}/rename → частичный ответ.
func TestHandlers_Rename_OK(t *testing.T) {
	lec := testLecture()
	repo := &mockRepo{
		rename: func(_ context.Context, _, _, title string) (int64, error) {
			return 1, nil
		},
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			updated := lec
			updated.Title = "Новое название"
			return &updated, nil
		},
	}
	svc := newTestService(repo, &mockCore{})
	handler := mountTestRouter(svc)

	form := url.Values{"title": {"Новое название"}}
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/lectures/"+lec.ID+"/rename",
		strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST rename = %d, ожидается 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Новое название") {
		t.Error("rename ответ должен содержать новый заголовок")
	}
}

// TestHandlers_Rename_EmptyTitle проверяет POST rename с пустым title → 422.
func TestHandlers_Rename_EmptyTitle(t *testing.T) {
	svc := newTestService(&mockRepo{}, &mockCore{})
	handler := mountTestRouter(svc)

	form := url.Values{"title": {""}}
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/lectures/lec-1/rename",
		strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code < 400 {
		t.Errorf("POST rename (пустой title) = %d, ожидается 4xx", rec.Code)
	}
}

// TestHandlers_SetVisibility_OK проверяет POST /lectures/{id}/visibility → частичный ответ.
func TestHandlers_SetVisibility_OK(t *testing.T) {
	lec := testLecture()
	repo := &mockRepo{
		setVisibility: func(_ context.Context, _, _, _ string) (int64, error) {
			return 1, nil
		},
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			updated := lec
			updated.Visibility = lecture.VisibilityPublic
			return &updated, nil
		},
	}
	svc := newTestService(repo, &mockCore{})
	handler := mountTestRouter(svc)

	form := url.Values{"visibility": {"public"}}
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/lectures/"+lec.ID+"/visibility",
		strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST visibility = %d, ожидается 200", rec.Code)
	}
}

// TestHandlers_Delete_OK проверяет DELETE /lectures/{id} → 200.
func TestHandlers_Delete_OK(t *testing.T) {
	lec := testLecture()
	repo := &mockRepo{
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			return &lec, nil
		},
		delete_: func(_ context.Context, _, _ string) (int64, error) {
			return 1, nil
		},
	}
	svc := newTestService(repo, &mockCore{})
	handler := mountTestRouter(svc)

	req := addSessionCookie(httptest.NewRequest(http.MethodDelete, "/lectures/"+lec.ID, nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("DELETE /lectures/{id} = %d, ожидается 200", rec.Code)
	}
}

// TestHandlers_Delete_NotFound проверяет DELETE на несуществующую → 404.
func TestHandlers_Delete_NotFound(t *testing.T) {
	repo := &mockRepo{
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			return nil, nil // не найдена
		},
	}
	svc := newTestService(repo, &mockCore{})
	handler := mountTestRouter(svc)

	req := addSessionCookie(httptest.NewRequest(http.MethodDelete, "/lectures/nonexistent-id", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE несуществующей = %d, ожидается 404", rec.Code)
	}
}

// TestHandlers_Retry_OK проверяет POST /lectures/{id}/retry → частичный ответ.
func TestHandlers_Retry_OK(t *testing.T) {
	failedLec := testLecture()
	failedLec.Status = lecture.StatusFailed
	failedLec.S3Key = "test/lecture.mp3"

	callCount := 0
	repo := &mockRepo{
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			callCount++
			if callCount >= 2 {
				// Второй вызов — после retry
				updated := failedLec
				updated.Status = lecture.StatusProcessing
				updated.CoreTaskID = "new-task-id"
				return &updated, nil
			}
			return &failedLec, nil
		},
		setCoreTaskProcessing: func(_ context.Context, _, _, _ string) (int64, error) {
			return 1, nil
		},
	}
	core := &mockCore{
		createTask: func(_ context.Context, _ lecture.CreateTaskParams) (string, error) {
			return "new-task-id", nil
		},
	}
	svc := newTestService(repo, core)
	handler := mountTestRouter(svc)

	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/lectures/"+failedLec.ID+"/retry", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("POST retry = %d, ожидается 200, тело: %s", rec.Code, rec.Body.String())
	}
}
