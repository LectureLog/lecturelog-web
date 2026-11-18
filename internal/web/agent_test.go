package web_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LectureLog/lecturelog-web/internal/web"
)

// TestOriginFromCallbackURL проверяет извлечение scheme://host из callback-URL.
func TestOriginFromCallbackURL(t *testing.T) {
	tests := []struct {
		name        string
		callbackURL string
		want        string
	}{
		{name: "https с путём callback", callbackURL: "https://lecturelog.example/auth/callback", want: "https://lecturelog.example"},
		{name: "http с query и fragment", callbackURL: "http://localhost:8080/auth/callback?code=1#frag", want: "http://localhost:8080"},
		{name: "без пути", callbackURL: "https://example.org", want: "https://example.org"},
		{name: "пробелы вокруг", callbackURL: "  https://example.org/auth/callback  ", want: "https://example.org"},
		{name: "пустая строка", callbackURL: "", want: ""},
		{name: "только пробелы", callbackURL: "   ", want: ""},
		{name: "без схемы", callbackURL: "example.org/auth/callback", want: ""},
		{name: "относительный путь", callbackURL: "/auth/callback", want: ""},
		{name: "неподдерживаемая схема", callbackURL: "ftp://example.org/auth/callback", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := web.OriginFromCallbackURL(tt.callbackURL); got != tt.want {
				t.Errorf("OriginFromCallbackURL(%q) = %q, ожидается %q", tt.callbackURL, got, tt.want)
			}
		})
	}
}

// TestResolveOrigin проверяет приоритет сконфигурированного origin и fallback из запроса.
func TestResolveOrigin(t *testing.T) {
	tests := []struct {
		name       string
		baseOrigin string
		request    func() *http.Request
		want       string
	}{
		{
			name:       "базовый origin из конфигурации",
			baseOrigin: "https://lecturelog.example",
			request:    func() *http.Request { return httptest.NewRequest(http.MethodGet, "/hub", nil) },
			want:       "https://lecturelog.example",
		},
		{
			name:       "базовый origin с пробелами и слэшем",
			baseOrigin: "  https://lecturelog.example/  ",
			request:    func() *http.Request { return httptest.NewRequest(http.MethodGet, "/hub", nil) },
			want:       "https://lecturelog.example",
		},
		{
			name: "fallback на X-Forwarded-Proto",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/hub", nil)
				req.Host = "lecturelog.example"
				req.Header.Set("X-Forwarded-Proto", "https")
				return req
			},
			want: "https://lecturelog.example",
		},
		{
			name: "fallback игнорирует неизвестную схему прокси",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/hub", nil)
				req.Host = "lecturelog.example"
				req.Header.Set("X-Forwarded-Proto", "ftp")
				return req
			},
			want: "http://lecturelog.example",
		},
		{
			name: "fallback на TLS запроса",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/hub", nil)
				req.Host = "lecturelog.example"
				req.TLS = &tls.ConnectionState{}
				return req
			},
			want: "https://lecturelog.example",
		},
		{
			name:    "nil-запрос без конфигурации",
			request: func() *http.Request { return nil },
			want:    "http://localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := web.ResolveOrigin(tt.baseOrigin, tt.request()); got != tt.want {
				t.Errorf("ResolveOrigin(%q, ...) = %q, ожидается %q", tt.baseOrigin, got, tt.want)
			}
		})
	}
}

// TestWantsMarkdown проверяет согласование Агентского представления по Accept и User-Agent.
func TestWantsMarkdown(t *testing.T) {
	const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

	tests := []struct {
		name   string
		accept string
		ua     string
		want   bool
	}{
		{name: "пустые заголовки", want: false},
		{name: "браузерный Accept без агентского UA", accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", ua: browserUA, want: false},
		{name: "браузер с Accept text/html", accept: "text/html", ua: browserUA, want: false},
		{name: "Accept text/markdown", accept: "text/markdown", want: true},
		{name: "Accept text/x-markdown", accept: "text/x-markdown", want: true},
		{name: "markdown с параметром charset", accept: "text/markdown; charset=utf-8", want: true},
		{name: "markdown весит больше html", accept: "text/markdown;q=0.9, text/html;q=0.8", want: true},
		{name: "html и markdown с равным весом не предпочитают markdown в браузере", accept: "text/html, text/markdown", ua: browserUA, want: false},
		{name: "html и markdown с равным весом у известного агента отдают markdown", accept: "text/html, text/markdown", ua: "ClaudeBot/1.0", want: true},
		{name: "html весит больше markdown", accept: "text/html, text/markdown;q=0.5", want: false},
		{name: "markdown отклонён через q=0 в браузере", accept: "text/markdown;q=0, text/html", ua: browserUA, want: false},
		{name: "markdown явно отклонён через q=0 даже в curl", accept: "text/markdown;q=0, text/html", ua: "curl/8.5.0", want: false},
		{name: "curl с Accept */*", accept: "*/*", ua: "curl/8.5.0", want: true},
		{name: "curl с браузерным Accept", accept: "text/html,application/xhtml+xml,*/*;q=0.8", ua: "curl/8.5.0", want: true},
		{name: "ChatGPT-User", accept: "text/html", ua: "Mozilla/5.0 (compatible; ChatGPT-User/1.0; +https://openai.com/bot)", want: true},
		{name: "ClaudeBot", accept: "*/*", ua: "ClaudeBot/1.0", want: true},
		{name: "Claude-User", ua: "Mozilla/5.0 Claude-User", want: true},
		{name: "PerplexityBot", ua: "PerplexityBot/1.0", want: true},
		{name: "python-requests", ua: "python-requests/2.32.3", want: true},
		{name: "Go-http-client", ua: "Go-http-client/1.1", want: true},
		{name: "node-fetch", ua: "node-fetch/1.0 (+https://github.com/bitinn/node-fetch)", want: true},
		{name: "регистр маркера не важен", ua: "gptbot/2.0", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/hub", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			if tt.ua != "" {
				req.Header.Set("User-Agent", tt.ua)
			}
			if got := web.WantsMarkdown(req); got != tt.want {
				t.Errorf("WantsMarkdown(Accept=%q, UA=%q) = %v, ожидается %v", tt.accept, tt.ua, got, tt.want)
			}
		})
	}
}

// TestRobotsTxt проверяет машиночитаемые правила обхода для ИИ-краулеров.
func TestRobotsTxt(t *testing.T) {
	rec := httptest.NewRecorder()
	web.NewRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /robots.txt = %d, ожидается 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, ожидается text/plain; charset=utf-8", got)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"User-agent: *",
		"Allow: /",
		"Disallow: /lectures",
		"Disallow: /upload",
		"Disallow: /settings",
		"Disallow: /auth",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("robots.txt не содержит %q:\n%s", want, body)
		}
	}
}
