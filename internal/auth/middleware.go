package auth

import (
	"net/http"
)

// LoadSession читает куку сессии и при наличии валидной сессии кладёт *User в контекст.
// НЕ блокирует анонимные запросы — используй RequireAuth для защищённых маршрутов.
// Сессия валидируется через GetSession (expires_at > now() на стороне БД).
func (s *Service) LoadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			// Куки нет — аноним, продолжаем без пользователя в контексте
			next.ServeHTTP(w, r)
			return
		}

		// Проверяем сессию в БД (протухшие фильтруются на стороне Postgres)
		sess, err := s.repo.GetSession(r.Context(), cookie.Value)
		if err != nil || sess == nil {
			// Невалидная/протухшая сессия → чистим куку и продолжаем как аноним
			s.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		// Загружаем пользователя по UserID из сессии
		user, err := s.repo.FindUserByID(r.Context(), sess.UserID)
		if err != nil || user == nil {
			// Пользователь удалён — очищаем сессию
			s.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		// Кладём пользователя в контекст — следующие хендлеры читают через UserFromContext
		next.ServeHTTP(w, withUser(r, user))
	})
}

// LoadAdmin кладёт в контекст флаг администратора для уже загруженного пользователя.
func (s *Service) LoadAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, withAdmin(r, s.isAdminEmail(user.Email)))
	})
}

// RequireAuth блокирует анонимные запросы.
// Обычный запрос без пользователя → 302 /auth/login.
// Htmx-запрос (HX-Request: true) → 401 (htmx не обрабатывает редирект как навигацию).
// Аутентифицированный запрос → передаётся в next.
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) != nil {
			// Пользователь аутентифицирован — пропускаем
			next.ServeHTTP(w, r)
			return
		}
		// Аноним
		if r.Header.Get("HX-Request") == "true" {
			// htmx-запрос: отдаём 401, фронтенд обрабатывает сам
			http.Error(w, "требуется авторизация", http.StatusUnauthorized)
			return
		}
		// Обычный запрос: редирект на страницу входа
		http.Redirect(w, r, "/auth/login", http.StatusFound)
	})
}
