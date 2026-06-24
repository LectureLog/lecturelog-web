package lecture_test

import (
	"context"
	"fmt"
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

// ─── Тесты SetVisibility ─────────────────────────────────────────────────────

// TestService_SetVisibility_PublicOnReady проверяет публикацию готовой лекции.
func TestService_SetVisibility_PublicOnReady(t *testing.T) {
	readyLecture := &lecture.Lecture{
		ID: "lec-1", OwnerID: "user-1",
		Status:     lecture.StatusReady,
		Visibility: lecture.VisibilityPrivate,
	}
	repo := &mockRepo{
		setVisibility: func(_ context.Context, _, _, _ string) (int64, error) {
			return 1, nil // affected=1 → успех
		},
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			updated := *readyLecture
			updated.Visibility = lecture.VisibilityPublic
			return &updated, nil
		},
	}
	svc := lecture.NewService(repo, &mockCore{})
	lec, err := svc.SetVisibility(context.Background(), "lec-1", "user-1", lecture.VisibilityPublic)
	if err != nil {
		t.Fatalf("SetVisibility public на ready: %v", err)
	}
	if lec.Visibility != lecture.VisibilityPublic {
		t.Errorf("Visibility = %q, ожидается %q", lec.Visibility, lecture.VisibilityPublic)
	}
}

// TestService_SetVisibility_PublicOnProcessing проверяет, что public на не-ready → ErrNotReady.
func TestService_SetVisibility_PublicOnProcessing(t *testing.T) {
	processingLecture := &lecture.Lecture{
		ID: "lec-1", OwnerID: "user-1",
		Status:     lecture.StatusProcessing,
		Visibility: lecture.VisibilityPrivate,
	}
	repo := &mockRepo{
		setVisibility: func(_ context.Context, _, _, _ string) (int64, error) {
			return 0, nil // affected=0 → статус не ready
		},
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			return processingLecture, nil // лекция существует, но не ready
		},
	}
	svc := lecture.NewService(repo, &mockCore{})
	_, err := svc.SetVisibility(context.Background(), "lec-1", "user-1", lecture.VisibilityPublic)
	if err == nil {
		t.Fatal("SetVisibility public на processing: ожидается ошибка")
	}
	// Ошибка должна быть ErrNotReady, не ErrNotFound
	if err != lecture.ErrNotReady {
		t.Errorf("ошибка = %v, ожидается ErrNotReady", err)
	}
}

// TestService_SetVisibility_Private проверяет снятие с публикации.
func TestService_SetVisibility_Private(t *testing.T) {
	repo := &mockRepo{
		setVisibility: func(_ context.Context, _, _, _ string) (int64, error) {
			return 1, nil
		},
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			return &lecture.Lecture{
				ID:         "lec-1",
				Visibility: lecture.VisibilityPrivate,
			}, nil
		},
	}
	svc := lecture.NewService(repo, &mockCore{})
	lec, err := svc.SetVisibility(context.Background(), "lec-1", "user-1", lecture.VisibilityPrivate)
	if err != nil {
		t.Fatalf("SetVisibility private: %v", err)
	}
	if lec.Visibility != lecture.VisibilityPrivate {
		t.Errorf("Visibility = %q, ожидается %q", lec.Visibility, lecture.VisibilityPrivate)
	}
}

// ─── Тесты Delete ────────────────────────────────────────────────────────────

// TestService_Delete_CoreBeforeRepo проверяет порядок: ядро → строка.
func TestService_Delete_CoreBeforeRepo(t *testing.T) {
	var callOrder []string

	repo := &mockRepo{
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			callOrder = append(callOrder, "FindByID")
			return &lecture.Lecture{
				ID: "lec-1", OwnerID: "user-1",
				CoreTaskID: "task-abc",
				Status:     lecture.StatusReady,
			}, nil
		},
		delete_: func(_ context.Context, _, _ string) (int64, error) {
			callOrder = append(callOrder, "Delete")
			return 1, nil
		},
	}
	core := &mockCore{
		deleteTask: func(_ context.Context, _ string) error {
			callOrder = append(callOrder, "DeleteTask")
			return nil
		},
	}
	svc := lecture.NewService(repo, core)
	if err := svc.Delete(context.Background(), "lec-1", "user-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Проверяем порядок: сначала ядро, потом БД
	if len(callOrder) != 3 {
		t.Fatalf("callOrder len = %d, ожидается 3, calls = %v", len(callOrder), callOrder)
	}
	if callOrder[0] != "FindByID" {
		t.Errorf("callOrder[0] = %q, ожидается FindByID", callOrder[0])
	}
	if callOrder[1] != "DeleteTask" {
		t.Errorf("callOrder[1] = %q, ожидается DeleteTask (ядро ПЕРЕД БД)", callOrder[1])
	}
	if callOrder[2] != "Delete" {
		t.Errorf("callOrder[2] = %q, ожидается Delete", callOrder[2])
	}
}

