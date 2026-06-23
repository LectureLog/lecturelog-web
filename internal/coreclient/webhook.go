package coreclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// WebhookPayload — тело исходящего вебхука ядра (ядро -> платформа).
// Источник правды по структуре — api-contract.md ядра.
// Все четыре ключа присутствуют в теле ВСЕГДА; error/error_code равны null
// при неошибочном статусе. Поэтому error/error_code — указатели: nil означает
// JSON null. error_code принимает значения {rate_limit, bad_input, internal}
// либо null.
type WebhookPayload struct {
	TaskID    string  `json:"task_id"`
	Status    string  `json:"status"`
	Error     *string `json:"error"`
	ErrorCode *string `json:"error_code"`
}

// VerifyWebhookSignature проверяет HMAC-подпись входящего вебхука ядра.
//
// Подпись считается ядром как HMAC-SHA256 от БАЙТОВ тела (ровно тех, что пришли
// по HTTP — НЕ от перемаршаленного payload) с ключом LECTURELOG_WEBHOOK_SECRET,
// и передаётся в заголовке X-Webhook-Signature в виде hex-строки.
//
// signatureHex — значение заголовка (hex; регистр не важен, т.к. сравнение идёт
// по декодированным байтам). secret — общий секрет вебхука.
//
// Сравнение выполняется в постоянном времени через hmac.Equal, чтобы исключить
// timing-атаки. Возвращает false при любой ошибке декодирования hex (без паники).
//
// ВАЖНО: это ТОЛЬКО верификатор входящего вебхука. Исходящие запросы к ядру
// coreclient НЕ подписывает (ядро инбаунд-подпись не проверяет).
func VerifyWebhookSignature(body []byte, signatureHex string, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	got, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}

	return hmac.Equal(got, expected)
}
