package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// mockOAuthProvider — мок OAuthProvider для тестов хендлеров.
type mockOAuthProvider struct {
	authCodeURL    string
	exchangeResult Profile
	exchangeErr    error
}

func (m *mockOAuthProvider) AuthCodeURL(state string) string {
	if m.authCodeURL != "" {
		return m.authCodeURL + "?state=" + state
	}
	return "https://example.com/auth?state=" + state
}

func (m *mockOAuthProvider) Exchange(_ context.Context, _ string) (Profile, error) {
	return m.exchangeResult, m.exchangeErr
}

// TestHandleLogin: HandleLogin устанавливает state-куку и редиректит на AuthCodeURL.
func TestHandleLogin(t *testing.T) {
	provider := &mockOAuthProvider{authCodeURL: "https://google.com/auth"}
	svc := &Service{
		repo:       &mockRepository{},
		provider:   provider,
		sessionTTL: time.Hour,
		secure:     false,
		now:        time.Now,
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()
	svc.HandleLogin(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("HandleLogin код = %d, ожидается 302", resp.StatusCode)
	}

	// Проверяем наличие state-куки
	var stateCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == stateCookieName {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("HandleLogin должен установить state-куку")
	}
	if stateCookie.Value == "" {
		t.Error("state-кука не должна быть пустой")
	}

	// Проверяем, что Location содержит state из куки
	loc := resp.Header.Get("Location")
	if loc == "" {
		t.Fatal("HandleLogin должен установить Location")
	}
	if !strings.Contains(loc, "state=") {
		t.Errorf("Location %q должен содержать state=", loc)
	}
}

// TestHandleCallback_WrongState: неверный state → 403.
func TestHandleCallback_WrongState(t *testing.T) {
	svc := &Service{
		repo:       &mockRepository{},
		provider:   &mockOAuthProvider{},
		sessionTTL: time.Hour,
		secure:     false,
		now:        time.Now,
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state=wrong-state", nil)
	// Устанавливаем другой state в куке
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "correct-state"})
	w := httptest.NewRecorder()
	svc.HandleCallback(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("неверный state должен вернуть 403, получили %d", w.Code)
	}
}

// TestHandleCallback_NoState: отсутствие state в запросе → 403.
func TestHandleCallback_NoState(t *testing.T) {
	svc := &Service{
		repo:       &mockRepository{},
		provider:   &mockOAuthProvider{},
		sessionTTL: time.Hour,
		secure:     false,
		now:        time.Now,
	}

	// Нет state-куки и нет state в параметрах
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc", nil)
	w := httptest.NewRecorder()
	svc.HandleCallback(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("отсутствие state должно вернуть 403, получили %d", w.Code)
	}
}

// TestHandleCallback_Success: верный state → сессия + кука + 302 /.
func TestHandleCallback_Success(t *testing.T) {
	userProfile := Profile{
		Provider:      "google",
		ProviderSub:   "sub-ok",
		Email:         "ok@example.com",
		EmailVerified: true,
		Name:          "OK Пользователь",
	}
	newUser := &User{ID: "u-ok", Email: "ok@example.com"}
	newSess := &Session{ID: "sess-ok", UserID: "u-ok", ExpiresAt: time.Now().Add(time.Hour)}

	mock := &mockRepository{
		findUserResult:   nil, // нет в БД → создаём
		createUserResult: newUser,
		createSessResult: newSess,
	}
	provider := &mockOAuthProvider{exchangeResult: userProfile}
	svc := &Service{
		repo:       mock,
		provider:   provider,
		sessionTTL: time.Hour,
		secure:     false,
		now:        time.Now,
	}

	state := "valid-state-value"
	params := url.Values{"code": {"valid-code"}, "state": {state}}
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?"+params.Encode(), nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	w := httptest.NewRecorder()
	svc.HandleCallback(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("успешный callback должен вернуть 302, получили %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/" {
		t.Errorf("Location = %q, ожидается %q", loc, "/")
	}

	// Проверяем наличие сессионной куки
	var sessCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			sessCookie = c
			break
		}
	}
	if sessCookie == nil {
		t.Fatal("успешный callback должен установить сессионную куку")
	}
	if sessCookie.Value != newSess.ID {
		t.Errorf("сессионная кука = %q, ожидается %q", sessCookie.Value, newSess.ID)
	}
}

// TestHandleLogout: удаляет сессию и очищает куку → 302 /.
func TestHandleLogout(t *testing.T) {
	user := &User{ID: "u-logout", Email: "logout@example.com"}
	mock := &mockRepository{}
	svc := &Service{
		repo:       mock,
		provider:   &mockOAuthProvider{},
		sessionTTL: time.Hour,
		secure:     false,
		now:        time.Now,
	}

	// Эмулируем авторизованный запрос: кладём пользователя в контекст
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-logout"})
	req = withUser(req, user)
	w := httptest.NewRecorder()
	svc.HandleLogout(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("HandleLogout должен вернуть 302, получили %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/" {
		t.Errorf("Location = %q, ожидается %q", loc, "/")
	}

	// Проверяем, что сессионная кука очищена (MaxAge=-1)
	var sessCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			sessCookie = c
			break
		}
	}
	if sessCookie == nil {
		t.Fatal("HandleLogout должен очистить сессионную куку")
	}
	if sessCookie.MaxAge != -1 {
		t.Errorf("сессионная кука MaxAge = %d, ожидается -1 после logout", sessCookie.MaxAge)
	}
}
