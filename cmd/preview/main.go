// Команда preview — локальный сервер дизайн-превью: рендерит все страницы
// web-слоя с фиктивными данными без БД, ядра и OAuth.
//
//	go run ./cmd/preview            # http://localhost:8901
//	PREVIEW_ADDR=:9000 go run ./cmd/preview
//
// Страницы:
//
//	/hub            — витрина (анонимный посетитель)
//	/hub-authed     — витрина (авторизованный)
//	/hub-empty      — витрина без лекций
//	/lectures       — «Мои лекции» со всеми статусами карточек
//	/lectures-empty — пустая библиотека
//	/upload         — форма загрузки
//	/settings       — настройки (cookies загружены)
//	/read/preview   — читалка на реальной фикстуре
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/LectureLog/lecturelog-web/internal/coreclient"
	"github.com/LectureLog/lecturelog-web/internal/reader"
	"github.com/LectureLog/lecturelog-web/internal/web"
)

func main() {
	addr := os.Getenv("PREVIEW_ADDR")
	if addr == "" {
		addr = ":8901"
	}

	handler := web.NewRouter(web.WithMount(func(r chi.Router) {
		r.Get("/landing", render(func() templ.Component {
			return web.LandingPage(layout("LectureLog"))
		}))
		r.Get("/landing-authed", render(func() templ.Component {
			return web.LandingPage(layoutAuthed("LectureLog"))
		}))
		r.Get("/hub", render(func() templ.Component {
			return web.HubPage(layout("Витрина"), hubCards())
		}))
		r.Get("/hub-authed", render(func() templ.Component {
			return web.HubPage(layoutAuthed("Витрина"), hubCards())
		}))
		r.Get("/hub-empty", render(func() templ.Component {
			return web.HubPage(layout("Витрина"), nil)
		}))
		r.Get("/lectures", render(func() templ.Component {
			return web.LecturesPage(layoutAuthed("Мои лекции"), lectureCards())
		}))
		r.Get("/lectures-empty", render(func() templ.Component {
			return web.LecturesPage(layoutAuthed("Мои лекции"), nil)
		}))
		r.Get("/upload", render(func() templ.Component {
			return web.UploadPage(layoutAuthed("Новый конспект"))
		}))
		r.Get("/settings", render(func() templ.Component {
			return web.SettingsPage(layoutAuthed("Настройки"), coreclient.CookieStatus{
				Exists:    true,
				Size:      48 * 1024,
				UpdatedAt: "2026-06-28T12:00:00Z",
			}, false)
		}))
		r.Get("/read/preview", render(func() templ.Component {
			vm, err := readerVM()
			if err != nil {
				log.Printf("preview: reader fixture: %v", err)
				return web.DemoPage()
			}
			return web.ReaderPage(layoutAuthed(vm.Title), vm)
		}))
	}))

	log.Printf("design-preview на %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

// render оборачивает конструктор компонента в http.HandlerFunc.
func render(build func() templ.Component) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := build().Render(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// layout — анонимный посетитель.
func layout(title string) web.LayoutData {
	return web.LayoutData{Title: title}
}

// layoutAuthed — авторизованный администратор.
func layoutAuthed(title string) web.LayoutData {
	return web.LayoutData{Title: title, IsAuthed: true, IsAdmin: true, SettingsAvailable: true}
}

func hubCards() []web.HubCardVM {
	return []web.HubCardVM{
		{ID: "1", Title: "Дифференциальные уравнения: линейные системы и фазовые портреты", SourceKind: "audio", SourceLabel: "Аудио", AuthorName: "Пётр Крылов", PublishedAt: "30 Jun 2026", ReadURL: "/read/preview"},
		{ID: "2", Title: "Операционные системы: планировщик и вытесняющая многозадачность", SourceKind: "video", SourceLabel: "Видео", AuthorName: "Анна Соколова", PublishedAt: "28 Jun 2026", ReadURL: "/read/preview"},
		{ID: "3", Title: "Математический анализ. Ряды Фурье", SourceKind: "video_url", SourceLabel: "Ссылка", AuthorName: "М. Ветров", PublishedAt: "25 Jun 2026", ReadURL: "/read/preview"},
		{ID: "4", Title: "Теория вероятностей: предельные теоремы", SourceKind: "audio", SourceLabel: "Аудио", AuthorName: "Пётр Крылов", PublishedAt: "21 Jun 2026", ReadURL: "/read/preview"},
		{ID: "5", Title: "Функциональное программирование: монады без страха", SourceKind: "video", SourceLabel: "Видео", AuthorName: "Анна Соколова", PublishedAt: "18 Jun 2026", ReadURL: "/read/preview"},
	}
}

func lectureCards() []web.LectureCardVM {
	return []web.LectureCardVM{
		{ID: "a1", Title: "Диффуры — лекция 17 февраля", Status: "ready", StatusLabel: "Готово", Visibility: "public", SourceKind: "audio", CanPublish: true, UpdatedAt: "30 Jun 2026"},
		{ID: "a2", Title: "ОС: планировщик", Status: "processing", StatusLabel: "Обработка", Visibility: "private", SourceKind: "video", ProgressPct: 62, StageLabel: "Распознавание речи", UpdatedAt: "2 Jul 2026"},
		{ID: "a3", Title: "Матан: ряды Фурье (YouTube)", Status: "failed", StatusLabel: "Ошибка", Visibility: "private", SourceKind: "video_url", CanRetry: true, ErrorText: "Не удалось скачать видео: требуется авторизация.", UpdatedAt: "1 Jul 2026"},
		{ID: "a4", Title: "Теорвер: предельные теоремы", Status: "ready", StatusLabel: "Готово", Visibility: "private", SourceKind: "audio", CanPublish: true, UpdatedAt: "21 Jun 2026"},
	}
}

func readerVM() (web.ReaderVM, error) {
	fixture, err := os.ReadFile("internal/reader/testdata/structure_valid.json")
	if err != nil {
		return web.ReaderVM{}, err
	}
	structure, err := reader.ParseStructure(fixture)
	if err != nil {
		return web.ReaderVM{}, err
	}
	vm := web.ReaderVM{
		LectureID:   "preview",
		Title:       structure.Source.Title,
		SourceTitle: structure.Source.Title,
		SourceKind:  structure.Source.Kind,
		Duration:    int(structure.Source.Duration),
		IsOwner:     true,
	}
	for si, section := range structure.Sections {
		sectionVM := web.ReaderSectionVM{
			Number: fmt.Sprintf("%02d", si+1),
			Title:  section.Title,
		}
		for bi, subtopic := range section.Subtopics {
			sectionVM.Subtopics = append(sectionVM.Subtopics, web.ReaderSubtopicVM{
				Number:      fmt.Sprintf("%d.%d", si+1, bi+1),
				Title:       subtopic.Title,
				ContentHTML: "<p>" + subtopic.ContentMD + "</p>",
			})
		}
		vm.Sections = append(vm.Sections, sectionVM)
	}
	return vm, nil
}
