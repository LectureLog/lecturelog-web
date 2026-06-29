package syncsvc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/go-chi/chi/v5"
)

func TestHandleWebhook_ValidSignatureUpdatesStatus(t *testing.T) {
	body := []byte(`{"task_id":"task-1","status":"ready","error":null,"error_code":null}`)
	repo := &mockRepo{}
	svc := NewService(repo, &mockCore{}, "secret")

	rec := performWebhook(svc, body, signWebhook(body, "secret"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код = %d, ожидается 204", rec.Code)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("updates = %d, ожидается 1", len(repo.updates))
	}
	assertUpdate(t, repo.updates[0], "task-1", "ready", "")
}

func TestHandleWebhook_InvalidSignatureUnauthorized(t *testing.T) {
	body := []byte(`{"task_id":"task-1","status":"ready","error":null,"error_code":null}`)
	repo := &mockRepo{}
	svc := NewService(repo, &mockCore{}, "secret")

	rec := performWebhook(svc, body, "bad-signature")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("код = %d, ожидается 401", rec.Code)
	}
	if len(repo.updates) != 0 {
		t.Fatalf("update не должен вызываться, вызовов: %d", len(repo.updates))
	}
}

func TestHandleWebhook_MissingSignatureUnauthorized(t *testing.T) {
	body := []byte(`{"task_id":"task-1","status":"ready","error":null,"error_code":null}`)
	repo := &mockRepo{}
	svc := NewService(repo, &mockCore{}, "secret")

	rec := performWebhook(svc, body, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("код = %d, ожидается 401", rec.Code)
	}
	if len(repo.updates) != 0 {
		t.Fatalf("update не должен вызываться, вызовов: %d", len(repo.updates))
	}
}

func TestHandleWebhook_BadJSONWithValidSignatureBadRequest(t *testing.T) {
	body := []byte(`{"task_id":`)
	repo := &mockRepo{}
	svc := NewService(repo, &mockCore{}, "secret")

	rec := performWebhook(svc, body, signWebhook(body, "secret"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код = %d, ожидается 400", rec.Code)
	}
	if len(repo.updates) != 0 {
		t.Fatalf("update не должен вызываться, вызовов: %d", len(repo.updates))
	}
}

func TestHandleWebhook_BodyTooLargeReturnsClientError(t *testing.T) {
	body := bytes.Repeat([]byte("a"), maxWebhookBodyBytes+1)
	repo := &mockRepo{}
	svc := NewService(repo, &mockCore{}, "secret")

	rec := performWebhook(svc, body, signWebhook(body, "secret"))

	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("код = %d, ожидается 4xx", rec.Code)
	}
	if rec.Code == http.StatusOK {
		t.Fatal("код не должен быть 200")
	}
	if len(repo.updates) != 0 {
		t.Fatalf("update не должен вызываться, вызовов: %d", len(repo.updates))
	}
}

