package reader

import (
	"strings"
	"testing"
)

func TestMarkdownRendererToHTML(t *testing.T) {
	renderer := NewMarkdownRenderer()
	html, err := renderer.ToHTML("# Заголовок\n\n- один\n- два\n\n**жирный**\n\n<script>alert(1)</script>")
	if err != nil {
		t.Fatalf("ToHTML() error = %v", err)
	}
	for _, want := range []string{"<h1>Заголовок</h1>", "<ul>", "<strong>жирный</strong>", "&lt;script&gt;alert(1)&lt;/script&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("ToHTML() = %q, want %q", html, want)
		}
	}
	if strings.Contains(html, "<script>") {
		t.Errorf("ToHTML() returned unsafe HTML: %q", html)
	}
}
