package lecture

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// maxTitleLen — максимальная длина заголовка лекции в символах.
const maxTitleLen = 200

// List возвращает лекции пользователя ownerID, упорядоченные по дате обновления.
func (s *Service) List(ctx context.Context, ownerID string) ([]Lecture, error) {
	lectures, err := s.repo.ListByOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("lecture: List: %w", err)
	}
	return lectures, nil
}

// Rename переименовывает лекцию lectureID пользователя ownerID.
// Trim + валидация: непустой, ≤maxTitleLen символов.
func (s *Service) Rename(ctx context.Context, lectureID, ownerID, title string) (Lecture, error) {
	// Валидация: trim пробелов, проверка непустоты и длины
	title = strings.TrimSpace(title)
	if title == "" {
		return Lecture{}, errors.New("lecture: заголовок не может быть пустым")
	}
	if len([]rune(title)) > maxTitleLen {
		return Lecture{}, fmt.Errorf("lecture: заголовок слишком длинный (max %d символов)", maxTitleLen)
	}

	affected, err := s.repo.Rename(ctx, lectureID, ownerID, title)
	if err != nil {
		return Lecture{}, fmt.Errorf("lecture: Rename: %w", err)
	}
	if affected == 0 {
		return Lecture{}, ErrNotFound
	}

	// Возвращаем свежие данные
	updated, err := s.repo.FindByID(ctx, lectureID)
	if err != nil {
		return Lecture{}, fmt.Errorf("lecture: Rename FindByID: %w", err)
	}
	if updated == nil {
		return Lecture{}, ErrNotFound
	}
	return *updated, nil
}
