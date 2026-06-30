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
	findEmail        string
	createdProfile   Profile

	// Счётчики вызовов для проверки инвариантов
	findCalled   int
	createCalled int
	upsertCalled int
}

func (m *mockRepository) FindUserByEmail(_ context.Context, email string) (*User, error) {
	m.findCalled++
	m.findEmail = email
	return m.findUserResult, m.findUserErr
}
func (m *mockRepository) FindUserByID(_ context.Context, _ string) (*User, error) {
	return m.findUserResult, m.findUserErr
}
func (m *mockRepository) CreateUser(_ context.Context, p Profile) (*User, error) {
	m.createCalled++
	m.createdProfile = p
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

func TestResolveUser_CanonicalizesEmailForExistingUser(t *testing.T) {
	existingUser := &User{ID: "user-uuid-123", Email: "admin@example.com"}
	mock := &mockRepository{findUserResult: existingUser}
	svc := NewService(mock, nil, time.Hour, false)

	profile := Profile{
		Provider:      "google",
		ProviderSub:   "sub-existing",
		Email:         " Admin@Example.COM ",
		EmailVerified: true,
	}

	user, err := svc.resolveUser(context.Background(), profile)
	if err != nil {
		t.Fatalf("resolveUser: %v", err)
	}
	if user.ID != existingUser.ID {
		t.Fatalf("user.ID = %q, ожидается %q", user.ID, existingUser.ID)
	}
	if mock.findEmail != "admin@example.com" {
		t.Fatalf("FindUserByEmail email = %q, ожидается canonical email", mock.findEmail)
	}
	if mock.createCalled != 0 {
		t.Fatalf("CreateUser вызван %d раз, ожидается 0", mock.createCalled)
	}
}

func TestResolveUser_CanonicalizesEmailForNewUser(t *testing.T) {
	newUser := &User{ID: "user-uuid-new", Email: "new@example.com"}
	mock := &mockRepository{createUserResult: newUser}
	svc := NewService(mock, nil, time.Hour, false)

	profile := Profile{
		Provider:      "google",
		ProviderSub:   "sub-new",
		Email:         " New@Example.COM ",
		EmailVerified: true,
	}

	_, err := svc.resolveUser(context.Background(), profile)
	if err != nil {
		t.Fatalf("resolveUser: %v", err)
	}
	if mock.findEmail != "new@example.com" {
		t.Fatalf("FindUserByEmail email = %q, ожидается canonical email", mock.findEmail)
	}
	if mock.createdProfile.Email != "new@example.com" {
		t.Fatalf("CreateUser email = %q, ожидается canonical email", mock.createdProfile.Email)
	}
}

func TestResolveUser_EmptyCanonicalEmailFailsBeforeRepository(t *testing.T) {
	mock := &mockRepository{}
	svc := NewService(mock, nil, time.Hour, false)

	profile := Profile{
		Provider:      "google",
		ProviderSub:   "sub-empty",
		Email:         " \t ",
		EmailVerified: true,
	}

	user, err := svc.resolveUser(context.Background(), profile)
	if !errors.Is(err, ErrEmailEmpty) {
		t.Fatalf("err = %v, ожидается ErrEmailEmpty", err)
	}
	if user != nil {
		t.Fatalf("user = %+v, ожидается nil", user)
	}
	if mock.findCalled != 0 || mock.createCalled != 0 || mock.upsertCalled != 0 {
		t.Fatalf("repository вызван: find=%d create=%d upsert=%d", mock.findCalled, mock.createCalled, mock.upsertCalled)
	}
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
