// Пакет server — точка входа платформы LectureLog (web-хаб).
// Загружает конфигурацию, подключается к Postgres, инициализирует Auth-сервис
// и запускает HTTP-сервер с chi-роутером.
package main

import (
	"context"
	"crypto/rand"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/csrf"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/config"
	"github.com/LectureLog/lecturelog-web/internal/db"
	"github.com/LectureLog/lecturelog-web/internal/web"

	"github.com/go-chi/chi/v5"
)

// dbAdapter реализует auth.Repository поверх db.UserDB и db.SessionDB.
// db не знает про auth (нет импорта auth→db), auth не знает про pgx (нет импорта db→pgx).
// Связка происходит здесь, в cmd/server.
type dbAdapter struct {
	users    *db.UserDB
	sessions *db.SessionDB
}

func (a *dbAdapter) FindUserByEmail(ctx context.Context, email string) (*auth.User, error) {
	row, err := a.users.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &auth.User{
		ID:        row.UserID,
		Email:     row.Email,
		Name:      row.Name,
		AvatarURL: row.AvatarURL,
	}, nil
}

func (a *dbAdapter) FindUserByID(ctx context.Context, userID string) (*auth.User, error) {
	row, err := a.users.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &auth.User{
		ID:        row.UserID,
		Email:     row.Email,
		Name:      row.Name,
		AvatarURL: row.AvatarURL,
	}, nil
}

func (a *dbAdapter) CreateUser(ctx context.Context, p auth.Profile) (*auth.User, error) {
	row, err := a.users.CreateUser(ctx, p.Email, p.Name, p.AvatarURL)
	if err != nil {
		return nil, err
	}
	return &auth.User{
		ID:        row.UserID,
		Email:     row.Email,
		Name:      row.Name,
		AvatarURL: row.AvatarURL,
	}, nil
}

func (a *dbAdapter) UpsertIdentity(ctx context.Context, provider, providerSub, userID string) error {
	return a.users.UpsertIdentity(ctx, provider, providerSub, userID)
}

func (a *dbAdapter) CreateSession(ctx context.Context, userID string, expiresAt time.Time) (*auth.Session, error) {
	row, err := a.sessions.CreateSession(ctx, userID, expiresAt)
	if err != nil {
		return nil, err
	}
	return &auth.Session{
		ID:        row.SessionID,
		UserID:    row.UserID,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (a *dbAdapter) GetSession(ctx context.Context, sessionID string) (*auth.Session, error) {
	row, err := a.sessions.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &auth.Session{
		ID:        row.SessionID,
		UserID:    row.UserID,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (a *dbAdapter) DeleteSession(ctx context.Context, sessionID string) error {
	return a.sessions.DeleteSession(ctx, sessionID)
}

func main() {
	ctx := context.Background()

	// Загружаем конфигурацию из переменных окружения
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("config.Load: %v", err)
	}

	// Подключаемся к Postgres и применяем миграции
	pool, err := db.New(ctx, cfg.PlatformDBDSN)
	if err != nil {
		log.Fatalf("db.New: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("db.Migrate: %v", err)
	}

	// Инициализируем адаптер репозитория
	repo := &dbAdapter{
		users:    &db.UserDB{Pool: pool},
		sessions: &db.SessionDB{Pool: pool},
	}

	// Определяем, работаем ли в prod-режиме (куки Secure)
	// В prod рекомендуется передать PLATFORM_SECURE=true через env.
	// По умолчанию false (локальная разработка по http).
	secureCookies := os.Getenv("PLATFORM_SECURE") == "true"

	// Настраиваем Google OAuth 2.0
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.OAuth.ClientID,
		ClientSecret: cfg.OAuth.ClientSecret,
		RedirectURL:  cfg.OAuth.CallbackURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
	// Реальный Google userinfo endpoint
	provider := auth.NewGoogleProvider(oauthCfg, "https://www.googleapis.com/oauth2/v3/userinfo")

	// Создаём auth.Service
	authSvc := auth.NewService(repo, provider, cfg.SessionTTL, secureCookies)

	// CSRF-ключ генерируется на старте (32 случайных байта).
	// ДОЛГ: для прод-стабильности вынести в env (PLATFORM_CSRF_KEY) — рестарт инвалидирует токены.
	// Решение оркестратора: internal/config НЕ модифицируем, беты хватает.
	csrfKey := make([]byte, 32)
	if _, err := rand.Read(csrfKey); err != nil {
		log.Fatalf("генерация CSRF-ключа: %v", err)
	}

	// gorilla/csrf middleware: токен из контекста (csrf.Token(r)) → templ-формы через hx-headers.
	// X-CSRF-Token — заголовок для htmx (hx-headers={"X-CSRF-Token": "..."}).
	csrfMiddleware := csrf.Protect(
		csrfKey,
		csrf.Secure(secureCookies),
		csrf.RequestHeader("X-CSRF-Token"),
	)

	// Собираем chi-роутер с auth-middleware и маршрутами
	handler := web.NewRouter(
		web.WithGlobalMiddleware(
			authSvc.LoadSession, // читает сессию → *User в контекст
			csrfMiddleware,      // CSRF-защита мутирующих маршрутов
		),
		web.WithMount(func(r chi.Router) {
			authSvc.Mount(r) // GET /auth/login, GET /auth/callback, POST /auth/logout
		}),
	)

	addr := envOr("PLATFORM_ADDR", ":8080")
	log.Printf("LectureLog web запускается на %s (secure=%v)", addr, secureCookies)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("ListenAndServe: %v", err)
	}
}

// envOr возвращает значение переменной окружения key или def если переменная не задана.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// _ — проверка на этапе компиляции: dbAdapter реализует auth.Repository.
var _ auth.Repository = (*dbAdapter)(nil)

