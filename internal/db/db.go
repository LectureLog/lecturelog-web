// Package db — слой подключения к Postgres и применения DDL-миграций платформы LectureLog.
// Не зависит от internal/config: получает DSN строкой от вызывающего кода (§9).
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
)

// migrationsFS содержит SQL-миграции, встроенные в бинарь на этапе компиляции.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsSubFS возвращает подфайловую систему только для каталога migrations/.
// Необходимо, потому что tern.FindMigrations ищет файлы в корне переданной FS.
func migrationsSubFS() (fs.FS, error) {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("db: ошибка получения подфайловой системы миграций: %w", err)
	}
	return sub, nil
}

// New создаёт пул соединений к Postgres платформы по DSN и проверяет связь ping-ом.
// DSN приходит из config.Config.PlatformDBDSN — слой db сам окружение не читает.
func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: неверный DSN подключения к Postgres: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: ошибка создания пула соединений: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: Postgres недоступен (ping): %w", err)
	}

	return pool, nil
}

// Migrate применяет все недостающие миграции «вверх» из встроенной FS.
// Идемпотентна: повторный вызов при актуальной схеме не делает ничего.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// Берём одно соединение из пула для migrator — tern требует *pgx.Conn.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("db: ошибка получения соединения из пула для миграций: %w", err)
	}
	defer conn.Release()

	migrator, err := migrate.NewMigrator(ctx, conn.Conn(), "public.schema_version")
	if err != nil {
		return fmt.Errorf("db: ошибка инициализации migrator: %w", err)
	}

	subFS, err := migrationsSubFS()
	if err != nil {
		return err
	}

	if err := migrator.LoadMigrations(subFS); err != nil {
		return fmt.Errorf("db: ошибка загрузки миграций: %w", err)
	}

	if err := migrator.Migrate(ctx); err != nil {
		return fmt.Errorf("db: ошибка применения миграций: %w", err)
	}

	return nil
}
