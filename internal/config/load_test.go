package config

import (
	"strings"
	"testing"
	"time"
)

// fullEnv возвращает map со всеми обязательными ключами и валидными значениями.
// Опциональные ключи (TTL, UseSSL) не включены — проверяем дефолты.
func fullEnv() map[string]string {
	return map[string]string{
		"GOOGLE_CLIENT_ID":           "gid-test",
		"GOOGLE_CLIENT_SECRET":       "gsecret-test",
		"PLATFORM_CALLBACK_URL":      "http://localhost:8080/auth/callback",
		"LECTURELOG_WEBHOOK_SECRET":  "super-secret-hmac-key",
		"PLATFORM_DB_DSN":            "postgres://user:pass@localhost/db",
		"CORE_API_BASE_URL":          "http://core:8000",
		"CORE_MINIO_ENDPOINT":        "minio:9000",
		"CORE_MINIO_ACCESS_KEY":      "minio-access",
		"CORE_MINIO_SECRET_KEY":      "minio-secret",
		"CORE_MINIO_BUCKET":          "lectures",
	}
}

// makeGetenv строит инъектируемый геттер из map.
func makeGetenv(m map[string]string) func(string) string {
	return func(key string) string {
		return m[key]
	}
}

// --- Фаза 2: успешная загрузка из полного окружения ---

// TestLoad_AllRequiredPresent проверяет, что Load корректно заполняет все поля
// при полном наборе обязательных ключей и возвращает дефолты для опциональных.
func TestLoad_AllRequiredPresent(t *testing.T) {
	cfg, err := Load(makeGetenv(fullEnv()))
	if err != nil {
		t.Fatalf("ожидали nil-ошибку, получили: %v", err)
	}
	if cfg == nil {
		t.Fatal("ожидали не-nil *Config")
	}

	// Проверяем все поля OAuth
	if cfg.OAuth.ClientID != "gid-test" {
		t.Errorf("OAuth.ClientID: хотели %q, получили %q", "gid-test", cfg.OAuth.ClientID)
	}
	if cfg.OAuth.ClientSecret != "gsecret-test" {
		t.Errorf("OAuth.ClientSecret: хотели %q, получили %q", "gsecret-test", cfg.OAuth.ClientSecret)
	}
	if cfg.OAuth.CallbackURL != "http://localhost:8080/auth/callback" {
		t.Errorf("OAuth.CallbackURL: хотели %q, получили %q", "http://localhost:8080/auth/callback", cfg.OAuth.CallbackURL)
	}

	// WebhookSecret
	if cfg.WebhookSecret != "super-secret-hmac-key" {
		t.Errorf("WebhookSecret: хотели %q, получили %q", "super-secret-hmac-key", cfg.WebhookSecret)
	}

	// Остальные строковые поля
	if cfg.PlatformDBDSN != "postgres://user:pass@localhost/db" {
		t.Errorf("PlatformDBDSN: хотели %q, получили %q", "postgres://user:pass@localhost/db", cfg.PlatformDBDSN)
	}
	if cfg.CoreAPIBaseURL != "http://core:8000" {
		t.Errorf("CoreAPIBaseURL: хотели %q, получили %q", "http://core:8000", cfg.CoreAPIBaseURL)
	}

	// CoreMinIO
	if cfg.CoreMinIO.Endpoint != "minio:9000" {
		t.Errorf("CoreMinIO.Endpoint: хотели %q, получили %q", "minio:9000", cfg.CoreMinIO.Endpoint)
	}
	if cfg.CoreMinIO.AccessKey != "minio-access" {
		t.Errorf("CoreMinIO.AccessKey: хотели %q, получили %q", "minio-access", cfg.CoreMinIO.AccessKey)
	}
	if cfg.CoreMinIO.SecretKey != "minio-secret" {
		t.Errorf("CoreMinIO.SecretKey: хотели %q, получили %q", "minio-secret", cfg.CoreMinIO.SecretKey)
	}
	if cfg.CoreMinIO.Bucket != "lectures" {
		t.Errorf("CoreMinIO.Bucket: хотели %q, получили %q", "lectures", cfg.CoreMinIO.Bucket)
	}

	// Дефолт UseSSL = false
	if cfg.CoreMinIO.UseSSL != false {
		t.Errorf("CoreMinIO.UseSSL: хотели false (дефолт), получили true")
	}

	// Дефолт PresignedTTL = 24h
	if cfg.PresignedTTL != 24*time.Hour {
		t.Errorf("PresignedTTL: хотели %v (дефолт), получили %v", 24*time.Hour, cfg.PresignedTTL)
	}

	// Дефолт SessionTTL = 720h (30 дней)
	if cfg.SessionTTL != 720*time.Hour {
		t.Errorf("SessionTTL: хотели %v (дефолт), получили %v", 720*time.Hour, cfg.SessionTTL)
	}
}

