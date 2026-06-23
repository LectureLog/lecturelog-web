package auth

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// HandleLogin запускает OAuth 2.0 flow:
//  1. Генерирует случайный state (crypto/rand).
//  2. Кладёт state в HttpOnly-куку (защита от CSRF при login).
//  3. Редиректит пользователя на AuthCodeURL провайдера.
//
// state НЕ логируется (секрет).
func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := newState()
	if err != nil {
		log.Printf("auth: HandleLogin: ошибка генерации state: %v", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	// Кладём state в куку — будем сверять в callback
	s.setStateCookie(w, state)

	// Редиректим на OAuth-провайдера
	http.Redirect(w, r, s.provider.AuthCodeURL(state), http.StatusFound)
}

// HandleCallback обрабатывает redirect от OAuth-провайдера:
//  1. Сверяет state из параметра запроса с state из куки (constant-time, защита от CSRF).
//  2. Обменивает код на профиль через OAuthProvider.Exchange.
//  3. Разрешает/создаёт пользователя через resolveUser (email_verified обязателен).
//  4. Создаёт серверную сессию и устанавливает сессионную куку.
//  5. Редиректит на /.
//
// Ошибки безопасности → 403. Внутренние ошибки → 500. Секреты не логируются.
func (s *Service) HandleCallback(w http.ResponseWriter, r *http.Request) {
	// Читаем state из куки (устанавливается в HandleLogin)
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		http.Error(w, "state отсутствует", http.StatusForbidden)
		return
	}
	cookieState := stateCookie.Value

	// Читаем state из параметра запроса
	queryState := r.URL.Query().Get("state")

	// Сверяем state constant-time (защита от timing-атак)
	if !compareState(cookieState, queryState) {
		// Очищаем куку даже при ошибке (одноразовая)
		s.clearStateCookie(w)
		http.Error(w, "неверный state", http.StatusForbidden)
		return
	}
	// State использован — очищаем (одноразовая)
	s.clearStateCookie(w)

	// Обмен кода на профиль (секрет не логируем)
	code := r.URL.Query().Get("code")
	profile, err := s.provider.Exchange(r.Context(), code)
	if err != nil {
		log.Printf("auth: HandleCallback: ошибка обмена кода: %v", err)
		http.Error(w, "ошибка авторизации", http.StatusInternalServerError)
		return
	}

	// Разрешаем/создаём пользователя (email_verified проверяется внутри)
	user, err := s.resolveUser(r.Context(), profile)
	if err != nil {
		if err == ErrEmailNotVerified {
			http.Error(w, "email не подтверждён провайдером", http.StatusForbidden)
			return
		}
		log.Printf("auth: HandleCallback: ошибка resolveUser: %v", err)
		http.Error(w, "ошибка авторизации", http.StatusInternalServerError)
		return
	}

	// Создаём серверную сессию
	expiresAt := s.now().Add(s.sessionTTL).UTC().Truncate(time.Second)
	sess, err := s.repo.CreateSession(r.Context(), user.ID, expiresAt)
	if err != nil {
		log.Printf("auth: HandleCallback: ошибка создания сессии: %v", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	// Устанавливаем сессионную куку (session_id не логируем)
	s.issueSessionCookie(w, sess)

	// Редирект на главную страницу
	http.Redirect(w, r, "/", http.StatusFound)
}

// HandleLogout завершает сессию пользователя:
//  1. Удаляет сессию из Postgres (если кука есть).
//  2. Очищает сессионную куку.
//  3. Редиректит на /.
func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Читаем session_id из куки — может быть уже очищен или отсутствовать
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		// Удаляем из БД (session_id не логируем)
		if delErr := s.repo.DeleteSession(r.Context(), cookie.Value); delErr != nil {
			log.Printf("auth: HandleLogout: ошибка удаления сессии: %v", delErr)
			// Продолжаем logout даже при ошибке БД — очищаем куку в любом случае
		}
	}

	// Очищаем куку
	s.clearSessionCookie(w)

	http.Redirect(w, r, "/", http.StatusFound)
}

// Mount монтирует auth-маршруты в chi-роутер.
// Роуты:
//   - GET  /auth/login    — начало OAuth flow
//   - GET  /auth/callback — обратный вызов от провайдера
//   - POST /auth/logout   — завершение сессии (POST защищает от CSRF)
func (s *Service) Mount(r chi.Router) {
	r.Get("/auth/login", s.HandleLogin)
	r.Get("/auth/callback", s.HandleCallback)
	r.Post("/auth/logout", s.HandleLogout)
}
