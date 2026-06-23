package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRow — строка таблицы sessions (без доменной логики auth).
type SessionRow struct {
	// SessionID — UUID сессии (PK), передаётся в куке.
	SessionID string
	// UserID — UUID владельца сессии (FK → users).
	UserID string
	// ExpiresAt — время истечения сессии (UTC).
	ExpiresAt time.Time
}

// SessionDB — объект доступа к sessions на основе пула pgx.
type SessionDB struct {
	Pool *pgxpool.Pool
}

// CreateSession создаёт новую сессию для пользователя с заданным временем истечения.
// UUID генерируется Postgres через gen_random_uuid().
func (db *SessionDB) CreateSession(ctx context.Context, userID string, expiresAt time.Time) (*SessionRow, error) {
	const q = `
		INSERT INTO sessions (user_id, expires_at)
		VALUES ($1, $2)
		RETURNING session_id, user_id, expires_at
	`
	row := &SessionRow{}
	err := db.Pool.QueryRow(ctx, q, userID, expiresAt).Scan(
		&row.SessionID, &row.UserID, &row.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("db: CreateSession: %w", err)
	}
	return row, nil
}

// GetSession возвращает активную (не протухшую) сессию по её ID.
// Возвращает (nil, nil) если сессия не найдена или истекла (expires_at > now()).
// Фильтрация протухших выполняется на стороне БД — защита от устаревших кук.
func (db *SessionDB) GetSession(ctx context.Context, sessionID string) (*SessionRow, error) {
	const q = `
		SELECT session_id, user_id, expires_at
		FROM sessions
		WHERE session_id = $1
		  AND expires_at > now()
	`
	row := &SessionRow{}
	err := db.Pool.QueryRow(ctx, q, sessionID).Scan(
		&row.SessionID, &row.UserID, &row.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("db: GetSession: %w", err)
	}
	return row, nil
}

// DeleteSession удаляет сессию по её ID.
// Идемпотентен: удаление несуществующей сессии не является ошибкой.
func (db *SessionDB) DeleteSession(ctx context.Context, sessionID string) error {
	const q = `DELETE FROM sessions WHERE session_id = $1`
	_, err := db.Pool.Exec(ctx, q, sessionID)
	if err != nil {
		return fmt.Errorf("db: DeleteSession: %w", err)
	}
	return nil
}
