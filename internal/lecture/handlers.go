package lecture

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/web"
	"github.com/go-chi/chi/v5"
)

// Mount монтирует маршруты ЛК «Мои лекции» в chi-роутер.
// Все маршруты предполагают наличие auth.RequireAuth выше по стеку (в cmd/server).
// Маршруты:
//
//	GET    /lectures              — список лекций (полная страница)
//	POST   /lectures/{id}/rename  — переименование (частичный ответ: карточка)
//	POST   /lectures/{id}/visibility — переключение видимости (частичный ответ)
//	POST   /lectures/{id}/retry   — повторная обработка (частичный ответ)
//	DELETE /lectures/{id}         — удаление (пустой ответ, htmx удаляет узел)
func (s *Service) Mount(r chi.Router) {
	r.Get("/lectures", s.handleListLectures)
	r.Post("/lectures/{id}/rename", s.handleRename)
	r.Post("/lectures/{id}/visibility", s.handleSetVisibility)
	r.Post("/lectures/{id}/retry", s.handleRetry)
	r.Delete("/lectures/{id}", s.handleDelete)
}

// handleListLectures обрабатывает GET /lectures — полная страница ЛК.
func (s *Service) handleListLectures(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	lectures, err := s.List(r.Context(), user.ID)
	if err != nil {
		log.Printf("lecture: handleListLectures: %v", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	// Маппинг домен → view-модели
	vms := make([]web.LectureCardVM, len(lectures))
	for i, lec := range lectures {
		vms[i] = lectureToVM(lec)
	}

	data := web.NewLayoutData(r.Context(), "Мои лекции")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.LecturesPage(data, vms).Render(r.Context(), w); err != nil {
		log.Printf("lecture: handleListLectures render: %v", err)
	}
}

// handleRename обрабатывает POST /lectures/{id}/rename.
// Ожидает form-поле "title". Возвращает обновлённую карточку (htmx partial).
func (s *Service) handleRename(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	lectureID := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "неверный запрос", http.StatusBadRequest)
		return
	}
	title := r.FormValue("title")

	lec, err := s.Rename(r.Context(), lectureID, user.ID, title)
	if err != nil {
		if isUserError(err) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if err == ErrNotFound {
			http.Error(w, "не найдена", http.StatusNotFound)
			return
		}
		log.Printf("lecture: handleRename %s: %v", lectureID, err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	// Частичный ответ: обновлённая карточка
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.LectureCard(lectureToVM(lec)).Render(r.Context(), w); err != nil {
		log.Printf("lecture: handleRename render: %v", err)
	}
}

// handleSetVisibility обрабатывает POST /lectures/{id}/visibility.
// Ожидает form-поле "visibility" (private|public). Возвращает обновлённую карточку.
func (s *Service) handleSetVisibility(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	lectureID := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "неверный запрос", http.StatusBadRequest)
		return
	}
	vis := Visibility(r.FormValue("visibility"))

	lec, err := s.SetVisibility(r.Context(), lectureID, user.ID, vis)
	if err != nil {
		if err == ErrNotReady {
			http.Error(w, "публикация возможна только для готовой лекции", http.StatusConflict)
			return
		}
		if err == ErrNotFound {
			http.Error(w, "не найдена", http.StatusNotFound)
			return
		}
		log.Printf("lecture: handleSetVisibility %s: %v", lectureID, err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.LectureCard(lectureToVM(lec)).Render(r.Context(), w); err != nil {
		log.Printf("lecture: handleSetVisibility render: %v", err)
	}
}

// handleRetry обрабатывает POST /lectures/{id}/retry.
// Возвращает обновлённую карточку (статус сменится на processing).
func (s *Service) handleRetry(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	lectureID := chi.URLParam(r, "id")

	lec, err := s.Retry(r.Context(), lectureID, user.ID)
	if err != nil {
		if err == ErrNotFailed {
			http.Error(w, "retry доступен только для failed-лекций", http.StatusConflict)
			return
		}
		if err == ErrNoRetrySource {
			http.Error(w, "нет источника для повтора", http.StatusConflict)
			return
		}
		if err == ErrNotFound {
			http.Error(w, "не найдена", http.StatusNotFound)
			return
		}
		log.Printf("lecture: handleRetry %s: %v", lectureID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.LectureCard(lectureToVM(lec)).Render(r.Context(), w); err != nil {
		log.Printf("lecture: handleRetry render: %v", err)
	}
}

// handleDelete обрабатывает DELETE /lectures/{id}.
// Возвращает пустой 200 (htmx удаляет карточку через hx-swap="outerHTML").
func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	lectureID := chi.URLParam(r, "id")

	if err := s.Delete(r.Context(), lectureID, user.ID); err != nil {
		if err == ErrNotFound {
			http.Error(w, "не найдена", http.StatusNotFound)
			return
		}
		log.Printf("lecture: handleDelete %s: %v", lectureID, err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	// Пустой 200 — htmx удаляет DOM-элемент через hx-swap="outerHTML"
	w.WriteHeader(http.StatusOK)
}

// ─── Маппинг Lecture → LectureCardVM ─────────────────────────────────────────

// lectureToVM конвертирует доменную Lecture в view-модель для templ-шаблонов.
func lectureToVM(lec Lecture) web.LectureCardVM {
	return web.LectureCardVM{
		ID:          lec.ID,
		Title:       lec.Title,
		Status:      string(lec.Status),
		StatusLabel: statusLabel(lec.Status),
		Visibility:  string(lec.Visibility),
		SourceKind:  lec.SourceKind,
		CanPublish:  lec.Status == StatusReady,
		CanRetry:    lec.Status == StatusFailed && (lec.S3Key != "" || lec.VideoURL != ""),
		ErrorText:   mapErrorCode(lec.ErrorCode),
		// Карточка из БД прогресса не знает: для processing покажет «Обработка · 0%»
		// с пустым баром до первого поллинга — это приемлемо.
		ProgressPct: 0,
		StageLabel:  "",
		UpdatedAt:   formatDate(lec.UpdatedAt),
	}
}

// statusLabel возвращает локализованную метку статуса.
func statusLabel(s Status) string {
	switch s {
	case StatusProcessing:
		return "Обработка"
	case StatusReady:
		return "Готово"
	case StatusFailed:
		return "Ошибка"
	default:
		return string(s)
	}
}

// mapErrorCode возвращает человекочитаемый текст ошибки по коду.
// Минимальный каталог; полный каталог — долг §11.
func mapErrorCode(code string) string {
	switch code {
	case "processing_error":
		return "Ошибка обработки"
	case "download_error":
		return "Ошибка загрузки"
	case "transcription_error":
		return "Ошибка распознавания речи"
	case "cookies_invalid":
		return "Cookies YouTube устарели — обратитесь к администратору"
	default:
		if code != "" {
			return fmt.Sprintf("Ошибка: %s", code)
		}
		return ""
	}
}

// formatDate форматирует время для отображения (tabular-nums).
func formatDate(t time.Time) string {
	return t.Format("2 Jan 2006")
}

// isUserError проверяет, является ли ошибка пользовательской (валидационной).
func isUserError(err error) bool {
	// Ошибки домена (кроме not-found) считаем пользовательскими
	return err != ErrNotFound && err != nil &&
		err != ErrNotFailed && err != ErrNotReady && err != ErrNoRetrySource
}
