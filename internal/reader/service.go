package reader

import (
	"context"
	"fmt"
)

// Load проверяет доступ и собирает модель читального зала.
func (s *Service) Load(ctx context.Context, lectureID, viewerID string) (ReaderView, error) {
	lecture, isOwner, err := s.accessLecture(ctx, lectureID, viewerID)
	if err != nil {
		return ReaderView{}, err
	}

	structureKey := "results/" + lecture.CoreTaskID + "/structure.json"
	b, err := s.store.GetObject(ctx, structureKey)
	if err != nil {
		return ReaderView{}, fmt.Errorf("%w: прочитать structure.json: %v", ErrCoreUnavailable, err)
	}
	structure, err := ParseStructure(b)
	if err != nil {
		return ReaderView{}, err
	}

	view := ReaderView{
		LectureID:   lecture.ID,
		Title:       lecture.Title,
		SourceTitle: structure.Source.Title,
		SourceKind:  structure.Source.Kind,
		Duration:    structure.Source.Duration,
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
			Start: subtopic.Media.Start,
			End:   subtopic.Media.End,
			URL:   url,
		}
	}
	for _, key := range subtopic.SlideKeys {
		url, err := s.presign.PresignGet(ctx, key, s.ttl)
		if err != nil {
			return ViewSubtopic{}, fmt.Errorf("подписать слайд: %w", err)
		}
		view.SlideURLs = append(view.SlideURLs, url)
	}
	return view, nil
}
