package reader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseStructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		wantErr bool
	}{
		{name: "valid", fixture: "structure_valid.json"},
		{name: "minimal", fixture: "structure_minimal.json"},
		{name: "bad", fixture: "structure_bad.json", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			got, err := ParseStructure(b)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseStructure() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseStructure() error = %v", err)
			}
			if got.Source.Title == "" || len(got.Sections) == 0 {
				t.Errorf("ParseStructure() = %#v, want populated structure", got)
			}
		})
	}
}

// TestParseStructure_ClockStrings проверяет реальный формат ядра:
// duration/start/end строками "HH:MM:SS", title: null.
func TestParseStructure_ClockStrings(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"source": {"title": null, "kind": "video", "duration": "00:18:08"},
		"sections": [{
			"title": "Раздел",
			"subtopics": [{
				"title": "Подтема",
				"media": {"kind": "video", "start": "00:01:30", "end": "01:00:00", "key": "k"},
				"slide_keys": [],
				"content_md": "текст"
			}]
		}]
	}`)
	structure, err := ParseStructure(raw)
	if err != nil {
		t.Fatalf("ParseStructure() error = %v", err)
	}
	if structure.Source.Duration != 18*60+8 {
		t.Errorf("Duration = %d, want 1088", structure.Source.Duration)
	}
	media := structure.Sections[0].Subtopics[0].Media
	if media.Start != 90 || media.End != 3600 {
		t.Errorf("Start/End = %d/%d, want 90/3600", media.Start, media.End)
	}
	if structure.Source.Title != "" {
		t.Errorf("Title = %q, want empty for null", structure.Source.Title)
	}
}

// TestParseStructure_BadClock проверяет отказ на мусорном тайм-коде.
func TestParseStructure_BadClock(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"source": {"title": "т", "kind": "audio", "duration": "пять минут"},
		"sections": [{"title": "р", "subtopics": []}]
	}`)
	if _, err := ParseStructure(raw); err == nil {
		t.Fatal("ParseStructure() error = nil, want error")
	}
}
