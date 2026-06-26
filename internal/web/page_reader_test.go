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
		Title:       "Лекция <введение>",
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
		"Основы &lt;систем&gt;", "01", "Первый &amp; главный", "1.1",
		`id="s-1-1"`, `data-target="s-1-1"`,
		"<strong>Санитизированный</strong> текст", "https://media.example/video.mp4?x=1&amp;y=2",
		"https://cdn.example/slide-1.png", "/read/lecture-42/export", "Владелец",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("в рендере отсутствует %q", want)
		}
	}
	if strings.Contains(html, "Лекция <введение>") || strings.Contains(html, "Основы <систем>") {
		t.Error("пользовательский текст должен быть экранирован")
	}
}
