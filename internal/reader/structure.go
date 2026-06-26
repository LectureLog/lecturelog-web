package reader

import (
	"encoding/json"
	"fmt"
)

// Structure описывает результат обработки лекции.
// JSON-теги — контракт §6, уточнить у ядра.
type Structure struct {
	Source   Source    `json:"source"`
	Sections []Section `json:"sections"`
}

// Source содержит исходный материал лекции.
// JSON-теги — контракт §6, уточнить у ядра.
type Source struct {
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Duration int    `json:"duration"`
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
	ContentMD string   `json:"content_md"`
}

// Media описывает фрагмент аудио или видео.
// JSON-теги — контракт §6, уточнить у ядра.
type Media struct {
	Kind  string `json:"kind"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Key   string `json:"key"`
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
