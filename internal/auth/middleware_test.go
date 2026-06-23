package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// makeService создаёт Service с mock-репозиторием для тестов middleware.
func makeService(mock *mockRepository) *Service {
	return NewService(mock, nil, time.Hour, false)
}

// TestLoadSession_ValidCookie: при валидной куке пользователь кладётся в контекст.
func TestLoadSession_ValidCookie(t *testing.T) {
	user := &User{ID: "u-1", Email: "test@example.com"}
	sess := &Session{ID: "sess-1", UserID: "u-1", ExpiresAt: time.Now().Add(time.Hour)}
	mock := &mockRepository{
		getSessionResult: sess,
		findUserResult:   user,
	}
	svc := makeService(mock)

	// Фиктивный следующий хендлер — читает user из контекста
	var gotUser *User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := svc.LoadSession(next)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-1"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("код = %d, ожидается 200", w.Code)
	}
	if gotUser == nil {
		t.Fatal("LoadSession должен положить пользователя в контекст")
	}
	if gotUser.ID != "u-1" {
		t.Errorf("User.ID = %q, ожидается %q", gotUser.ID, "u-1")
	}
}

// TestLoadSession_NoCookie: без куки сессии запрос проходит (аноним), user=nil в контексте.
func TestLoadSession_NoCookie(t *testing.T) {
	svc := makeService(&mockRepository{})

	var gotUser *User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := svc.LoadSession(next)
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("код = %d, ожидается 200", w.Code)
	}
	if gotUser != nil {
		t.Errorf("без куки user должен быть nil, получили %+v", gotUser)
	}
}

// TestRequireAuth_Anon_Redirect: аноним → 302 /auth/login (обычный запрос).
func TestRequireAuth_Anon_Redirect(t *testing.T) {
	svc := makeService(&mockRepository{})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := svc.RequireAuth(next)
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("аноним должен получить 302, получил %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/auth/login" {
		t.Errorf("Location = %q, ожидается %q", loc, "/auth/login")
	}
}

// TestRequireAuth_Anon_HXRequest: аноним + HX-Request → 401 (htmx не умеет в redirect).
func TestRequireAuth_Anon_HXRequest(t *testing.T) {
	svc := makeService(&mockRepository{})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := svc.RequireAuth(next)
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("htmx аноним должен получить 401, получил %d", w.Code)
	}
}

// TestRequireAuth_AuthUser_Next: аутентифицированный пользователь → пропускается к next.
func TestRequireAuth_AuthUser_Next(t *testing.T) {
	user := &User{ID: "u-auth", Email: "auth@example.com"}
	sess := &Session{ID: "sess-auth", UserID: "u-auth", ExpiresAt: time.Now().Add(time.Hour)}
	mock := &mockRepository{
		getSessionResult: sess,
		findUserResult:   user,
	}
	svc := makeService(mock)

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// LoadSession кладёт пользователя в контекст, затем RequireAuth его пропускает
	chain := svc.LoadSession(svc.RequireAuth(next))
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-auth"})
	w := httptest.NewRecorder()
	chain.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("авторизованный должен получить 200, получил %d", w.Code)
	}
	if !nextCalled {
		t.Error("next хендлер не был вызван для авторизованного пользователя")
	}
}

// TestUserFromContext_Nil: из пустого контекста возвращается nil.
func TestUserFromContext_Nil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	u := UserFromContext(req.Context())
	if u != nil {
		t.Errorf("UserFromContext должен вернуть nil для пустого контекста, вернул %+v", u)
	}
}
