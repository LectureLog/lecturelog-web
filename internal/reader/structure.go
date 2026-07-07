package reader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Seconds — длительность/тайм-код в секундах. Ядро отдаёт эти поля
// строками "HH:MM:SS" (см. structure.json реальных лекций), но контракт §6
// изначально описывал целые секунды — поддерживаем оба представления.
type Seconds int

// UnmarshalJSON принимает целое число секунд, null или строку
// "HH:MM:SS" / "MM:SS" / "123".
func (s *Seconds) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*s = 0
		return nil
	}
	if b[0] == '"' {
		var raw string
		if err := json.Unmarshal(b, &raw); err != nil {
			return err
		}
		v, err := parseClock(raw)
		if err != nil {
			return err
		}
		*s = Seconds(v)
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*s = Seconds(n)
	return nil
}

// parseClock разбирает "HH:MM:SS", "MM:SS" или "123" в секунды.
func parseClock(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	parts := strings.Split(raw, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("structure.json: некорректный тайм-код %q", raw)
	}
	total := 0
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("structure.json: некорректный тайм-код %q", raw)
		}
		total = total*60 + n
	}
	return total, nil
}

// Structure описывает результат обработки лекции.
// JSON-теги — контракт §6, уточнить у ядра.
type Structure struct {
	Source   Source    `json:"source"`
	Sections []Section `json:"sections"`
}

// Source содержит исходный материал лекции.
// JSON-теги — контракт §6; Title у ядра бывает null, Duration — "HH:MM:SS".
type Source struct {
	Title    string  `json:"title"`
	Kind     string  `json:"kind"`
	Duration Seconds `json:"duration"`
}

// Section объединяет подтемы лекции.
// JSON-теги — контракт §6, уточнить у ядра.
type Section struct {
	Title     string     `json:"title"`
	Subtopics []Subtopic `json:"subtopics"`
}

// Subtopic содержит материал одной подтемы.
// JSON-теги — контракт §6, уточнить у ядра.
type Subtopic struct {
	Title     string   `json:"title"`
	Media     *Media   `json:"media"`
	SlideKeys []string `json:"slide_keys"`
	// SlideNums[i] — глобальный номер кадра для SlideKeys[i]; тем же номером N
	// ядро метит позицию кадра в content_md маркером <!-- slide:N -->.
	// В старых конспектах поля нет — рендер падает в галерею.
	SlideNums []int  `json:"slide_nums"`
	ContentMD string `json:"content_md"`
}

// Media описывает фрагмент аудио или видео.
// JSON-теги — контракт §6; Start/End у ядра — строки "HH:MM:SS".
type Media struct {
	Kind  string  `json:"kind"`
	Start Seconds `json:"start"`
	End   Seconds `json:"end"`
	Key   string  `json:"key"`
}

// ParseStructure разбирает и проверяет structure.json, полученный из ядра.
func ParseStructure(b []byte) (Structure, error) {
	var structure Structure
	if err := json.Unmarshal(b, &structure); err != nil {
		return Structure{}, fmt.Errorf("разобрать structure.json: %w", err)
	}
	if len(structure.Sections) == 0 {
		return Structure{}, fmt.Errorf("structure.json: отсутствуют разделы")
	}
	if !validMediaKind(structure.Source.Kind) {
		return Structure{}, fmt.Errorf("structure.json: недопустимый тип источника %q", structure.Source.Kind)
	}

	for sectionIndex, section := range structure.Sections {
		for subtopicIndex, subtopic := range section.Subtopics {
			if subtopic.Media == nil {
				continue
			}
			if !validMediaKind(subtopic.Media.Kind) {
				return Structure{}, fmt.Errorf("structure.json: недопустимый тип медиа в разделе %d, подтеме %d: %q", sectionIndex+1, subtopicIndex+1, subtopic.Media.Kind)
			}
			if subtopic.Media.End < subtopic.Media.Start {
				return Structure{}, fmt.Errorf("structure.json: конец медиа раньше начала в разделе %d, подтеме %d", sectionIndex+1, subtopicIndex+1)
			}
		}
	}

	return structure, nil
}

func validMediaKind(kind string) bool {
	return kind == "video" || kind == "audio"
}
