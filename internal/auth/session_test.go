package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestIssueSessionCookie проверяет флаги куки сессии.
func TestIssueSessionCookie(t *testing.T) {
	mock := &mockRepository{}
	svc := NewService(mock, nil, 24*time.Hour, true) // secure=true

	sess := &Session{
		ID:        "sess-uuid-123",
		UserID:    "user-uuid",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	w := httptest.NewRecorder()
	svc.issueSessionCookie(w, sess)

	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("ожидается Set-Cookie заголовок")
	}

	var cookie *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatalf("кука %q не найдена", sessionCookieName)
	}

	// Значение куки должно совпадать с ID сессии
	if cookie.Value != sess.ID {
		t.Errorf("cookie.Value = %q, ожидается %q", cookie.Value, sess.ID)
	}
	// HttpOnly обязателен
	if !cookie.HttpOnly {
		t.Error("ожидается HttpOnly=true")
	}
	// Secure обязателен (передали secure=true)
	if !cookie.Secure {
		t.Error("ожидается Secure=true при secure=true")
	}
	// SameSite=Lax
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, ожидается Lax", cookie.SameSite)
	}
	// MaxAge должен быть положительным
	if cookie.MaxAge <= 0 {
		t.Errorf("MaxAge = %d, ожидается > 0", cookie.MaxAge)
	}
	// Path = "/"
	if cookie.Path != "/" {
		t.Errorf("Path = %q, ожидается %q", cookie.Path, "/")
	}
}

// TestIssueSessionCookie_NotSecure проверяет, что при secure=false кука не имеет флага Secure.
func TestIssueSessionCookie_NotSecure(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false) // secure=false

	sess := &Session{ID: "sess-abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)}
	w := httptest.NewRecorder()
	svc.issueSessionCookie(w, sess)

	resp := w.Result()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatalf("кука %q не найдена", sessionCookieName)
	}
	if cookie.Secure {
		t.Error("Secure должен быть false при secure=false (локальный http)")
	}
}

// TestClearSessionCookie проверяет, что clearSessionCookie обнуляет куку.
func TestClearSessionCookie(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false)
	w := httptest.NewRecorder()
	svc.clearSessionCookie(w)

	resp := w.Result()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatalf("кука %q не найдена после clear", sessionCookieName)
	}
	// MaxAge=-1 → браузер удаляет куку
	if cookie.MaxAge != -1 {
		t.Errorf("MaxAge после clear = %d, ожидается -1", cookie.MaxAge)
	}
}

// TestStateCokie проверяет установку и очистку OAuth state-куки.
func TestStateCookie(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, true)
	w := httptest.NewRecorder()
	svc.setStateCookie(w, "my-state-value")

	resp := w.Result()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == stateCookieName {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatalf("кука %q не найдена", stateCookieName)
	}
	if cookie.Value != "my-state-value" {
		t.Errorf("state cookie.Value = %q, ожидается %q", cookie.Value, "my-state-value")
	}
	if !cookie.HttpOnly {
		t.Error("state кука должна иметь HttpOnly")
	}
	if cookie.MaxAge <= 0 || cookie.MaxAge > 700 {
		t.Errorf("state кука MaxAge = %d, ожидается ~600s", cookie.MaxAge)
	}
}
