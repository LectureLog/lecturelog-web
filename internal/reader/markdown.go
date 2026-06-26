package reader

import (
	"bytes"
	"fmt"
	"html"

	"github.com/yuin/goldmark"
)

// MarkdownRenderer преобразует Markdown в HTML без разрешения сырого HTML.
type MarkdownRenderer struct {
	markdown goldmark.Markdown
}

// NewMarkdownRenderer создаёт безопасный рендерер Markdown.
func NewMarkdownRenderer() *MarkdownRenderer {
	return &MarkdownRenderer{markdown: goldmark.New()}
}

// ToHTML преобразует Markdown в HTML. Сырой HTML предварительно экранируется.
func (r *MarkdownRenderer) ToHTML(md string) (string, error) {
	var out bytes.Buffer
	if err := r.markdown.Convert([]byte(html.EscapeString(md)), &out); err != nil {
		return "", fmt.Errorf("преобразовать Markdown в HTML: %w", err)
	}
	return out.String(), nil
}
