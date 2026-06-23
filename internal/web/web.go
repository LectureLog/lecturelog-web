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

// NewRouter создаёт минимальный chi-роутер web-слоя.
//
// Маршруты:
//   - /static/* — отдача встроенной статики (CSS, htmx, шрифты)
//   - /          — демо-страница с базовым layout «Читальный зал»
//
// C0-auth смонтирует дополнительные маршруты поверх этого роутера.
func NewRouter() http.Handler {
	r := chi.NewRouter()

	// Стандартные middleware: восстановление после паники, логирование.
	r.Use(middleware.Recoverer)

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

	return r
}
