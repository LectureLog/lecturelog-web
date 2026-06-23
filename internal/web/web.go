// Пакет web реализует слой презентации «Читальный зал» платформы LectureLog.
//
// Стек: chi-роутер, templ-компоненты, Tailwind (собранный CSS через go:embed), htmx.
// Пакет самодостаточен: не зависит от internal/config, internal/db, internal/coreclient.
// Полноценный cmd/server (с конфигом, БД, OAuth) — C0-auth/интеграция.
package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// options — внутренняя конфигурация NewRouter.
type options struct {
	// globalMiddleware — middleware, применяемые ко всем маршрутам роутера.
	globalMiddleware []func(http.Handler) http.Handler
	// mounts — функции монтирования дополнительных маршрутов (auth и др.).
	mounts []func(chi.Router)
}

// Option — функциональная опция NewRouter.
type Option func(*options)

// WithGlobalMiddleware добавляет middleware ко всем маршрутам роутера.
// Порядок применения: в порядке передачи опций.
// Используется для LoadSession (auth), CSRF, логирования и т.п.
func WithGlobalMiddleware(mw ...func(http.Handler) http.Handler) Option {
	return func(o *options) {
		o.globalMiddleware = append(o.globalMiddleware, mw...)
	}
}

// WithMount добавляет функцию монтирования дополнительных маршрутов.
// fn получает chi.Router и монтирует маршруты/группы.
// Используется для auth.Service.Mount и других доменных модулей.
func WithMount(fn func(chi.Router)) Option {
	return func(o *options) {
		o.mounts = append(o.mounts, fn)
	}
}

// NewRouter создаёт минимальный chi-роутер web-слоя.
//
// Маршруты:
//   - /static/* — отдача встроенной статики (CSS, htmx, шрифты)
//   - /          — демо-страница с базовым layout «Читальный зал»
//
// Опции (variadic, обратная совместимость — существующие NewRouter() вызовы не ломаются):
//   - WithGlobalMiddleware — дополнительные middleware для всех маршрутов
//   - WithMount — монтирование дополнительных маршрутов (auth и др.)
func NewRouter(opts ...Option) http.Handler {
	// Применяем опции
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	r := chi.NewRouter()

	// Стандартные middleware: восстановление после паники, логирование.
	r.Use(middleware.Recoverer)

	// Применяем глобальные middleware из опций (LoadSession, CSRF и т.п.)
	for _, mw := range o.globalMiddleware {
		r.Use(mw)
	}

	// Статика через go:embed — CSS, htmx и другие ресурсы.
	sfs, err := staticFS()
	if err != nil {
		// Паника при старте: статика встроена в бинарь и не должна отсутствовать.
		panic("web: не удалось инициализировать статику: " + err.Error())
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(sfs)))

	// Демо-страница: рендерит базовый layout для проверки работоспособности.
	// C1 заменит этот маршрут доменными страницами.
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := DemoPage().Render(req.Context(), w); err != nil {
			http.Error(w, "ошибка рендера", http.StatusInternalServerError)
		}
	})

	// Монтируем дополнительные маршруты из опций (auth и др.)
	for _, mount := range o.mounts {
		mount(r)
	}

	return r
}
