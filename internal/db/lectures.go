// Package db — слой доступа к данным лекций платформы LectureLog.
// LectureDB предоставляет CRUD-операции над таблицей lectures.
// Логика домена (порядок вызовов ядро→БД, валидация) — в пакете lecture, не здесь.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LectureRow — строка таблицы lectures (без доменной логики пакета lecture).
type LectureRow struct {
	// LectureID — UUID лекции (PK).
	LectureID string
	// OwnerID — UUID владельца (FK → users).
	OwnerID string
	// CoreTaskID — текущая задача ядра; "" если NULL (COALESCE).
	CoreTaskID string
	// Status — статус обработки: processing|ready|failed.
	Status string
	// ErrorCode — код ошибки ядра при failed; "" если NULL (COALESCE).
	ErrorCode string
	// Visibility — видимость: private|public.
	Visibility string
	// SourceKind — тип источника: audio|video|video_url.
	SourceKind string
	// S3Key — ключ объектного хранилища; "" если NULL (COALESCE).
	S3Key string
	// VideoURL — URL видео; "" если NULL (COALESCE).
	VideoURL string
	// Title — редактируемый заголовок лекции.
	Title string
	// CreatedAt — время создания (UTC).
	CreatedAt time.Time
	// UpdatedAt — время последнего обновления (UTC).
	UpdatedAt time.Time
	// PublishedAt — время публикации; nil если не опубликована.
	// Намеренно nullable (отличаем «не опубликована» от «опубликована и снята»).
	PublishedAt *time.Time
}

// LectureDB — объект доступа к таблице lectures на основе пула pgx.
type LectureDB struct {
	Pool *pgxpool.Pool
}

// CreateLectureParams — параметры создания новой лекции.
type CreateLectureParams struct {
	// OwnerID — UUID владельца.
	OwnerID string
	// Title — начальный заголовок (обычно имя файла).
	Title string
	// SourceKind — тип источника: audio|video|video_url.
	SourceKind string
	// CoreTaskID — задача ядра; пустая строка сохраняется как SQL NULL.
	CoreTaskID string
	// S3Key — ключ в объектном хранилище; может быть пустым для video_url.
	S3Key string
	// VideoURL — URL видео; может быть пустым для файловых источников.
	VideoURL string
}

