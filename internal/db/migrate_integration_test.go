//go:build integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func newTestPostgresPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

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
	t.Cleanup(func() {
		if terr := testcontainers.TerminateContainer(pgContainer); terr != nil {
			t.Logf("TerminateContainer: %v", terr)
		}
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}

	pool, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func migrateToVersionForTest(ctx context.Context, t *testing.T, pool *pgxpool.Pool, version int32) {
	t.Helper()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()

	migrator, err := migrate.NewMigrator(ctx, conn.Conn(), "public.schema_version")
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}

	subFS, err := migrationsSubFS()
	if err != nil {
		t.Fatalf("migrationsSubFS: %v", err)
	}
	if err := migrator.LoadMigrations(subFS); err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if err := migrator.MigrateTo(ctx, version); err != nil {
		t.Fatalf("MigrateTo(%d): %v", version, err)
	}
}

// TestMigrateIntegration запускает контейнер Postgres, применяет миграции
// и проверяет структуру схемы: таблицы, индексы, перечисления.
// Запуск: go test -tags=integration ./internal/db/...
func TestMigrateIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool := newTestPostgresPool(t, ctx)
	var err error

	// Применяем миграции
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate (первый вызов): %v", err)
	}

	// Проверяем наличие таблицы lectures
	var oid interface{}
	err = pool.QueryRow(ctx, "SELECT to_regclass('public.lectures')").Scan(&oid)
	if err != nil {
		t.Fatalf("to_regclass('public.lectures'): %v", err)
	}
	if oid == nil {
		t.Fatal("таблица lectures не создана после Migrate")
	}

	// Проверяем наличие таблицы users
	err = pool.QueryRow(ctx, "SELECT to_regclass('public.users')").Scan(&oid)
	if err != nil {
		t.Fatalf("to_regclass('public.users'): %v", err)
	}
	if oid == nil {
		t.Fatal("таблица users не создана после Migrate")
	}

	// Проверяем наличие таблицы identities
	err = pool.QueryRow(ctx, "SELECT to_regclass('public.identities')").Scan(&oid)
	if err != nil {
		t.Fatalf("to_regclass('public.identities'): %v", err)
	}
	if oid == nil {
		t.Fatal("таблица identities не создана после Migrate")
	}

	// Проверяем наличие таблицы sessions
	err = pool.QueryRow(ctx, "SELECT to_regclass('public.sessions')").Scan(&oid)
	if err != nil {
		t.Fatalf("to_regclass('public.sessions'): %v", err)
	}
	if oid == nil {
		t.Fatal("таблица sessions не создана после Migrate")
	}

	// Проверяем частичный индекс idx_lectures_core_task_id
	var indexExists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename  = 'lectures'
			  AND indexname  = 'idx_lectures_core_task_id'
		)`,
	).Scan(&indexExists)
	if err != nil {
		t.Fatalf("запрос pg_indexes: %v", err)
	}
	if !indexExists {
		t.Fatal("индекс idx_lectures_core_task_id не найден в pg_indexes")
	}

	// Проверяем частичный индекс для витрины публичных лекций.
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename = 'lectures'
			  AND indexname = 'idx_lectures_public_published_at'
		)`,
	).Scan(&indexExists)
	if err != nil {
		t.Fatalf("запрос pg_indexes для хаба: %v", err)
	}
	if !indexExists {
		t.Fatal("индекс idx_lectures_public_published_at не найден в pg_indexes")
	}

	// Проверяем наличие enum lecture_status
	var enumExists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_type
			WHERE typname = 'lecture_status'
			  AND typtype = 'e'
		)`,
	).Scan(&enumExists)
	if err != nil {
		t.Fatalf("запрос pg_type для lecture_status: %v", err)
	}
	if !enumExists {
		t.Fatal("enum lecture_status не найден в pg_type")
	}

	// Проверяем наличие enum lecture_visibility
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_type
			WHERE typname = 'lecture_visibility'
			  AND typtype = 'e'
		)`,
	).Scan(&enumExists)
	if err != nil {
		t.Fatalf("запрос pg_type для lecture_visibility: %v", err)
	}
	if !enumExists {
		t.Fatal("enum lecture_visibility не найден в pg_type")
	}

	// Проверяем наличие enum lecture_source_kind
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_type
			WHERE typname = 'lecture_source_kind'
			  AND typtype = 'e'
		)`,
	).Scan(&enumExists)
	if err != nil {
		t.Fatalf("запрос pg_type для lecture_source_kind: %v", err)
	}
	if !enumExists {
		t.Fatal("enum lecture_source_kind не найден в pg_type")
	}

	// Проверяем идемпотентность: повторный вызов Migrate не должен падать
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate (повторный вызов — идемпотентность): %v", err)
	}

	t.Log("Все проверки пройдены: таблицы, индекс, перечисления на месте; Migrate идемпотентна")
}

func TestMigrateIntegration_NormalizesExistingUserEmails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool := newTestPostgresPool(t, ctx)

	migrateToVersionForTest(ctx, t, pool, 3)

	_, err := pool.Exec(ctx, `
		INSERT INTO users (email, name)
		VALUES (' Admin@Example.COM ', 'Admin')
	`)
	if err != nil {
		t.Fatalf("insert dirty user: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var email string
	err = pool.QueryRow(ctx, `SELECT email FROM users WHERE name = 'Admin'`).Scan(&email)
	if err != nil {
		t.Fatalf("select email: %v", err)
	}
	if email != "admin@example.com" {
		t.Fatalf("email = %q, ожидается admin@example.com", email)
	}
}

func TestMigrateIntegration_FailsOnDuplicateCanonicalEmails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool := newTestPostgresPool(t, ctx)

	migrateToVersionForTest(ctx, t, pool, 3)

	_, err := pool.Exec(ctx, `
		INSERT INTO users (email, name)
		VALUES ('Admin@Example.COM', 'Admin 1'), (' admin@example.com ', 'Admin 2')
	`)
	if err != nil {
		t.Fatalf("insert duplicate users: %v", err)
	}

	err = Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate должен упасть на дублях после canonicalization")
	}
	if !strings.Contains(err.Error(), "duplicate users.email after canonicalization") {
		t.Fatalf("ошибка = %v, ожидается понятное сообщение про дубли", err)
	}
}
