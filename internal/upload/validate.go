package upload

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

type SourceMode string

const (
	ModeFile SourceMode = "file"
	ModeURL  SourceMode = "url"

	// Грубый потолок беты.
	maxUploadBytes int64 = 5 << 30
)

var (
	ErrUnsupportedMedia = errors.New("неподдерживаемый тип медиа")
	ErrEmptyFile        = errors.New("пустой файл")
	ErrTooLarge         = errors.New("файл слишком большой")
	ErrEmptyFilename    = errors.New("пустое имя файла")
	ErrInvalidURL       = errors.New("некорректная ссылка")
)

func DetectMedia(filename string) (media string, ok bool) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp4", ".mov", ".mkv", ".webm", ".avi":
		return "video", true
	case ".mp3", ".wav", ".m4a", ".aac", ".ogg", ".flac":
		return "audio", true
	default:
		return "", false
	}
}

func ValidateFileMeta(filename string, size int64) error {
	if filename == "" {
		return ErrEmptyFilename
	}
	if _, ok := DetectMedia(filename); !ok {
		return ErrUnsupportedMedia
	}
	if size <= 0 {
		return ErrEmptyFile
	}
	if size > maxUploadBytes {
		return ErrTooLarge
	}
	return nil
}

func ValidateYouTubeURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ErrInvalidURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ErrInvalidURL
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "youtube.com", "www.youtube.com", "youtu.be", "m.youtube.com":
		return nil
	default:
		return ErrInvalidURL
	}
}
