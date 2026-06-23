// Package db — слой доступа к данным пользователей и идентичностей платформы LectureLog.
// Типы UserRow/SessionRow принадлежат пакету db; пакет auth не импортируется отсюда.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRow — строка таблицы users (без доменной логики auth).
type UserRow struct {
	// UserID — UUID пользователя (PK).
	UserID string
	// Email — канонический email, матч входа по нему (политика §3).
	Email string
	// Name — отображаемое имя из OAuth.
	Name string
	// AvatarURL — URL аватара из OAuth.
	AvatarURL string
}

// UserDB — объект доступа к users/identities на основе пула pgx.
type UserDB struct {
	Pool *pgxpool.Pool
}

// FindUserByEmail ищет пользователя по email.
// Возвращает (nil, nil) если пользователь не найден.
func (db *UserDB) FindUserByEmail(ctx context.Context, email string) (*UserRow, error) {
	const q = `
		SELECT user_id, email, COALESCE(name, ''), COALESCE(avatar_url, '')
		FROM users
		WHERE email = $1
	`
	row := &UserRow{}
	err := db.Pool.QueryRow(ctx, q, email).Scan(&row.UserID, &row.Email, &row.Name, &row.AvatarURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("db: FindUserByEmail: %w", err)
	}
	return row, nil
}

// FindUserByID ищет пользователя по UUID.
// Возвращает (nil, nil) если пользователь не найден.
func (db *UserDB) FindUserByID(ctx context.Context, userID string) (*UserRow, error) {
	const q = `
		SELECT user_id, email, COALESCE(name, ''), COALESCE(avatar_url, '')
		FROM users
		WHERE user_id = $1
	`
	row := &UserRow{}
	err := db.Pool.QueryRow(ctx, q, userID).Scan(&row.UserID, &row.Email, &row.Name, &row.AvatarURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("db: FindUserByID: %w", err)
	}
	return row, nil
}

// CreateUser создаёт нового пользователя и возвращает созданную строку.
// UUID генерируется Postgres через gen_random_uuid().
func (db *UserDB) CreateUser(ctx context.Context, email, name, avatarURL string) (*UserRow, error) {
	const q = `
		INSERT INTO users (email, name, avatar_url)
		VALUES ($1, $2, $3)
		RETURNING user_id, email, COALESCE(name, ''), COALESCE(avatar_url, '')
	`
	row := &UserRow{}
	err := db.Pool.QueryRow(ctx, q, email, name, avatarURL).Scan(
		&row.UserID, &row.Email, &row.Name, &row.AvatarURL,
	)
	if err != nil {
		return nil, fmt.Errorf("db: CreateUser: %w", err)
	}
	return row, nil
}

// UpsertIdentity создаёт или обновляет связь провайдер↔пользователь.
// Идемпотентна: повторный вызов с теми же (provider, providerSub) не падает.
// Email не хранится в identities (политика §3).
func (db *UserDB) UpsertIdentity(ctx context.Context, provider, providerSub, userID string) error {
	const q = `
		INSERT INTO identities (provider, provider_sub, user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (provider, provider_sub) DO NOTHING
	`
	_, err := db.Pool.Exec(ctx, q, provider, providerSub, userID)
	if err != nil {
		return fmt.Errorf("db: UpsertIdentity: %w", err)
	}
	return nil
}
