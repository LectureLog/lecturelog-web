package syncsvc

import (
	"context"
	"testing"
	"time"

	"github.com/LectureLog/lecturelog-web/internal/auth"
)

func TestNewServiceStoresDependencies(t *testing.T) {
	repo := &mockRepo{}
	core := &mockCore{}

	svc := NewService(repo, core, "secret")

	if svc.repo != repo {
		t.Fatal("repo не сохранён в сервисе")
	}
	if svc.core != core {
		t.Fatal("core не сохранён в сервисе")
	}
	if svc.webhookSecret != "secret" {
		t.Fatalf("webhookSecret = %q, ожидается secret", svc.webhookSecret)
	}
}

func TestHandleWebhook_AffectedZeroStillNoContent(t *testing.T) {
	body := []byte(`{"task_id":"task-dup","status":"ready","error":null,"error_code":null}`)
	repo := &mockRepo{affected: 0}
	svc := NewService(repo, &mockCore{}, "secret")

	rec := performWebhook(svc, body, signWebhook(body, "secret"))

	if rec.Code != 204 {
		t.Fatalf("код = %d, ожидается 204", rec.Code)
	}
	if len(repo.updates) != 1 {
		t.Fatalf("updates = %d, ожидается 1", len(repo.updates))
	}
}

func TestHandleWebhook_TerminalAffectedTriggersEmailHookPlaceholder(t *testing.T) {
	body := []byte(`{"task_id":"task-ready","status":"ready","error":null,"error_code":null}`)
	repo := &mockRepo{affected: 1}
	svc := NewService(repo, &mockCore{}, "secret")
	var called bool
	svc.onTerminalWebhook = func() { called = true }

	rec := performWebhook(svc, body, signWebhook(body, "secret"))

	if rec.Code != 204 {
		t.Fatalf("код = %d, ожидается 204", rec.Code)
	}
	if !called {
		t.Fatal("ветка email-хука должна быть достигнута для terminal+affected>0")
	}
}

func TestHandleWebhook_NonTerminalDoesNotTriggerEmailHookPlaceholder(t *testing.T) {
	body := []byte(`{"task_id":"task-processing","status":"processing","error":null,"error_code":null}`)
	repo := &mockRepo{affected: 1}
	svc := NewService(repo, &mockCore{}, "secret")
	svc.onTerminalWebhook = func() { t.Fatal("email-хук не должен вызываться для processing") }

	rec := performWebhook(svc, body, signWebhook(body, "secret"))

	if rec.Code != 204 {
		t.Fatalf("код = %d, ожидается 204", rec.Code)
	}
}

type updateCall struct {
	taskID    string
	status    string
	errorCode string
}

type mockRepo struct {
	lecture  *LectureView
	updates  []updateCall
	affected int64
	err      error
}

func (m *mockRepo) UpdateStatusConditional(ctx context.Context, taskID, status, errorCode string) (int64, error) {
	m.updates = append(m.updates, updateCall{taskID: taskID, status: status, errorCode: errorCode})
	if m.err != nil {
		return 0, m.err
	}
	return m.affected, nil
}

func (m *mockRepo) FindByID(ctx context.Context, id string) (*LectureView, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.lecture, nil
}

type mockCore struct {
	progress          *TaskProgress
	err               error
	getTaskStatusFunc func(ctx context.Context, taskID string) (*TaskProgress, error)
}

func (m *mockCore) GetTaskStatus(ctx context.Context, taskID string) (*TaskProgress, error) {
	if m.getTaskStatusFunc != nil {
		return m.getTaskStatusFunc(ctx, taskID)
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.progress, nil
}

type mockAuthRepo struct {
	session *auth.Session
	user    *auth.User
}

func (m *mockAuthRepo) FindUserByEmail(ctx context.Context, email string) (*auth.User, error) {
	return nil, nil
}

func (m *mockAuthRepo) FindUserByID(ctx context.Context, userID string) (*auth.User, error) {
	return m.user, nil
}

func (m *mockAuthRepo) CreateUser(ctx context.Context, p auth.Profile) (*auth.User, error) {
	return nil, nil
}

func (m *mockAuthRepo) UpsertIdentity(ctx context.Context, provider, providerSub, userID string) error {
	return nil
}

func (m *mockAuthRepo) CreateSession(ctx context.Context, userID string, expiresAt time.Time) (*auth.Session, error) {
	return nil, nil
}

func (m *mockAuthRepo) GetSession(ctx context.Context, sessionID string) (*auth.Session, error) {
	return m.session, nil
}

func (m *mockAuthRepo) DeleteSession(ctx context.Context, sessionID string) error {
	return nil
}
