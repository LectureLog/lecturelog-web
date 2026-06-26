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
