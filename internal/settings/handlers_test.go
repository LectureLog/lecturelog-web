package settings

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/coreclient"
	"github.com/go-chi/chi/v5"
)

// testUser — фиктивный авторизованный пользователь для тестов хендлеров.
var testUser = &auth.User{
	ID:    "user-test-uuid",
	Email: "test@example.com",
	Name:  "Тест",
}

const testSessionCookie = "ll_session"

// fakeCore — фейк ядра, реализующий Core (аналог upload.mockCore).
type fakeCore struct {
	status    coreclient.CookieStatus
	statusErr error
	putErr    error
	deleteErr error
	putCalled bool
	delCalled bool
}

func (f *fakeCore) GetYouTubeCookieStatus(_ context.Context) (coreclient.CookieStatus, error) {
	if f.statusErr != nil {
		return coreclient.CookieStatus{}, f.statusErr
	}
	return f.status, nil
}

func (f *fakeCore) PutYouTubeCookies(_ context.Context, b []byte) (coreclient.CookieStatus, error) {
	f.putCalled = true
	if f.putErr != nil {
		return coreclient.CookieStatus{}, f.putErr
	}
	return coreclient.CookieStatus{Exists: true, Size: len(b)}, nil
}

func (f *fakeCore) DeleteYouTubeCookies(_ context.Context) (coreclient.CookieStatus, error) {
	f.delCalled = true
	if f.deleteErr != nil {
		return coreclient.CookieStatus{}, f.deleteErr
	}
	return coreclient.CookieStatus{Exists: false}, nil
}

// testAuthRepository — фейковый репозиторий auth, всегда возвращающий testUser
// по сессии (паттерн upload/handlers_test.go).
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
	return &auth.Session{ID: "test-session-id", UserID: r.user.ID, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (r *testAuthRepository) DeleteSession(_ context.Context, _ string) error {
	return nil
}

// mountTestRouter монтирует роуты settings поверх auth.LoadSession, чтобы
// авторизованный запрос (с cookie сессии) клал *auth.User в контекст —
// без обёртки RequireAuth/RequireAdmin (это забота Task 5/main.go).
func mountTestRouter(svc *Service) http.Handler {
	authRepo := &testAuthRepository{user: testUser}
	authSvc := auth.NewService(authRepo, nil, time.Hour, false)

	r := chi.NewRouter()
	r.Use(authSvc.LoadSession)
	svc.Mount(r)
	return r
}

func addSessionCookie(req *http.Request) *http.Request {
	req.AddCookie(&http.Cookie{Name: testSessionCookie, Value: "test-session-id"})
	return req
}

func TestHandleStatus_Authorized(t *testing.T) {
	core := &fakeCore{status: coreclient.CookieStatus{Exists: true, Size: 120}}
	handler := mountTestRouter(NewService(core))

	req := addSessionCookie(httptest.NewRequest(http.MethodGet, "/settings/cookies/status", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/cookies/status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleStatus_Anonymous(t *testing.T) {
	core := &fakeCore{status: coreclient.CookieStatus{Exists: true}}
	handler := mountTestRouter(NewService(core))

	req := httptest.NewRequest(http.MethodGet, "/settings/cookies/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /settings/cookies/status (аноним) = %d, want 401", rec.Code)
	}
}

func TestHandleUpload_Success(t *testing.T) {
	core := &fakeCore{}
	handler := mountTestRouter(NewService(core))

	body, contentType := multipartCookiesBody(t, "cookies.txt", "cookie-content")
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/settings/cookies", body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /settings/cookies = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if !core.putCalled {
		t.Error("ожидался вызов PutYouTubeCookies")
	}
}

func TestHandleUpload_NoFile(t *testing.T) {
	core := &fakeCore{}
	handler := mountTestRouter(NewService(core))

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.Close()

	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/settings/cookies", &buf))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /settings/cookies без файла = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
	if core.putCalled {
		t.Error("PutYouTubeCookies не должен вызываться без файла")
	}
}

func TestHandleUpload_BadFormat(t *testing.T) {
	core := &fakeCore{putErr: coreclient.ErrCookiesBadFormat}
	handler := mountTestRouter(NewService(core))

	body, contentType := multipartCookiesBody(t, "cookies.txt", "not-netscape")
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/settings/cookies", body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /settings/cookies (bad format) = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("формат")) {
		t.Errorf("тело ответа должно упоминать формат: %q", rec.Body.String())
	}
}

func TestHandleUpload_TooLarge(t *testing.T) {
	core := &fakeCore{putErr: coreclient.ErrCookiesTooLarge}
	handler := mountTestRouter(NewService(core))

	body, contentType := multipartCookiesBody(t, "cookies.txt", "abc")
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/settings/cookies", body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST /settings/cookies (too large) = %d, want 413, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDelete_Success(t *testing.T) {
	core := &fakeCore{}
	handler := mountTestRouter(NewService(core))

	req := addSessionCookie(httptest.NewRequest(http.MethodDelete, "/settings/cookies", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /settings/cookies = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if !core.delCalled {
		t.Error("ожидался вызов DeleteYouTubeCookies")
	}
}

func TestHandleDelete_CoreUnavailable(t *testing.T) {
	core := &fakeCore{deleteErr: errors.New("core down")}
	handler := mountTestRouter(NewService(core))

	req := addSessionCookie(httptest.NewRequest(http.MethodDelete, "/settings/cookies", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("DELETE /settings/cookies (core down) = %d, want 502, body: %s", rec.Code, rec.Body.String())
	}
}

// multipartCookiesBody собирает multipart-тело с полем file для запроса загрузки.
func multipartCookiesBody(t *testing.T, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return &buf, mw.FormDataContentType()
}
