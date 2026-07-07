package reader

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Load проверяет доступ и собирает модель читального зала.
func (s *Service) Load(ctx context.Context, lectureID, viewerID string) (ReaderView, error) {
	lecture, isOwner, err := s.accessLecture(ctx, lectureID, viewerID)
	if err != nil {
		return ReaderView{}, err
	}

	// Ядро кладёт выход задачи под results/<taskID>/output/ (см. контракт пути
	// выхода core). Сегмент output/ обязателен, иначе GetObject вернёт NoSuchKey.
	structureKey := "results/" + lecture.CoreTaskID + "/output/structure.json"
	b, err := s.store.GetObject(ctx, structureKey)
	if err != nil {
		return ReaderView{}, fmt.Errorf("%w: прочитать structure.json: %v", ErrCoreUnavailable, err)
	}
	structure, err := ParseStructure(b)
	if err != nil {
		return ReaderView{}, err
	}

	// Ядро может не знать названия источника (title: null) — показываем
	// заголовок лекции, чтобы карточка источника не была пустой.
	sourceTitle := structure.Source.Title
	if sourceTitle == "" {
		sourceTitle = lecture.Title
	}

	view := ReaderView{
		LectureID:   lecture.ID,
		Title:       lecture.Title,
		SourceTitle: sourceTitle,
		SourceKind:  structure.Source.Kind,
		Duration:    int(structure.Source.Duration),
		Sections:    make([]ViewSection, 0, len(structure.Sections)),
		IsOwner:     isOwner,
	}
	for sectionIndex, section := range structure.Sections {
		viewSection := ViewSection{
			Number:    fmt.Sprintf("%02d", sectionIndex+1),
			Title:     section.Title,
			Subtopics: make([]ViewSubtopic, 0, len(section.Subtopics)),
		}
		for subtopicIndex, subtopic := range section.Subtopics {
			viewSubtopic, err := s.buildSubtopic(ctx, sectionIndex+1, subtopicIndex+1, subtopic)
			if err != nil {
				return ReaderView{}, err
			}
			viewSection.Subtopics = append(viewSection.Subtopics, viewSubtopic)
		}
		view.Sections = append(view.Sections, viewSection)
	}

	return view, nil
}

// AccessTaskID проверяет доступ к лекции и возвращает идентификатор задачи ядра.
func (s *Service) AccessTaskID(ctx context.Context, lectureID, viewerID string) (string, error) {
	lecture, _, err := s.accessLecture(ctx, lectureID, viewerID)
	if err != nil {
		return "", err
	}
	return lecture.CoreTaskID, nil
}

// accessLecture централизует проверку доступа для чтения и экспорта.
func (s *Service) accessLecture(ctx context.Context, lectureID, viewerID string) (*LectureMeta, bool, error) {
	lecture, err := s.repo.FindByID(ctx, lectureID)
	if err != nil {
		return nil, false, fmt.Errorf("найти лекцию: %w", err)
	}
	if lecture == nil {
		return nil, false, ErrNotFound
	}

	isOwner := viewerID != "" && viewerID == lecture.OwnerID
	if lecture.Status != "ready" {
		if isOwner {
			return nil, false, ErrNotReady
		}
		return nil, false, ErrNotFound
	}
	if lecture.Visibility == "private" && !isOwner {
		return nil, false, ErrNotFound
	}
	return lecture, isOwner, nil
}

func (s *Service) buildSubtopic(ctx context.Context, sectionNumber, subtopicNumber int, subtopic Subtopic) (ViewSubtopic, error) {
	html, err := s.md.ToHTML(subtopic.ContentMD)
	if err != nil {
		return ViewSubtopic{}, fmt.Errorf("отрендерить Markdown: %w", err)
	}

	view := ViewSubtopic{
		Number:      fmt.Sprintf("%d.%d", sectionNumber, subtopicNumber),
		Title:       subtopic.Title,
		SlideURLs:   make([]string, 0, len(subtopic.SlideKeys)),
		ContentHTML: html,
	}
	if subtopic.Media != nil {
		url, err := s.presign.PresignGet(ctx, subtopic.Media.Key, s.ttl)
		if err != nil {
			return ViewSubtopic{}, fmt.Errorf("подписать медиа: %w", err)
		}
		view.Media = &ViewMedia{
			Kind:  subtopic.Media.Kind,
			Start: int(subtopic.Media.Start),
			End:   int(subtopic.Media.End),
			URL:   url,
		}
	}
	// Подписанные URL кадров по глобальному номеру N (из slide_nums);
	// кадры без номера (старый structure.json) — сразу в галерею.
	urlByNum := make(map[int]string, len(subtopic.SlideKeys))
	for i, key := range subtopic.SlideKeys {
		url, err := s.presign.PresignGet(ctx, key, s.ttl)
		if err != nil {
			return ViewSubtopic{}, fmt.Errorf("подписать слайд: %w", err)
		}
		if i < len(subtopic.SlideNums) {
			urlByNum[subtopic.SlideNums[i]] = url
		} else {
			view.SlideURLs = append(view.SlideURLs, url)
		}
	}

	blocks, placed, err := s.splitByMarkers(subtopic.ContentMD, urlByNum)
	if err != nil {
		return ViewSubtopic{}, err
	}
	view.Blocks = blocks
	// Кадры, не нашедшие маркер в тексте, — в галерею (в порядке slide_keys).
	for i, num := range subtopic.SlideNums {
		if i < len(subtopic.SlideKeys) && !placed[num] {
			view.SlideURLs = append(view.SlideURLs, urlByNum[num])
		}
	}
	return view, nil
}

// slideMarker — маркер позиции кадра из ядра: строка вида <!-- slide:N -->.
var slideMarker = regexp.MustCompile(`(?m)^[ \t]*<!-- slide:(\d+) -->[ \t]*$`)

// splitByMarkers режет markdown по маркерам <!-- slide:N --> и рендерит куски
// в HTML; между кусками — кадры по номеру N. Маркер без известного URL
// выбрасывается. Возвращает блоки и множество вставленных номеров.
func (s *Service) splitByMarkers(md string, urlByNum map[int]string) ([]ViewBlock, map[int]bool, error) {
	placed := make(map[int]bool)
	var blocks []ViewBlock
	appendHTML := func(chunk string) error {
		if strings.TrimSpace(chunk) == "" {
			return nil
		}
		html, err := s.md.ToHTML(strings.TrimSpace(chunk))
		if err != nil {
			return fmt.Errorf("отрендерить Markdown: %w", err)
		}
		blocks = append(blocks, ViewBlock{HTML: html})
		return nil
	}
	rest := md
	for {
		loc := slideMarker.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		if err := appendHTML(rest[:loc[0]]); err != nil {
			return nil, nil, err
		}
		if num, err := strconv.Atoi(rest[loc[2]:loc[3]]); err == nil {
			if url, ok := urlByNum[num]; ok {
				blocks = append(blocks, ViewBlock{Slide: &ViewSlide{URL: url, Num: num}})
				placed[num] = true
			}
		}
		rest = rest[loc[1]:]
	}
	if err := appendHTML(rest); err != nil {
		return nil, nil, err
	}
	return blocks, placed, nil
}
