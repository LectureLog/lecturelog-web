package web_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/LectureLog/lecturelog-web/internal/web"
)

// TestReaderPage_Render проверяет ключевые данные и безопасное экранирование страницы читалки.
func TestReaderPage_Render(t *testing.T) {
	vm := web.ReaderVM{
		LectureID:   "lecture-42",
		Title:       "Лекция <script>",
		SourceTitle: "Курс & практика",
		SourceKind:  "video",
		Duration:    372,
		IsOwner:     true,
		Sections: []web.ReaderSectionVM{{
			Number: "01",
			Title:  "Основы <систем>",
			Subtopics: []web.ReaderSubtopicVM{{
				Number:      "1.1",
				Title:       "Первый & главный",
				ContentHTML: "<p><strong>Санитизированный</strong> текст</p>",
				Media:       &web.ReaderMediaVM{Kind: "video", Start: 65, End: 125, URL: "https://media.example/video.mp4?x=1&y=2"},
				SlideURLs:   []string{"https://cdn.example/slide-1.png"},
			}},
		}},
	}

	var b bytes.Buffer
	if err := web.ReaderPage(web.LayoutData{Title: vm.Title}, vm).Render(context.Background(), &b); err != nil {
		t.Fatalf("ReaderPage.Render: %v", err)
	}
	html := b.String()

	for _, want := range []string{
		"Лекция &lt;script&gt;", "Курс &amp; практика",
		"Основы &lt;систем&gt;", "01", "Первый &amp; главный", "1.1",
		`id="s-1-1"`, `data-target="s-1-1"`,
		"<strong>Санитизированный</strong> текст", "https://media.example/video.mp4?x=1&amp;y=2",
		"https://cdn.example/slide-1.png", "/read/lecture-42/export", "Владелец",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в рендере отсутствует %q", want)
		}
	}
	if strings.Contains(html, "Лекция <script>") || strings.Contains(html, "Курс & практика") || strings.Contains(html, "Основы <систем>") {
		t.Error("пользовательский текст должен быть экранирован")
	}
}

// TestReaderPage_RenderBlocks проверяет инлайн-кадры между HTML-блоками,
// которые reader-сервис построил из маркеров <!-- slide:N -->.
func TestReaderPage_RenderBlocks(t *testing.T) {
	vm := web.ReaderVM{
		LectureID:   "lecture-42",
		Title:       "Тестовая лекция",
		SourceTitle: "Источник",
		SourceKind:  "video",
		Sections: []web.ReaderSectionVM{{
			Number: "01",
			Title:  "Раздел",
			Subtopics: []web.ReaderSubtopicVM{{
				Number: "1.1",
				Title:  "Подтема",
				Blocks: []web.ReaderBlockVM{
					{HTML: "<p>До кадра</p>"},
					{Slide: &web.ReaderSlideVM{URL: "https://cdn.example/inline.png", Num: 3}},
					{HTML: "<p>После кадра</p>"},
				},
			}},
		}},
	}

	var b bytes.Buffer
	if err := web.ReaderPage(web.LayoutData{Title: vm.Title}, vm).Render(context.Background(), &b); err != nil {
		t.Fatalf("ReaderPage.Render: %v", err)
	}
	html := b.String()

	before := strings.Index(html, "<p>До кадра</p>")
	img := strings.Index(html, "https://cdn.example/inline.png")
	after := strings.Index(html, "<p>После кадра</p>")
	if before == -1 || img == -1 || after == -1 {
		t.Fatalf("не все блоки попали в рендер: before=%d img=%d after=%d", before, img, after)
	}
	if !(before < img && img < after) {
		t.Errorf("кадр должен стоять между абзацами: before=%d img=%d after=%d", before, img, after)
	}
	if strings.Contains(html, "Слайд 1 из") {
		t.Error("инлайн-кадр не должен дублироваться в галерее")
	}
}

// TestReaderPage_RenderEmptyData проверяет рендер без необязательных данных читалки.
func TestReaderPage_RenderEmptyData(t *testing.T) {
	tests := []struct {
		name string
		vm   web.ReaderVM
	}{
		{
			name: "пустые разделы",
			vm:   web.ReaderVM{},
		},
		{
			name: "без медиа и слайдов",
			vm: web.ReaderVM{Sections: []web.ReaderSectionVM{{
				Subtopics: []web.ReaderSubtopicVM{{}},
			}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := web.ReaderPage(web.LayoutData{}, tt.vm).Render(context.Background(), &b); err != nil {
				t.Fatalf("ReaderPage.Render: %v", err)
			}
		})
	}
}
