package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
)

// fakeTokenServer поднимает httptest-сервер, имитирующий OAuth token endpoint.
// Возвращает сервер и его URL.
func fakeTokenServer(t *testing.T, accessToken string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Минимальный ответ token endpoint
		resp := map[string]interface{}{
			"access_token": accessToken,
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// fakeUserInfoServer поднимает httptest-сервер, имитирующий Google userinfo endpoint.
func fakeUserInfoServer(t *testing.T, profile map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Проверяем Bearer токен в заголовке
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "нет токена", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(profile)
	}))
}

// TestGoogleProvider_Exchange_OK проверяет успешный обмен кода на профиль.
func TestGoogleProvider_Exchange_OK(t *testing.T) {
	accessToken := "test-access-token"
	tokenSrv := fakeTokenServer(t, accessToken)
	defer tokenSrv.Close()

	userInfo := map[string]interface{}{
		"sub":            "google-sub-123",
		"email":          "user@example.com",
		"email_verified": true,
		"name":           "Тест Пользователь",
		"picture":        "https://example.com/avatar.jpg",
	}
	userInfoSrv := fakeUserInfoServer(t, userInfo)
	defer userInfoSrv.Close()

	// Создаём провайдера с инъектированным endpoint и URL userinfo
	cfg := &oauth2.Config{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:  tokenSrv.URL + "/auth",
			TokenURL: tokenSrv.URL + "/token",
		},
		RedirectURL: "http://localhost/callback",
	}
	provider := &googleProvider{
		cfg:         cfg,
		userinfoURL: userInfoSrv.URL,
	}

	profile, err := provider.Exchange(context.Background(), "fake-code")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if profile.Email != "user@example.com" {
		t.Errorf("Email = %q, ожидается %q", profile.Email, "user@example.com")
	}
	if !profile.EmailVerified {
		t.Error("EmailVerified должен быть true")
	}
	if profile.ProviderSub != "google-sub-123" {
		t.Errorf("ProviderSub = %q, ожидается %q", profile.ProviderSub, "google-sub-123")
	}
	if profile.Provider != "google" {
		t.Errorf("Provider = %q, ожидается %q", profile.Provider, "google")
	}
	if profile.Name != "Тест Пользователь" {
		t.Errorf("Name = %q, ожидается %q", profile.Name, "Тест Пользователь")
	}
}

// TestGoogleProvider_Exchange_EmailVerifiedString проверяет, что email_verified
// как строка "true" тоже корректно парсится (defensive parsing, §design).
func TestGoogleProvider_Exchange_EmailVerifiedString(t *testing.T) {
	tokenSrv := fakeTokenServer(t, "token-xyz")
	defer tokenSrv.Close()

	userInfo := map[string]interface{}{
		"sub":            "sub-456",
		"email":          "str@example.com",
		"email_verified": "true", // строка вместо bool
		"name":           "Строка",
	}
	userInfoSrv := fakeUserInfoServer(t, userInfo)
	defer userInfoSrv.Close()

	cfg := &oauth2.Config{
		ClientID:     "id",
		ClientSecret: "secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:  tokenSrv.URL + "/auth",
			TokenURL: tokenSrv.URL + "/token",
		},
	}
	provider := &googleProvider{cfg: cfg, userinfoURL: userInfoSrv.URL}

	profile, err := provider.Exchange(context.Background(), "code")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !profile.EmailVerified {
		t.Error("EmailVerified должен быть true при строке 'true'")
	}
}

// TestGoogleProvider_Exchange_UserInfoError проверяет, что ошибка userinfo → ошибка Exchange.
func TestGoogleProvider_Exchange_UserInfoError(t *testing.T) {
	tokenSrv := fakeTokenServer(t, "token-ok")
	defer tokenSrv.Close()

	// Сервер userinfo возвращает 500
	brokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "сервер сломан", http.StatusInternalServerError)
	}))
	defer brokenSrv.Close()

	cfg := &oauth2.Config{
		ClientID:     "id",
		ClientSecret: "secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:  tokenSrv.URL + "/auth",
			TokenURL: tokenSrv.URL + "/token",
		},
	}
	provider := &googleProvider{cfg: cfg, userinfoURL: brokenSrv.URL}

	_, err := provider.Exchange(context.Background(), "code")
	if err == nil {
		t.Fatal("Exchange должен вернуть ошибку при неудачном userinfo")
	}
	t.Logf("Ожидаемая ошибка: %v", err)
}

// TestGoogleProvider_AuthCodeURL проверяет формирование URL авторизации.
func TestGoogleProvider_AuthCodeURL(t *testing.T) {
	cfg := &oauth2.Config{
		ClientID: "test-id",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
	}
	provider := &googleProvider{cfg: cfg, userinfoURL: "unused"}
	url := provider.AuthCodeURL("test-state")
	if url == "" {
		t.Fatal("AuthCodeURL вернул пустую строку")
	}
	// URL должен содержать state
	if !containsParam(url, "state", "test-state") {
		t.Errorf("AuthCodeURL не содержит state=test-state: %s", url)
	}
}

// containsParam — вспомогательная проверка наличия query-параметра.
func containsParam(rawURL, key, value string) bool {
	return fmt.Sprintf("%s=%s", key, value) != "" &&
		len(rawURL) > 0 &&
		(contains(rawURL, key+"="+value))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findStr(s, sub))
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
