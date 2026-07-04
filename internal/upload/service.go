package upload

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/coreclient"
)

var ErrForbidden = errors.New("доступ к загрузке запрещён")

type Core interface {
	CreateUpload(ctx context.Context, filename string) (coreclient.UploadResult, error)
	CreateTask(ctx context.Context, p coreclient.CreateTaskParams) (string, error)
}

type Repository interface {
	CreateLecture(ctx context.Context, p CreateLectureParams) (lectureID string, err error)
}

type CreateLectureParams struct {
	OwnerID    string
	Title      string
	SourceKind string
	S3Key      string
	VideoURL   string
	CoreTaskID string
}

type PrepareResult struct {
	Token     string
	PutURL    string
	S3Key     string
	Media     string
	Title     string
	ExpiresIn int
}

type ConfirmInput struct {
	Token         string
	S3Key         string
	Title         string
	HasPDF        bool
	ExtractSlides bool
}

type YouTubeInput struct {
	URL           string
	Title         string
	HasPDF        bool
	ExtractSlides bool
}

type Service struct {
	core      Core
	repo      Repository
	signer    *Signer
	uploadTTL time.Duration
}

func NewService(core Core, repo Repository, signer *Signer, uploadTTL time.Duration) *Service {
	return &Service{
		core:      core,
		repo:      repo,
		signer:    signer,
		uploadTTL: uploadTTL,
	}
}

func (s *Service) PrepareFileUpload(ctx context.Context, userID, filename string, size int64, mime string) (PrepareResult, error) {
	if err := ValidateFileMeta(filename, mime, size); err != nil {
		return PrepareResult{}, err
	}

	media, _ := DetectMedia(filename)
	res, err := s.core.CreateUpload(ctx, filename)
	if err != nil {
		return PrepareResult{}, err
	}

	return PrepareResult{
		Token:     s.signer.Sign(userID, res.Key, media, s.uploadTTL),
		PutURL:    res.URL,
		S3Key:     res.Key,
		Media:     media,
		Title:     titleFromFilename(filename),
		ExpiresIn: res.ExpiresIn,
	}, nil
}

func (s *Service) ConfirmFileUpload(ctx context.Context, userID string, in ConfirmInput) (string, error) {
	media, err := s.signer.Verify(in.Token, userID, in.S3Key)
	if err != nil {
		return "", ErrForbidden
	}

	taskID, err := s.core.CreateTask(ctx, coreclient.CreateTaskParams{
		S3Key:    in.S3Key,
		Media:    media,
		NoSlides: noSlides(in.HasPDF, extractSlidesEffective(in.ExtractSlides)),
	})
	if err != nil {
		return "", err
	}

	return s.repo.CreateLecture(ctx, CreateLectureParams{
		OwnerID:    userID,
		Title:      in.Title,
		SourceKind: media,
		S3Key:      in.S3Key,
		CoreTaskID: taskID,
	})
}

func (s *Service) CreateYouTube(ctx context.Context, userID string, in YouTubeInput) (string, error) {
	if err := ValidateYouTubeURL(in.URL); err != nil {
		return "", err
	}

	// Долг: PDF-слайды пока не передаются в ядро, потому что CreateTaskParams
	// не принимает файл слайдов; HasPDF только отключает извлечение слайдов.
	taskID, err := s.core.CreateTask(ctx, coreclient.CreateTaskParams{
		VideoURL: in.URL,
		NoSlides: noSlides(in.HasPDF, extractSlidesEffective(in.ExtractSlides)),
	})
	if err != nil {
		return "", err
	}

	return s.repo.CreateLecture(ctx, CreateLectureParams{
		OwnerID:    userID,
		Title:      in.Title,
		SourceKind: "video_url",
		VideoURL:   in.URL,
		CoreTaskID: taskID,
	})
}

// videoSlideExtractionDisabled — временный форсинг: извлечение слайдов из видео
// в ядре отключено (тихая деградация), поэтому веб не должен передавать запрос
// на него в core, даже если клиент прислал ExtractSlides=true (защита в глубину
// на случай обхода фронтенда прямым POST). Чтобы вернуть фичу, установить false.
const videoSlideExtractionDisabled = true

// extractSlidesEffective — единая точка правды: приводит входящий ExtractSlides
// к фактическому значению с учётом форсированного отключения видео-извлечения.
func extractSlidesEffective(requested bool) bool {
	if videoSlideExtractionDisabled {
		return false
	}
	return requested
}

func noSlides(hasPDF, extractSlides bool) bool {
	if hasPDF {
		return true
	}
	return !extractSlides
}

func titleFromFilename(filename string) string {
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
