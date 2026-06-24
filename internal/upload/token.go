package upload

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
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

func NewSigner(key []byte) *Signer {
	return &Signer{
		key: key,
		now: time.Now,
	}
}

func (s *Signer) Sign(userID, s3Key string, ttl time.Duration) string {
	exp := s.now().Add(ttl).Unix()
	payload := strings.Join([]string{userID, s3Key, strconv.FormatInt(exp, 10)}, "|")
	mac := signPayload(s.key, []byte(payload))

	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac)
}

func (s *Signer) Verify(token, expectedUserID, expectedS3Key string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return ErrBadToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrBadToken
	}

	gotMAC, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrBadToken
	}

	wantMAC := signPayload(s.key, payload)
	if !hmac.Equal(gotMAC, wantMAC) {
		return ErrBadToken
	}

	payloadParts := strings.SplitN(string(payload), "|", 3)
	if len(payloadParts) != 3 {
		return ErrBadToken
	}

	exp, err := strconv.ParseInt(payloadParts[2], 10, 64)
	if err != nil {
		return ErrBadToken
	}

	if payloadParts[0] != expectedUserID || payloadParts[1] != expectedS3Key {
		return ErrTokenMismatch
	}

	if exp <= s.now().Unix() {
		return ErrTokenExpired
	}

	return nil
}

func signPayload(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)

	return mac.Sum(nil)
}
