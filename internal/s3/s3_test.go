package s3

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPresignGet(t *testing.T) {
	t.Parallel()

	const ttl = 24 * time.Hour
	tests := []struct {
		name   string
		useSSL bool
		want   string
	}{
		{name: "HTTP", want: "http"},
		{name: "HTTPS", useSSL: true, want: "https"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := New(Config{
				Endpoint:  "minio.example.test:9000",
				AccessKey: "access-key",
				SecretKey: "secret-key",
				Bucket:    "lectures",
				UseSSL:    tt.useSSL,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			got, err := client.PresignGet(context.Background(), "results/task/slide 1.png", ttl)
			if err != nil {
				t.Fatalf("PresignGet() error = %v", err)
			}

			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("url.Parse() error = %v", err)
			}
			if parsed.Scheme != tt.want {
				t.Errorf("scheme = %q, want %q", parsed.Scheme, tt.want)
			}
			if !strings.Contains(parsed.EscapedPath(), "/lectures/results/task/slide%201.png") {
				t.Errorf("path = %q, want bucket and key", parsed.EscapedPath())
			}
			expires, err := strconv.Atoi(parsed.Query().Get("X-Amz-Expires"))
			if err != nil {
				t.Fatalf("X-Amz-Expires is not an integer: %v", err)
			}
			if expires != int(ttl.Seconds()) {
				t.Errorf("X-Amz-Expires = %d, want %d", expires, int(ttl.Seconds()))
			}
		})
	}
}

func TestPresignGetEmptyKey(t *testing.T) {
	client, err := New(Config{
		Endpoint:  "minio.example.test:9000",
		AccessKey: "access-key",
		SecretKey: "secret-key",
		Bucket:    "lectures",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.PresignGet(context.Background(), "", time.Hour); err == nil {
		t.Fatal("PresignGet() error = nil, want error for empty key")
	}
}
