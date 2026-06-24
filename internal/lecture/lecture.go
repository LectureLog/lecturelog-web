// Package lecture — доменный модуль лекций платформы LectureLog.
// Содержит типы, интерфейсы и бизнес-логику управления лекциями пользователя.
//
// Архитектура: доменная логика отделена от HTTP и БД через интерфейсы Repository и CoreTasks.
// Адаптеры реализуются в cmd/server — мокабельность из коробки.
package lecture

import (
	"context"
	"errors"
	"time"
)

// Status — статус обработки лекции.
type Status string

const (
	StatusProcessing Status = "processing"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
)

// Visibility — видимость лекции.
type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityPublic  Visibility = "public"
)

// Lecture — доменная лекция пользователя.
type Lecture struct {
	ID          string
	OwnerID     string
	CoreTaskID  string
	Status      Status
	ErrorCode   string
	Visibility  Visibility
	SourceKind  string
	S3Key       string
	VideoURL    string
	Title       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time
}

// Repository — узкий интерфейс данных для доменной логики лекций.
// Пакет lecture ВЛАДЕЕТ интерфейсом → адаптер поверх internal/db реализуется в cmd/server.
type Repository interface {
	// ListByOwner возвращает лекции владельца, упорядоченные по updated_at DESC.
	ListByOwner(ctx context.Context, ownerID string) ([]Lecture, error)
	// FindByID возвращает лекцию по ID. (nil, nil) если не найдена.
	FindByID(ctx context.Context, lectureID string) (*Lecture, error)
	// Rename обновляет заголовок; фильтр по owner_id. Возвращает affected.
	Rename(ctx context.Context, lectureID, ownerID, title string) (int64, error)
	// SetVisibility меняет видимость; для public — только если status=ready. Возвращает affected.
	SetVisibility(ctx context.Context, lectureID, ownerID, visibility string) (int64, error)
	// Delete удаляет лекцию; фильтр по owner_id. Возвращает affected.
	Delete(ctx context.Context, lectureID, ownerID string) (int64, error)
	// SetCoreTaskProcessing переводит failed→processing и обновляет core_task_id (для retry).
	SetCoreTaskProcessing(ctx context.Context, lectureID, ownerID, coreTaskID string) (int64, error)
}

// CoreTasks — узкий интерфейс к ядру обработки лекций.
// Реальная реализация через coreclient передаётся в долг следующему атому C1.
type CoreTasks interface {
	// DeleteTask удаляет задачу из ядра по ID задачи.
	DeleteTask(ctx context.Context, coreTaskID string) error
	// CreateTask создаёт новую задачу обработки в ядре.
	CreateTask(ctx context.Context, p CreateTaskParams) (coreTaskID string, err error)
}

// CreateTaskParams — параметры создания задачи в ядре.
type CreateTaskParams struct {
	// S3Key — ключ в объектном хранилище.
	S3Key string
	// VideoURL — URL видео для скачивания.
	VideoURL string
	// Media — тип медиа (audio/video/video_url).
	Media string
}

// Ошибки домена лекций.
var (
	// ErrNotFound — лекция не найдена или нет прав (не раскрываем чужое существование).
	ErrNotFound = errors.New("lecture: не найдена или нет прав")
	// ErrNotReady — публикация возможна только для готовых лекций.
	ErrNotReady = errors.New("lecture: публиковать можно только готовую")
	// ErrNotFailed — retry доступен только для лекций со статусом failed.
	ErrNotFailed = errors.New("lecture: retry доступен только для failed")
	// ErrNoRetrySource — нет источника для повторной обработки.
	ErrNoRetrySource = errors.New("lecture: нет источника для повтора")
)

// Service — доменный сервис лекций. Содержит бизнес-логику управления лекциями.
type Service struct {
	repo Repository
	core CoreTasks
	now  func() time.Time
}

// NewService создаёт Service с зависимостями.
func NewService(repo Repository, core CoreTasks) *Service {
	return &Service{
		repo: repo,
		core: core,
		now:  time.Now,
	}
}
