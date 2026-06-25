package syncsvc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/coreclient"
	"github.com/LectureLog/lecturelog-web/internal/web"
	"github.com/go-chi/chi/v5"
)

const (
	webhookSignatureHeader = "X-Webhook-Signature"

	// максимальный размер тела вебхука (1 МБ)
	maxWebhookBodyBytes = 1 << 20
)

// HandleWebhook принимает подписанный вебхук ядра со сменой статуса задачи.
func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "метод не поддержан", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "тело вебхука слишком большое", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "не удалось прочитать тело вебхука", http.StatusBadRequest)
		return
	}

	if !coreclient.VerifyWebhookSignature(body, r.Header.Get(webhookSignatureHeader), s.webhookSecret) {
		http.Error(w, "неверная подпись", http.StatusUnauthorized)
		return
	}

	var payload coreclient.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "неверный JSON", http.StatusBadRequest)
		return
	}
	if !isWebhookStatus(payload.Status) {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	errorCode := ""
	if payload.ErrorCode != nil {
		errorCode = *payload.ErrorCode
	}

	affected, err := s.repo.UpdateStatusConditional(r.Context(), payload.TaskID, payload.Status, errorCode)
	if err != nil {
		log.Printf("syncsvc: webhook update %s: %v", payload.TaskID, err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}
	if affected > 0 && isTerminalStatus(payload.Status) {
		// Здесь будет email-уведомление о готовой или упавшей лекции.
		if s.onTerminalWebhook != nil {
			s.onTerminalWebhook()
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandlePollStatus возвращает htmx-фрагмент карточки с актуальным статусом лекции.
func (s *Service) HandlePollStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "метод не поддержан", http.StatusMethodNotAllowed)
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	lectureID := chi.URLParam(r, "id")
	lec, err := s.repo.FindByID(r.Context(), lectureID)
	if err != nil {
		log.Printf("syncsvc: find lecture %s: %v", lectureID, err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}
	if lec == nil || lec.OwnerID != user.ID {
		http.Error(w, "не найдена", http.StatusNotFound)
		return
	}

	renderLecture := *lec
	if !isTerminalStatus(lec.Status) && lec.CoreTaskID != "" && s.core != nil {
		if progress, err := s.core.GetTaskStatus(r.Context(), lec.CoreTaskID); err == nil && progress != nil {
			renderLecture = mergeProgress(*lec, *progress)
			if isTerminalStatus(progress.Status) {
				if _, err := s.repo.UpdateStatusConditional(r.Context(), lec.CoreTaskID, progress.Status, progress.ErrorCode); err != nil {
					log.Printf("syncsvc: poll fallback update %s: %v", lec.CoreTaskID, err)
				}
			}
		} else if err != nil && !errors.Is(err, ErrTaskNotFound) {
			log.Printf("syncsvc: core status %s: %v", lec.CoreTaskID, err)
		}
	}

	renderCard(w, r, renderLecture)
}

func renderCard(w http.ResponseWriter, r *http.Request, lec LectureView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.LectureCard(lectureToVM(lec)).Render(r.Context(), w); err != nil {
		log.Printf("syncsvc: render card %s: %v", lec.ID, err)
	}
}

func mergeProgress(lec LectureView, progress TaskProgress) LectureView {
	if progress.Status != "" {
		lec.Status = progress.Status
	}
	if progress.ErrorCode != "" {
		lec.ErrorCode = progress.ErrorCode
	}
	return lec
}

func isTerminalStatus(status string) bool {
	return status == "ready" || status == "failed"
}

func isWebhookStatus(status string) bool {
	switch status {
	case "processing", "ready", "failed":
		return true
	default:
		return false
	}
}

func lectureToVM(lec LectureView) web.LectureCardVM {
	return web.LectureCardVM{
		ID:          lec.ID,
		Title:       lec.Title,
		Status:      lec.Status,
		StatusLabel: statusLabel(lec.Status),
		Visibility:  lec.Visibility,
		SourceKind:  lec.SourceKind,
		CanPublish:  lec.Status == "ready",
		CanRetry:    lec.Status == "failed",
		ErrorText:   mapErrorCode(lec.ErrorCode),
		UpdatedAt:   formatDate(lec.UpdatedAt),
	}
}

func statusLabel(status string) string {
	switch status {
	case "processing":
		return "Обработка"
	case "ready":
		return "Готово"
	case "failed":
		return "Ошибка"
	default:
		return status
	}
}

func mapErrorCode(code string) string {
	switch code {
	case "rate_limit":
		return "Превышен лимит обработки"
	case "bad_input":
		return "Некорректный источник"
	case "internal":
		return "Внутренняя ошибка обработки"
	case "processing_error":
		return "Ошибка обработки"
	case "download_error":
		return "Ошибка загрузки"
	case "transcription_error":
		return "Ошибка распознавания речи"
	default:
		if code != "" {
			return fmt.Sprintf("Ошибка: %s", code)
		}
		return ""
	}
}

func formatDate(t time.Time) string {
	return t.Format("2 Jan 2006")
}