// --- Фаза 3: fail-fast на пустом webhook-секрете (долг B1) ---

// TestLoad_EmptyWebhookSecretFails проверяет, что пустой или незаданный
// LECTURELOG_WEBHOOK_SECRET вызывает ошибку загрузки.
//
// Долг B1: VerifyWebhookSignature при пустом секрете принимает любой HMAC
// (вычисляет по ключу ""), что открывает дыру безопасности в C1-sync.
// Валидация — здесь, в config, а не в coreclient (Р4).
func TestLoad_EmptyWebhookSecretFails(t *testing.T) {
	t.Run("пустая строка", func(t *testing.T) {
		env := fullEnv()
		env["LECTURELOG_WEBHOOK_SECRET"] = ""
		cfg, err := Load(makeGetenv(env))
		if err == nil {
			t.Fatal("ожидали ошибку при пустом LECTURELOG_WEBHOOK_SECRET, получили nil")
		}
		if cfg != nil {
			t.Fatal("ожидали nil *Config при ошибке, получили не-nil")
		}
		if !strings.Contains(err.Error(), "LECTURELOG_WEBHOOK_SECRET") {
			t.Errorf("ошибка должна упоминать LECTURELOG_WEBHOOK_SECRET, получили: %v", err)
		}
	})

	t.Run("ключ не задан", func(t *testing.T) {
		env := fullEnv()
		delete(env, "LECTURELOG_WEBHOOK_SECRET")
		cfg, err := Load(makeGetenv(env))
		if err == nil {
			t.Fatal("ожидали ошибку при отсутствии LECTURELOG_WEBHOOK_SECRET, получили nil")
		}
		if cfg != nil {
			t.Fatal("ожидали nil *Config при ошибке, получили не-nil")
		}
		if !strings.Contains(err.Error(), "LECTURELOG_WEBHOOK_SECRET") {
			t.Errorf("ошибка должна упоминать LECTURELOG_WEBHOOK_SECRET, получили: %v", err)
		}
	})
}

// --- Фаза 4: агрегированная ошибка по всем недостающим обязательным ---

// TestLoad_AggregatesAllMissing проверяет, что при полностью пустом окружении
// ошибка содержит имена ВСЕХ обязательных ключей.
func TestLoad_AggregatesAllMissing(t *testing.T) {
	cfg, err := Load(makeGetenv(map[string]string{}))
	if err == nil {
		t.Fatal("ожидали ошибку при пустом окружении, получили nil")
	}
	if cfg != nil {
		t.Fatal("ожидали nil *Config при ошибке, получили не-nil")
	}

	// Все обязательные ключи должны присутствовать в сообщении
	requiredKeys := []string{
		"GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET",
		"PLATFORM_CALLBACK_URL",
		"LECTURELOG_WEBHOOK_SECRET",
		"PLATFORM_DB_DSN",
		"CORE_API_BASE_URL",
		"CORE_MINIO_ENDPOINT",
		"CORE_MINIO_ACCESS_KEY",
		"CORE_MINIO_SECRET_KEY",
		"CORE_MINIO_BUCKET",
	}

	errMsg := err.Error()
	for _, key := range requiredKeys {
		if !strings.Contains(errMsg, key) {
			t.Errorf("ожидали ключ %q в ошибке, но его нет: %v", key, errMsg)
		}
	}
}

// TestLoad_PartialMissing проверяет, что при частично заданном окружении
// ошибка содержит только недостающие ключи и в стабильном порядке.
func TestLoad_PartialMissing(t *testing.T) {
	// Задаём только часть ключей
	env := map[string]string{
		"GOOGLE_CLIENT_ID":          "gid-test",
		"GOOGLE_CLIENT_SECRET":      "gsecret-test",
		"PLATFORM_CALLBACK_URL":     "http://localhost/cb",
		"LECTURELOG_WEBHOOK_SECRET": "secret",
		"PLATFORM_DB_DSN":           "postgres://...",
		// CORE_API_BASE_URL, CORE_MINIO_* — не заданы
	}

	cfg, err := Load(makeGetenv(env))
	if err == nil {
		t.Fatal("ожидали ошибку при неполном окружении, получили nil")
	}
	if cfg != nil {
		t.Fatal("ожидали nil *Config при ошибке, получили не-nil")
	}

	errMsg := err.Error()

	// Эти ключи должны быть в ошибке
	missingKeys := []string{
		"CORE_API_BASE_URL",
		"CORE_MINIO_ENDPOINT",
		"CORE_MINIO_ACCESS_KEY",
		"CORE_MINIO_SECRET_KEY",
		"CORE_MINIO_BUCKET",
	}
	for _, key := range missingKeys {
		if !strings.Contains(errMsg, key) {
			t.Errorf("ожидали ключ %q в ошибке (недостающий), но его нет: %v", key, errMsg)
		}
	}

	// Заданные ключи НЕ должны быть в ошибке
	presentKeys := []string{
		"GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET",
		"PLATFORM_CALLBACK_URL",
		"LECTURELOG_WEBHOOK_SECRET",
		"PLATFORM_DB_DSN",
	}
	for _, key := range presentKeys {
		if strings.Contains(errMsg, key) {
			t.Errorf("ключ %q задан, но присутствует в ошибке: %v", key, errMsg)
		}
	}
}

