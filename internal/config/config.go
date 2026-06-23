// Package config — единая точка чтения и валидации конфигурации платформы LectureLog.
// Конфигурация читается из переменных окружения через инъектируемый геттер;
// прямое чтение os.Getenv/файлов здесь не выполняется (детерминизм тестов).
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
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

// Load читает конфигурацию платформы из переменных окружения через getenv.
//
// Функция является чистой (без побочных эффектов): она не вызывает os.Getenv,
// не читает файлы и не обращается к глобальному состоянию — детерминизм тестов.
//
// Алгоритм агрегации ошибок (Р3):
//   - Сначала собираются все пустые/незаданные обязательные ключи в срез missing
//     (в порядке объявления, чтобы сообщение об ошибке было стабильным).
//   - Затем выполняется парсинг опциональных типов; ошибки добавляются в errList.
//   - При непустом missing ИЛИ непустом errList возвращается одна агрегированная
//     ошибка; cfg == nil.
func Load(getenv func(string) string) (*Config, error) {
	// ---- обязательные строковые ключи (порядок важен — стабильность ошибки) ----
	type requiredField struct {
		key  string
		dest *string
	}

	cfg := &Config{}

	required := []requiredField{
		{"GOOGLE_CLIENT_ID", &cfg.OAuth.ClientID},
		{"GOOGLE_CLIENT_SECRET", &cfg.OAuth.ClientSecret},
		{"PLATFORM_CALLBACK_URL", &cfg.OAuth.CallbackURL},
		// LECTURELOG_WEBHOOK_SECRET — обязателен явно (долг B1, Р4):
		// VerifyWebhookSignature при пустом ключе принимает любой HMAC,
		// что открывает молчаливую дыру в C1-sync.
		{"LECTURELOG_WEBHOOK_SECRET", &cfg.WebhookSecret},
		{"PLATFORM_DB_DSN", &cfg.PlatformDBDSN},
		{"CORE_API_BASE_URL", &cfg.CoreAPIBaseURL},
		{"CORE_MINIO_ENDPOINT", &cfg.CoreMinIO.Endpoint},
		{"CORE_MINIO_ACCESS_KEY", &cfg.CoreMinIO.AccessKey},
		{"CORE_MINIO_SECRET_KEY", &cfg.CoreMinIO.SecretKey},
		{"CORE_MINIO_BUCKET", &cfg.CoreMinIO.Bucket},
	}

	// Накапливаем имена пустых обязательных ключей
	var missing []string
	for _, f := range required {
		val := getenv(f.key)
		if val == "" {
			missing = append(missing, f.key)
		} else {
			*f.dest = val
		}
	}

	// ---- опциональные ключи с парсингом ----
	var parseErrors []string

	// CORE_MINIO_USE_SSL — bool, дефолт false
	if raw := getenv("CORE_MINIO_USE_SSL"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			parseErrors = append(parseErrors,
				fmt.Sprintf("CORE_MINIO_USE_SSL: неверное булево значение %q: %v", raw, err))
		} else {
			cfg.CoreMinIO.UseSSL = v
		}
	}
	// при пустом raw UseSSL остаётся false (zero-value)

	// PRESIGNED_TTL — time.Duration, дефолт 24h
	cfg.PresignedTTL = 24 * time.Hour
	if raw := getenv("PRESIGNED_TTL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			parseErrors = append(parseErrors,
				fmt.Sprintf("PRESIGNED_TTL: неверная длительность %q: %v", raw, err))
		} else {
			cfg.PresignedTTL = d
		}
	}

	// SESSION_TTL — time.Duration, дефолт 720h (30 дней)
	cfg.SessionTTL = 720 * time.Hour
	if raw := getenv("SESSION_TTL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			parseErrors = append(parseErrors,
				fmt.Sprintf("SESSION_TTL: неверная длительность %q: %v", raw, err))
		} else {
			cfg.SessionTTL = d
		}
	}

	// ---- агрегация ошибок (Р3) ----
	if len(missing) > 0 || len(parseErrors) > 0 {
		var parts []string
		if len(missing) > 0 {
			parts = append(parts,
				"config: отсутствуют обязательные переменные окружения: "+
					strings.Join(missing, ", "))
		}
		parts = append(parts, parseErrors...)
		return nil, errors.New(strings.Join(parts, "; "))
	}

	return cfg, nil
}
