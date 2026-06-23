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

// setupTestDB запускает Postgres в Docker и возвращает подготовленный пул с миграциями.
// Пропускает тест, если Docker недоступен.
func setupTestDB(t *testing.T) (context.Context, func()) {
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

	// Сохраняем пул в тестовой переменной через closure
	t.Cleanup(func() {
		pool.Close()
		if terr := testcontainers.TerminateContainer(pgContainer); terr != nil {
			t.Logf("TerminateContainer: %v", terr)
		}
	})

	return ctx, func() {}
}

// setupTestDBPool возвращает контекст и пул — удобная обёртка поверх setupTestDB.
func setupTestDBPool(t *testing.T) (context.Context, *UserDB) {
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

	return ctx, &UserDB{Pool: pool}
}

// TestCreateUser_FindByEmail проверяет создание пользователя и поиск по email.
func TestCreateUser_FindByEmail(t *testing.T) {
	ctx, userDB := setupTestDBPool(t)

	// Создаём пользователя
	row, err := userDB.CreateUser(ctx, "test@example.com", "Тест Тестовый", "https://example.com/avatar.jpg")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if row.UserID == "" {
		t.Fatal("CreateUser: пустой UserID")
	}
	if row.Email != "test@example.com" {
		t.Errorf("Email = %q, ожидается %q", row.Email, "test@example.com")
	}
	if row.Name != "Тест Тестовый" {
		t.Errorf("Name = %q, ожидается %q", row.Name, "Тест Тестовый")
	}

	// Ищем по email — должны найти
	found, err := userDB.FindUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail (найдено): %v", err)
	}
	if found == nil {
		t.Fatal("FindUserByEmail вернул nil для существующего пользователя")
	}
	if found.UserID != row.UserID {
		t.Errorf("UserID не совпадает: %q != %q", found.UserID, row.UserID)
	}

	// Ищем по несуществующему email — должны получить nil, nil
	missing, err := userDB.FindUserByEmail(ctx, "nobody@example.com")
	if err != nil {
		t.Fatalf("FindUserByEmail (не найдено): %v", err)
	}
	if missing != nil {
		t.Fatalf("FindUserByEmail должен вернуть nil для несуществующего email, вернул %+v", missing)
	}
}

// TestUpsertIdentity_Idempotent проверяет идемпотентность UpsertIdentity.
func TestUpsertIdentity_Idempotent(t *testing.T) {
	ctx, userDB := setupTestDBPool(t)

	row, err := userDB.CreateUser(ctx, "identity@example.com", "Пользователь", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Первый upsert — создаёт запись
	if err := userDB.UpsertIdentity(ctx, "google", "google-sub-123", row.UserID); err != nil {
		t.Fatalf("UpsertIdentity (первый): %v", err)
	}

	// Второй upsert с теми же данными — идемпотентен, не должен падать
	if err := userDB.UpsertIdentity(ctx, "google", "google-sub-123", row.UserID); err != nil {
		t.Fatalf("UpsertIdentity (повторный): %v", err)
	}

	t.Log("UpsertIdentity идемпотентна — OK")
}
