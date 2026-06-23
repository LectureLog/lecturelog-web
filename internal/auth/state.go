package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
)

// newState генерирует случайный state для OAuth 2.0 (защита от CSRF при login).
// 32 байта из crypto/rand → base64url без padding ≈ 43 символа.
// Секрет не логируется — передаётся только в куку и AuthCodeURL.
func newState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// compareState сверяет полученный state с ожидаемым constant-time способом
// (защита от timing-атак). Пустой state → всегда false (анти-takeover).
func compareState(a, b string) bool {
	// Пустая строка с любой стороны — немедленный отказ.
	if a == "" || b == "" {
		return false
	}
	// subtle.ConstantTimeCompare работает с байтами; длины должны совпадать.
	// Разная длина → 0 (false) без временной утечки.
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
