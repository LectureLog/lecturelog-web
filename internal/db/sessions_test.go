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

// setupSessionDB возвращает контекст и оба DB-объекта для сессионных тестов.
func setupSessionDB(t *testing.T) (context.Context, *UserDB, *SessionDB) {
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

	return ctx, &UserDB{Pool: pool}, &SessionDB{Pool: pool}
}

// TestCreateGetDeleteSession проверяет полный цикл сессии: создание, чтение, удаление.
func TestCreateGetDeleteSession(t *testing.T) {
	ctx, userDB, sessDB := setupSessionDB(t)

	// Сначала создаём пользователя (sessions.user_id FK)
	user, err := userDB.CreateUser(ctx, "session@example.com", "Сессионный", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	expiresAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	sess, err := sessDB.CreateSession(ctx, user.UserID, expiresAt)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.SessionID == "" {
		t.Fatal("CreateSession: пустой SessionID")
	}
	if sess.UserID != user.UserID {
		t.Errorf("UserID = %q, ожидается %q", sess.UserID, user.UserID)
	}

	// GetSession — должны найти активную сессию
	got, err := sessDB.GetSession(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got == nil {
		t.Fatal("GetSession вернул nil для активной сессии")
	}
	if got.SessionID != sess.SessionID {
		t.Errorf("SessionID не совпадает: %q != %q", got.SessionID, sess.SessionID)
	}

	// DeleteSession
	if err := sessDB.DeleteSession(ctx, sess.SessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	// После удаления GetSession должен вернуть nil, nil
	deleted, err := sessDB.GetSession(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("GetSession после Delete: %v", err)
	}
	if deleted != nil {
		t.Fatal("GetSession должен вернуть nil после удаления сессии")
	}
}

// TestGetSession_Expired проверяет, что протухшая сессия не возвращается GetSession.
func TestGetSession_Expired(t *testing.T) {
	ctx, userDB, sessDB := setupSessionDB(t)

	user, err := userDB.CreateUser(ctx, "expired@example.com", "Устаревший", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Создаём сессию с expires_at в прошлом
	expiresAt := time.Now().Add(-1 * time.Hour).UTC()
	sess, err := sessDB.CreateSession(ctx, user.UserID, expiresAt)
	if err != nil {
		t.Fatalf("CreateSession (протухшая): %v", err)
	}

	// GetSession должен вернуть nil (expires_at > now() не выполнено)
	got, err := sessDB.GetSession(ctx, sess.SessionID)
	if err != nil {
		t.Fatalf("GetSession (протухшая): %v", err)
	}
	if got != nil {
		t.Fatalf("GetSession должен вернуть nil для протухшей сессии, вернул %+v", got)
	}
}
