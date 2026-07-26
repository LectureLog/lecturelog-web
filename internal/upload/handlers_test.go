package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestPresign_TrailingGarbage(t *testing.T) {
	core := &mockCore{
		createUploadFunc: func(context.Context, string) (coreclient.UploadResult, error) {
			t.Fatal("CreateUpload called for invalid JSON")
			return coreclient.UploadResult{}, nil
		},
	}
	handler := mountTestRouter(newTestHTTPService(core, &mockRepo{}))

	body := bytes.NewBufferString(`{"filename":"lecture.mp4","size":1024,"mime":"video/mp4"} garbage`)
	req := addSessionCookie(httptest.NewRequest(http.MethodPost, "/upload/presign", body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /upload/presign trailing garbage = %d, want 400", rec.Code)
	}
}

func TestConfirm_Success(t *testing.T) {
	s3Key := "uploads/user-test-uuid/lecture.mp4"
	signer := newTestServiceSigner()
	core := &mockCore{
		createTaskFunc: func(_ context.Context, p coreclient.CreateTaskParams) (string, error) {
			if p.S3Key != s3Key {
				t.Fatalf("S3Key = %q, want %s", p.S3Key, s3Key)
			}
			if p.Media != "video" {
				t.Fatalf("Media = %q, want video", p.Media)
			}
			if p.NoSlides {
				t.Fatal("NoSlides = true, want false")
			}
			return "task-confirm", nil
		},
	}
	repo := &mockRepo{
		createLectureFunc: func(_ context.Context, p CreateLectureParams) (string, error) {
			if p.OwnerID != testUser.ID {
				t.Fatalf("OwnerID = %q, want %s", p.OwnerID, testUser.ID)
			}
			if p.Title != "Lecture" {
				t.Fatalf("Title = %q, want Lecture", p.Title)
			}
			if p.SourceKind != "video" {
				t.Fatalf("SourceKind = %q, want video", p.SourceKind)
			}
			if p.S3Key != s3Key {
				t.Fatalf("S3Key = %q, want %s", p.S3Key, s3Key)
			}
			if p.CoreTaskID != "task-confirm" {
				t.Fatalf("CoreTaskID = %q, want task-confirm", p.CoreTaskID)
			}
			return "lecture-confirm", nil
		},
	}
	handler := mountTestRouter(NewService(core, repo, signer, time.Hour))

	form := url.Values{
		"token":          {signer.Sign(testUser.ID, s3Key, "video", time.Hour)},
		"s3_key":         {s3Key},
		"title":          {"Lecture"},
		"extract_slides": {"on"},
	}
	req := addSessionCookie(newFormRequest(http.MethodPost, "/upload/confirm", form))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /upload/confirm = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/lectures" {
		t.Fatalf("HX-Redirect = %q, want /lectures", got)
	}
}

func TestConfirm_ExtractSlidesUnchecked(t *testing.T) {
	assertConfirmNoSlidesFromForm(t, url.Values{}, true)
}

func TestConfirm_HasPDFWithoutFileRejected(t *testing.T) {
	s3Key := "uploads/user-test-uuid/lecture.mp4"
	signer := newTestServiceSigner()
	handler := mountTestRouter(NewService(&mockCore{}, &mockRepo{}, signer, time.Hour))
	form := url.Values{
		"token":   {signer.Sign(testUser.ID, s3Key, "video", time.Hour)},
		"s3_key":  {s3Key},
		"title":   {"Lecture"},
		"has_pdf": {"on"},
	}
	req := addSessionCookie(newFormRequest(http.MethodPost, "/upload/confirm", form))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /upload/confirm without slides = %d, want 422", rec.Code)
	}
}

