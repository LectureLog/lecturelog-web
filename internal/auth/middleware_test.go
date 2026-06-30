package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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

func TestIsAdminFromContext_DefaultFalse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if IsAdminFromContext(req.Context()) {
		t.Fatal("IsAdminFromContext должен вернуть false для пустого контекста")
	}
}

func TestLoadAdmin_AdminUserSetsContextFlag(t *testing.T) {
	user := &User{ID: "u-1", Email: " Admin@Example.COM "}
	sess := &Session{ID: "sess-1", UserID: "u-1", ExpiresAt: time.Now().Add(time.Hour)}
	mock := &mockRepository{getSessionResult: sess, findUserResult: user}
	svc := NewService(mock, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))

	var isAdmin bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAdmin = IsAdminFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := svc.LoadSession(svc.LoadAdmin(next))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-1"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", w.Code)
	}
	if !isAdmin {
		t.Fatal("LoadAdmin должен положить IsAdmin=true для email из allowlist")
	}
}

func TestLoadAdmin_NonAdminAndAnonStayFalse(t *testing.T) {
	t.Run("non-admin", func(t *testing.T) {
		user := &User{ID: "u-1", Email: "user@example.com"}
		sess := &Session{ID: "sess-1", UserID: "u-1", ExpiresAt: time.Now().Add(time.Hour)}
		mock := &mockRepository{getSessionResult: sess, findUserResult: user}
		svc := NewService(mock, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))

		var isAdmin bool
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isAdmin = IsAdminFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-1"})
		w := httptest.NewRecorder()
		svc.LoadSession(svc.LoadAdmin(next)).ServeHTTP(w, req)

		if isAdmin {
			t.Fatal("LoadAdmin не должен ставить IsAdmin=true для не-админа")
		}
	})

	t.Run("anon", func(t *testing.T) {
		svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))

		var isAdmin bool
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isAdmin = IsAdminFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		svc.LoadSession(svc.LoadAdmin(next)).ServeHTTP(w, req)

		if isAdmin {
			t.Fatal("LoadAdmin не должен ставить IsAdmin=true для анонима")
		}
	})
}

func TestRequireAdmin_AdminPasses(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req = withUser(req, &User{ID: "u-admin", Email: "admin@example.com"})

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	svc.RequireAdmin(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", w.Code)
	}
	if !nextCalled {
		t.Fatal("RequireAdmin должен вызвать next для админа")
	}
}

func TestRequireAdmin_NonAdminRedirectsToLectures(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req = withUser(req, &User{ID: "u-user", Email: "user@example.com"})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next не должен вызываться для не-админа")
	})

	w := httptest.NewRecorder()
	svc.RequireAdmin(next).ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("код = %d, ожидается 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/lectures" {
		t.Fatalf("Location = %q, ожидается /lectures", loc)
	}
}

func TestRequireAdmin_NonAdminHtmxRedirectHeader(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.Header.Set("HX-Request", "true")
	req = withUser(req, &User{ID: "u-user", Email: "user@example.com"})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next не должен вызываться для не-админа")
	})

	w := httptest.NewRecorder()
	svc.RequireAdmin(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", w.Code)
	}
	if got := w.Header().Get("HX-Redirect"); got != "/lectures" {
		t.Fatalf("HX-Redirect = %q, ожидается /lectures", got)
	}
}

func TestRequireAdmin_AnonDirectCallFailsClosed(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next не должен вызываться без пользователя")
	})

	w := httptest.NewRecorder()
	svc.RequireAdmin(next).ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("код = %d, ожидается 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/lectures" {
		t.Fatalf("Location = %q, ожидается /lectures", loc)
	}
}

func TestAdminGateComposition_LoadSessionLoadAdminRequireAuthRequireAdmin(t *testing.T) {
	user := &User{ID: "u-admin", Email: "admin@example.com"}
	sess := &Session{ID: "sess-admin", UserID: "u-admin", ExpiresAt: time.Now().Add(time.Hour)}
	mock := &mockRepository{getSessionResult: sess, findUserResult: user}
	svc := NewService(mock, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))

	r := chi.NewRouter()
	r.Use(svc.LoadSession)
	r.Use(svc.LoadAdmin)
	r.Group(func(pr chi.Router) {
		pr.Use(svc.RequireAuth)
		pr.Use(svc.RequireAdmin)
		pr.Get("/settings", func(w http.ResponseWriter, r *http.Request) {
			if !IsAdminFromContext(r.Context()) {
				t.Fatal("IsAdmin должен быть true внутри защищённого settings-хендлера")
			}
			w.WriteHeader(http.StatusNoContent)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-admin"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("код = %d, ожидается 204", w.Code)
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
