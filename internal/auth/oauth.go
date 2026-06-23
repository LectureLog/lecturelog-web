package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

// googleProvider реализует OAuthProvider для Google OAuth 2.0.
// Endpoint и userinfoURL инъектируются → в тестах подменяем на httptest-серверы.
// Реальный Google не дёргается в юнит/интеграционных тестах.
type googleProvider struct {
	cfg         *oauth2.Config
	userinfoURL string
}

// NewGoogleProvider создаёт OAuthProvider для Google.
// В prod передаются реальные endpoint и "https://www.googleapis.com/oauth2/v3/userinfo".
// В тестах — адреса httptest-серверов.
func NewGoogleProvider(cfg *oauth2.Config, userinfoURL string) OAuthProvider {
	return &googleProvider{cfg: cfg, userinfoURL: userinfoURL}
}

// AuthCodeURL формирует URL редиректа на страницу входа Google.
// access_type=offline запрашивает refresh_token (для долгоживущих сессий).
func (p *googleProvider) AuthCodeURL(state string) string {
	return p.cfg.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Exchange обменивает код авторизации на профиль пользователя.
// Шаги: (1) обмен кода на токен через x/oauth2; (2) GET userinfo endpoint с access_token.
// Возвращает Profile с EmailVerified — вызывающий ОБЯЗАН проверить его перед созданием сессии.
// Секреты (access_token, код) НЕ логируются.
func (p *googleProvider) Exchange(ctx context.Context, code string) (Profile, error) {
	// Шаг 1: обмен кода на токен (секрет не логируем)
	token, err := p.cfg.Exchange(ctx, code)
	if err != nil {
		return Profile{}, fmt.Errorf("auth: обмен кода на токен: %w", err)
	}

	// Шаг 2: запрос userinfo endpoint с Bearer-токеном
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userinfoURL, nil)
	if err != nil {
		return Profile{}, fmt.Errorf("auth: создание запроса userinfo: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	// Используем HTTP-клиент из oauth2 (поддерживает refresh при необходимости)
	client := p.cfg.Client(ctx, token)
	resp, err := client.Do(req)
	if err != nil {
		return Profile{}, fmt.Errorf("auth: запрос userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Profile{}, fmt.Errorf("auth: userinfo вернул %d: %s", resp.StatusCode, body)
	}

	// Шаг 3: декодируем JSON ответ userinfo
	var raw struct {
		Sub           string      `json:"sub"`
		Email         string      `json:"email"`
		EmailVerified interface{} `json:"email_verified"` // bool или строка (defensive §design)
		Name          string      `json:"name"`
		Picture       string      `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Profile{}, fmt.Errorf("auth: декодирование userinfo: %w", err)
	}

	// Defensive парсинг email_verified: может быть bool или строка "true"/"false"
	emailVerified := parseEmailVerified(raw.EmailVerified)

	return Profile{
		Provider:      "google",
		ProviderSub:   raw.Sub,
		Email:         raw.Email,
		EmailVerified: emailVerified,
		Name:          raw.Name,
		AvatarURL:     raw.Picture,
	}, nil
}

// parseEmailVerified разбирает email_verified из userinfo defensively:
// поддерживает bool и строку "true" (некоторые провайдеры возвращают строку).
func parseEmailVerified(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	// JSON-декодер может вернуть float64 для чисел: 1 → true
	case float64:
		return val != 0
	default:
		return false
	}
}
