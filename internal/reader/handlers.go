package reader

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

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
	// baseOrigin — внешний origin платформы (scheme://host) для абсолютных
	// ссылок в Агентском представлении. Пусто — origin выводится из запроса.
	baseOrigin string
}

// HandlerOption — функциональная опция HTTP-хендлеров читального зала.
type HandlerOption func(*Handlers)

// WithBaseOrigin задаёт внешний origin платформы (обычно извлекается из
// PLATFORM_CALLBACK_URL) для абсолютных ссылок в Агентском представлении.
func WithBaseOrigin(origin string) HandlerOption {
	return func(h *Handlers) {
		h.baseOrigin = origin
	}
}

// NewHandlers создаёт HTTP-хендлеры читального зала.
func NewHandlers(svc *Service, export ExportURLProvider, opts ...HandlerOption) *Handlers {
	h := &Handlers{svc: svc, export: export}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Mount регистрирует публичные маршруты читального зала.
func (h *Handlers) Mount(r chi.Router) {
	r.Get("/read/{id}", h.handleRead)
	r.Get("/read/{id}.md", h.handleReadMarkdown)
	r.Get("/read/{id}/slides/{n}", h.handleSlide)
	r.Get("/read/{id}/export", h.handleExport)
}

func (h *Handlers) handleRead(w http.ResponseWriter, r *http.Request) {
	lectureID := chi.URLParam(r, "id")
	// Ответ зависит от Accept и User-Agent — обязательно сообщаем об этом кешам.
	w.Header().Set("Vary", "Accept, User-Agent")

	// chi разбирает `{id}.md` как параметр `{id}` и статический суффикс, но
	// страховка на случай иного порядка матчинга: `.md` всегда Markdown.
	if strings.HasSuffix(lectureID, ".md") {
		h.serveReadMarkdown(w, r, strings.TrimSuffix(lectureID, ".md"))
		return
	}
	if web.WantsMarkdown(r) {
		h.serveReadMarkdown(w, r, lectureID)
		return
	}

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
	data.AlternateMarkdownURL = "/read/" + view.LectureID + ".md"
	if err := web.ReaderPage(data, readerToVM(view)).Render(r.Context(), w); err != nil {
		log.Printf("reader: render: %v", err)
	}
}

// handleReadMarkdown детерминированно отдаёт Агентское представление
// Конспекта независимо от Accept и User-Agent.
func (h *Handlers) handleReadMarkdown(w http.ResponseWriter, r *http.Request) {
	h.serveReadMarkdown(w, r, chi.URLParam(r, "id"))
}

// serveReadMarkdown проверяет доступ и отдаёт текст Агентского представления
// с заголовками кеширования и индексации по видимости Лекции.
func (h *Handlers) serveReadMarkdown(w http.ResponseWriter, r *http.Request, lectureID string) {
	origin := web.ResolveOrigin(h.baseOrigin, r)
	doc, err := h.svc.LoadMarkdown(r.Context(), lectureID, viewerID(r.Context()), origin)

	if doc.Visibility != "" && doc.Visibility != "public" {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "private, no-store")
	}
	if err != nil {
		status, message := resolveLoadError(err, "Лекция не найдена", "markdown")
		writeTextMessage(w, status, message)
		return
	}
	if doc.Visibility == "public" {
		w.Header().Set("Cache-Control", "public, max-age=60")
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = io.WriteString(w, doc.Markdown)
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
