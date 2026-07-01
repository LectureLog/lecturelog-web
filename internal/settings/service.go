package settings

import (
	"context"

	"github.com/LectureLog/lecturelog-web/internal/coreclient"
)

// Core — нужная сервису часть API ядра (для тестируемости через фейк).
type Core interface {
	GetYouTubeCookieStatus(ctx context.Context) (coreclient.CookieStatus, error)
	PutYouTubeCookies(ctx context.Context, content []byte) (coreclient.CookieStatus, error)
	DeleteYouTubeCookies(ctx context.Context) (coreclient.CookieStatus, error)
}

// Service — сервис настроек (сейчас единственная секция — YouTube cookies).
type Service struct {
	core Core
}

// NewService создаёт сервис настроек поверх клиента ядра.
func NewService(core Core) *Service {
	return &Service{core: core}
}
