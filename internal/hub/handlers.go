package hub

import (
	"log"
	"net/http"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/web"
	"github.com/go-chi/chi/v5"
)

// Mount монтирует витрину. RequireAuth не нужен: маршрут доступен анонимам.
func (s *Service) Mount(r chi.Router) {
	r.Get("/hub", s.handleHub)
}

// handleHub рендерит публичную витрину лекций.
func (s *Service) handleHub(w http.ResponseWriter, r *http.Request) {
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
