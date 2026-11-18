package reader

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// MarkdownView — Агентское представление Конспекта и видимость Лекции.
// Visibility заполняется даже при ошибке доступа (если лекция найдена),
// чтобы HTTP-слой мог выставить заголовки индексации и кеширования.
type MarkdownView struct {
	Markdown   string
	Visibility string
}

// LoadMarkdown проверяет доступ и собирает Агентское представление Конспекта:
// заголовок и метаданные источника, двухуровневое оглавление, Разделы и
// Подтемы с таймкодами, текстом, чистым Markdown-цитатами вместо callout'ов
// и абсолютными ссылками на Слайды. Аудио- и видеофрагменты не включаются.
func (s *Service) LoadMarkdown(ctx context.Context, lectureID, viewerID, origin string) (MarkdownView, error) {
	lecture, _, err := s.accessLecture(ctx, lectureID, viewerID)
	if err != nil {
		if lecture != nil {
			return MarkdownView{Visibility: lecture.Visibility}, err
		}
		return MarkdownView{}, err
	}

	structure, err := s.loadStructure(ctx, lecture.CoreTaskID)
	if err != nil {
		return MarkdownView{Visibility: lecture.Visibility}, err
	}

	return MarkdownView{
		Markdown:   buildAgentMarkdown(lecture, structure, normalizeMarkdownOrigin(origin)),
		Visibility: lecture.Visibility,
	}, nil
}

// normalizeMarkdownOrigin убирает хвостовой слэш и подставляет безопасный
// fallback, чтобы ссылки Конспекта всегда оставались абсолютными.
func normalizeMarkdownOrigin(origin string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return "http://localhost"
	}
	return origin
}

// buildAgentMarkdown собирает Агентское представление Конспекта целиком.
func buildAgentMarkdown(lecture *LectureMeta, structure Structure, origin string) string {
	var b strings.Builder
	slideMap := BuildSlideMap(structure)
	longLecture := int(structure.Source.Duration) >= 3600

	// Ядро может не знать названия источника (title: null) — берём заголовок
	// Лекции, чтобы блок метаданных не был пустым.
	sourceTitle := structure.Source.Title
	if sourceTitle == "" {
		sourceTitle = lecture.Title
	}
	sourceKind := lecture.SourceKind
	if sourceKind == "" {
		sourceKind = structure.Source.Kind
	}

	fmt.Fprintf(&b, "# %s\n\n", lecture.Title)
	fmt.Fprintf(&b, "- Источник: %s\n", sourceTitle)
	fmt.Fprintf(&b, "- Тип источника: %s\n", agentSourceLabel(sourceKind))
	fmt.Fprintf(&b, "- Длительность: %s\n", formatClock(int(structure.Source.Duration)))
	fmt.Fprintf(&b, "- Канонический URL: %s/read/%s\n\n", origin, lecture.ID)

	b.WriteString("## Содержание\n\n")
	for sectionIndex, section := range structure.Sections {
		fmt.Fprintf(&b, "- %02d. %s\n", sectionIndex+1, section.Title)
		for subtopicIndex, subtopic := range section.Subtopics {
			fmt.Fprintf(&b, "  - %d.%d %s\n", sectionIndex+1, subtopicIndex+1, subtopic.Title)
		}
	}
	b.WriteString("\n")

	for sectionIndex, section := range structure.Sections {
		fmt.Fprintf(&b, "## %02d. %s\n\n", sectionIndex+1, section.Title)
		for subtopicIndex, subtopic := range section.Subtopics {
			writeAgentSubtopic(&b, origin, lecture.ID, subtopic, slideMap, sectionIndex, subtopicIndex, longLecture)
		}
	}
	return b.String()
}

