package settings

import (
	"errors"
	"net/http"

	"github.com/LectureLog/lecturelog-web/internal/coreclient"
)

// writeMessage отдаёт короткое текстовое сообщение с нужным статусом
// (по образцу internal/reader/handlers.go:88).
func writeMessage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message))
}

// writeCoreError маппит ошибку ядра при загрузке cookies в статус+текст.
func writeCoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, coreclient.ErrCookiesBadFormat):
		writeMessage(w, http.StatusBadRequest,
			"Неверный формат: нужен файл cookies.txt в формате Netscape")
	case errors.Is(err, coreclient.ErrCookiesTooLarge):
		writeMessage(w, http.StatusRequestEntityTooLarge,
			"Файл слишком большой")
	default:
		writeMessage(w, http.StatusBadGateway,
			"Ядро недоступно, попробуйте позже")
	}
}
