package upload

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/coreclient"
)

func TestPrepareFileUpload_Valid(t *testing.T) {
	ctx := context.Background()
	userID := "user-1"
	signer := newTestServiceSigner()
	core := &mockCore{
		createUploadFunc: func(ctx context.Context, filename string) (coreclient.UploadResult, error) {
			if filename != "lecture.mp4" {
				t.Fatalf("filename = %q, want lecture.mp4", filename)
			}
			return coreclient.UploadResult{
				Key:       "uploads/user-1/lecture.mp4",
				URL:       "https://storage.example/put",
				ExpiresIn: 300,
			}, nil
		},
	}

	service := NewService(core, &mockRepo{}, signer, time.Hour)
	got, err := service.PrepareFileUpload(ctx, userID, "lecture.mp4", 1024, "video/mp4")
	if err != nil {
		t.Fatalf("PrepareFileUpload() error = %v, want nil", err)
	}

	if got.Token == "" {
		t.Fatal("Token is empty")
	}
	media, err := signer.Verify(got.Token, userID, "uploads/user-1/lecture.mp4")
	if err != nil {
		t.Fatalf("Verify(token) error = %v, want nil", err)
	}
	if media != "video" {
		t.Fatalf("Verify(token) media = %q, want video", media)
	}
	if got.PutURL != "https://storage.example/put" {
		t.Fatalf("PutURL = %q, want storage URL", got.PutURL)
	}
	if got.S3Key != "uploads/user-1/lecture.mp4" {
		t.Fatalf("S3Key = %q, want uploads/user-1/lecture.mp4", got.S3Key)
	}
	if got.Media != "video" {
		t.Fatalf("Media = %q, want video", got.Media)
	}
	if got.Title != "lecture" {
		t.Fatalf("Title = %q, want lecture", got.Title)
	}
	if got.ExpiresIn != 300 {
		t.Fatalf("ExpiresIn = %d, want 300", got.ExpiresIn)
	}
	if core.createUploadCalls != 1 {
		t.Fatalf("CreateUpload calls = %d, want 1", core.createUploadCalls)
	}
}

func TestPrepareFileUpload_BadExtension(t *testing.T) {
	core := &mockCore{}
	service := NewService(core, &mockRepo{}, newTestServiceSigner(), time.Hour)

	_, err := service.PrepareFileUpload(context.Background(), "user-1", "notes.pdf", 1024, "application/pdf")
	if !errors.Is(err, ErrUnsupportedMedia) {
		t.Fatalf("PrepareFileUpload() error = %v, want %v", err, ErrUnsupportedMedia)
	}
	if core.createUploadCalls != 0 {
		t.Fatalf("CreateUpload calls = %d, want 0", core.createUploadCalls)
	}
}

func TestPrepareFileUpload_MIMEMismatch(t *testing.T) {
	core := &mockCore{}
	service := NewService(core, &mockRepo{}, newTestServiceSigner(), time.Hour)

	_, err := service.PrepareFileUpload(context.Background(), "user-1", "lecture.mp4", 1024, "image/png")
	if !errors.Is(err, ErrMediaMismatch) {
		t.Fatalf("PrepareFileUpload() error = %v, want %v", err, ErrMediaMismatch)
	}
	if core.createUploadCalls != 0 {
		t.Fatalf("CreateUpload calls = %d, want 0", core.createUploadCalls)
	}
}

func TestConfirmFileUpload_Success(t *testing.T) {
	ctx := context.Background()
	signer := newTestServiceSigner()
	token := signer.Sign("user-1", "uploads/user-1/lecture.mp4", "audio", time.Hour)
	order := make([]string, 0, 2)
	core := &mockCore{
		createTaskFunc: func(ctx context.Context, p coreclient.CreateTaskParams) (string, error) {
			order = append(order, "core")
			if p.S3Key != "uploads/user-1/lecture.mp4" {
				t.Fatalf("S3Key = %q, want uploads/user-1/lecture.mp4", p.S3Key)
			}
			if p.Media != "audio" {
				t.Fatalf("Media = %q, want audio", p.Media)
			}
			return "task-1", nil
		},
	}
	repo := &mockRepo{
		createLectureFunc: func(ctx context.Context, p CreateLectureParams) (string, error) {
			order = append(order, "repo")
			if p.OwnerID != "user-1" {
				t.Fatalf("OwnerID = %q, want user-1", p.OwnerID)
			}
			if p.Title != "Lecture" {
				t.Fatalf("Title = %q, want Lecture", p.Title)
			}
			if p.SourceKind != "audio" {
				t.Fatalf("SourceKind = %q, want audio", p.SourceKind)
			}
			if p.S3Key != "uploads/user-1/lecture.mp4" {
				t.Fatalf("S3Key = %q, want uploads/user-1/lecture.mp4", p.S3Key)
			}
			if p.CoreTaskID != "task-1" {
				t.Fatalf("CoreTaskID = %q, want task-1", p.CoreTaskID)
			}
			return "lecture-1", nil
		},
	}

	service := NewService(core, repo, signer, time.Hour)
	got, err := service.ConfirmFileUpload(ctx, "user-1", ConfirmInput{
		Token:         token,
		S3Key:         "uploads/user-1/lecture.mp4",
		Title:         "Lecture",
		ExtractSlides: true,
	})
	if err != nil {
		t.Fatalf("ConfirmFileUpload() error = %v, want nil", err)
	}
	if got != "lecture-1" {
		t.Fatalf("lectureID = %q, want lecture-1", got)
	}
	if len(order) != 2 || order[0] != "core" || order[1] != "repo" {
		t.Fatalf("call order = %v, want [core repo]", order)
	}
}

