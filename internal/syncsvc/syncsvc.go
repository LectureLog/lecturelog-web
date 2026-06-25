// Package syncsvc синхронизирует статус лекций с ядром LectureLog.
package syncsvc

import (
	"context"
	"errors"
	"time"
)

// ErrTaskNotFound возвращается адаптером ядра, когда задача больше не найдена.
var ErrTaskNotFound = errors.New("syncsvc: задача не найдена")

// Repository — узкий порт БД для синхронизации статуса.
type Repository interface {
	UpdateStatusConditional(ctx context.Context, coreTaskID, status, errorCode string) (int64, error)
	FindByID(ctx context.Context, lectureID string) (*LectureView, error)
}

// CoreStatus — узкий порт ядра для получения текущего прогресса задачи.
type CoreStatus interface {
	GetTaskStatus(ctx context.Context, coreTaskID string) (*TaskProgress, error)
}

// TaskProgress — статус задачи ядра, приведённый к статусам платформы.
type TaskProgress struct {
	Stage       string
	ProgressPct int
	Status      string
	ErrorCode   string
}

// LectureView — минимальный срез лекции, нужный для карточки и проверки владельца.
type LectureView struct {
	ID         string
	OwnerID    string
	CoreTaskID string
	Status     string
	ErrorCode  string
	Title      string
	SourceKind string
	Visibility string
	UpdatedAt  time.Time
}

// Service содержит сценарии синхронизации с ядром.
type Service struct {
	repo          Repository
	core          CoreStatus
	webhookSecret string

	onTerminalWebhook func()
}

// NewService создаёт сервис синхронизации.
func NewService(repo Repository, core CoreStatus, webhookSecret string) *Service {
	return &Service{
		repo:          repo,
		core:          core,
		webhookSecret: webhookSecret,
	}
}
