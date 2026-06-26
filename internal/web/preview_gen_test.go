//go:build preview

package web_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/LectureLog/lecturelog-web/internal/reader"
	"github.com/LectureLog/lecturelog-web/internal/web"
)

const readerPreviewPath = "/root/lecturelog-web/.worktrees/reader-ui/reader-preview.html"

// TestGenerateReaderPreview создаёт автономный HTML-предпросмотр читалки из реальной фикстуры.
func TestGenerateReaderPreview(t *testing.T) {
	fixture, err := os.ReadFile("../reader/testdata/structure_valid.json")
	if err != nil {
		t.Fatalf("прочитать фикстуру: %v", err)
	}

	structure, err := reader.ParseStructure(fixture)
	if err != nil {
		t.Fatalf("разобрать фикстуру: %v", err)
	}

	vm := web.ReaderVM{
		LectureID:   "preview",
		Title:       structure.Source.Title,
		SourceTitle: structure.Source.Title,
		SourceKind:  structure.Source.Kind,
		Duration:    structure.Source.Duration,
		IsOwner:     true,
		Sections:    make([]web.ReaderSectionVM, 0, len(structure.Sections)),
	}
	for sectionIndex, section := range structure.Sections {
		sectionVM := web.ReaderSectionVM{
			Number:    fmt.Sprintf("%02d", sectionIndex+1),
			Title:     section.Title,
			Subtopics: make([]web.ReaderSubtopicVM, 0, len(section.Subtopics)),
		}
		for subtopicIndex, subtopic := range section.Subtopics {
			subtopicVM := web.ReaderSubtopicVM{
				Number:      fmt.Sprintf("%d.%d", sectionIndex+1, subtopicIndex+1),
				Title:       subtopic.Title,
				ContentHTML: "<p>" + subtopic.ContentMD + "</p>",
				SlideURLs: []string{
					"https://placehold.co/640x360?text=Slide+1",
					"https://placehold.co/640x360?text=Slide+2",
				},
			}
			if subtopic.Media != nil {
				subtopicVM.Media = &web.ReaderMediaVM{
					Kind:  subtopic.Media.Kind,
					Start: subtopic.Media.Start,
					End:   subtopic.Media.End,
					URL:   "https://example.invalid/media/placeholder.mp4",
				}
			}
			sectionVM.Subtopics = append(sectionVM.Subtopics, subtopicVM)
		}
		vm.Sections = append(vm.Sections, sectionVM)
	}

	var rendered bytes.Buffer
	if err := web.ReaderPage(web.LayoutData{Title: vm.Title}, vm).Render(context.Background(), &rendered); err != nil {
		t.Fatalf("отрендерить страницу: %v", err)
	}

	css, err := os.ReadFile("static/css/app.css")
	if err != nil {
		t.Fatalf("прочитать CSS: %v", err)
	}
	readerJS, err := os.ReadFile("static/js/reader.js")
	if err != nil {
		t.Fatalf("прочитать JavaScript: %v", err)
	}

	html := rendered.String()
	html = replaceRequired(t, html, `<link rel="stylesheet" href="/static/css/app.css">`, "<style>"+string(css)+"</style>")
	html = replaceRequired(t, html, `<script src="/static/vendor/htmx.min.js" defer></script>`, "")
	html = replaceRequired(t, html, `<script src="/static/js/reader.js" defer></script>`, "")
	html = replaceRequired(t, html, "</body>", "<script>"+string(readerJS)+"</script></body>")

	if err := os.WriteFile(readerPreviewPath, []byte(html), 0o644); err != nil {
		t.Fatalf("записать предпросмотр: %v", err)
	}
	info, err := os.Stat(readerPreviewPath)
	if err != nil {
		t.Fatalf("проверить предпросмотр: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("предпросмотр пуст")
	}
	t.Logf("предпросмотр: %s (%d байт)", readerPreviewPath, info.Size())
}

// replaceRequired предотвращает генерацию файла с внешними зависимостями при изменении шаблона.
func replaceRequired(t *testing.T, source, old, replacement string) string {
	t.Helper()
	if !strings.Contains(source, old) {
		t.Fatalf("в отрендеренном HTML не найдена строка %q", old)
	}
	return strings.Replace(source, old, replacement, 1)
}
