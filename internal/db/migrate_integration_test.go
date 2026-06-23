//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestMigrateIntegration запускает контейнер Postgres, применяет миграции
// и проверяет структуру схемы: таблицы, индексы, перечисления.
// Запуск: go test -tags=integration ./internal/db/...
func TestMigrateIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Запуск Postgres в Docker через testcontainers. При отсутствии демона пропускаем тест.
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
		// Не можем поднять контейнер — пропускаем (инфраструктура недоступна)
		t.Skipf("testcontainers: не удалось запустить Postgres: %v", err)
	}
	defer func() {
		if terr := testcontainers.TerminateContainer(pgContainer); terr != nil {
			t.Logf("TerminateContainer: %v", terr)
		}
	}()

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}

	// Создаём пул через наш New
	pool, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Close()

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
