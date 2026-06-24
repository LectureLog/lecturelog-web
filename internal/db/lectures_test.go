//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupLectureTestDB запускает Postgres в Docker и возвращает UserDB + LectureDB.
// Пропускает тест если Docker недоступен.
func setupLectureTestDB(t *testing.T) (context.Context, *UserDB, *LectureDB) {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("lecturelogtest"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("testcontainers: не удалось запустить Postgres: %v", err)
	}

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(pgContainer)
		t.Fatalf("ConnectionString: %v", err)
	}

	pool, err := New(ctx, dsn)
	if err != nil {
		_ = testcontainers.TerminateContainer(pgContainer)
		t.Fatalf("New: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		_ = testcontainers.TerminateContainer(pgContainer)
		t.Fatalf("Migrate: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if terr := testcontainers.TerminateContainer(pgContainer); terr != nil {
			t.Logf("TerminateContainer: %v", terr)
		}
	})

	return ctx, &UserDB{Pool: pool}, &LectureDB{Pool: pool}
}

// createTestUser — вспомогательная функция: создаёт пользователя для тестов.
func createTestUser(t *testing.T, ctx context.Context, userDB *UserDB, email string) *UserRow {
	t.Helper()
	row, err := userDB.CreateUser(ctx, email, "Тестовый пользователь", "")
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	return row
}

// createTestLecture — вспомогательная функция: создаёт лекцию для тестов.
func createTestLecture(t *testing.T, ctx context.Context, lectureDB *LectureDB, ownerID, title, sourceKind string) *LectureRow {
	t.Helper()
	row, err := lectureDB.CreateLecture(ctx, CreateLectureParams{
		OwnerID:    ownerID,
		Title:      title,
		SourceKind: sourceKind,
		S3Key:      "test/key.mp3",
	})
	if err != nil {
		t.Fatalf("createTestLecture: %v", err)
	}
	return row
}

// setReadyStatus — вспомогательная: устанавливает статус ready для лекции.
func setReadyStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, lectureID string) {
	t.Helper()
	_, err := pool.Exec(ctx,
		"UPDATE lectures SET status='ready' WHERE lecture_id=$1", lectureID)
	if err != nil {
		t.Fatalf("setReadyStatus: %v", err)
	}
}

// setFailedStatus — вспомогательная: устанавливает статус failed с кодом ошибки.
func setFailedStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, lectureID, errorCode string) {
	t.Helper()
	_, err := pool.Exec(ctx,
		"UPDATE lectures SET status='failed', error_code=$1 WHERE lecture_id=$2",
		errorCode, lectureID)
	if err != nil {
		t.Fatalf("setFailedStatus: %v", err)
	}
}

// setCoreTaskID — вспомогательная: устанавливает core_task_id для лекции.
func setCoreTaskID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, lectureID, coreTaskID string) {
	t.Helper()
	_, err := pool.Exec(ctx,
		"UPDATE lectures SET core_task_id=$1 WHERE lecture_id=$2", coreTaskID, lectureID)
	if err != nil {
		t.Fatalf("setCoreTaskID: %v", err)
	}
}

// TestLectureDB_ListByOwner_SortedByUpdatedAt проверяет порядок сортировки DESC.
func TestLectureDB_ListByOwner_SortedByUpdatedAt(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "owner-sort@example.com")

	lec1 := createTestLecture(t, ctx, lectureDB, user.UserID, "Лекция 1", "audio")
	time.Sleep(5 * time.Millisecond) // гарантируем разные updated_at в БД
	lec2 := createTestLecture(t, ctx, lectureDB, user.UserID, "Лекция 2", "audio")

	rows, err := lectureDB.ListByOwner(ctx, user.UserID)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListByOwner: len = %d, ожидается 2", len(rows))
	}
	// Сортировка DESC по updated_at: последняя созданная — первая в списке
	if rows[0].LectureID != lec2.LectureID {
		t.Errorf("rows[0].LectureID = %q, ожидается %q (последняя)", rows[0].LectureID, lec2.LectureID)
	}
	if rows[1].LectureID != lec1.LectureID {
		t.Errorf("rows[1].LectureID = %q, ожидается %q (первая)", rows[1].LectureID, lec1.LectureID)
	}
}

// TestLectureDB_ListByOwner_Empty проверяет, что для нового пользователя — пустой срез, не nil.
func TestLectureDB_ListByOwner_Empty(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "empty-list@example.com")

	rows, err := lectureDB.ListByOwner(ctx, user.UserID)
	if err != nil {
		t.Fatalf("ListByOwner empty: %v", err)
	}
	if rows == nil {
		t.Error("ListByOwner должен вернуть пустой срез, не nil")
	}
	if len(rows) != 0 {
		t.Errorf("ListByOwner: len = %d, ожидается 0", len(rows))
	}
}

