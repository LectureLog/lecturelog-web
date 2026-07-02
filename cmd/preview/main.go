// Команда preview — локальный сервер дизайн-превью: рендерит все страницы
// web-слоя с фиктивными данными без БД, ядра и OAuth.
//
//	go run ./cmd/preview            # http://localhost:8901
//	PREVIEW_ADDR=:9000 go run ./cmd/preview
//
// Страницы:
//
//	/               — лендинг (аноним; /landing-authed — авторизованный)
//	/hub            — витрина (аноним; /hub-authed, /hub-empty)
//	/lectures       — «Мои лекции» со всеми статусами (/lectures-empty)
//	/upload         — форма загрузки
//	/settings       — настройки (cookies загружены)
//	/read/video     — читалка: реальная видео-лекция (без слайдов)
//	/read/slides    — читалка: реальная лекция со слайдами
//	/read/audio     — читалка: та же лекция как аудио
//
// Реальные конспекты читаются из .preview-data/ (structure.json + медиа,
// скачаны из MinIO прод-сервера; каталог в .gitignore). Отсутствующие
// локально файлы медиа/слайдов маппятся на имеющиеся образцы по кругу.
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

	handler := web.NewRouter(web.WithLandingLectures(func(r *http.Request) []web.HubCardVM {
		return hubCards()
	}), web.WithMount(func(r chi.Router) {
		// Локальные медиа для страниц читалки.
		r.Handle("/preview-data/*", http.StripPrefix("/preview-data/", http.FileServer(http.Dir(".preview-data"))))

		r.Get("/landing-authed", render(func() templ.Component {
			return web.LandingPage(layoutAuthed("LectureLog"), hubCards())
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
		r.Get("/read/video", renderReader("lec-video", "Кодинг с ИИ: практика внедрения", "video", true))
		r.Get("/read/slides", renderReader("lec-slides", "Фундаментальные основы разработки в эпоху ИИ", "video", false))
		r.Get("/read/audio", renderReader("lec-video", "Кодинг с ИИ (аудиозапись)", "audio", false))
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

// renderReader строит страницу читалки из реального structure.json.
// kind принудительно переопределяет тип медиа (для аудио-варианта).
func renderReader(dir, title, kind string, isOwner bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vm, err := readerVM(dir, title, kind, isOwner)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := web.ReaderPage(layoutAuthed(vm.Title), vm).Render(r.Context(), w); err != nil {
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
		{ID: "1", Title: "Дифференциальные уравнения: линейные системы и фазовые портреты", SourceKind: "audio", SourceLabel: "Аудио", AuthorName: "Пётр Крылов", PublishedAt: "30 Jun 2026", ReadURL: "/read/audio"},
		{ID: "2", Title: "Кодинг с ИИ: практика внедрения", SourceKind: "video", SourceLabel: "Видео", AuthorName: "Анна Соколова", PublishedAt: "28 Jun 2026", ReadURL: "/read/video"},
		{ID: "3", Title: "Фундаментальные основы разработки в эпоху ИИ", SourceKind: "video_url", SourceLabel: "Ссылка", AuthorName: "М. Ветров", PublishedAt: "25 Jun 2026", ReadURL: "/read/slides"},
		{ID: "4", Title: "Теория вероятностей: предельные теоремы", SourceKind: "audio", SourceLabel: "Аудио", AuthorName: "Пётр Крылов", PublishedAt: "21 Jun 2026", ReadURL: "/read/audio"},
		{ID: "5", Title: "Функциональное программирование: монады без страха", SourceKind: "video", SourceLabel: "Видео", AuthorName: "Анна Соколова", PublishedAt: "18 Jun 2026", ReadURL: "/read/video"},
	}
}

func lectureCards() []web.LectureCardVM {
	return []web.LectureCardVM{
		{ID: "video", Title: "Кодинг с ИИ: практика внедрения", Status: "ready", StatusLabel: "Готово", Visibility: "public", SourceKind: "video", CanPublish: true, UpdatedAt: "30 Jun 2026"},
		{ID: "a2", Title: "ОС: планировщик", Status: "processing", StatusLabel: "Обработка", Visibility: "private", SourceKind: "video", ProgressPct: 62, StageLabel: "Распознавание речи", UpdatedAt: "2 Jul 2026"},
		{ID: "a3", Title: "Матан: ряды Фурье (YouTube)", Status: "failed", StatusLabel: "Ошибка", Visibility: "private", SourceKind: "video_url", CanRetry: true, ErrorText: "Не удалось скачать видео: требуется авторизация.", UpdatedAt: "1 Jul 2026"},
		{ID: "slides", Title: "Фундаментальные основы разработки в эпоху ИИ", Status: "ready", StatusLabel: "Готово", Visibility: "private", SourceKind: "audio", CanPublish: true, UpdatedAt: "21 Jun 2026"},
	}
}

// sampleFiles — локально скачанные образцы медиа для подмены недостающих.
var sampleVideos = []string{
	"/preview-data/video/01-вступление-и-знакомство-со-спикерами.mp4",
	"/preview-data/video/01-проблема-подхода-от-спецификации-к-коду-specs-to-code.mp4",
}

var sampleAudio = []string{
	"/preview-data/video/01-вступление.m4a",
}

var sampleSlides = []string{
	"/preview-data/slides/slide-01.png",
	"/preview-data/slides/slide-02.png",
	"/preview-data/slides/slide-03.png",
	"/preview-data/slides/slide-04.png",
	"/preview-data/slides/slide-05.png",
}

// readerVM собирает view-модель читалки из реального structure.json,
// подменяя ключи MinIO на локальные образцы.
func readerVM(dir, title, kind string, isOwner bool) (web.ReaderVM, error) {
	raw, err := os.ReadFile(".preview-data/" + dir + "/structure.json")
	if err != nil {
		return web.ReaderVM{}, fmt.Errorf("нет %s (см. комментарий пакета): %w", dir, err)
	}
	structure, err := reader.ParseStructure(raw)
	if err != nil {
		return web.ReaderVM{}, err
	}

	md := reader.NewMarkdownRenderer()
	sourceTitle := structure.Source.Title
	if sourceTitle == "" {
		sourceTitle = title
	}
	vm := web.ReaderVM{
		LectureID:   dir,
		Title:       title,
		SourceTitle: sourceTitle,
		SourceKind:  kind,
		Duration:    int(structure.Source.Duration),
		IsOwner:     isOwner,
	}

	mediaIndex, slideIndex := 0, 0
	for si, section := range structure.Sections {
		sectionVM := web.ReaderSectionVM{
			Number: fmt.Sprintf("%02d", si+1),
			Title:  section.Title,
		}
		for bi, subtopic := range section.Subtopics {
			html, err := md.ToHTML(subtopic.ContentMD)
			if err != nil {
				return web.ReaderVM{}, err
			}
			subVM := web.ReaderSubtopicVM{
				Number:      fmt.Sprintf("%d.%d", si+1, bi+1),
				Title:       subtopic.Title,
				ContentHTML: html,
			}
			if subtopic.Media != nil {
				url := sampleVideos[mediaIndex%len(sampleVideos)]
				if kind == "audio" {
					url = sampleAudio[mediaIndex%len(sampleAudio)]
				}
				mediaIndex++
				subVM.Media = &web.ReaderMediaVM{
					Kind:  kind,
					Start: int(subtopic.Media.Start),
					End:   int(subtopic.Media.End),
					URL:   url,
				}
			}
			for range subtopic.SlideKeys {
				subVM.SlideURLs = append(subVM.SlideURLs, sampleSlides[slideIndex%len(sampleSlides)])
				slideIndex++
			}
			sectionVM.Subtopics = append(sectionVM.Subtopics, subVM)
		}
		vm.Sections = append(vm.Sections, sectionVM)
	}

	return vm, nil
}
