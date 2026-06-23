package auth

import (
	"net/http"
)

const (
	// sessionCookieName — имя куки сессии пользователя.
	sessionCookieName = "ll_session"
	// stateCookieName — имя куки OAuth state (живёт ~10 минут, защита от CSRF при login).
	stateCookieName = "ll_oauth_state"
	// stateCookieMaxAge — TTL state-куки в секундах (~10 минут достаточно для OAuth round-trip).
	stateCookieMaxAge = 600
)

// issueSessionCookie устанавливает HttpOnly-куку сессии в ответ.
// Флаги безопасности (§security):
//   - HttpOnly: JS не читает session_id.
//   - Secure: только HTTPS (s.secure=true в prod, false для локального http).
//   - SameSite=Lax: базовая защита от CSRF (второй слой — gorilla/csrf).
//   - MaxAge: из config.SessionTTL.
//   - Path="/": кука видна всему сайту.
//
// session_id НЕ логируется (секрет).
func (s *Service) issueSessionCookie(w http.ResponseWriter, sess *Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.ID, // секрет — не логировать
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.sessionTTL.Seconds()),
	})
}

// clearSessionCookie сбрасывает куку сессии (MaxAge=-1 → браузер удаляет).
// Вызывается при logout.
func (s *Service) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// setStateCookie устанавливает куку OAuth state (TTL ~10 минут).
// Используется в HandleLogin перед редиректом к провайдеру.
// state НЕ логируется (секрет).
func (s *Service) setStateCookie(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state, // секрет — не логировать
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   stateCookieMaxAge,
	})
}

// clearStateCookie сбрасывает куку state после использования (одноразовая).
func (s *Service) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
