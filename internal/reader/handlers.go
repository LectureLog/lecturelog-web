package reader

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/web"
	"github.com/go-chi/chi/v5"
)

// ExportURLProvider выдаёт временную ссылку на экспорт результата обработки.
type ExportURLProvider interface {
	GetResultURL(ctx context.Context, taskID, filename string) (string, error)
}

// Handlers обслуживает HTTP-маршруты читального зала.
type Handlers struct {
	svc    *Service
	export ExportURLProvider
}

// NewHandlers создаёт HTTP-хендлеры читального зала.
func NewHandlers(svc *Service, export ExportURLProvider) *Handlers {
	return &Handlers{svc: svc, export: export}
}

// Mount регистрирует публичные маршруты читального зала.
func (h *Handlers) Mount(r chi.Router) {
	r.Get("/read/{id}", h.handleRead)
	r.Get("/read/{id}/slides/{n}", h.handleSlide)
	r.Get("/read/{id}/export", h.handleExport)
}

func (h *Handlers) handleRead(w http.ResponseWriter, r *http.Request) {
	lectureID := chi.URLParam(r, "id")
	viewerID := viewerID(r.Context())
	view, err := h.svc.Load(r.Context(), lectureID, viewerID)
	if err != nil {
		h.writeLoadError(w, err)
		return
	}

	// Не-публичная лекция (unlisted или private для владельца) не должна
	// попадать в поисковые индексы и кеши общих прокси.
	if view.Visibility != "public" {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "private, no-store")
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := web.NewLayoutData(r.Context(), view.Title)
	data.NoIndex = view.Visibility != "public"
	if err := web.ReaderPage(data, readerToVM(view)).Render(r.Context(), w); err != nil {
		log.Printf("reader: render: %v", err)
	}
}

// handleSlide раздаёт байты Слайда напрямую с домена платформы.
func (h *Handlers) handleSlide(w http.ResponseWriter, r *http.Request) {
	num, parseErr := strconv.Atoi(chi.URLParam(r, "n"))
	if parseErr != nil || num <= 0 {
		num = 0
	}

	slide, err := h.svc.LoadSlide(r.Context(), chi.URLParam(r, "id"), viewerID(r.Context()), num)
	if slide.Visibility != "" && slide.Visibility != "public" {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	}
	if err != nil {
		h.writeSlideError(w, err)
		return
	}

	w.Header().Set("Content-Type", slide.ContentType)
	if slide.Visibility == "public" {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	_, _ = w.Write(slide.Bytes)
}

func (h *Handlers) handleExport(w http.ResponseWriter, r *http.Request) {
	taskID, err := h.svc.AccessTaskID(r.Context(), chi.URLParam(r, "id"), viewerID(r.Context()))
	if err != nil {
		h.writeLoadError(w, err)
		return
	}
	if h.export == nil {
		writeMessage(w, http.StatusNotImplemented, "Экспорт временно недоступен")
		return
	}

	// TODO долг: имя артефакта ZIP уточнить по контракту ядра.
	url, err := h.export.GetResultURL(r.Context(), taskID, "")
	if err != nil {
		log.Printf("reader: export: %v", err)
		writeMessage(w, http.StatusBadGateway, "Результат обработки временно недоступен, попробуйте позже")
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *Handlers) writeLoadError(w http.ResponseWriter, err error) {
	status, message := resolveLoadError(err, "Лекция не найдена", "request")
	writeMessage(w, status, message)
}

func writeMessage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "<!doctype html><html lang=\"ru\"><body><p>"+message+"</p></body></html>")
}

// writeTextMessage отправляет лаконичный текстовый ответ без HTML-обвязки.
func writeTextMessage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, message+"\n")
}

// writeSlideError отдаёт текстовую ошибку раздачи Слайда, не раскрывая
// внутренние пути и ошибки хранилища.
func (h *Handlers) writeSlideError(w http.ResponseWriter, err error) {
	status, message := resolveLoadError(err, "Слайд не найден", "slide")
	writeTextMessage(w, status, message)
}

// resolveLoadError отображает доменную ошибку чтения в HTTP-статус и безопасное сообщение.
func resolveLoadError(err error, notFoundMessage, logScope string) (int, string) {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, notFoundMessage
	case errors.Is(err, ErrNotReady):
		return http.StatusAccepted, "Лекция ещё обрабатывается"
	case errors.Is(err, ErrCoreUnavailable):
		// Логируем причину: молчаливое проглатывание уже раз скрыло
		// расхождение ключа structure.json с реальным путём выхода core.
		log.Printf("reader: %s core unavailable: %v", logScope, err)
		return http.StatusBadGateway, "Результат обработки временно недоступен, попробуйте позже"
	default:
		log.Printf("reader: %s: %v", logScope, err)
		return http.StatusInternalServerError, "Внутренняя ошибка"
	}
}

func viewerID(ctx context.Context) string {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return ""
	}
	return user.ID
}

func readerToVM(view ReaderView) web.ReaderVM {
	vm := web.ReaderVM{LectureID: view.LectureID, Title: view.Title, SourceTitle: view.SourceTitle, SourceKind: view.SourceKind, Duration: view.Duration, IsOwner: view.IsOwner, Visibility: view.Visibility, Sections: make([]web.ReaderSectionVM, 0, len(view.Sections))}
	for _, section := range view.Sections {
		sectionVM := web.ReaderSectionVM{Number: section.Number, Title: section.Title, Subtopics: make([]web.ReaderSubtopicVM, 0, len(section.Subtopics))}
		for _, subtopic := range section.Subtopics {
			subtopicVM := web.ReaderSubtopicVM{Number: subtopic.Number, Title: subtopic.Title, ContentHTML: subtopic.ContentHTML, SlideURLs: subtopic.SlideURLs}
			for _, block := range subtopic.Blocks {
				blockVM := web.ReaderBlockVM{HTML: block.HTML}
				if block.Slide != nil {
					blockVM.Slide = &web.ReaderSlideVM{URL: block.Slide.URL, Num: block.Slide.Num}
				}
				subtopicVM.Blocks = append(subtopicVM.Blocks, blockVM)
			}
			if subtopic.Media != nil {
				subtopicVM.Media = &web.ReaderMediaVM{Kind: subtopic.Media.Kind, Start: subtopic.Media.Start, End: subtopic.Media.End, URL: subtopic.Media.URL}
			}
			sectionVM.Subtopics = append(sectionVM.Subtopics, subtopicVM)
		}
		vm.Sections = append(vm.Sections, sectionVM)
	}
	return vm
}
