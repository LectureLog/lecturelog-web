package coreclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// signBody считает эталонную подпись тем же способом, что и ядро:
// HMAC-SHA256 от БАЙТОВ тела, результат в hex (нижний регистр).
func signBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignature_Valid(t *testing.T) {
	body := []byte(`{"task_id":"t1","status":"ready","error":null,"error_code":null}`)
	secret := "supersecret"
	sig := signBody(body, secret)

	if !VerifyWebhookSignature(body, sig, secret) {
		t.Fatal("ожидалась валидная подпись, получили false")
	}
}

func TestVerifyWebhookSignature_WrongSecret(t *testing.T) {
	body := []byte(`{"task_id":"t1","status":"ready","error":null,"error_code":null}`)
	sig := signBody(body, "supersecret")

	if VerifyWebhookSignature(body, sig, "anothersecret") {
		t.Fatal("подпись с чужим секретом должна отвергаться")
	}
}

func TestVerifyWebhookSignature_TamperedBody(t *testing.T) {
	body := []byte(`{"task_id":"t1","status":"ready","error":null,"error_code":null}`)
	secret := "supersecret"
	sig := signBody(body, secret)

	// Меняем один байт тела.
	tampered := make([]byte, len(body))
	copy(tampered, body)
	tampered[1] ^= 0x01

	if VerifyWebhookSignature(tampered, sig, secret) {
		t.Fatal("изменённое тело должно отвергаться")
	}
}

func TestVerifyWebhookSignature_BadHex(t *testing.T) {
	body := []byte(`{"task_id":"t1","status":"ready"}`)
	// "zz" не парсится как hex — ожидаем false без паники.
	if VerifyWebhookSignature(body, "zzzz", "supersecret") {
		t.Fatal("некорректный hex должен давать false")
	}
}

func TestVerifyWebhookSignature_EmptySignature(t *testing.T) {
	body := []byte(`{"task_id":"t1","status":"ready"}`)
	if VerifyWebhookSignature(body, "", "supersecret") {
		t.Fatal("пустая подпись должна давать false")
	}
}

// case_insensitive_hex: ядро шлёт hex в нижнем регистре, но сравнение идёт по
// ДЕКОДИРОВАННЫМ байтам через hmac.Equal, поэтому регистр hex не важен.
func TestVerifyWebhookSignature_CaseInsensitiveHex(t *testing.T) {
	body := []byte(`{"task_id":"t1","status":"ready"}`)
	secret := "supersecret"
	sig := signBody(body, secret)
	upper := strings.ToUpper(sig)

	if !VerifyWebhookSignature(body, upper, secret) {
		t.Fatal("hex в верхнем регистре должен считаться валидным")
	}
}

// Десериализация тела вебхука: все ключи присутствуют всегда; error/error_code
// — указатели, чтобы отличать null от отсутствия.
func TestWebhookPayload_Unmarshal(t *testing.T) {
	t.Run("failed", func(t *testing.T) {
		raw := []byte(`{"task_id":"t1","status":"failed","error":"boom","error_code":"bad_input"}`)
		var p WebhookPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("неожиданная ошибка размаршалинга: %v", err)
		}
		if p.TaskID != "t1" || p.Status != "failed" {
			t.Fatalf("неверные поля: %+v", p)
		}
		if p.Error == nil || *p.Error != "boom" {
			t.Fatalf("ожидали error=boom, получили %v", p.Error)
		}
		if p.ErrorCode == nil || *p.ErrorCode != "bad_input" {
			t.Fatalf("ожидали error_code=bad_input, получили %v", p.ErrorCode)
		}
	})

	t.Run("ready_nulls", func(t *testing.T) {
		raw := []byte(`{"task_id":"t1","status":"ready","error":null,"error_code":null}`)
		var p WebhookPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("неожиданная ошибка размаршалинга: %v", err)
		}
		if p.Error != nil {
			t.Fatalf("ожидали error=nil, получили %v", *p.Error)
		}
		if p.ErrorCode != nil {
			t.Fatalf("ожидали error_code=nil, получили %v", *p.ErrorCode)
		}
	})
}
