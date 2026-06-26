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
}

// defaultLimit — потолок выдачи витрины (пагинации в MVP нет).
const defaultLimit = 200

// NewService создаёт Service. limit <= 0 заменяется значением по умолчанию.
func NewService(repo Repository, limit int) *Service {
	if limit <= 0 {
		limit = defaultLimit
	}
	return &Service{repo: repo, limit: limit}
}

// List возвращает публичные лекции (ORDER гарантирует Repository).
func (s *Service) List(ctx context.Context) ([]PublicLecture, error) {
	return s.repo.ListPublic(ctx, s.limit)
}
