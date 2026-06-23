package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockRepository — мок интерфейса Repository для юнит-тестов логики входа.
// Не требует Postgres или сети.
type mockRepository struct {
	// Результаты вызовов
	findUserResult   *User
	findUserErr      error
	createUserResult *User
	createUserErr    error
	upsertErr        error
	createSessResult *Session
	createSessErr    error
	getSessionResult *Session
	getSessionErr    error
	deleteSessionErr error

	// Счётчики вызовов для проверки инвариантов
	findCalled   int
	createCalled int
	upsertCalled int
}

func (m *mockRepository) FindUserByEmail(_ context.Context, _ string) (*User, error) {
	m.findCalled++
	return m.findUserResult, m.findUserErr
}
func (m *mockRepository) FindUserByID(_ context.Context, _ string) (*User, error) {
	return m.findUserResult, m.findUserErr
}
func (m *mockRepository) CreateUser(_ context.Context, _ Profile) (*User, error) {
	m.createCalled++
	return m.createUserResult, m.createUserErr
}
func (m *mockRepository) UpsertIdentity(_ context.Context, _, _, _ string) error {
	m.upsertCalled++
	return m.upsertErr
}
func (m *mockRepository) CreateSession(_ context.Context, _ string, _ time.Time) (*Session, error) {
	return m.createSessResult, m.createSessErr
}
func (m *mockRepository) GetSession(_ context.Context, _ string) (*Session, error) {
	return m.getSessionResult, m.getSessionErr
}
func (m *mockRepository) DeleteSession(_ context.Context, _ string) error {
	return m.deleteSessionErr
}

// TestResolveUser_EmailNotVerified: email_verified=false → ошибка, ничего не создаётся (анти-takeover §3).
func TestResolveUser_EmailNotVerified(t *testing.T) {
	mock := &mockRepository{}
	svc := NewService(mock, nil, time.Hour, false)

	profile := Profile{
		Provider:      "google",
		ProviderSub:   "sub-1",
		Email:         "unverified@example.com",
		EmailVerified: false, // НЕ подтверждён
	}
	user, err := svc.resolveUser(context.Background(), profile)
	if err == nil {
		t.Fatal("resolveUser должен вернуть ошибку при email_verified=false")
	}
	if user != nil {
		t.Fatal("resolveUser не должен возвращать пользователя при email_verified=false")
	}
	// Ни FindUser, ни CreateUser не должны вызываться
	if mock.findCalled != 0 {
		t.Errorf("FindUserByEmail вызван %d раз, ожидается 0", mock.findCalled)
	}
	if mock.createCalled != 0 {
		t.Errorf("CreateUser вызван %d раз, ожидается 0", mock.createCalled)
	}
	t.Logf("Ожидаемая ошибка: %v", err)
}

// TestResolveUser_ExistingUser: пользователь найден → upsert identity, без CreateUser.
func TestResolveUser_ExistingUser(t *testing.T) {
	existingUser := &User{
		ID:    "user-uuid-123",
		Email: "existing@example.com",
		Name:  "Существующий",
	}
	mock := &mockRepository{
		findUserResult: existingUser,
	}
	svc := NewService(mock, nil, time.Hour, false)

	profile := Profile{
		Provider:      "google",
		ProviderSub:   "sub-existing",
		Email:         "existing@example.com",
		EmailVerified: true,
		Name:          "Существующий",
	}
	user, err := svc.resolveUser(context.Background(), profile)
	if err != nil {
		t.Fatalf("resolveUser (существующий): %v", err)
	}
	if user == nil || user.ID != existingUser.ID {
		t.Fatalf("resolveUser вернул неверного пользователя: %+v", user)
	}
	// FindUserByEmail вызван 1 раз
	if mock.findCalled != 1 {
		t.Errorf("FindUserByEmail вызван %d раз, ожидается 1", mock.findCalled)
	}
	// CreateUser НЕ должен вызываться (пользователь уже есть)
	if mock.createCalled != 0 {
		t.Errorf("CreateUser вызван %d раз, ожидается 0 (пользователь существует)", mock.createCalled)
	}
	// UpsertIdentity должен вызываться 1 раз
	if mock.upsertCalled != 1 {
		t.Errorf("UpsertIdentity вызван %d раз, ожидается 1", mock.upsertCalled)
	}
}

// TestResolveUser_NewUser: пользователь не найден → CreateUser + UpsertIdentity.
func TestResolveUser_NewUser(t *testing.T) {
	newUser := &User{
		ID:    "user-uuid-new",
		Email: "new@example.com",
		Name:  "Новый",
	}
	mock := &mockRepository{
		findUserResult:   nil, // не найден
		createUserResult: newUser,
	}
	svc := NewService(mock, nil, time.Hour, false)

	profile := Profile{
		Provider:      "google",
		ProviderSub:   "sub-new",
		Email:         "new@example.com",
		EmailVerified: true,
		Name:          "Новый",
	}
	user, err := svc.resolveUser(context.Background(), profile)
	if err != nil {
		t.Fatalf("resolveUser (новый): %v", err)
	}
	if user == nil || user.ID != newUser.ID {
		t.Fatalf("resolveUser вернул неверного пользователя: %+v", user)
	}
	// FindUserByEmail вызван 1 раз
	if mock.findCalled != 1 {
		t.Errorf("FindUserByEmail вызван %d раз, ожидается 1", mock.findCalled)
	}
	// CreateUser вызван 1 раз
	if mock.createCalled != 1 {
		t.Errorf("CreateUser вызван %d раз, ожидается 1", mock.createCalled)
	}
	// UpsertIdentity вызван 1 раз
	if mock.upsertCalled != 1 {
		t.Errorf("UpsertIdentity вызван %d раз, ожидается 1", mock.upsertCalled)
	}
}

// TestResolveUser_FindError: ошибка FindUserByEmail → ошибка возвращается наверх.
func TestResolveUser_FindError(t *testing.T) {
	mock := &mockRepository{
		findUserErr: errors.New("db: connection refused"),
	}
	svc := NewService(mock, nil, time.Hour, false)

	profile := Profile{
		Provider:      "google",
		ProviderSub:   "sub-err",
		Email:         "err@example.com",
		EmailVerified: true,
	}
	_, err := svc.resolveUser(context.Background(), profile)
	if err == nil {
		t.Fatal("resolveUser должен вернуть ошибку при ошибке FindUserByEmail")
	}
}
