package lecture_test

import (
	"context"
	"testing"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/lecture"
)

// ─── Моки ────────────────────────────────────────────────────────────────────

// mockRepo — мок-реализация lecture.Repository для юнит-тестов (без Postgres).
type mockRepo struct {
	listByOwner           func(ctx context.Context, ownerID string) ([]lecture.Lecture, error)
	findByID              func(ctx context.Context, lectureID string) (*lecture.Lecture, error)
	rename                func(ctx context.Context, lectureID, ownerID, title string) (int64, error)
	setVisibility         func(ctx context.Context, lectureID, ownerID, visibility string) (int64, error)
	delete_               func(ctx context.Context, lectureID, ownerID string) (int64, error)
	setCoreTaskProcessing func(ctx context.Context, lectureID, ownerID, coreTaskID string) (int64, error)
	// журнал вызовов для проверки порядка
	calls []string
}

func (m *mockRepo) ListByOwner(ctx context.Context, ownerID string) ([]lecture.Lecture, error) {
	m.calls = append(m.calls, "ListByOwner")
	if m.listByOwner != nil {
		return m.listByOwner(ctx, ownerID)
	}
	return nil, nil
}

func (m *mockRepo) FindByID(ctx context.Context, lectureID string) (*lecture.Lecture, error) {
	m.calls = append(m.calls, "FindByID")
	if m.findByID != nil {
		return m.findByID(ctx, lectureID)
	}
	return nil, nil
}

func (m *mockRepo) Rename(ctx context.Context, lectureID, ownerID, title string) (int64, error) {
	m.calls = append(m.calls, "Rename")
	if m.rename != nil {
		return m.rename(ctx, lectureID, ownerID, title)
	}
	return 1, nil
}

func (m *mockRepo) SetVisibility(ctx context.Context, lectureID, ownerID, visibility string) (int64, error) {
	m.calls = append(m.calls, "SetVisibility")
	if m.setVisibility != nil {
		return m.setVisibility(ctx, lectureID, ownerID, visibility)
	}
	return 1, nil
}

func (m *mockRepo) Delete(ctx context.Context, lectureID, ownerID string) (int64, error) {
	m.calls = append(m.calls, "Delete")
	if m.delete_ != nil {
		return m.delete_(ctx, lectureID, ownerID)
	}
	return 1, nil
}

func (m *mockRepo) SetCoreTaskProcessing(ctx context.Context, lectureID, ownerID, coreTaskID string) (int64, error) {
	m.calls = append(m.calls, "SetCoreTaskProcessing")
	if m.setCoreTaskProcessing != nil {
		return m.setCoreTaskProcessing(ctx, lectureID, ownerID, coreTaskID)
	}
	return 1, nil
}

// mockCore — мок-реализация lecture.CoreTasks для юнит-тестов.
type mockCore struct {
	deleteTask func(ctx context.Context, coreTaskID string) error
	createTask func(ctx context.Context, p lecture.CreateTaskParams) (string, error)
	calls      []string
}

func (m *mockCore) DeleteTask(ctx context.Context, coreTaskID string) error {
	m.calls = append(m.calls, "DeleteTask")
	if m.deleteTask != nil {
		return m.deleteTask(ctx, coreTaskID)
	}
	return nil
}

func (m *mockCore) CreateTask(ctx context.Context, p lecture.CreateTaskParams) (string, error) {
	m.calls = append(m.calls, "CreateTask")
	if m.createTask != nil {
		return m.createTask(ctx, p)
	}
	return "new-task-id", nil
}

// ─── Тесты List ──────────────────────────────────────────────────────────────

// TestService_List_OK проверяет, что List возвращает лекции владельца.
func TestService_List_OK(t *testing.T) {
	now := time.Now()
	repo := &mockRepo{
		listByOwner: func(_ context.Context, ownerID string) ([]lecture.Lecture, error) {
			if ownerID != "user-1" {
				t.Errorf("ListByOwner ownerID = %q, ожидается %q", ownerID, "user-1")
			}
			return []lecture.Lecture{
				{ID: "lec-1", OwnerID: "user-1", Title: "Алгебра", Status: lecture.StatusReady, UpdatedAt: now},
				{ID: "lec-2", OwnerID: "user-1", Title: "Физика", Status: lecture.StatusProcessing, UpdatedAt: now},
			}, nil
		},
	}
	svc := lecture.NewService(repo, &mockCore{})
	lectures, err := svc.List(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(lectures) != 2 {
		t.Fatalf("List: len = %d, ожидается 2", len(lectures))
	}
	if lectures[0].ID != "lec-1" {
		t.Errorf("lectures[0].ID = %q, ожидается %q", lectures[0].ID, "lec-1")
	}
}

// ─── Тесты Rename ─────────────────────────────────────────────────────────────

// TestService_Rename_EmptyTitle проверяет, что пустой заголовок — ошибка валидации.
func TestService_Rename_EmptyTitle(t *testing.T) {
	svc := lecture.NewService(&mockRepo{}, &mockCore{})
	_, err := svc.Rename(context.Background(), "lec-1", "user-1", "   ")
	if err == nil {
		t.Fatal("Rename с пустым title: ожидается ошибка")
	}
}

// TestService_Rename_TitleTooLong проверяет лимит длины заголовка (~200 символов).
func TestService_Rename_TitleTooLong(t *testing.T) {
	longTitle := ""
	for i := 0; i < 201; i++ {
		longTitle += "а"
	}
	svc := lecture.NewService(&mockRepo{}, &mockCore{})
	_, err := svc.Rename(context.Background(), "lec-1", "user-1", longTitle)
	if err == nil {
		t.Fatal("Rename с длинным title: ожидается ошибка")
	}
}

// TestService_Rename_NotOwner проверяет, что чужая лекция → ErrNotFound.
func TestService_Rename_NotOwner(t *testing.T) {
	repo := &mockRepo{
		rename: func(_ context.Context, _, _, _ string) (int64, error) {
			return 0, nil // affected=0 → не его
		},
	}
	svc := lecture.NewService(repo, &mockCore{})
	_, err := svc.Rename(context.Background(), "lec-1", "user-1", "Новое название")
	if err == nil {
		t.Fatal("Rename чужой лекции: ожидается ошибка")
	}
}
