package web

import "context"

// settingsAvailableContextKey — тип ключа контекста для флага доступности настроек.
type settingsAvailableContextKey struct{}

// WithSettingsAvailable отмечает контекст запроса как имеющий смонтированные settings-маршруты.
// Вызывается middleware верхнего уровня, когда cmd/server подключает сервис настроек.
func WithSettingsAvailable(ctx context.Context) context.Context {
	return context.WithValue(ctx, settingsAvailableContextKey{}, true)
}

// SettingsAvailableFromContext извлекает флаг доступности settings-маршрутов.
// Возвращает false по умолчанию, чтобы layout не показывал битую ссылку /settings.
func SettingsAvailableFromContext(ctx context.Context) bool {
	available, _ := ctx.Value(settingsAvailableContextKey{}).(bool)
	return available
}