// writeAgentSubtopic пишет Подтему: заголовок, таймкод, текст с инлайн-Слайдами
// по маркерам <!-- slide:N --> и галерею оставшихся Слайдов в конце.
func writeAgentSubtopic(b *strings.Builder, origin, lectureID string, subtopic Subtopic, slideMap SlideMap, sectionIndex, subtopicIndex int, longLecture bool) {
	fmt.Fprintf(b, "### %d.%d %s\n\n", sectionIndex+1, subtopicIndex+1, subtopic.Title)
	if subtopic.Media != nil {
		// Для длинных записей весь Конспект держит часы в таймкодах, чтобы
		// Подтемы не смешивали MM:SS и HH:MM:SS в одном представлении.
		start, end := int(subtopic.Media.Start), int(subtopic.Media.End)
		withHours := longLecture || start >= 3600 || end >= 3600
		fmt.Fprintf(b, "[%s – %s]\n\n",
			formatClockValue(start, withHours),
			formatClockValue(end, withHours),
		)
	}

	subtopicSlideNums := slideMap.Numbers[sectionIndex][subtopicIndex]
	placed := make(map[int]bool)
	rest := subtopic.ContentMD
	for {
		loc := slideMarker.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		writeAgentText(b, rest[:loc[0]])
		if num, err := strconv.Atoi(rest[loc[2]:loc[3]]); err == nil {
			if slices.Contains(subtopicSlideNums, num) && !placed[num] {
				writeAgentSlide(b, origin, lectureID, num, subtopic.Title)
				placed[num] = true
			}
		}
		rest = rest[loc[1]:]
	}
	writeAgentText(b, rest)

	for _, num := range subtopicSlideNums {
		if placed[num] {
			continue
		}
		writeAgentSlide(b, origin, lectureID, num, subtopic.Title)
		placed[num] = true
	}
}

// writeAgentSlide пишет Markdown-картинку Слайда с абсолютной ссылкой.
func writeAgentSlide(b *strings.Builder, origin, lectureID string, num int, subtopicTitle string) {
	fmt.Fprintf(b, "![Слайд %d: %s](%s/read/%s/slides/%d)\n\n", num, subtopicTitle, origin, lectureID, num)
}

// writeAgentText пишет текстовый фрагмент Подтемы: обычный Markdown остаётся
// как есть, Obsidian-callout'ы превращаются в чистые Markdown-цитаты.
func writeAgentText(b *strings.Builder, md string) {
	for _, block := range splitCallouts(md) {
		if block.callout == nil {
			text := strings.TrimSpace(block.text)
			if text == "" {
				continue
			}
			b.WriteString(text)
			b.WriteString("\n\n")
			continue
		}
		b.WriteString(agentCalloutMarkdown(*block.callout, block.text))
		b.WriteString("\n\n")
	}
}

// agentCalloutMarkdown собирает Markdown-цитату вместо свёрнутого HTML-callout'а:
// первая строка — полужирный локализованный тип и заголовок, далее тело.
func agentCalloutMarkdown(meta calloutMeta, body string) string {
	var b strings.Builder
	title := calloutTitle(meta.kind)
	if meta.title != "" {
		fmt.Fprintf(&b, "> **%s: %s**", title, meta.title)
	} else {
		fmt.Fprintf(&b, "> **%s**", title)
	}
	if body == "" {
		return b.String()
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			b.WriteString("\n>")
			continue
		}
		b.WriteString("\n> ")
		b.WriteString(line)
	}
	return b.String()
}

// agentSourceLabel локализует тип источника для блока метаданных.
func agentSourceLabel(kind string) string {
	switch kind {
	case "audio":
		return "Аудио"
	case "video":
		return "Видео"
	case "video_url":
		return "Ссылка"
	default:
		return kind
	}
}

// formatClock форматирует секунды в MM:SS, а для записей от часа — в HH:MM:SS.
func formatClock(seconds int) string {
	return formatClockValue(seconds, seconds >= 3600)
}

// formatClockValue форматирует секунды в MM:SS либо HH:MM:SS при withHours.
func formatClockValue(seconds int, withHours bool) string {
	if seconds < 0 {
		seconds = 0
	}
	if withHours {
		return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