// TestLectureDB_FindByID проверяет поиск по ID: найти существующую и nil для отсутствующей.
func TestLectureDB_FindByID(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "findme@example.com")
	created := createTestLecture(t, ctx, lectureDB, user.UserID, "Тестовая лекция", "audio")

	// Найти существующую
	row, err := lectureDB.FindByID(ctx, created.LectureID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if row == nil {
		t.Fatal("FindByID вернул nil для существующей лекции")
	}
	if row.LectureID != created.LectureID {
		t.Errorf("LectureID = %q, ожидается %q", row.LectureID, created.LectureID)
	}
	if row.Title != "Тестовая лекция" {
		t.Errorf("Title = %q, ожидается %q", row.Title, "Тестовая лекция")
	}

	// Несуществующая → (nil, nil)
	missing, err := lectureDB.FindByID(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("FindByID (не найдена): %v", err)
	}
	if missing != nil {
		t.Errorf("FindByID должен вернуть nil для несуществующей лекции, вернул %+v", missing)
	}
}

// TestLectureDB_Rename_Owner проверяет переименование своей лекции.
func TestLectureDB_Rename_Owner(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "rename@example.com")
	lec := createTestLecture(t, ctx, lectureDB, user.UserID, "Исходный заголовок", "audio")

	affected, err := lectureDB.Rename(ctx, lec.LectureID, user.UserID, "Новый заголовок")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if affected != 1 {
		t.Errorf("Rename affected = %d, ожидается 1", affected)
	}

	updated, _ := lectureDB.FindByID(ctx, lec.LectureID)
	if updated.Title != "Новый заголовок" {
		t.Errorf("Title после Rename = %q, ожидается %q", updated.Title, "Новый заголовок")
	}
}

// TestLectureDB_Rename_NotOwner проверяет: чужая лекция → affected=0.
func TestLectureDB_Rename_NotOwner(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	owner := createTestUser(t, ctx, userDB, "owner-rename@example.com")
	stranger := createTestUser(t, ctx, userDB, "stranger-rename@example.com")
	lec := createTestLecture(t, ctx, lectureDB, owner.UserID, "Заголовок", "audio")

	affected, err := lectureDB.Rename(ctx, lec.LectureID, stranger.UserID, "Чужой заголовок")
	if err != nil {
		t.Fatalf("Rename (чужой): %v", err)
	}
	if affected != 0 {
		t.Errorf("Rename (чужой) affected = %d, ожидается 0", affected)
	}
}

// TestLectureDB_SetVisibility_PublicOnReady проверяет публикацию готовой лекции + published_at.
func TestLectureDB_SetVisibility_PublicOnReady(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "publish@example.com")
	lec := createTestLecture(t, ctx, lectureDB, user.UserID, "Лекция для публикации", "audio")

	// Устанавливаем статус ready
	setReadyStatus(t, ctx, lectureDB.Pool, lec.LectureID)

	affected, err := lectureDB.SetVisibility(ctx, lec.LectureID, user.UserID, "public")
	if err != nil {
		t.Fatalf("SetVisibility public: %v", err)
	}
	if affected != 1 {
		t.Errorf("SetVisibility public affected = %d, ожидается 1", affected)
	}

	// published_at должен быть выставлен
	updated, _ := lectureDB.FindByID(ctx, lec.LectureID)
	if updated.Visibility != "public" {
		t.Errorf("Visibility = %q, ожидается public", updated.Visibility)
	}
	if updated.PublishedAt == nil {
		t.Error("PublishedAt должен быть выставлен после публикации")
	}
}

// TestLectureDB_SetVisibility_PublicOnProcessing проверяет: public на not-ready → affected=0.
func TestLectureDB_SetVisibility_PublicOnProcessing(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "nopublish@example.com")
	lec := createTestLecture(t, ctx, lectureDB, user.UserID, "Лекция в обработке", "audio")
	// Статус по умолчанию = processing

	affected, err := lectureDB.SetVisibility(ctx, lec.LectureID, user.UserID, "public")
	if err != nil {
		t.Fatalf("SetVisibility public на processing: %v", err)
	}
	if affected != 0 {
		t.Errorf("SetVisibility public на processing: affected = %d, ожидается 0", affected)
	}
}

// TestLectureDB_SetVisibility_PrivateNoPublishedAtReset проверяет: private не обнуляет published_at.
func TestLectureDB_SetVisibility_PrivateNoPublishedAtReset(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "unpublish@example.com")
	lec := createTestLecture(t, ctx, lectureDB, user.UserID, "Публичная лекция", "audio")

	// Сначала публикуем
	setReadyStatus(t, ctx, lectureDB.Pool, lec.LectureID)
	lectureDB.SetVisibility(ctx, lec.LectureID, user.UserID, "public") //nolint

	published, _ := lectureDB.FindByID(ctx, lec.LectureID)
	if published.PublishedAt == nil {
		t.Fatal("Настройка теста: PublishedAt должен быть установлен")
	}
	savedPublishedAt := *published.PublishedAt

	// Теперь снимаем с публикации
	affected, err := lectureDB.SetVisibility(ctx, lec.LectureID, user.UserID, "private")
	if err != nil {
		t.Fatalf("SetVisibility private: %v", err)
	}
	if affected != 1 {
		t.Errorf("SetVisibility private affected = %d, ожидается 1", affected)
	}

	// published_at НЕ должен обнуляться (§8: повторная публикация перепишет)
	updated, _ := lectureDB.FindByID(ctx, lec.LectureID)
	if updated.Visibility != "private" {
		t.Errorf("Visibility = %q, ожидается private", updated.Visibility)
	}
	if updated.PublishedAt == nil {
		t.Error("PublishedAt НЕ должен обнуляться при снятии с публикации (§8)")
	}
	if !updated.PublishedAt.Equal(savedPublishedAt) {
		t.Errorf("PublishedAt изменился: было %v, стало %v", savedPublishedAt, *updated.PublishedAt)
	}
}

