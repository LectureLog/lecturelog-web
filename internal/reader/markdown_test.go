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

// TestToHTML_Callout проверяет преобразование Obsidian-callout в <details>.
func TestToHTML_Callout(t *testing.T) {
	renderer := NewMarkdownRenderer()
	md := "> [!tangent]- Отступление\n> Первая строка.\n> Вторая строка.\n\nОбычный **текст**."
	html, err := renderer.ToHTML(md)
	if err != nil {
		t.Fatalf("ToHTML() error = %v", err)
	}
	for _, want := range []string{
		`<details class="ll-callout">`,
		"<summary>Отступление",
		"Первая строка.",
		"<strong>текст</strong>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("ToHTML(): нет %q в результате:\n%s", want, html)
		}
	}
	if strings.Contains(html, "[!tangent]") {
		t.Error("маркер callout не должен попадать в HTML")
	}
}

// TestToHTML_CalloutDefaultTitleOpen: без заголовка и без "-" — заголовок
// по типу и развёрнутое состояние.
func TestToHTML_CalloutDefaultTitleOpen(t *testing.T) {
	renderer := NewMarkdownRenderer()
	html, err := renderer.ToHTML("> [!note]\n> Тело.")
	if err != nil {
		t.Fatalf("ToHTML() error = %v", err)
	}
	if !strings.Contains(html, `<details class="ll-callout" open>`) {
		t.Errorf("ожидается открытый details, получено:\n%s", html)
	}
	if !strings.Contains(html, "<summary>Заметка") {
		t.Errorf("ожидается заголовок по типу, получено:\n%s", html)
	}
}