func TestHandleWebhook_InvalidStatusWithValidSignatureBadRequest(t *testing.T) {
	body := []byte(`{"task_id":"task-1","status":"garbage","error":null,"error_code":null}`)
	repo := &mockRepo{}
	svc := NewService(repo, &mockCore{}, "secret")

	rec := performWebhook(svc, body, signWebhook(body, "secret"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("код = %d, ожидается 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid status") {
		t.Fatalf("ответ должен содержать invalid status: %s", rec.Body.String())
	}
	if len(repo.updates) != 0 {
		t.Fatalf("UpdateStatusConditional не должен вызываться, вызовов: %d", len(repo.updates))
	}
}

func TestHandleWebhook_FailedPassesErrorCode(t *testing.T) {
	body := []byte(`{"task_id":"task-2","status":"failed","error":"bad file","error_code":"bad_input"}`)
	repo := &mockRepo{}
	svc := NewService(repo, &mockCore{}, "secret")

	rec := performWebhook(svc, body, signWebhook(body, "secret"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("код = %d, ожидается 204", rec.Code)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("updates = %d, ожидается 1", len(repo.updates))
	}
	assertUpdate(t, repo.updates[0], "task-2", "failed", "bad_input")
}

func TestHandlePollStatus_NotFoundAndForeignAre404(t *testing.T) {
	cases := []struct {
		name    string
		lecture *LectureView
	}{
		{name: "нет лекции"},
		{name: "чужая лекция", lecture: testLecture("lec-1", "other", "processing")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{lecture: tc.lecture}
			svc := NewService(repo, &mockCore{}, "secret")

			rec := performPoll(svc, "lec-1", "owner")

			if rec.Code != http.StatusNotFound {
				t.Fatalf("код = %d, ожидается 404", rec.Code)
			}
		})
	}
}

func TestHandlePollStatus_DBReadySkipsCore(t *testing.T) {
	repo := &mockRepo{lecture: testLecture("lec-1", "owner", "ready")}
	core := &mockCore{
		getTaskStatusFunc: func(ctx context.Context, taskID string) (*TaskProgress, error) {
			t.Fatal("GetTaskStatus не должен вызываться для терминального статуса БД")
			return nil, nil
		},
	}
	svc := NewService(repo, core, "secret")

	rec := performPoll(svc, "lec-1", "owner")

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Готово") {
		t.Fatalf("карточка должна содержать статус Готово: %s", rec.Body.String())
	}
	if len(repo.updates) != 0 {
		t.Fatalf("БД не должна обновляться, вызовов: %d", len(repo.updates))
	}
}

func TestHandlePollStatus_CoreReadyFallbackUpdatesAndRendersReady(t *testing.T) {
	repo := &mockRepo{lecture: testLecture("lec-1", "owner", "processing")}
	core := &mockCore{progress: &TaskProgress{Status: "ready", ProgressPct: 100}}
	svc := NewService(repo, core, "secret")

	rec := performPoll(svc, "lec-1", "owner")

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", rec.Code)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("updates = %d, ожидается 1", len(repo.updates))
	}
	assertUpdate(t, repo.updates[0], "task-lec-1", "ready", "")
	if !strings.Contains(rec.Body.String(), "Готово") {
		t.Fatalf("карточка должна содержать статус Готово: %s", rec.Body.String())
	}
}

func TestHandlePollStatus_CoreFailedFallbackUpdatesAndRendersFailed(t *testing.T) {
	repo := &mockRepo{lecture: testLecture("lec-1", "owner", "processing")}
	core := &mockCore{progress: &TaskProgress{Status: "failed", ErrorCode: "rate_limit"}}
	svc := NewService(repo, core, "secret")

	rec := performPoll(svc, "lec-1", "owner")

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", rec.Code)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("updates = %d, ожидается 1", len(repo.updates))
	}
	assertUpdate(t, repo.updates[0], "task-lec-1", "failed", "rate_limit")
	if !strings.Contains(rec.Body.String(), "Ошибка") {
		t.Fatalf("карточка должна содержать статус Ошибка: %s", rec.Body.String())
	}
}

func TestHandlePollStatus_CoreStillProcessingDoesNotUpdateDB(t *testing.T) {
	repo := &mockRepo{lecture: testLecture("lec-1", "owner", "processing")}
	core := &mockCore{progress: &TaskProgress{Status: "processing", Stage: "transcribing", ProgressPct: 42}}
	svc := NewService(repo, core, "secret")

	rec := performPoll(svc, "lec-1", "owner")

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", rec.Code)
	}
	if len(repo.updates) != 0 {
		t.Fatalf("БД не должна обновляться, вызовов: %d", len(repo.updates))
	}
	if !strings.Contains(rec.Body.String(), "Обработка") {
		t.Fatalf("карточка должна содержать статус Обработка: %s", rec.Body.String())
	}
}

