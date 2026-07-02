package reader

import (
	"bytes"
	"fmt"
	"html"
	"strings"

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
// Obsidian-callout'ы вида «> [!tangent]- Заголовок» (их генерирует ядро)
// превращаются в сворачиваемый <details> — см. STYLE_GUIDE §5 «Callout».
func (r *MarkdownRenderer) ToHTML(md string) (string, error) {
	var out strings.Builder
	for _, block := range splitCallouts(md) {
		if block.callout == nil {
			html, err := r.convert(block.text)
			if err != nil {
				return "", err
			}
			out.WriteString(html)
			continue
		}
		bodyHTML, err := r.convert(block.text)
		if err != nil {
			return "", err
		}
		out.WriteString(renderCallout(*block.callout, bodyHTML))
	}
	return out.String(), nil
}

// convert — базовый безопасный Markdown→HTML (goldmark, raw HTML экранирован).
func (r *MarkdownRenderer) convert(md string) (string, error) {
	var out bytes.Buffer
	if err := r.markdown.Convert([]byte(html.EscapeString(md)), &out); err != nil {
		return "", fmt.Errorf("преобразовать Markdown в HTML: %w", err)
	}
	return out.String(), nil
}

// calloutMeta — распознанный заголовок callout-блока.
type calloutMeta struct {
	// kind — тип callout ("tangent", "note", ...).
	kind string
	// title — заголовок после типа; пустой → берётся заголовок по типу.
	title string
	// folded — суффикс "-": блок свёрнут по умолчанию.
	folded bool
}

// mdBlock — фрагмент Markdown: обычный текст или тело callout.
type mdBlock struct {
	text    string
	callout *calloutMeta
}

// splitCallouts делит Markdown на обычные фрагменты и callout-блоки.
// Callout — цитата, начинающаяся с "> [!тип]" по синтаксису Obsidian;
// все последующие строки-цитаты относятся к его телу.
func splitCallouts(md string) []mdBlock {
	lines := strings.Split(md, "\n")
	var blocks []mdBlock
	var plain []string

	flushPlain := func() {
		if len(plain) > 0 {
			blocks = append(blocks, mdBlock{text: strings.Join(plain, "\n")})
			plain = nil
		}
	}

	for i := 0; i < len(lines); i++ {
		meta, ok := parseCalloutHead(lines[i])
		if !ok {
			plain = append(plain, lines[i])
			continue
		}
		var body []string
		for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), ">") {
			line := strings.TrimSpace(lines[i+1])
			line = strings.TrimPrefix(line, ">")
			line = strings.TrimPrefix(line, " ")
			body = append(body, line)
			i++
		}
		flushPlain()
		blocks = append(blocks, mdBlock{text: strings.Join(body, "\n"), callout: &meta})
	}
	flushPlain()
	return blocks
}

// parseCalloutHead распознаёт первую строку callout: «> [!тип]± Заголовок».
func parseCalloutHead(line string) (calloutMeta, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, ">") {
		return calloutMeta{}, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
	if !strings.HasPrefix(rest, "[!") {
		return calloutMeta{}, false
	}
	end := strings.Index(rest, "]")
	if end < 0 {
		return calloutMeta{}, false
	}
	kind := strings.ToLower(strings.TrimSpace(rest[2:end]))
	if kind == "" {
		return calloutMeta{}, false
	}
	tail := rest[end+1:]
	meta := calloutMeta{kind: kind}
	if strings.HasPrefix(tail, "-") {
		meta.folded = true
		tail = tail[1:]
	} else if strings.HasPrefix(tail, "+") {
		tail = tail[1:]
	}
	meta.title = strings.TrimSpace(tail)
	return meta, true
}

// renderCallout собирает <details> с экранированным заголовком.
func renderCallout(meta calloutMeta, bodyHTML string) string {
	title := meta.title
	if title == "" {
		title = calloutTitle(meta.kind)
	}
	openAttr := " open"
	if meta.folded {
		openAttr = ""
	}
	return `<details class="ll-callout"` + openAttr + `><summary>` +
		html.EscapeString(title) +
		`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"></path></svg>` +
		`</summary><div class="ll-callout-body">` + bodyHTML + `</div></details>`
}

// calloutTitle — заголовок по умолчанию для известных типов callout.
func calloutTitle(kind string) string {
	switch kind {
	case "tangent":
		return "Отступление от темы"
	case "note", "info":
		return "Заметка"
	case "warning", "caution":
		return "Внимание"
	case "example":
		return "Пример"
	case "quote":
		return "Цитата"
	default:
		return "Примечание"
	}
}