func TestConfirmFileUpload_TamperedToken(t *testing.T) {
	core := &mockCore{}
	repo := &mockRepo{}
	service := NewService(core, repo, newTestServiceSigner(), time.Hour)

	_, err := service.ConfirmFileUpload(context.Background(), "user-1", ConfirmInput{
		Token: "bad-token",
		S3Key: "uploads/user-1/lecture.mp4",
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("ConfirmFileUpload() error = %v, want %v", err, ErrForbidden)
	}
	if core.createTaskCalls != 0 {
		t.Fatalf("CreateTask calls = %d, want 0", core.createTaskCalls)
	}
	if repo.createLectureCalls != 0 {
		t.Fatalf("CreateLecture calls = %d, want 0", repo.createLectureCalls)
	}
}

func TestConfirmFileUpload_CoreError(t *testing.T) {
	wantErr := errors.New("core unavailable")
	signer := newTestServiceSigner()
	core := &mockCore{
		createTaskFunc: func(ctx context.Context, p coreclient.CreateTaskParams) (string, error) {
			return "", wantErr
		},
	}
	repo := &mockRepo{}
	service := NewService(core, repo, signer, time.Hour)

	_, err := service.ConfirmFileUpload(context.Background(), "user-1", ConfirmInput{
		Token: signer.Sign("user-1", "uploads/user-1/lecture.mp4", "video", time.Hour),
		S3Key: "uploads/user-1/lecture.mp4",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ConfirmFileUpload() error = %v, want %v", err, wantErr)
	}
	if repo.createLectureCalls != 0 {
		t.Fatalf("CreateLecture calls = %d, want 0", repo.createLectureCalls)
	}
}

func TestConfirmFileUpload_PDFForcesNoSlides(t *testing.T) {
	assertConfirmNoSlides(t, ConfirmInput{HasPDF: true, ExtractSlides: true}, true)
}

func TestConfirmFileUpload_ExtractToggleOff(t *testing.T) {
	assertConfirmNoSlides(t, ConfirmInput{HasPDF: false, ExtractSlides: false}, true)
}

func TestConfirmFileUpload_ExtractOn(t *testing.T) {
	assertConfirmNoSlides(t, ConfirmInput{HasPDF: false, ExtractSlides: true}, false)
}

func TestCreateYouTube_Success(t *testing.T) {
	ctx := context.Background()
	order := make([]string, 0, 2)
	core := &mockCore{
		createTaskFunc: func(ctx context.Context, p coreclient.CreateTaskParams) (string, error) {
			order = append(order, "core")
			if p.VideoURL != "https://youtu.be/video" {
				t.Fatalf("VideoURL = %q, want https://youtu.be/video", p.VideoURL)
			}
			if p.Media != "" {
				t.Fatalf("Media = %q, want empty", p.Media)
			}
			if p.NoSlides {
				t.Fatal("NoSlides = true, want false")
			}
			return "task-yt", nil
		},
	}
	repo := &mockRepo{
		createLectureFunc: func(ctx context.Context, p CreateLectureParams) (string, error) {
			order = append(order, "repo")
			if p.OwnerID != "user-1" {
				t.Fatalf("OwnerID = %q, want user-1", p.OwnerID)
			}
			if p.Title != "YouTube lecture" {
				t.Fatalf("Title = %q, want YouTube lecture", p.Title)
			}
			if p.SourceKind != "video_url" {
				t.Fatalf("SourceKind = %q, want video_url", p.SourceKind)
			}
			if p.VideoURL != "https://youtu.be/video" {
				t.Fatalf("VideoURL = %q, want https://youtu.be/video", p.VideoURL)
			}
			if p.CoreTaskID != "task-yt" {
				t.Fatalf("CoreTaskID = %q, want task-yt", p.CoreTaskID)
			}
			return "lecture-yt", nil
		},
	}

	service := NewService(core, repo, newTestServiceSigner(), time.Hour)
	got, err := service.CreateYouTube(ctx, "user-1", YouTubeInput{
		URL:           "https://youtu.be/video",
		Title:         "YouTube lecture",
		ExtractSlides: true,
	})
	if err != nil {
		t.Fatalf("CreateYouTube() error = %v, want nil", err)
	}
	if got != "lecture-yt" {
		t.Fatalf("lectureID = %q, want lecture-yt", got)
	}
	if len(order) != 2 || order[0] != "core" || order[1] != "repo" {
		t.Fatalf("call order = %v, want [core repo]", order)
	}
}

func TestCreateYouTube_ExtractSlidesOn(t *testing.T) {
	assertYouTubeNoSlides(t, YouTubeInput{HasPDF: false, ExtractSlides: true}, false)
}

func TestCreateYouTube_PDFForcesNoSlides(t *testing.T) {
	assertYouTubeNoSlides(t, YouTubeInput{HasPDF: true, ExtractSlides: true}, true)
}

func TestCreateYouTube_NoSlidesAtAll(t *testing.T) {
	assertYouTubeNoSlides(t, YouTubeInput{HasPDF: false, ExtractSlides: false}, true)
}

func TestCreateYouTube_InvalidURL(t *testing.T) {
	core := &mockCore{}
	repo := &mockRepo{}
	service := NewService(core, repo, newTestServiceSigner(), time.Hour)

	_, err := service.CreateYouTube(context.Background(), "user-1", YouTubeInput{
		URL:   "https://example.com/video",
		Title: "Bad video",
	})
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("CreateYouTube() error = %v, want %v", err, ErrInvalidURL)
	}
	if core.createTaskCalls != 0 {
		t.Fatalf("CreateTask calls = %d, want 0", core.createTaskCalls)
	}
	if repo.createLectureCalls != 0 {
		t.Fatalf("CreateLecture calls = %d, want 0", repo.createLectureCalls)
	}
}

func assertConfirmNoSlides(t *testing.T, input ConfirmInput, want bool) {
	t.Helper()

	signer := newTestServiceSigner()
	input.Token = signer.Sign("user-1", "uploads/user-1/lecture.mp4", "video", time.Hour)
	input.S3Key = "uploads/user-1/lecture.mp4"
	core := &mockCore{
		createTaskFunc: func(ctx context.Context, p coreclient.CreateTaskParams) (string, error) {
			if p.NoSlides != want {
				t.Fatalf("NoSlides = %v, want %v", p.NoSlides, want)
			}
			return "task-1", nil
		},
	}
	repo := &mockRepo{
		createLectureFunc: func(ctx context.Context, p CreateLectureParams) (string, error) {
			return "lecture-1", nil
		},
	}
	service := NewService(core, repo, signer, time.Hour)

	if _, err := service.ConfirmFileUpload(context.Background(), "user-1", input); err != nil {
		t.Fatalf("ConfirmFileUpload() error = %v, want nil", err)
	}
}

func assertYouTubeNoSlides(t *testing.T, input YouTubeInput, want bool) {
	t.Helper()

	input.URL = "https://youtu.be/video"
	core := &mockCore{
		createTaskFunc: func(ctx context.Context, p coreclient.CreateTaskParams) (string, error) {
			if p.NoSlides != want {
				t.Fatalf("NoSlides = %v, want %v", p.NoSlides, want)
			}
			return "task-yt", nil
		},
	}
	repo := &mockRepo{
		createLectureFunc: func(ctx context.Context, p CreateLectureParams) (string, error) {
			return "lecture-yt", nil
		},
	}
	service := NewService(core, repo, newTestServiceSigner(), time.Hour)

	if _, err := service.CreateYouTube(context.Background(), "user-1", input); err != nil {
		t.Fatalf("CreateYouTube() error = %v, want nil", err)
	}
}

func newTestServiceSigner() *Signer {
	signer := NewSigner([]byte("service-secret-key"))
	signer.now = fixedNow
	return signer
}

type mockCore struct {
	createUploadFunc func(context.Context, string) (coreclient.UploadResult, error)
	createTaskFunc   func(context.Context, coreclient.CreateTaskParams) (string, error)

	createUploadCalls int
	createTaskCalls   int
}

func (m *mockCore) CreateUpload(ctx context.Context, filename string) (coreclient.UploadResult, error) {
	m.createUploadCalls++
	if m.createUploadFunc != nil {
		return m.createUploadFunc(ctx, filename)
	}
	return coreclient.UploadResult{}, nil
}

func (m *mockCore) CreateTask(ctx context.Context, p coreclient.CreateTaskParams) (string, error) {
	m.createTaskCalls++
	if m.createTaskFunc != nil {
		return m.createTaskFunc(ctx, p)
	}
	return "", nil
}

type mockRepo struct {
	createLectureFunc func(context.Context, CreateLectureParams) (string, error)

	createLectureCalls int
}

func (m *mockRepo) CreateLecture(ctx context.Context, p CreateLectureParams) (string, error) {
	m.createLectureCalls++
	if m.createLectureFunc != nil {
		return m.createLectureFunc(ctx, p)
	}
	return "", nil
}
