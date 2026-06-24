package upload

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrBadToken      = errors.New("upload: некорректный токен")
	ErrTokenExpired  = errors.New("upload: срок действия токена истёк")
	ErrTokenMismatch = errors.New("upload: токен не соответствует загрузке")
)

type Signer struct {
	key []byte
	now func() time.Time
}

type tokenPayload struct {
	UserID string `json:"user_id"`
	S3Key  string `json:"s3_key"`
	Media  string `json:"media"`
	Exp    int64  `json:"exp"`
}

func NewSigner(key []byte) *Signer {
	return &Signer{
		key: key,
		now: time.Now,
	}
}

func (s *Signer) Sign(userID, s3Key, media string, ttl time.Duration) string {
	payload, err := json.Marshal(tokenPayload{
		UserID: userID,
		S3Key:  s3Key,
		Media:  media,
		Exp:    s.now().Add(ttl).Unix(),
	})
	if err != nil {
		return ""
	}

	mac := signPayload(s.key, payload)

	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac)
}

func (s *Signer) Verify(token, expectedUserID, expectedS3Key string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", ErrBadToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrBadToken
	}

	gotMAC, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrBadToken
	}

	wantMAC := signPayload(s.key, payload)
	if !hmac.Equal(gotMAC, wantMAC) {
		return "", ErrBadToken
	}

	var decoded tokenPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", ErrBadToken
	}

	if decoded.Exp == 0 {
		return "", ErrBadToken
	}

	if decoded.UserID != expectedUserID || decoded.S3Key != expectedS3Key {
		return "", ErrTokenMismatch
	}

	if decoded.Exp <= s.now().Unix() {
		return "", ErrTokenExpired
	}

	return decoded.Media, nil
}

func signPayload(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)

	return mac.Sum(nil)
}
