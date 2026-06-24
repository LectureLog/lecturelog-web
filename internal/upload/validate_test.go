package upload

import (
	"errors"
	"testing"
)

func TestDetectMedia(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
		wantOK   bool
	}{
		{name: "video mp4", filename: "lecture.mp4", want: "video", wantOK: true},
		{name: "audio mp3", filename: "voice.mp3", want: "audio", wantOK: true},
		{name: "unsupported extension", filename: "notes.pdf", want: "", wantOK: false},
		{name: "without extension", filename: "recording", want: "", wantOK: false},
		{name: "upper case extension", filename: "SCREEN.MOV", want: "video", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := DetectMedia(tt.filename)
			if got != tt.want || gotOK != tt.wantOK {
				t.Fatalf("DetectMedia(%q) = (%q, %v), want (%q, %v)", tt.filename, got, gotOK, tt.want, tt.wantOK)
			}
		})
	}
}

func TestValidateFileMeta(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		mime     string
		size     int64
		wantErr  error
	}{
		{name: "empty filename", filename: "", size: 1, wantErr: ErrEmptyFilename},
		{name: "zero size", filename: "lecture.mp4", size: 0, wantErr: ErrEmptyFile},
		{name: "too large", filename: "lecture.mp4", size: (5 << 30) + 1, wantErr: ErrTooLarge},
		{name: "valid mp4", filename: "lecture.mp4", size: 1, wantErr: nil},
		{name: "valid mp3", filename: "voice.mp3", size: 1024, wantErr: nil},
		{name: "mime mismatch", filename: "video.mp4", mime: "image/png", size: 1, wantErr: ErrMediaMismatch},
		{name: "empty mime", filename: "video.mp4", mime: "", size: 1, wantErr: nil},
		{name: "valid video mime", filename: "video.mp4", mime: "video/mp4", size: 1, wantErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFileMeta(tt.filename, tt.mime, tt.size)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateFileMeta(%q, %q, %d) error = %v, want %v", tt.filename, tt.mime, tt.size, err, tt.wantErr)
			}
		})
	}
}

func TestValidateYouTubeURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{name: "valid short", raw: "https://youtu.be/X", wantErr: nil},
		{name: "valid youtube watch", raw: "https://youtube.com/watch?v=X", wantErr: nil},
		{name: "valid www", raw: "https://www.youtube.com/watch?v=X", wantErr: nil},
		{name: "valid mobile", raw: "https://m.youtube.com/watch?v=X", wantErr: nil},
		{name: "empty", raw: "", wantErr: ErrInvalidURL},
		{name: "not url", raw: "not-url", wantErr: ErrInvalidURL},
		{name: "foreign host", raw: "https://example.com/watch?v=X", wantErr: ErrInvalidURL},
		{name: "host confusion", raw: "https://youtu.be.evil.com/x", wantErr: ErrInvalidURL},
		{name: "ftp scheme", raw: "ftp://youtube.com/watch?v=X", wantErr: ErrInvalidURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateYouTubeURL(tt.raw)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateYouTubeURL(%q) error = %v, want %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}
