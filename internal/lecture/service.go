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

// SetVisibility меняет видимость лекции.
// Для public: лекция должна быть в статусе ready (иначе ErrNotReady).
// Для private: снятие с публикации в любой момент; published_at не обнуляем.
func (s *Service) SetVisibility(ctx context.Context, lectureID, ownerID string, vis Visibility) (Lecture, error) {
	affected, err := s.repo.SetVisibility(ctx, lectureID, ownerID, string(vis))
	if err != nil {
		return Lecture{}, fmt.Errorf("lecture: SetVisibility: %w", err)
	}
	if affected == 0 {
		if vis == VisibilityPublic {
			// Нужно различить: лекция не существует/чужая vs существует но не ready.
			// FindByID без фильтра owner_id невозможен через Repository.
			// Используем FindByID — адаптер вернёт то, что принадлежит или что нашлось.
			// Если возвращает nil — лекции нет или нет прав (ErrNotFound).
			// Если есть и статус != ready — ErrNotReady.
			existing, ferr := s.repo.FindByID(ctx, lectureID)
			if ferr != nil {
				return Lecture{}, fmt.Errorf("lecture: SetVisibility FindByID: %w", ferr)
			}
			if existing == nil {
				return Lecture{}, ErrNotFound
			}
			if existing.Status != StatusReady {
				return Lecture{}, ErrNotReady
			}
			// Если лекция ready но affected=0 — скорее всего чужая (owner_id в WHERE)
			return Lecture{}, ErrNotFound
		}
		// private: affected=0 → не найдена или не его
		return Lecture{}, ErrNotFound
	}

	// Возвращаем свежие данные
	updated, err := s.repo.FindByID(ctx, lectureID)
	if err != nil {
		return Lecture{}, fmt.Errorf("lecture: SetVisibility FindByID: %w", err)
	}
	if updated == nil {
		return Lecture{}, ErrNotFound
	}
	return *updated, nil
}

// Delete удаляет лекцию. Сначала сигнализирует ядру (если есть core_task_id),
// затем удаляет строку из БД. Ошибка ядра прерывает операцию.
func (s *Service) Delete(ctx context.Context, lectureID, ownerID string) error {
	// Шаг 1: загружаем лекцию (проверка прав + получаем core_task_id)
	lec, err := s.repo.FindByID(ctx, lectureID)
	if err != nil {
		return fmt.Errorf("lecture: Delete FindByID: %w", err)
	}
	if lec == nil {
		return ErrNotFound
	}

	// Шаг 2: удаляем задачу из ядра ПЕРЕД удалением строки (§8)
	if lec.CoreTaskID != "" {
		if err := s.core.DeleteTask(ctx, lec.CoreTaskID); err != nil {
			return fmt.Errorf("lecture: Delete core.DeleteTask: %w", err)
		}
	}

	// Шаг 3: удаляем строку из БД
	affected, err := s.repo.Delete(ctx, lectureID, ownerID)
	if err != nil {
		return fmt.Errorf("lecture: Delete repo.Delete: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Retry повторно запускает обработку лекции со статусом failed.
// Алгоритм: проверяем статус → определяем источник → CreateTask в ядре →
// SetCoreTaskProcessing в БД → возвращаем свежую лекцию.
func (s *Service) Retry(ctx context.Context, lectureID, ownerID string) (Lecture, error) {
	// Шаг 1: загружаем лекцию
	lec, err := s.repo.FindByID(ctx, lectureID)
	if err != nil {
		return Lecture{}, fmt.Errorf("lecture: Retry FindByID: %w", err)
	}
	if lec == nil {
		return Lecture{}, ErrNotFound
	}
	if lec.Status != StatusFailed {
		return Lecture{}, ErrNotFailed
	}

	// Шаг 2: определяем источник
	if lec.S3Key == "" && lec.VideoURL == "" {
		return Lecture{}, ErrNoRetrySource
	}

	// Шаг 3: строим параметры и создаём задачу в ядре
	params := CreateTaskParams{
		S3Key:    lec.S3Key,
		VideoURL: lec.VideoURL,
		Media:    lec.SourceKind,
	}
	newTaskID, err := s.core.CreateTask(ctx, params)
	if err != nil {
		return Lecture{}, fmt.Errorf("lecture: Retry core.CreateTask: %w", err)
	}

	// Шаг 4: обновляем статус в БД
	affected, err := s.repo.SetCoreTaskProcessing(ctx, lectureID, ownerID, newTaskID)
	if err != nil {
		return Lecture{}, fmt.Errorf("lecture: Retry SetCoreTaskProcessing: %w", err)
	}
	if affected == 0 {
		return Lecture{}, ErrNotFound
	}

	// Шаг 5: возвращаем свежие данные
	updated, err := s.repo.FindByID(ctx, lectureID)
	if err != nil {
		return Lecture{}, fmt.Errorf("lecture: Retry FindByID fresh: %w", err)
	}
	if updated == nil {
		return Lecture{}, ErrNotFound
	}
	return *updated, nil
}
