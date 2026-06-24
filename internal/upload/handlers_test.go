package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/coreclient"
	"github.com/go-chi/chi/v5"
)

var testUser = &auth.User{
	ID:    "user-test-uuid",
	Email: "test@example.com",
	Name:  "Тест",
}

const testSessionCookie = "ll_session"

func newTestHTTPService(core Core, repo Repository) *Service {
	return NewService(core, repo, newTestServiceSigner(), time.Hour)
}

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

func TestPresign_Valid(t *testing.T) {
	core := &mockCore{
		createUploadFunc: func(_ context.Context, filename string) (coreclient.UploadResult, error) {
			if filename != "lecture.mp4" {
				t.Fatalf("filename = %q, want lecture.mp4", filename)
			}
			return coreclient.UploadResult{
				Key:       "uploads/user-test-uuid/lecture.mp4",
				URL:       "https://storage.example/put",
				ExpiresIn: 300,
			}, nil
		},
	}
	handler := mountTestRouter(newTestHTTPService(core, &mockRepo{}))

	body := bytes.NewBufferString(`{"filename":"lecture.mp4","size":1024,"mime":"video/mp4"}`)
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/upload/presign", body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /upload/presign = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var got struct {
		Token     string `json:"token"`
		PutURL    string `json:"put_url"`
		S3Key     string `json:"s3_key"`
		Media     string `json:"media"`
		Title     string `json:"title"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Token == "" {
		t.Fatal("token is empty")
	}
	if got.PutURL != "https://storage.example/put" {
		t.Fatalf("put_url = %q, want storage URL", got.PutURL)
	}
	if got.S3Key != "uploads/user-test-uuid/lecture.mp4" {
		t.Fatalf("s3_key = %q, want uploads/user-test-uuid/lecture.mp4", got.S3Key)
	}
	if got.Media != "video" {
		t.Fatalf("media = %q, want video", got.Media)
	}
	if got.Title != "lecture" {
		t.Fatalf("title = %q, want lecture", got.Title)
	}
	if got.ExpiresIn != 300 {
		t.Fatalf("expires_in = %d, want 300", got.ExpiresIn)
	}
}

func TestPresign_BadExtension(t *testing.T) {
	handler := mountTestRouter(newTestHTTPService(&mockCore{}, &mockRepo{}))

	body := bytes.NewBufferString(`{"filename":"notes.pdf","size":1024,"mime":"application/pdf"}`)
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/upload/presign", body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /upload/presign bad extension = %d, want 422", rec.Code)
	}
}

func TestPresign_Unauthorized(t *testing.T) {
	handler := mountTestRouter(newTestHTTPService(&mockCore{}, &mockRepo{}))

	body := bytes.NewBufferString(`{"filename":"lecture.mp4","size":1024,"mime":"video/mp4"}`)
	req := httptest.NewRequest(http.MethodPost, "/upload/presign", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /upload/presign unauthorized = %d, want 401", rec.Code)
	}
}
