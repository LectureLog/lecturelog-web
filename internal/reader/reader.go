package reader

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound не раскрывает существование недоступной лекции.
	ErrNotFound = errors.New("лекция не найдена")
	// ErrNotReady сообщает владельцу, что обработка ещё не завершена.
	ErrNotReady = errors.New("лекция ещё не готова")
	// ErrCoreUnavailable сообщает о недоступности результата обработки в ядре.
	ErrCoreUnavailable = errors.New("результат обработки временно недоступен")
)

// LectureRepo загружает метаданные лекции.
type LectureRepo interface {
	FindByID(ctx context.Context, id string) (*LectureMeta, error)
}

// LectureMeta содержит метаданные, нужные для авторизации и чтения результата.
type LectureMeta struct {
	ID         string
	OwnerID    string
	Status     string
	Visibility string
	CoreTaskID string
	Title      string
	SourceKind string
}

// ObjectStore читает объекты из хранилища.
type ObjectStore interface {
	GetObject(ctx context.Context, key string) ([]byte, error)
}

// Presigner выдаёт временные ссылки на объекты.
type Presigner interface {
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Renderer преобразует Markdown в безопасный HTML.
type Renderer interface {
	ToHTML(md string) (string, error)
}

// ReaderView — нейтральная модель данных страницы читального зала.
type ReaderView struct {
	LectureID   string
	Title       string
	SourceTitle string
	SourceKind  string
	Duration    int
	Sections    []ViewSection
	IsOwner     bool
}

// ViewSection — раздел читального зала.
type ViewSection struct {
	Number    string
	Title     string
	Subtopics []ViewSubtopic
}

// ViewSubtopic — подтема с материалом, медиа и слайдами.
type ViewSubtopic struct {
	Number      string
	Title       string
	Media       *ViewMedia
	SlideURLs   []string
	ContentHTML string
}

// ViewMedia — готовое для показа медиа с временной ссылкой.
type ViewMedia struct {
	Kind  string
	Start int
	End   int
	URL   string
}

// Service собирает данные читального зала.
type Service struct {
	repo    LectureRepo
	store   ObjectStore
	presign Presigner
	md      Renderer
	ttl     time.Duration
	now     func() time.Time
}

// NewService создаёт сервис читального зала.
func NewService(repo LectureRepo, store ObjectStore, presign Presigner, md Renderer, ttl time.Duration) *Service {
	return &Service{
		repo:    repo,
		store:   store,
		presign: presign,
		md:      md,
		ttl:     ttl,
		now:     time.Now,
	}
}
