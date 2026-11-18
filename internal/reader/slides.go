package reader

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
)

// SlideData — изображение Слайда, готовое к прямой раздаче.
type SlideData struct {
	Bytes       []byte
	ContentType string
	Visibility  string
}

// SlideMap — детерминированная 1-based карта Слайдов Конспекта.
// Numbers повторяет форму дерева: номер Слайда по индексам
// [раздел][подтема][слайд], Keys связывает номер с ключом хранилища.
type SlideMap struct {
	Numbers [][][]int
	Keys    map[int]string
}

// BuildSlideMap обходит Конспект в порядке Разделов и Подтем. Сначала
// учитываются явные slide_nums, затем оставшимся Слайдам (старые Конспекты
// без slide_nums) присваиваются последовательные номера после максимального.
func BuildSlideMap(structure Structure) SlideMap {
	numbers := make([][][]int, len(structure.Sections))
	keys := make(map[int]string)
	maxNum := 0

	for sectionIndex, section := range structure.Sections {
		numbers[sectionIndex] = make([][]int, len(section.Subtopics))
		for subtopicIndex, subtopic := range section.Subtopics {
			numbers[sectionIndex][subtopicIndex] = make([]int, len(subtopic.SlideKeys))
			for slideIndex := range subtopic.SlideKeys {
				if slideIndex >= len(subtopic.SlideNums) || subtopic.SlideNums[slideIndex] < 1 {
					continue
				}
				num := subtopic.SlideNums[slideIndex]
				numbers[sectionIndex][subtopicIndex][slideIndex] = num
				keys[num] = subtopic.SlideKeys[slideIndex]
				if num > maxNum {
					maxNum = num
				}
			}
		}
	}

	next := maxNum
	for sectionIndex, section := range structure.Sections {
		for subtopicIndex, subtopic := range section.Subtopics {
			for slideIndex, key := range subtopic.SlideKeys {
				if numbers[sectionIndex][subtopicIndex][slideIndex] >= 1 {
					continue
				}
				next++
				numbers[sectionIndex][subtopicIndex][slideIndex] = next
				keys[next] = key
			}
		}
	}

	return SlideMap{Numbers: numbers, Keys: keys}
}

// LoadSlide проверяет доступ к Лекции и читает байты Слайда по его номеру.
func (s *Service) LoadSlide(ctx context.Context, lectureID, viewerID string, slideNum int) (SlideData, error) {
	lecture, _, err := s.accessLecture(ctx, lectureID, viewerID)
	if err != nil {
		if lecture != nil {
			return SlideData{Visibility: lecture.Visibility}, err
		}
		return SlideData{}, err
	}
	out := SlideData{Visibility: lecture.Visibility}
	if slideNum <= 0 {
		return out, ErrNotFound
	}

	structure, err := s.loadStructure(ctx, lecture.CoreTaskID)
	if err != nil {
		return out, err
	}

	key, ok := BuildSlideMap(structure).Keys[slideNum]
	if !ok {
		return out, ErrNotFound
	}

	slideBytes, err := s.store.GetObject(ctx, key)
	if err != nil {
		return out, fmt.Errorf("%w: прочитать слайд: %v", ErrCoreUnavailable, err)
	}

	out.Bytes = slideBytes
	out.ContentType = slideContentType(key, slideBytes)
	return out, nil
}

// slideContentType определяет тип изображения по расширению ключа,
// а при неизвестном расширении — по первым байтам.
func slideContentType(key string, b []byte) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return http.DetectContentType(b)
	}
}