// TestLectureDB_Delete_Owner проверяет hard-delete своей лекции.
func TestLectureDB_Delete_Owner(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "delete@example.com")
	lec := createTestLecture(t, ctx, lectureDB, user.UserID, "Удалить меня", "audio")

	affected, err := lectureDB.Delete(ctx, lec.LectureID, user.UserID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if affected != 1 {
		t.Errorf("Delete affected = %d, ожидается 1", affected)
	}

	row, _ := lectureDB.FindByID(ctx, lec.LectureID)
	if row != nil {
		t.Error("Лекция должна быть удалена, но FindByID вернул не nil")
	}
}

// TestLectureDB_SetCoreTaskProcessing проверяет переход failed→processing + обновление task ID.
func TestLectureDB_SetCoreTaskProcessing(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "retry@example.com")
	lec := createTestLecture(t, ctx, lectureDB, user.UserID, "Провальная лекция", "audio")

	// Устанавливаем статус failed
	setFailedStatus(t, ctx, lectureDB.Pool, lec.LectureID, "processing_error")

	affected, err := lectureDB.SetCoreTaskProcessing(ctx, lec.LectureID, user.UserID, "new-task-abc")
	if err != nil {
		t.Fatalf("SetCoreTaskProcessing: %v", err)
	}
	if affected != 1 {
		t.Errorf("SetCoreTaskProcessing affected = %d, ожидается 1", affected)
	}

	// Проверяем обновление
	updated, _ := lectureDB.FindByID(ctx, lec.LectureID)
	if updated.Status != "processing" {
		t.Errorf("Status = %q, ожидается processing", updated.Status)
	}
	if updated.CoreTaskID != "new-task-abc" {
		t.Errorf("CoreTaskID = %q, ожидается new-task-abc", updated.CoreTaskID)
	}
	if updated.ErrorCode != "" {
		t.Errorf("ErrorCode = %q, ожидается пустой (сброшен при retry)", updated.ErrorCode)
	}
}

// TestLectureDB_SetCoreTaskProcessing_OnlyFromFailed проверяет: no-op если статус != failed.
func TestLectureDB_SetCoreTaskProcessing_OnlyFromFailed(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "retry-noaction@example.com")
	lec := createTestLecture(t, ctx, lectureDB, user.UserID, "Обрабатывающаяся лекция", "audio")
	// Статус по умолчанию = processing

	affected, err := lectureDB.SetCoreTaskProcessing(ctx, lec.LectureID, user.UserID, "task-xyz")
	if err != nil {
		t.Fatalf("SetCoreTaskProcessing на processing: %v", err)
	}
	if affected != 0 {
		t.Errorf("SetCoreTaskProcessing на processing: affected = %d, ожидается 0 (no-op)", affected)
	}
}

// TestLectureDB_UpdateStatusConditional проверяет смену статуса только из processing (анти-гонка).
func TestLectureDB_UpdateStatusConditional(t *testing.T) {
	ctx, userDB, lectureDB := setupLectureTestDB(t)

	user := createTestUser(t, ctx, userDB, "conditional@example.com")
	lec := createTestLecture(t, ctx, lectureDB, user.UserID, "Синк лекция", "audio")

	// Устанавливаем core_task_id вручную (для матча вебхука)
	setCoreTaskID(t, ctx, lectureDB.Pool, lec.LectureID, "task-sync")

	// Переход processing → ready
	affected, err := lectureDB.UpdateStatusConditional(ctx, "task-sync", "ready", "")
	if err != nil {
		t.Fatalf("UpdateStatusConditional: %v", err)
	}
	if affected != 1 {
		t.Errorf("UpdateStatusConditional affected = %d, ожидается 1", affected)
	}

	updated, _ := lectureDB.FindByID(ctx, lec.LectureID)
	if updated.Status != "ready" {
		t.Errorf("Status = %q, ожидается ready", updated.Status)
	}

	// Повторный вызов: уже ready → no-op (affected=0); анти-гонка §7
	affected2, err := lectureDB.UpdateStatusConditional(ctx, "task-sync", "failed", "err")
	if err != nil {
		t.Fatalf("UpdateStatusConditional повторный: %v", err)
	}
	if affected2 != 0 {
		t.Errorf("UpdateStatusConditional повторный: affected = %d, ожидается 0 (no-op)", affected2)
	}

	// Статус не изменился
	final, _ := lectureDB.FindByID(ctx, lec.LectureID)
	if final.Status != "ready" {
		t.Errorf("Status после no-op = %q, ожидается ready (неизменный)", final.Status)
	}
}
