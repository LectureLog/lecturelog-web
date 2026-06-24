package web

import "context"

// csrfContextKey — тип ключа контекста для CSRF-токена.
type csrfContextKey struct{}

// WithCSRFToken кладёт CSRF-токен в контекст запроса.
// Вызывается csrf-инжектором в cmd/server после gorilla/csrf middleware.
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfContextKey{}, token)
}

// CSRFTokenFromContext извлекает CSRF-токен из контекста.
// Возвращает "" если токен не установлен (анонимный контекст, пустой layout).
func CSRFTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}
