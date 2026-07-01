package settings

import (
	"io"
	"net/http"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/web"
	"github.com/go-chi/chi/v5"
)

// maxCookieBytes — лимит на cookies.txt (ядро тоже лимитирует ~1 МБ).
const maxCookieBytes = 1 << 20

// Mount регистрирует роуты settings ВНУТРИ уже защищённой группы
// (RequireAuth → RequireAdmin монтирует cmd/server, см. Task 5).
func (s *Service) Mount(r chi.Router) {
	r.Get("/settings", s.handlePage)
	r.Get("/settings/cookies/status", s.handleStatus)
	r.Post("/settings/cookies", s.handleUpload)
	r.Delete("/settings/cookies", s.handleDelete)
}

// handlePage рендерит полную страницу настроек. Статус берётся сразу на сервере;
// если ядро недоступно — страница всё равно рендерится (со статусом «недоступен»),
// не роняем 502 всей страницей.
func (s *Service) handlePage(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}
	st, err := s.core.GetYouTubeCookieStatus(r.Context())
	data := web.NewLayoutData(r.Context(), "Настройки")
	if err := web.SettingsPage(data, st, err != nil).Render(r.Context(), w); err != nil {
		http.Error(w, "render settings page", http.StatusInternalServerError)
	}
}

// handleStatus отдаёт htmx-фрагмент статуса (для свапа после действий).
func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}
	st, err := s.core.GetYouTubeCookieStatus(r.Context())
	if err != nil {
		writeMessage(w, http.StatusBadGateway, "Ядро недоступно, попробуйте позже")
		return
	}
	_ = web.CookieStatusFragment(st).Render(r.Context(), w)
}

// handleUpload принимает cookies.txt (multipart file) и проксирует в ядро.
func (s *Service) handleUpload(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}
	if err := r.ParseMultipartForm(maxCookieBytes); err != nil {
		writeMessage(w, http.StatusBadRequest, "Файл слишком большой или некорректен")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeMessage(w, http.StatusBadRequest, "Выберите файл cookies.txt")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxCookieBytes+1))
	if err != nil || len(content) > maxCookieBytes {
		writeMessage(w, http.StatusRequestEntityTooLarge, "Файл слишком большой")
		return
	}
	if len(content) == 0 {
		writeMessage(w, http.StatusBadRequest, "Файл пустой")
		return
	}
	st, err := s.core.PutYouTubeCookies(r.Context(), content)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	// Мутация: логируем факт без содержимого секрета (Task 5 добавит user.ID/email).
	_ = web.CookieStatusFragment(st).Render(r.Context(), w)
}

// handleDelete удаляет cookies из ядра. Подтверждение — hx-confirm на клиенте.
func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}
	st, err := s.core.DeleteYouTubeCookies(r.Context())
	if err != nil {
		writeMessage(w, http.StatusBadGateway, "Ядро недоступно, попробуйте позже")
		return
	}
	_ = web.CookieStatusFragment(st).Render(r.Context(), w)
}
