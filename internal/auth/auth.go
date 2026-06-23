// Package auth — доменный модуль входа через Google OAuth 2.0, серверных сессий,
// CSRF/state и middleware прав платформы LectureLog.
//
// Архитектура: доменная логика отделена от HTTP и БД через интерфейсы Repository и OAuthProvider.
// Это обеспечивает юнит-тесты без Postgres и без реальных запросов к Google.
package auth

import (
	"context"
	"net/http"
	"time"
)

// Profile — данные профиля пользователя, полученные от OAuth-провайдера.
// Заполняется из ответа userinfo endpoint (§design: OAuth-библиотека).
type Profile struct {
	// Provider — имя провайдера, напр. "google".
	Provider string
	// ProviderSub — стабильный ID пользователя у провайдера (sub из userinfo).
	ProviderSub string
	// Email — email из OAuth.
	Email string
	// EmailVerified — подтверждён ли email провайдером.
	// false → вход ЗАПРЕЩЁН (политика §3, анти-takeover).
	EmailVerified bool
	// Name — отображаемое имя.
	Name string
	// AvatarURL — URL аватара.
	AvatarURL string
}

// User — доменный пользователь платформы.
type User struct {
	// ID — UUID пользователя (из таблицы users).
	ID string
	// Email — канонический email (матч входа по нему, политика §3).
	Email string
	// Name — отображаемое имя.
	Name string
	// AvatarURL — URL аватара.
	AvatarURL string
}

// Session — серверная сессия пользователя (хранится в Postgres).
type Session struct {
	// ID — UUID сессии, передаётся в HttpOnly-куке.
	// НЕ логировать (секрет).
	ID string
	// UserID — UUID владельца сессии.
	UserID string
	// ExpiresAt — время истечения сессии (UTC).
	ExpiresAt time.Time
}

// Repository — узкий интерфейс данных для доменной логики auth.
// Потребитель (auth) владеет интерфейсом → адаптер над internal/db реализуется в cmd/server.
// Это гарантирует мокабельность: юнит-тесты используют mock, не pgx.
type Repository interface {
	// FindUserByEmail ищет пользователя по email.
	// Возвращает (nil, nil) если пользователь не найден.
	FindUserByEmail(ctx context.Context, email string) (*User, error)

	// FindUserByID ищет пользователя по UUID.
	// Возвращает (nil, nil) если пользователь не найден.
	FindUserByID(ctx context.Context, userID string) (*User, error)

	// CreateUser создаёт нового пользователя из профиля OAuth.
	CreateUser(ctx context.Context, p Profile) (*User, error)

	// UpsertIdentity создаёт или обновляет связь провайдер↔пользователь.
	// Идемпотентна (ON CONFLICT DO NOTHING).
	UpsertIdentity(ctx context.Context, provider, providerSub, userID string) error

	// CreateSession создаёт новую серверную сессию.
	CreateSession(ctx context.Context, userID string, expiresAt time.Time) (*Session, error)

	// GetSession возвращает активную (не протухшую) сессию.
	// Возвращает (nil, nil) если сессия не найдена или истекла.
	GetSession(ctx context.Context, sessionID string) (*Session, error)

	// DeleteSession удаляет сессию. Идемпотентен.
	DeleteSession(ctx context.Context, sessionID string) error
}

// OAuthProvider — интерфейс OAuth-провайдера (Google).
// Endpoint и userinfoURL инъектируются → в тестах подменяем на httptest.
type OAuthProvider interface {
	// AuthCodeURL формирует URL редиректа на страницу входа провайдера.
	AuthCodeURL(state string) string

	// Exchange обменивает код авторизации на профиль пользователя.
	// Выполняет обмен кода на токен + запрос к userinfo endpoint.
	// Возвращает профиль с EmailVerified — вызывающий код ОБЯЗАН проверить его.
	Exchange(ctx context.Context, code string) (Profile, error)
}

// Service — доменный сервис auth. Содержит бизнес-логику входа, сессий и middleware.
// Создаётся через NewService в cmd/server с реальными зависимостями.
type Service struct {
	repo       Repository
	provider   OAuthProvider
	sessionTTL time.Duration
	// secure — флаг Secure для кук (true в prod, false для локального http).
	secure bool
	// now — инъектируемые часы для тестируемости.
	now func() time.Time
}

// NewService создаёт Service с зависимостями.
// secure=true → куки с флагом Secure (для HTTPS prod-окружения).
func NewService(repo Repository, provider OAuthProvider, sessionTTL time.Duration, secure bool) *Service {
	return &Service{
		repo:       repo,
		provider:   provider,
		sessionTTL: sessionTTL,
		secure:     secure,
		now:        time.Now,
	}
}

// contextKey — тип для ключей контекста (избегаем конфликтов с другими пакетами).
type contextKey string

// ctxKeyUser — ключ для хранения *User в контексте запроса.
const ctxKeyUser contextKey = "auth_user"

// UserFromContext извлекает авторизованного пользователя из контекста.
// Возвращает nil если пользователь не аутентифицирован (анонимный запрос).
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKeyUser).(*User)
	return u
}

// withUser кладёт пользователя в контекст запроса.
func withUser(r *http.Request, u *User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeyUser, u))
}