func TestHandlePollStatus_CoreErrorFallsBackToDBCard(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{name: "обычная ошибка", err: errors.New("core down")},
		{name: "задача не найдена", err: ErrTaskNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{lecture: testLecture("lec-1", "owner", "processing")}
			core := &mockCore{err: tc.err}
			svc := NewService(repo, core, "secret")

			rec := performPoll(svc, "lec-1", "owner")

			if rec.Code != http.StatusOK {
				t.Fatalf("код = %d, ожидается 200", rec.Code)
			}
			if len(repo.updates) != 0 {
				t.Fatalf("БД не должна обновляться, вызовов: %d", len(repo.updates))
			}
			if !strings.Contains(rec.Body.String(), "Обработка") {
				t.Fatalf("карточка должна содержать статус БД: %s", rec.Body.String())
			}
		})
	}
}

func TestHandlePollStatus_ProcessingCardHasPollingTrigger(t *testing.T) {
	repo := &mockRepo{lecture: testLecture("lec-1", "owner", "processing")}
	// ядро всё ещё обрабатывает → карточка остаётся processing
	core := &mockCore{progress: &TaskProgress{Status: "processing", ProgressPct: 10}}
	svc := NewService(repo, core, "secret")

	rec := performPoll(svc, "lec-1", "owner")

	body := rec.Body.String()
	if !strings.Contains(body, `hx-trigger`) {
		t.Fatalf("processing-карточка должна содержать hx-trigger для self-polling: %s", body)
	}
	if !strings.Contains(body, "/lectures/lec-1/status") {
		t.Fatalf("processing-карточка должна опрашивать /lectures/lec-1/status: %s", body)
	}
	// поллинг только при видимой вкладке — не долбим ядро в фоне
	if !strings.Contains(body, "document.visibilityState") {
		t.Fatalf("hx-trigger должен ограничиваться видимой вкладкой: %s", body)
	}
}

func TestHandlePollStatus_TerminalCardHasNoPollingTrigger(t *testing.T) {
	cases := []string{"ready", "failed"}
	for _, status := range cases {
		t.Run(status, func(t *testing.T) {
			repo := &mockRepo{lecture: testLecture("lec-1", "owner", status)}
			svc := NewService(repo, &mockCore{}, "secret")

			rec := performPoll(svc, "lec-1", "owner")

			body := rec.Body.String()
			if strings.Contains(body, "hx-trigger") {
				t.Fatalf("терминальная карточка (%s) НЕ должна содержать hx-trigger (поллинг обязан остановиться): %s", status, body)
			}
		})
	}
}

func performWebhook(svc *Service, body []byte, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/core", bytes.NewReader(body))
	if signature != "" {
		req.Header.Set(webhookSignatureHeader, signature)
	}
	rec := httptest.NewRecorder()
	svc.HandleWebhook(rec, req)
	return rec
}

func performPoll(svc *Service, lectureID, ownerID string) *httptest.ResponseRecorder {
	authSvc := auth.NewService(&mockAuthRepo{
		session: &auth.Session{ID: "sess-1", UserID: ownerID, ExpiresAt: time.Now().Add(time.Hour)},
		user:    &auth.User{ID: ownerID, Email: ownerID + "@example.com"},
	}, nil, time.Hour, false)

	r := chi.NewRouter()
	r.Use(authSvc.LoadSession)
	r.Group(func(pr chi.Router) {
		pr.Use(authSvc.RequireAuth)
		pr.Get("/lectures/{id}/status", svc.HandlePollStatus)
	})

	req := httptest.NewRequest(http.MethodGet, "/lectures/"+lectureID+"/status", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: "ll_session", Value: "sess-1"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func signWebhook(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func testLecture(id, ownerID, status string) *LectureView {
	return &LectureView{
		ID:         id,
		OwnerID:    ownerID,
		CoreTaskID: "task-" + id,
		Status:     status,
		Title:      "Тестовая лекция",
		SourceKind: "audio",
		Visibility: "private",
		UpdatedAt:  time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC),
	}
}

func assertUpdate(t *testing.T, got updateCall, taskID, status, errorCode string) {
	t.Helper()
	if got.taskID != taskID || got.status != status || got.errorCode != errorCode {
		t.Fatalf("update = (%q,%q,%q), ожидается (%q,%q,%q)",
			got.taskID, got.status, got.errorCode, taskID, status, errorCode)
	}
}
