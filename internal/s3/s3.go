// Package s3 предоставляет доступ к объектам в S3-совместимом хранилище.
package s3

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config содержит параметры подключения к S3-совместимому хранилищу.
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// Client работает с одним S3-бакетом.
type Client struct {
	mc     *minio.Client
	bucket string
}

// New создаёт клиент S3-совместимого хранилища.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("S3 endpoint не задан")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("S3 bucket не задан")
	}

	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		// Фиксированный регион исключает сетевой запрос определения региона при presign.
		Region: "us-east-1",
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("создать S3-клиент: %w", err)
	}

	return &Client{mc: mc, bucket: cfg.Bucket}, nil
}

// PresignGet возвращает временную ссылку на чтение объекта. Подпись вычисляется локально.
func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("ключ объекта не задан")
	}

	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("подписать ссылку на объект %q: %w", key, err)
	}
	return u.String(), nil
}

// GetObject скачивает объект полностью в память.
func (c *Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("ключ объекта не задан")
	}

	object, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("получить объект %q: %w", key, err)
	}
	defer object.Close()

	b, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("прочитать объект %q: %w", key, err)
	}
	return b, nil
}
