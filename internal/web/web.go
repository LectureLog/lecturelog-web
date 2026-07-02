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
	// landingLectures — источник свежих публичных лекций для лендинга «/».
	// nil или пустой результат → секция примеров не показывается.
	landingLectures func(r *http.Request) []HubCardVM
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

// WithLandingLectures задаёт источник свежих публичных лекций для лендинга.
// Передаётся замыканием из cmd/server (hub.Service), чтобы web не зависел
// от доменных пакетов.
func WithLandingLectures(fn func(r *http.Request) []HubCardVM) Option {
	return func(o *options) {
		o.landingLectures = fn
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

	// Главная — лендинг сервиса: hero, свежие публичные конспекты, форматы.
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		var examples []HubCardVM
		if o.landingLectures != nil {
			examples = o.landingLectures(req)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := NewLayoutData(req.Context(), "LectureLog")
		if err := LandingPage(data, examples).Render(req.Context(), w); err != nil {
			http.Error(w, "ошибка рендера", http.StatusInternalServerError)
		}
	})

	// Стилизованная 404 вместо голого текста chi. Глобальные middleware
	// применяются и к NotFound-хендлеру, поэтому шапка отражает сессию.
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		data := NewLayoutData(req.Context(), "Страница не найдена")
		if err := NotFoundPage(data).Render(req.Context(), w); err != nil {
			http.Error(w, "ошибка рендера", http.StatusInternalServerError)
		}
	})

	// Монтируем дополнительные маршруты из опций (auth и др.)
	for _, mount := range o.mounts {
		mount(r)
	}

	return r
}