// --- Фаза 5: парсинг и валидация опциональных типов ---

// TestLoad_OptionalOverrides проверяет, что опциональные ключи переопределяют дефолты.
func TestLoad_OptionalOverrides(t *testing.T) {
	env := fullEnv()
	env["PRESIGNED_TTL"] = "1h"
	env["SESSION_TTL"] = "48h"
	env["CORE_MINIO_USE_SSL"] = "true"

	cfg, err := Load(makeGetenv(env))
	if err != nil {
		t.Fatalf("ожидали nil-ошибку, получили: %v", err)
	}
	if cfg == nil {
		t.Fatal("ожидали не-nil *Config")
	}

	if cfg.PresignedTTL != time.Hour {
		t.Errorf("PresignedTTL: хотели %v, получили %v", time.Hour, cfg.PresignedTTL)
	}
	if cfg.SessionTTL != 48*time.Hour {
		t.Errorf("SessionTTL: хотели %v, получили %v", 48*time.Hour, cfg.SessionTTL)
	}
	if !cfg.CoreMinIO.UseSSL {
		t.Errorf("CoreMinIO.UseSSL: хотели true, получили false")
	}
}

// TestLoad_InvalidDurationFails проверяет, что невалидный PRESIGNED_TTL
// возвращает ошибку с упоминанием ключа.
func TestLoad_InvalidDurationFails(t *testing.T) {
	env := fullEnv()
	env["PRESIGNED_TTL"] = "abc"

	cfg, err := Load(makeGetenv(env))
	if err == nil {
		t.Fatal("ожидали ошибку при невалидном PRESIGNED_TTL, получили nil")
	}
	if cfg != nil {
		t.Fatal("ожидали nil *Config при ошибке, получили не-nil")
	}
	if !strings.Contains(err.Error(), "PRESIGNED_TTL") {
		t.Errorf("ошибка должна упоминать PRESIGNED_TTL, получили: %v", err)
	}
}

// TestLoad_InvalidSessionTTLFails проверяет невалидный SESSION_TTL.
func TestLoad_InvalidSessionTTLFails(t *testing.T) {
	env := fullEnv()
	env["SESSION_TTL"] = "not-a-duration"

	cfg, err := Load(makeGetenv(env))
	if err == nil {
		t.Fatal("ожидали ошибку при невалидном SESSION_TTL, получили nil")
	}
	if cfg != nil {
		t.Fatal("ожидали nil *Config при ошибке, получили не-nil")
	}
	if !strings.Contains(err.Error(), "SESSION_TTL") {
		t.Errorf("ошибка должна упоминать SESSION_TTL, получили: %v", err)
	}
}

// TestLoad_InvalidBoolFails проверяет невалидный CORE_MINIO_USE_SSL.
func TestLoad_InvalidBoolFails(t *testing.T) {
	env := fullEnv()
	env["CORE_MINIO_USE_SSL"] = "yes-please"

	cfg, err := Load(makeGetenv(env))
	if err == nil {
		t.Fatal("ожидали ошибку при невалидном CORE_MINIO_USE_SSL, получили nil")
	}
	if cfg != nil {
		t.Fatal("ожидали nil *Config при ошибке, получили не-nil")
	}
	if !strings.Contains(err.Error(), "CORE_MINIO_USE_SSL") {
		t.Errorf("ошибка должна упоминать CORE_MINIO_USE_SSL, получили: %v", err)
	}
}

// --- Фаза 6: фабрика coreclient.Config ---

// TestConfig_CoreClientFactory проверяет, что фабрика CoreClient() корректно
// передаёт BaseURL без дублирования env-поля и без /api/v1.
func TestConfig_CoreClientFactory(t *testing.T) {
	env := fullEnv()
	env["CORE_API_BASE_URL"] = "http://core:8000"

	cfg, err := Load(makeGetenv(env))
	if err != nil {
		t.Fatalf("ожидали nil-ошибку, получили: %v", err)
	}
	if cfg == nil {
		t.Fatal("ожидали не-nil *Config")
	}

	cc := cfg.CoreClient()
	if cc.BaseURL != "http://core:8000" {
		t.Errorf("CoreClient().BaseURL: хотели %q, получили %q", "http://core:8000", cc.BaseURL)
	}
	// Убеждаемся, что /api/v1 не добавлен (Р5, R1)
	if strings.Contains(cc.BaseURL, "/api/v1") {
		t.Errorf("CoreClient().BaseURL не должен содержать /api/v1, получили %q", cc.BaseURL)
	}
}
