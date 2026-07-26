package upload

import (
	"errors"
	"io"
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
	maxSlidesBytes int64 = 100 << 20
)

var (
	ErrUnsupportedMedia  = errors.New("неподдерживаемый тип медиа")
	ErrEmptyFile         = errors.New("пустой файл")
	ErrTooLarge          = errors.New("файл слишком большой")
	ErrEmptyFilename     = errors.New("пустое имя файла")
	ErrInvalidURL        = errors.New("некорректная ссылка")
	ErrMediaMismatch     = errors.New("тип медиа не соответствует расширению файла")
	ErrSlidesRequired    = errors.New("выберите файл презентации")
	ErrUnsupportedSlides = errors.New("поддерживаются только PDF и PPTX")
	ErrSlidesTooLarge    = errors.New("презентация слишком большая")
)

type SlidesUpload struct {
	Filename string
	Content  io.Reader
}

func ValidateSlidesMeta(filename string, size int64) error {
	if filename == "" {
		return ErrSlidesRequired
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf", ".pptx":
	default:
		return ErrUnsupportedSlides
	}
	if size <= 0 {
		return ErrEmptyFile
	}
	if size > maxSlidesBytes {
		return ErrSlidesTooLarge
	}
	return nil
}

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

func ValidateFileMeta(filename, mime string, size int64) error {
	if filename == "" {
		return ErrEmptyFilename
	}
	media, ok := DetectMedia(filename)
	if !ok {
		return ErrUnsupportedMedia
	}
	// MIME от клиента недоверенный, пустой MIME допускается: некоторые клиенты не шлют Content-Type.
	if mime != "" && !strings.HasPrefix(mime, media+"/") {
		return ErrMediaMismatch
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
	case "x.com", "www.x.com", "mobile.x.com", "m.x.com":
		return nil
	case "twitter.com", "www.twitter.com", "mobile.twitter.com", "m.twitter.com":
		return nil
	default:
		return ErrInvalidURL
	}
}
