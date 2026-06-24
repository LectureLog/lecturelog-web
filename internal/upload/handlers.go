package upload

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/go-chi/chi/v5"
)

// Mount монтирует API-маршруты загрузки.
func (s *Service) Mount(r chi.Router) {
	r.Post("/upload/presign", s.handlePresign)
	r.Post("/upload/confirm", s.handleConfirm)
	r.Post("/upload/youtube", s.handleYouTube)
}

func (s *Service) handlePresign(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	var in struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
		MIME     string `json:"mime"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&in); err != nil {
		http.Error(w, "неверный запрос", http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	res, err := s.PrepareFileUpload(r.Context(), user.ID, in.Filename, in.Size, in.MIME)
	if err != nil {
		status := uploadErrStatus(err)
		if status == http.StatusInternalServerError {
			log.Printf("upload: handlePresign: %v", err)
			http.Error(w, "внутренняя ошибка", status)
			return
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(struct {
		Token     string `json:"token"`
		PutURL    string `json:"put_url"`
		S3Key     string `json:"s3_key"`
		Media     string `json:"media"`
		Title     string `json:"title"`
		ExpiresIn int    `json:"expires_in"`
	}{
		Token:     res.Token,
		PutURL:    res.PutURL,
		S3Key:     res.S3Key,
		Media:     res.Media,
		Title:     res.Title,
		ExpiresIn: res.ExpiresIn,
	}); err != nil {
		log.Printf("upload: handlePresign encode: %v", err)
	}
}

func (s *Service) handleConfirm(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "неверный запрос", http.StatusBadRequest)
		return
	}

	_, err := s.ConfirmFileUpload(r.Context(), user.ID, ConfirmInput{
		Token:         r.FormValue("token"),
		S3Key:         r.FormValue("s3_key"),
		Title:         r.FormValue("title"),
		HasPDF:        parseUploadCheckbox(r.FormValue("has_pdf")),
		ExtractSlides: parseUploadCheckbox(r.FormValue("extract_slides")),
	})
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		// TODO: показать отдельный 502-экран, когда сервис обработки недоступен.
		log.Printf("upload: handleConfirm: %v", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/lectures")
	w.WriteHeader(http.StatusOK)
}

func parseUploadCheckbox(v string) bool {
	return v == "on" || v == "true" || v == "1"
}

func (s *Service) handleYouTube(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Error(w, "требуется авторизация", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "неверный запрос", http.StatusBadRequest)
		return
	}

	_, err := s.CreateYouTube(r.Context(), user.ID, YouTubeInput{
		URL:           r.FormValue("url"),
		Title:         r.FormValue("title"),
		HasPDF:        parseUploadCheckbox(r.FormValue("has_pdf")),
		ExtractSlides: parseUploadCheckbox(r.FormValue("extract_slides")),
	})
	if err != nil {
		if errors.Is(err, ErrInvalidURL) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("upload: handleYouTube: %v", err)
		http.Error(w, "внутренняя ошибка", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/lectures")
	w.WriteHeader(http.StatusOK)
}

func uploadErrStatus(err error) int {
	if errors.Is(err, ErrForbidden) {
		return http.StatusForbidden
	}
	if errors.Is(err, ErrUnsupportedMedia) ||
		errors.Is(err, ErrEmptyFile) ||
		errors.Is(err, ErrTooLarge) ||
		errors.Is(err, ErrEmptyFilename) ||
		errors.Is(err, ErrMediaMismatch) ||
		errors.Is(err, ErrInvalidURL) {
		return http.StatusUnprocessableEntity
	}
	return http.StatusInternalServerError
}
