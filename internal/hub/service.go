package hub

import "context"

// Repository — источник публичных лекций (адаптер поверх db.LectureDB в cmd/server).
type Repository interface {
	ListPublic(ctx context.Context, limit int) ([]PublicLecture, error)
}

// Service — доменный сервис витрины.
type Service struct {
	repo  Repository
	limit int
	// baseOrigin — внешний origin платформы (scheme://host) для абсолютных
	// ссылок в агентских каталогах. Пусто — origin выводится из запроса.
	baseOrigin string
}

// Option — функциональная опция Service.
type Option func(*Service)

// WithBaseOrigin задаёт внешний origin платформы для абсолютных ссылок
// в /hub.md и /llms.txt (обычно извлекается из PLATFORM_CALLBACK_URL).
func WithBaseOrigin(origin string) Option {
	return func(s *Service) {
		s.baseOrigin = origin
	}
}

// defaultLimit — потолок выдачи витрины (пагинации в MVP нет).
const defaultLimit = 200

// NewService создаёт Service. limit <= 0 заменяется значением по умолчанию.
func NewService(repo Repository, limit int, opts ...Option) *Service {
	if limit <= 0 {
		limit = defaultLimit
	}
	s := &Service{repo: repo, limit: limit}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// List возвращает публичные лекции (ORDER гарантирует Repository).
func (s *Service) List(ctx context.Context) ([]PublicLecture, error) {
	return s.repo.ListPublic(ctx, s.limit)
}
