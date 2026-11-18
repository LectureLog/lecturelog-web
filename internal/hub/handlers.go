package hub

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/web"
	"github.com/go-chi/chi/v5"
)

// Mount монтирует витрину. RequireAuth не нужен: маршруты доступны анонимам.
func (s *Service) Mount(r chi.Router) {
	r.Get("/hub", s.handleHub)
	r.Get("/hub.md", s.handleHubMarkdown)
	r.Get("/llms.txt", s.handleLLMsTxt)
}

// handleHub рендерит публичную витрину лекций: HTML для людей и Markdown для агентов.
func (s *Service) handleHub(w http.ResponseWriter, r *http.Request) {
	// Ответ зависит от Accept и User-Agent — обязательно сообщаем об этом кешам.
	w.Header().Set("Vary", "Accept, User-Agent")
	if web.WantsMarkdown(r) {
		s.handleHubMarkdown(w, r)
		return
	}

	lectures, err := s.List(r.Context())
	if err != nil {
		log.Printf("hub: handleHub: %v", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	vms := make([]web.HubCardVM, len(lectures))
	for i, lecture := range lectures {
		vms[i] = hubToVM(lecture)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.HubPage(web.NewLayoutData(r.Context(), "Витрина"), vms).Render(r.Context(), w); err != nil {
		log.Printf("hub: handleHub render: %v", err)
	}
}

// handleHubMarkdown отдаёт Markdown-каталог Витрины (агентское представление).
func (s *Service) handleHubMarkdown(w http.ResponseWriter, r *http.Request) {
	lectures, err := s.List(r.Context())
	if err != nil {
		log.Printf("hub: handleHubMarkdown: %v", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	var b strings.Builder
	b.WriteString("# Витрина LectureLog\n\n")
	writeLectureCatalog(&b, lectures, web.ResolveOrigin(s.baseOrigin, r))

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}

// handleLLMsTxt отдаёт индекс платформы для внешних ИИ-агентов (llms.txt).
func (s *Service) handleLLMsTxt(w http.ResponseWriter, r *http.Request) {
	lectures, err := s.List(r.Context())
	if err != nil {
		log.Printf("hub: handleLLMsTxt: %v", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	origin := web.ResolveOrigin(s.baseOrigin, r)
	var b strings.Builder
	b.WriteString("# LectureLog\n\n")
	b.WriteString("> Веб-платформа для чтения структурированных конспектов лекций с тайм-кодами и слайдами.\n\n")
	b.WriteString("## Как читать лекции агентом\n\n")
	b.WriteString("Конспект открытой лекции доступен в Markdown по адресу " + origin + "/read/{id}.md\n")
	b.WriteString("или по адресу " + origin + "/read/{id} с заголовком `Accept: text/markdown`.\n")
	b.WriteString("Изображения слайдов отдаются по " + origin + "/read/{id}/slides/{n}, где `{n}` — номер слайда начиная с 1.\n")
	b.WriteString("Каталог Витрины в Markdown: " + origin + "/hub.md.\n\n")
	b.WriteString("## Публичные лекции\n\n")
	writeLectureCatalog(&b, lectures, origin)

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}

// writeLectureCatalog пишет Markdown-список публичных лекций с абсолютными
// ссылками на их агентское представление.
func writeLectureCatalog(b *strings.Builder, lectures []PublicLecture, origin string) {
	if len(lectures) == 0 {
		b.WriteString("Пока нет публичных лекций.\n")
		return
	}
	for _, lecture := range lectures {
		fmt.Fprintf(b, "- [%s](%s/read/%s.md) — %s, автор: %s, опубликовано: %s\n",
			lecture.Title,
			origin,
			lecture.ID,
			sourceLabel(lecture.SourceKind),
			lecture.AuthorName,
			lecture.PublishedAt.UTC().Format("2006-01-02"),
		)
	}
}

// hubToVM конвертирует доменную лекцию в данные карточки витрины.
func hubToVM(lecture PublicLecture) web.HubCardVM {
	return web.HubCardVM{
		ID:              lecture.ID,
		Title:           lecture.Title,
		SourceKind:      lecture.SourceKind,
		SourceLabel:     sourceLabel(lecture.SourceKind),
		AuthorName:      lecture.AuthorName,
		AuthorAvatarURL: lecture.AuthorAvatarURL,
		PublishedAt:     formatDate(lecture.PublishedAt),
		ReadURL:         readURL(lecture.ID),
	}
}

// readURL задаёт путь к читалке.
func readURL(id string) string {
	return "/read/" + id
}

// formatDate форматирует дату публикации для метаданных с tabular-nums.
func formatDate(value time.Time) string {
	return value.Format("2 Jan 2006")
}

// sourceLabel возвращает локализованную метку типа источника.
func sourceLabel(sourceKind string) string {
	switch sourceKind {
	case "audio":
		return "Аудио"
	case "video":
		return "Видео"
	case "video_url":
		return "Ссылка"
	default:
		return sourceKind
	}
}

// CardVMs возвращает карточки свежих публичных лекций для лендинга.
// limit <= 0 или больше количества — вернуть все. Ошибка не пробрасывается:
// лендинг не должен падать из-за витрины, секция просто скрывается.
func (s *Service) CardVMs(ctx context.Context, limit int) []web.HubCardVM {
	lectures, err := s.List(ctx)
	if err != nil {
		log.Printf("hub: CardVMs: %v", err)
		return nil
	}
	if limit > 0 && len(lectures) > limit {
		lectures = lectures[:limit]
	}
	vms := make([]web.HubCardVM, len(lectures))
	for i, lecture := range lectures {
		vms[i] = hubToVM(lecture)
	}
	return vms
}
