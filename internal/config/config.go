// Package config — единая точка чтения и валидации конфигурации платформы LectureLog.
// Конфигурация читается из переменных окружения через инъектируемый геттер;
// прямое чтение os.Getenv/файлов здесь не выполняется (детерминизм тестов).
package config

import (
	"time"

	"github.com/LectureLog/lecturelog-web/internal/coreclient"
)

// OAuth содержит параметры Google OAuth 2.0.
type OAuth struct {
	// ClientID — идентификатор приложения OAuth (GOOGLE_CLIENT_ID).
	ClientID string
	// ClientSecret — секрет приложения OAuth (GOOGLE_CLIENT_SECRET).
	ClientSecret string
	// CallbackURL — URL обратного вызова после авторизации (PLATFORM_CALLBACK_URL).
	CallbackURL string
}

// CoreMinIO содержит параметры доступа к MinIO ядра.
type CoreMinIO struct {
	// Endpoint — адрес MinIO ядра (CORE_MINIO_ENDPOINT).
	Endpoint string
	// AccessKey — ключ доступа MinIO (CORE_MINIO_ACCESS_KEY).
	AccessKey string
	// SecretKey — секретный ключ MinIO (CORE_MINIO_SECRET_KEY).
	SecretKey string
	// Bucket — имя бакета MinIO (CORE_MINIO_BUCKET).
	Bucket string
	// UseSSL — использовать TLS при подключении (CORE_MINIO_USE_SSL, дефолт false).
	UseSSL bool
}

// Config — конфигурация всей платформы LectureLog.
// Это единственная структура, владеющая значениями env;
// всем остальным компонентам передаются нужные поля или фабричные методы.
type Config struct {
	// OAuth — параметры Google OAuth 2.0.
	OAuth OAuth

	// WebhookSecret — HMAC-секрет для проверки входящих вебхуков ядра
	// (LECTURELOG_WEBHOOK_SECRET). Обязателен: пустое значение — дыра безопасности
	// (VerifyWebhookSignature при пустом ключе примет любую подпись, долг B1).
	WebhookSecret string

	// PlatformDBDSN — строка подключения к Postgres платформы (PLATFORM_DB_DSN).
	PlatformDBDSN string

	// CoreAPIBaseURL — базовый URL ядра без /api/v1, напр. "http://core:8000"
	// (CORE_API_BASE_URL). Используется только через фабрику CoreClient().
	CoreAPIBaseURL string

	// CoreMinIO — параметры MinIO ядра.
	CoreMinIO CoreMinIO

	// PresignedTTL — TTL presigned-URL для загрузки файлов (PRESIGNED_TTL, дефолт 24h).
	PresignedTTL time.Duration

	// SessionTTL — TTL пользовательской сессии (SESSION_TTL, дефолт 720h = 30 дней).
	SessionTTL time.Duration
}

// CoreClient возвращает конфигурацию клиента ядра.
// Config является единственным местом чтения env; coreclient получает уже
// готовое значение BaseURL, а не читает окружение самостоятельно (Р5).
func (c *Config) CoreClient() coreclient.Config {
	return coreclient.Config{BaseURL: c.CoreAPIBaseURL}
}

// Load читает конфигурацию из переменных окружения через getenv.
// Функция агрегирует ВСЕ ошибки обязательных и невалидных ключей в одну ошибку
// (Р3) вместо остановки на первой — чтобы оператор мог исправить всё сразу.
// Временная заглушка — реализация в load.go.
func Load(getenv func(string) string) (*Config, error) {
	return nil, nil
}
