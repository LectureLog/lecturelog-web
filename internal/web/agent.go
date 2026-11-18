package web

// Хелперы агентского представления: базовый origin платформы и согласование
// Markdown/HTML. Живут в пакете web, чтобы ими пользовались и витрина (hub),
// и читальный зал (reader), не дублируя разбор заголовков.

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// agentUserAgentMarkers — подстроки известных ИИ-фетчеров и CLI-клиентов
// (без учёта регистра). Такой клиент получает Markdown даже с `Accept: text/html`.
var agentUserAgentMarkers = []string{
	"chatgpt-user",
	"oai-searchbot",
	"gptbot",
	"claude-web",
	"claude-user",
	"claudebot",
	"anthropic",
	"perplexitybot",
	"perplexity-user",
	"google-extended",
	"cohere-ai",
	"duckassistbot",
	"youbot",
	"curl/",
	"wget/",
	"python-httpx",
	"python-requests",
	"go-http-client",
	"undici",
	"node-fetch",
	"axios/",
}

// robotsTxt — правила обхода платформы для краулеров и агентских фетчеров.
const robotsTxt = `User-agent: *
Allow: /
Disallow: /lectures
Disallow: /upload
Disallow: /settings
Disallow: /auth
`

// OriginFromCallbackURL извлекает `scheme://host` из обязательного callback-URL
// OAuth (PLATFORM_CALLBACK_URL), отбрасывая путь, query и fragment.
// Пустая строка означает, что origin не сконфигурирован и нужен fallback из запроса.
func OriginFromCallbackURL(callbackURL string) string {
	u, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	if u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + u.Host
}

// ResolveOrigin возвращает внешний origin платформы для абсолютных ссылок.
// Сконфигурированный базовый origin имеет приоритет (защита от Host-header
// injection); без него используется безопасный fallback из самого запроса.
func ResolveOrigin(baseOrigin string, r *http.Request) string {
	if trimmed := strings.TrimSpace(baseOrigin); trimmed != "" {
		return strings.TrimRight(trimmed, "/")
	}

	scheme := "http"
	host := "localhost"
	if r != nil {
		if forwarded := forwardedProto(r); forwarded != "" {
			scheme = forwarded
		} else if r.TLS != nil {
			scheme = "https"
		}
		if r.Host != "" {
			host = r.Host
		}
	}
	return scheme + "://" + host
}

// forwardedProto берёт первое значение X-Forwarded-Proto, если это http или https.
func forwardedProto(r *http.Request) string {
	value := r.Header.Get("X-Forwarded-Proto")
	if idx := strings.IndexByte(value, ','); idx >= 0 {
		value = value[:idx]
	}
	proto := strings.ToLower(strings.TrimSpace(value))
	if proto == "http" || proto == "https" {
		return proto
	}
	return ""
}

// WantsMarkdown решает, отдавать ли Агентское представление вместо HTML.
//
// Правило выбора:
//  1. `Accept` явно предпочитает `text/markdown` / `text/x-markdown` перед
//     `text/html` (с учётом весов `q=`); при `qHTML > qMD` ответ всегда HTML.
//  2. Известный агентский или CLI `User-Agent` получает Markdown независимо
//     от `Accept` — облачные боты часто шлют `Accept: text/html` или `*/*`.
//  3. Во всех остальных случаях (обычный браузер, пустые заголовки) — HTML.
func WantsMarkdown(r *http.Request) bool {
	if r == nil {
		return false
	}

	markdownWeight, markdownSet, htmlWeight, htmlSet := acceptWeights(r.Header.Get("Accept"))
	if markdownSet && markdownWeight > 0 {
		if htmlSet && htmlWeight > markdownWeight {
			return false
		}
		if markdownWeight >= htmlWeight {
			return true
		}
	}

	userAgent := strings.ToLower(r.Header.Get("User-Agent"))
	for _, marker := range agentUserAgentMarkers {
		if strings.Contains(userAgent, marker) {
			return true
		}
	}
	return false
}

// acceptWeights возвращает максимальные веса markdown- и html-типов из Accept.
// Флаги set показывают, прислал ли клиент соответствующее семейство типов.
func acceptWeights(accept string) (markdownWeight float64, markdownSet bool, htmlWeight float64, htmlSet bool) {
	for _, part := range strings.Split(accept, ",") {
		mediaType, quality := parseAcceptPart(part)
		switch mediaType {
		case "text/markdown", "text/x-markdown":
			markdownSet = true
			if quality > markdownWeight {
				markdownWeight = quality
			}
		case "text/html", "application/xhtml+xml":
			htmlSet = true
			if quality > htmlWeight {
				htmlWeight = quality
			}
		}
	}
	return markdownWeight, markdownSet, htmlWeight, htmlSet
}

// parseAcceptPart разбирает один элемент Accept: медиатип и вес q (по умолчанию 1.0).
// Пустой медиатип означает, что элемент пропускается.
func parseAcceptPart(part string) (mediaType string, quality float64) {
	segments := strings.Split(part, ";")
	mediaType = strings.ToLower(strings.TrimSpace(segments[0]))
	if mediaType == "" {
		return "", 0
	}

	quality = 1.0
	for _, param := range segments[1:] {
		key, value, ok := strings.Cut(param, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
			continue
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || parsed < 0 {
			continue
		}
		if parsed > 1 {
			parsed = 1
		}
		quality = parsed
	}
	return mediaType, quality
}