// TestService_Delete_CoreError_NoDBDelete проверяет: ошибка ядра → строка НЕ удаляется.
func TestService_Delete_CoreError_NoDBDelete(t *testing.T) {
	repoDeleted := false

	repo := &mockRepo{
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			return &lecture.Lecture{
				ID: "lec-1", OwnerID: "user-1",
				CoreTaskID: "task-abc",
			}, nil
		},
		delete_: func(_ context.Context, _, _ string) (int64, error) {
			repoDeleted = true
			return 1, nil
		},
	}
	core := &mockCore{
		deleteTask: func(_ context.Context, _ string) error {
			return fmt.Errorf("ядро недоступно")
		},
	}
	svc := lecture.NewService(repo, core)
	err := svc.Delete(context.Background(), "lec-1", "user-1")
	if err == nil {
		t.Fatal("Delete при ошибке ядра: ожидается ошибка")
	}
	if repoDeleted {
		t.Error("Delete при ошибке ядра: строка БД НЕ должна быть удалена")
	}
}

// ─── Тесты Retry ─────────────────────────────────────────────────────────────

// TestService_Retry_NotFailed проверяет, что retry на не-failed → ErrNotFailed.
func TestService_Retry_NotFailed(t *testing.T) {
	repo := &mockRepo{
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			return &lecture.Lecture{
				ID: "lec-1", OwnerID: "user-1",
				Status: lecture.StatusProcessing, // не failed
			}, nil
		},
	}
	svc := lecture.NewService(repo, &mockCore{})
	_, err := svc.Retry(context.Background(), "lec-1", "user-1")
	if err == nil {
		t.Fatal("Retry на processing: ожидается ошибка")
	}
	if err != lecture.ErrNotFailed {
		t.Errorf("ошибка = %v, ожидается ErrNotFailed", err)
	}
}

// TestService_Retry_NoSource проверяет retry без источника → ErrNoRetrySource.
func TestService_Retry_NoSource(t *testing.T) {
	repo := &mockRepo{
		findByID: func(_ context.Context, _ string) (*lecture.Lecture, error) {
			return &lecture.Lecture{
				ID: "lec-1", OwnerID: "user-1",
				Status: lecture.StatusFailed,
				S3Key:  "", VideoURL: "", // нет источника
			}, nil
		},
	}
	svc := lecture.NewService(repo, &mockCore{})
	_, err := svc.Retry(context.Background(), "lec-1", "user-1")
	if err == nil {
		t.Fatal("Retry без источника: ожидается ошибка")
	}
	if err != lecture.ErrNoRetrySource {
		t.Errorf("ошибка = %v, ожидается ErrNoRetrySource", err)
	}
}

// TestService_Retry_Success проверяет успешный retry: CreateTask + SetCoreTaskProcessing.
func TestService_Retry_Success(t *testing.T) {
	var coreTaskIDUsed string

	repo := &mockRepo{
		findByID: func(_ context.Context, id string) (*lecture.Lecture, error) {
			// Первый вызов (для проверки), второй вызов (свежие данные после retry)
			return &lecture.Lecture{
				ID: "lec-1", OwnerID: "user-1",
				Status:     lecture.StatusFailed,
				S3Key:      "lectures/user-1/audio.mp3",
				SourceKind: "audio",
			}, nil
		},
		setCoreTaskProcessing: func(_ context.Context, _, _, coreTaskID string) (int64, error) {
			coreTaskIDUsed = coreTaskID
			return 1, nil
		},
	}
	core := &mockCore{
		createTask: func(_ context.Context, p lecture.CreateTaskParams) (string, error) {
			return "new-task-xyz", nil
		},
	}
	svc := lecture.NewService(repo, core)
	lec, err := svc.Retry(context.Background(), "lec-1", "user-1")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	// Проверяем, что SetCoreTaskProcessing получил новый task ID
	if coreTaskIDUsed != "new-task-xyz" {
		t.Errorf("core_task_id = %q, ожидается %q", coreTaskIDUsed, "new-task-xyz")
	}
	// Статус должен быть processing (findByID возвращает лекцию, статус не меняется в моке,
	// но логика должна вернуть что-то без ошибки)
	_ = lec
}