// scanLectureRow сканирует строку запроса в LectureRow.
// Использует COALESCE для nullable текстовых полей, *time.Time для published_at.
func scanLectureRow(row pgx.Row) (*LectureRow, error) {
	r := &LectureRow{}
	err := row.Scan(
		&r.LectureID,
		&r.OwnerID,
		&r.CoreTaskID, // COALESCE(core_task_id, '')
		&r.Status,
		&r.ErrorCode, // COALESCE(error_code, '')
		&r.Visibility,
		&r.SourceKind,
		&r.S3Key,    // COALESCE(s3_key, '')
		&r.VideoURL, // COALESCE(video_url, '')
		&r.Title,
		&r.CreatedAt,
		&r.UpdatedAt,
		&r.PublishedAt, // *time.Time, pgx умеет сканировать nullable timestamptz
	)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// lectureSelectCols — общий список колонок для SELECT-запросов.
const lectureSelectCols = `
    lecture_id,
    owner_id,
    COALESCE(core_task_id, ''),
    status::text,
    COALESCE(error_code, ''),
    visibility::text,
    source_kind::text,
    COALESCE(s3_key, ''),
    COALESCE(video_url, ''),
    title,
    created_at,
    updated_at,
    published_at
`

// CreateLecture создаёт новую лекцию и возвращает созданную строку.
// UUID генерируется Postgres через gen_random_uuid().
// Используется внутренне и в тест-хелперах; в боевом коде — через доменный слой C1-upload.
func (db *LectureDB) CreateLecture(ctx context.Context, p CreateLectureParams) (*LectureRow, error) {
	const q = `
        INSERT INTO lectures (owner_id, title, source_kind, core_task_id, s3_key, video_url)
        VALUES ($1, $2, $3::lecture_source_kind, NULLIF($4,''), NULLIF($5,''), NULLIF($6,''))
        RETURNING ` + lectureSelectCols

	row, err := scanLectureRow(db.Pool.QueryRow(ctx, q,
		p.OwnerID, p.Title, p.SourceKind, p.CoreTaskID, p.S3Key, p.VideoURL))
	if err != nil {
		return nil, fmt.Errorf("db: CreateLecture: %w", err)
	}
	return row, nil
}

// ListByOwner возвращает лекции владельца, упорядоченные по updated_at DESC.
// Возвращает пустой срез (не nil), если лекций нет.
func (db *LectureDB) ListByOwner(ctx context.Context, ownerID string) ([]LectureRow, error) {
	const q = `
        SELECT ` + lectureSelectCols + `
        FROM lectures
        WHERE owner_id = $1
        ORDER BY updated_at DESC
    `
	rows, err := db.Pool.Query(ctx, q, ownerID)
	if err != nil {
		return nil, fmt.Errorf("db: ListByOwner: %w", err)
	}
	defer rows.Close()

	// Инициализируем пустой срез (не nil) — домен не должен получать nil
	result := make([]LectureRow, 0)
	for rows.Next() {
		r := LectureRow{}
		if err := rows.Scan(
			&r.LectureID, &r.OwnerID, &r.CoreTaskID, &r.Status,
			&r.ErrorCode, &r.Visibility, &r.SourceKind, &r.S3Key,
			&r.VideoURL, &r.Title, &r.CreatedAt, &r.UpdatedAt, &r.PublishedAt,
		); err != nil {
			return nil, fmt.Errorf("db: ListByOwner scan: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: ListByOwner rows: %w", err)
	}
	return result, nil
}

// FindByID возвращает лекцию по PK. Возвращает (nil, nil) если не найдена.
func (db *LectureDB) FindByID(ctx context.Context, lectureID string) (*LectureRow, error) {
	const q = `
        SELECT ` + lectureSelectCols + `
        FROM lectures
        WHERE lecture_id = $1
    `
	row, err := scanLectureRow(db.Pool.QueryRow(ctx, q, lectureID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("db: FindByID: %w", err)
	}
	return row, nil
}

// Rename обновляет заголовок лекции; фильтр по owner_id.
// Также обновляет updated_at = now(). Возвращает affected (0 = нет прав/не найдена).
func (db *LectureDB) Rename(ctx context.Context, lectureID, ownerID, title string) (int64, error) {
	const q = `
        UPDATE lectures
        SET title = $1, updated_at = now()
        WHERE lecture_id = $2 AND owner_id = $3
    `
	tag, err := db.Pool.Exec(ctx, q, title, lectureID, ownerID)
	if err != nil {
		return 0, fmt.Errorf("db: Rename: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SetVisibility меняет видимость лекции.
//   - public: WHERE owner_id=? AND status='ready'; SET published_at = now() (обновляем при каждой публикации).
//   - private: WHERE owner_id=?; published_at НЕ обнуляем (§8: снятие в любой момент).
//
// Возвращает affected (0 при public+не-ready → домен трактует как «нельзя опубликовать»).
func (db *LectureDB) SetVisibility(ctx context.Context, lectureID, ownerID, visibility string) (int64, error) {
	var q string
	var args []any

	if visibility == "public" {
		// Публикация только готовых лекций; обновляем published_at при каждой публикации
		q = `
            UPDATE lectures
            SET visibility = 'public', published_at = now(), updated_at = now()
            WHERE lecture_id = $1 AND owner_id = $2 AND status = 'ready'
        `
		args = []any{lectureID, ownerID}
	} else {
		// Снятие с публикации: published_at сохраняем (§8: повторная публикация перепишет)
		q = `
            UPDATE lectures
            SET visibility = 'private', updated_at = now()
            WHERE lecture_id = $1 AND owner_id = $2
        `
		args = []any{lectureID, ownerID}
	}

	tag, err := db.Pool.Exec(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("db: SetVisibility: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Delete выполняет hard-delete строки лекции; фильтр по owner_id.
// Возвращает affected (0 = не его/не найдена).
// ВАЖНО (§8): ядро дёргается ДО этого вызова, в домене (lecture.Service.Delete).
func (db *LectureDB) Delete(ctx context.Context, lectureID, ownerID string) (int64, error) {
	const q = `
        DELETE FROM lectures
        WHERE lecture_id = $1 AND owner_id = $2
    `
	tag, err := db.Pool.Exec(ctx, q, lectureID, ownerID)
	if err != nil {
		return 0, fmt.Errorf("db: Delete: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SetCoreTaskProcessing переводит лекцию failed→processing и обновляет core_task_id.
// Используется для retry: WHERE owner_id=? AND status='failed'.
// Также очищает error_code и обновляет updated_at.
// Возвращает affected (0 = статус не failed или нет прав — no-op норма).
func (db *LectureDB) SetCoreTaskProcessing(ctx context.Context, lectureID, ownerID, coreTaskID string) (int64, error) {
	const q = `
        UPDATE lectures
        SET status = 'processing',
            core_task_id = $1,
            error_code = NULL,
            updated_at = now()
        WHERE lecture_id = $2 AND owner_id = $3 AND status = 'failed'
    `
	tag, err := db.Pool.Exec(ctx, q, coreTaskID, lectureID, ownerID)
	if err != nil {
		return 0, fmt.Errorf("db: SetCoreTaskProcessing: %w", err)
	}
	return tag.RowsAffected(), nil
}

// UpdateStatusConditional — анти-гонка §7: переводит статус ТОЛЬКО из processing.
//
//	UPDATE ... SET status=?, error_code=?, updated_at=now()
//	WHERE core_task_id=? AND status='processing'
//
// Потребитель — C1-sync (вебхук/поллинг), НЕ пакет lecture.
// affected=0 — no-op (норма: дубль или уже обработан).
func (db *LectureDB) UpdateStatusConditional(ctx context.Context, coreTaskID, status, errorCode string) (int64, error) {
	const q = `
        UPDATE lectures
        SET status = $1::lecture_status,
            error_code = NULLIF($2, ''),
            updated_at = now()
        WHERE core_task_id = $3 AND status = 'processing'
    `
	tag, err := db.Pool.Exec(ctx, q, status, errorCode, coreTaskID)
	if err != nil {
		return 0, fmt.Errorf("db: UpdateStatusConditional: %w", err)
	}
	return tag.RowsAffected(), nil
}
