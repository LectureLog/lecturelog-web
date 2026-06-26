//go:build integration

package s3

import (
	"context"
	"os"
	"testing"
)

func TestGetObjectIntegration(t *testing.T) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("MINIO_ENDPOINT не задан")
	}

	client, err := New(Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("MINIO_SECRET_KEY"),
		Bucket:    os.Getenv("MINIO_BUCKET"),
		UseSSL:    os.Getenv("MINIO_USE_SSL") == "true",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	key := os.Getenv("MINIO_TEST_KEY")
	if key == "" {
		t.Skip("MINIO_TEST_KEY не задан")
	}
	if _, err := client.GetObject(context.Background(), key); err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
}
