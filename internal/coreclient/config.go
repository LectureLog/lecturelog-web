package coreclient

import (
	"fmt"
	"net/http"
)

// Config — настройки клиента ядра.
//
// BaseURL — корневой адрес ядра, напр. "http://core:8000". Сгенерированные
// пути уже содержат префикс /api/v1, поэтому BaseURL — это ТОЛЬКО хост (схема +
// host[:port]), без /api/v1.
//
// Значение BaseURL берётся из .env-переменной CORE_API_BASE_URL. Само чтение
// .env здесь НЕ выполняется (это слой конфигурации, C0-config); B1 принимает уже
// готовую строку.
type Config struct {
	BaseURL string
}

// CoreClient — доменная обёртка над сгенерированным ClientWithResponses.
// Хранит сгенерированный ИНТЕРФЕЙС (а не конкретный тип) ради мокабельности.
// Имя CoreClient выбрано, чтобы не конфликтовать со сгенерированным типом Client.
type CoreClient struct {
	api ClientWithResponsesInterface
}

// Option — функциональная опция конструктора Client.
type Option func(*options)

// options — внутреннее накопление настроек конструктора.
type options struct {
	httpClient *http.Client
}

// WithHTTPDoer позволяет подставить собственный *http.Client
// (таймауты, транспорт, тесты).
// Имя отличается от сгенерированного WithHTTPClient, чтобы не конфликтовать с ним
// в одном пакете.
func WithHTTPDoer(hc *http.Client) Option {
	return func(o *options) {
		o.httpClient = hc
	}
}

// New собирает клиент ядра поверх сгенерированного ClientWithResponses.
func New(cfg Config, opts ...Option) (*CoreClient, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("coreclient: BaseURL не задан")
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	var genOpts []ClientOption
	if o.httpClient != nil {
		genOpts = append(genOpts, WithHTTPClient(genDoer(o.httpClient)))
	}

	api, err := NewClientWithResponses(cfg.BaseURL, genOpts...)
	if err != nil {
		return nil, fmt.Errorf("coreclient: не удалось создать клиент: %w", err)
	}

	return &CoreClient{api: api}, nil
}

// genDoer приводит *http.Client к интерфейсу HttpRequestDoer генератора.
// (*http.Client уже удовлетворяет интерфейсу через метод Do, обёртка нужна лишь
// для явности типа.)
func genDoer(hc *http.Client) HttpRequestDoer {
	return hc
}