func TestConfirm_WithSlidesPassesDocumentToCore(t *testing.T) {
	s3Key := "uploads/user-test-uuid/lecture.mp4"
	signer := newTestServiceSigner()
	core := &mockCore{
		createTaskFunc: func(_ context.Context, p coreclient.CreateTaskParams) (string, error) {
			if p.SlidesName != "deck.pdf" {
				t.Fatalf("SlidesName = %q, want deck.pdf", p.SlidesName)
			}
			got, err := io.ReadAll(p.SlidesContent)
			if err != nil || string(got) != "%PDF-test" {
				t.Fatalf("SlidesContent = %q, err=%v", got, err)
			}
			if p.NoSlides {
				t.Fatal("NoSlides = true would disable the attached document in core")
			}
			return "task-confirm", nil
		},
	}
	handler := mountTestRouter(NewService(core, &mockRepo{}, signer, time.Hour))
	fields := url.Values{
		"token":          {signer.Sign(testUser.ID, s3Key, "video", time.Hour)},
		"s3_key":         {s3Key},
		"title":          {"Lecture"},
		"has_pdf":        {"true"},
		"extract_slides": {"true"},
	}
	req := addSessionCookie(newMultipartRequest(t, http.MethodPost, "/upload/confirm", fields, "deck.pdf", "%PDF-test"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /upload/confirm with slides = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestConfirm_ExtractSlidesCheckedEnablesSlides(t *testing.T) {
	assertConfirmNoSlidesFromForm(t, url.Values{
		"extract_slides": {"on"},
	}, false)
}

func TestConfirm_BadToken(t *testing.T) {
	form := url.Values{
		"token":  {"bad-token"},
		"s3_key": {"uploads/user-test-uuid/lecture.mp4"},
		"title":  {"Lecture"},
	}
	handler := mountTestRouter(newTestHTTPService(&mockCore{}, &mockRepo{}))

	req := addSessionCookie(newFormRequest(http.MethodPost, "/upload/confirm", form))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /upload/confirm bad token = %d, want 403", rec.Code)
	}
}

func TestYouTube_Success(t *testing.T) {
	videoURL := "https://youtu.be/video"
	core := &mockCore{
		createTaskFunc: func(_ context.Context, p coreclient.CreateTaskParams) (string, error) {
			if p.VideoURL != videoURL {
				t.Fatalf("VideoURL = %q, want %s", p.VideoURL, videoURL)
			}
			if p.NoSlides {
				t.Fatal("NoSlides = true, want false")
			}
			return "task-youtube", nil
		},
	}
	repo := &mockRepo{
		createLectureFunc: func(_ context.Context, p CreateLectureParams) (string, error) {
			if p.OwnerID != testUser.ID {
				t.Fatalf("OwnerID = %q, want %s", p.OwnerID, testUser.ID)
			}
			if p.Title != "YouTube Lecture" {
				t.Fatalf("Title = %q, want YouTube Lecture", p.Title)
			}
			if p.SourceKind != "video_url" {
				t.Fatalf("SourceKind = %q, want video_url", p.SourceKind)
			}
			if p.VideoURL != videoURL {
				t.Fatalf("VideoURL = %q, want %s", p.VideoURL, videoURL)
			}
			if p.CoreTaskID != "task-youtube" {
				t.Fatalf("CoreTaskID = %q, want task-youtube", p.CoreTaskID)
			}
			return "lecture-youtube", nil
		},
	}
	handler := mountTestRouter(newTestHTTPService(core, repo))

	form := url.Values{
		"url":            {videoURL},
		"title":          {"YouTube Lecture"},
		"extract_slides": {"on"},
	}
	req := addSessionCookie(newFormRequest(http.MethodPost, "/upload/youtube", form))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /upload/youtube = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/lectures" {
		t.Fatalf("HX-Redirect = %q, want /lectures", got)
	}
}

func TestYouTube_BadURL(t *testing.T) {
	handler := mountTestRouter(newTestHTTPService(&mockCore{}, &mockRepo{}))

	form := url.Values{
		"url":   {"https://example.com/video"},
		"title": {"Bad video"},
	}
	req := addSessionCookie(newFormRequest(http.MethodPost, "/upload/youtube", form))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /upload/youtube bad URL = %d, want 422", rec.Code)
	}
}

func TestYouTube_WithSlidesPassesDocumentToCore(t *testing.T) {
	core := &mockCore{
		createTaskFunc: func(_ context.Context, p coreclient.CreateTaskParams) (string, error) {
			if p.VideoURL != "https://youtu.be/video" || p.SlidesName != "deck.pptx" {
				t.Fatalf("params = %+v", p)
			}
			got, err := io.ReadAll(p.SlidesContent)
			if err != nil || string(got) != "pptx-test" {
				t.Fatalf("SlidesContent = %q, err=%v", got, err)
			}
			if p.NoSlides {
				t.Fatal("NoSlides = true would disable the attached document")
			}
			return "task-youtube", nil
		},
	}
	handler := mountTestRouter(newTestHTTPService(core, &mockRepo{}))
	fields := url.Values{
		"url":     {"https://youtu.be/video"},
		"title":   {"Lecture"},
		"has_pdf": {"true"},
	}
	req := addSessionCookie(newMultipartRequest(t, http.MethodPost, "/upload/youtube", fields, "deck.pptx", "pptx-test"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /upload/youtube with slides = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestYouTube_RejectsUnsupportedSlides(t *testing.T) {
	handler := mountTestRouter(newTestHTTPService(&mockCore{}, &mockRepo{}))
	fields := url.Values{
		"url":     {"https://youtu.be/video"},
		"has_pdf": {"true"},
	}
	req := addSessionCookie(newMultipartRequest(t, http.MethodPost, "/upload/youtube", fields, "deck.key", "key-test"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /upload/youtube unsupported slides = %d, want 422", rec.Code)
	}
}

func newFormRequest(method, target string, form url.Values) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func newMultipartRequest(
	t *testing.T,
	method string,
	target string,
	fields url.Values,
	filename string,
	content string,
) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, values := range fields {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if filename != "" {
		part, err := writer.CreateFormFile("slides", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func assertConfirmNoSlidesFromForm(t *testing.T, fields url.Values, want bool) {
	t.Helper()

	s3Key := "uploads/user-test-uuid/lecture.mp4"
	signer := newTestServiceSigner()
	core := &mockCore{
		createTaskFunc: func(_ context.Context, p coreclient.CreateTaskParams) (string, error) {
			if p.NoSlides != want {
				t.Fatalf("NoSlides = %v, want %v", p.NoSlides, want)
			}
			return "task-confirm", nil
		},
	}
	repo := &mockRepo{
		createLectureFunc: func(_ context.Context, _ CreateLectureParams) (string, error) {
			return "lecture-confirm", nil
		},
	}
	handler := mountTestRouter(NewService(core, repo, signer, time.Hour))

	form := url.Values{
		"token":  {signer.Sign(testUser.ID, s3Key, "video", time.Hour)},
		"s3_key": {s3Key},
		"title":  {"Lecture"},
	}
	for k, v := range fields {
		form[k] = v
	}

	req := addSessionCookie(newFormRequest(http.MethodPost, "/upload/confirm", form))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /upload/confirm = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}
