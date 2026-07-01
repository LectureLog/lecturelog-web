package settings

import (
	"errors"
	"net/http"

	"github.com/LectureLog/lecturelog-web/internal/coreclient"
	"github.com/LectureLog/lecturelog-web/internal/web"
)

// writeMessage отдаёт короткое текстовое сообщение с нужным статусом
// (по образцу internal/reader/handlers.go:88). Используется для не-htmx
// текстовых ответов — сейчас это 401 «требуется авторизация» во всех
// хендлерах settings: он отдаётся раньше, чем запрос долетает до
// htmx-таргета #cookie-status, поэтому стилизованный errnote тут не нужен.
func writeMessage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message))
}

// writeCookieError рендерит стилизованный errnote (web.CookieStatusError) в
// узел #cookie-status вместо голого текста.
//
// HTTP-статус ответа — ВСЕГДА 200, независимо от смысловой ошибки (был бы
// 400/413/502). Причина: форма загрузки — htmx-запрос с hx-target="#cookie-status"
// hx-swap="outerHTML", а htmx по умолчанию НЕ свапает тело ответа с кодом 4xx/5xx —
// пользователь увидел бы пропавший статус вместо стилизованной ошибки.
// Смысл ошибки (формат/размер/недоступность ядра) остаётся различим по ТЕКСТУ
// сообщения (см. handlers_test.go: проверка подстрок «формат»/«велик»/«недоступно»),
// теряется только транспортный код — у этого эндпоинта нет иных потребителей, кроме htmx-формы.
func writeCookieError(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = web.CookieStatusError(message).Render(r.Context(), w)
}

// writeCoreError маппит ошибку ядра при загрузке cookies в текст errnote.
func writeCoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, coreclient.ErrCookiesBadFormat):
		writeCookieError(w, r, "Неверный формат: нужен файл cookies.txt в формате Netscape")
	case errors.Is(err, coreclient.ErrCookiesTooLarge):
		writeCookieError(w, r, "Файл слишком большой")
	default:
		writeCookieError(w, r, "Ядро недоступно, попробуйте позже")
	}
}
