package upload

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestSignVerify_RoundTrip(t *testing.T) {
	signer := NewSigner([]byte("secret-key"))
	signer.now = fixedNow

	token := signer.Sign("user-1", "uploads/user-1/video.mp4", time.Minute)

	if err := signer.Verify(token, "user-1", "uploads/user-1/video.mp4"); err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
}

func TestSignVerify_RoundTripWithPayloadSeparators(t *testing.T) {
	signer := NewSigner([]byte("secret-key"))
	signer.now = fixedNow

	token := signer.Sign("user|1", "uploads/user|1/video|part.mp4", time.Minute)

	if err := signer.Verify(token, "user|1", "uploads/user|1/video|part.mp4"); err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
}

func TestVerify_WrongUser(t *testing.T) {
	signer := NewSigner([]byte("secret-key"))
	signer.now = fixedNow

	token := signer.Sign("user-1", "uploads/user-1/video.mp4", time.Minute)

	if err := signer.Verify(token, "user-2", "uploads/user-1/video.mp4"); err != ErrTokenMismatch {
		t.Fatalf("Verify() error = %v, want %v", err, ErrTokenMismatch)
	}
}

func TestVerify_WrongS3Key(t *testing.T) {
	signer := NewSigner([]byte("secret-key"))
	signer.now = fixedNow

	token := signer.Sign("user-1", "uploads/user-1/video.mp4", time.Minute)

	if err := signer.Verify(token, "user-1", "uploads/user-1/other.mp4"); err != ErrTokenMismatch {
		t.Fatalf("Verify() error = %v, want %v", err, ErrTokenMismatch)
	}
}

func TestVerify_TamperedPayload(t *testing.T) {
	signer := NewSigner([]byte("secret-key"))
	signer.now = fixedNow

	token := signer.Sign("user-1", "uploads/user-1/video.mp4", time.Minute)
	token = tamperPayload(t, token)

	if err := signer.Verify(token, "user-1", "uploads/user-1/video.mp4"); err != ErrBadToken {
		t.Fatalf("Verify() error = %v, want %v", err, ErrBadToken)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	signer := NewSigner([]byte("secret-key"))
	signer.now = fixedNow
	otherSigner := NewSigner([]byte("other-secret-key"))
	otherSigner.now = fixedNow

	token := signer.Sign("user-1", "uploads/user-1/video.mp4", time.Minute)

	if err := otherSigner.Verify(token, "user-1", "uploads/user-1/video.mp4"); err != ErrBadToken {
		t.Fatalf("Verify() error = %v, want %v", err, ErrBadToken)
	}
}

func TestVerify_Expired(t *testing.T) {
	signer := NewSigner([]byte("secret-key"))
	signer.now = fixedNow

	token := signer.Sign("user-1", "uploads/user-1/video.mp4", -time.Second)

	if err := signer.Verify(token, "user-1", "uploads/user-1/video.mp4"); err != ErrTokenExpired {
		t.Fatalf("Verify() error = %v, want %v", err, ErrTokenExpired)
	}
}

func TestVerify_Garbage(t *testing.T) {
	signer := NewSigner([]byte("secret-key"))
	signer.now = fixedNow

	for _, token := range []string{"no-dot", "not.base64"} {
		t.Run(token, func(t *testing.T) {
			if err := signer.Verify(token, "user-1", "uploads/user-1/video.mp4"); err != ErrBadToken {
				t.Fatalf("Verify() error = %v, want %v", err, ErrBadToken)
			}
		})
	}
}

func fixedNow() time.Time {
	return time.Unix(1_700_000_000, 0)
}

func tamperPayload(t *testing.T, token string) string {
	t.Helper()

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("token parts = %d, want 2", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("payload is empty")
	}

	payload[0] ^= 1
	parts[0] = base64.RawURLEncoding.EncodeToString(payload)

	return strings.Join(parts, ".")
}
