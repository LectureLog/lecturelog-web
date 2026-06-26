package reader

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

type fakeLectureRepo struct {
	lecture *LectureMeta
	err     error
}

func (r fakeLectureRepo) FindByID(context.Context, string) (*LectureMeta, error) {
	return r.lecture, r.err
}

type fakeObjectStore struct {
	data []byte
	err  error
	key  string
}

func (s *fakeObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	s.key = key
	return s.data, s.err
}

type fakePresigner struct {
	urls map[string]string
	keys []string
	ttls []time.Duration
}

func (p *fakePresigner) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	p.keys = append(p.keys, key)
	p.ttls = append(p.ttls, ttl)
	return p.urls[key], nil
}

type fakeRenderer struct{}

func (fakeRenderer) ToHTML(md string) (string, error) { return "<p>" + md + "</p>", nil }

func TestServiceLoadAccess(t *testing.T) {
	t.Parallel()

	readyPrivate := &LectureMeta{ID: "lecture", OwnerID: "owner", Status: "ready", Visibility: "private", CoreTaskID: "task"}
	readyPublic := &LectureMeta{ID: "lecture", OwnerID: "owner", Status: "ready", Visibility: "public", CoreTaskID: "task"}
	processingPrivate := &LectureMeta{ID: "lecture", OwnerID: "owner", Status: "processing", Visibility: "private", CoreTaskID: "task"}
	processingPublic := &LectureMeta{ID: "lecture", OwnerID: "owner", Status: "processing", Visibility: "public", CoreTaskID: "task"}

	tests := []struct {
		name    string
		lecture *LectureMeta
		viewer  string
		wantErr error
	}{
		{name: "private ready owner", lecture: readyPrivate, viewer: "owner"},
		{name: "private ready other user", lecture: readyPrivate, viewer: "other", wantErr: ErrNotFound},
		{name: "private ready anonymous", lecture: readyPrivate, wantErr: ErrNotFound},
		{name: "public ready owner", lecture: readyPublic, viewer: "owner"},
		{name: "public ready other user", lecture: readyPublic, viewer: "other"},
		{name: "public ready anonymous", lecture: readyPublic},
		{name: "private processing owner", lecture: processingPrivate, viewer: "owner", wantErr: ErrNotReady},
		{name: "private processing other user", lecture: processingPrivate, viewer: "other", wantErr: ErrNotFound},
		{name: "private processing anonymous", lecture: processingPrivate, wantErr: ErrNotFound},
		{name: "public processing owner", lecture: processingPublic, viewer: "owner", wantErr: ErrNotReady},
		{name: "public processing other user", lecture: processingPublic, viewer: "other", wantErr: ErrNotFound},
		{name: "public processing anonymous", lecture: processingPublic, wantErr: ErrNotFound},
		{name: "missing lecture", wantErr: ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeObjectStore{data: validStructure(t)}
			service := NewService(fakeLectureRepo{lecture: tt.lecture}, store, &fakePresigner{}, fakeRenderer{}, 24*time.Hour)

			_, err := service.Load(context.Background(), "lecture", tt.viewer)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Load() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && store.key != "" {
				t.Fatalf("GetObject() called with %q before access was denied", store.key)
			}
		})
	}
}

func TestServiceLoadBuildsView(t *testing.T) {
	store := &fakeObjectStore{data: validStructure(t)}
	presigner := &fakePresigner{urls: map[string]string{
		"results/task-1/clip.mp4":    "https://storage.example/clip",
		"results/task-1/slide-1.png": "https://storage.example/slide",
	}}
	service := NewService(
		fakeLectureRepo{lecture: &LectureMeta{ID: "lecture", OwnerID: "owner", Status: "ready", Visibility: "private", CoreTaskID: "task-1", Title: "Моя лекция", SourceKind: "video"}},
		store,
		presigner,
		fakeRenderer{},
		24*time.Hour,
	)

	view, err := service.Load(context.Background(), "lecture", "owner")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if store.key != "results/task-1/structure.json" {
		t.Errorf("GetObject key = %q", store.key)
	}
	if !view.IsOwner || view.Title != "Моя лекция" || view.Duration != 3600 {
		t.Errorf("ReaderView = %#v", view)
	}
	if len(view.Sections) != 1 || view.Sections[0].Number != "01" || view.Sections[0].Subtopics[0].Number != "1.1" {
		t.Errorf("sections = %#v", view.Sections)
	}
	subtopic := view.Sections[0].Subtopics[0]
	if subtopic.ContentHTML != "<p># План\n\n**Важное**</p>" || subtopic.Media.URL != "https://storage.example/clip" || subtopic.SlideURLs[0] != "https://storage.example/slide" {
		t.Errorf("subtopic = %#v", subtopic)
	}
	if len(presigner.ttls) != 2 {
		t.Fatalf("PresignGet calls = %d, want 2", len(presigner.ttls))
	}
	for _, ttl := range presigner.ttls {
		if ttl != 24*time.Hour {
			t.Errorf("ttl = %s, want 24h", ttl)
		}
	}
}

func TestServiceLoadCoreUnavailable(t *testing.T) {
	service := NewService(
		fakeLectureRepo{lecture: &LectureMeta{OwnerID: "owner", Status: "ready", Visibility: "private", CoreTaskID: "task"}},
		&fakeObjectStore{err: errors.New("хранилище недоступно")},
		&fakePresigner{},
		fakeRenderer{},
		time.Hour,
	)
	if _, err := service.Load(context.Background(), "lecture", "owner"); !errors.Is(err, ErrCoreUnavailable) {
		t.Fatalf("Load() error = %v, want ErrCoreUnavailable", err)
	}
}

func TestServiceLoadInvalidStructure(t *testing.T) {
	service := NewService(
		fakeLectureRepo{lecture: &LectureMeta{OwnerID: "owner", Status: "ready", Visibility: "private", CoreTaskID: "task"}},
		&fakeObjectStore{data: []byte("{")},
		&fakePresigner{},
		fakeRenderer{},
		time.Hour,
	)
	if _, err := service.Load(context.Background(), "lecture", "owner"); err == nil {
		t.Fatal("Load() error = nil, want structure error")
	}
}

func validStructure(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/structure_valid.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return b
}
