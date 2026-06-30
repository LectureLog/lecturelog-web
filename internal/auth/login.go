package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrEmailNotVerified возвращается, когда провайдер не подтвердил email пользователя.
// Политика §3: вход разрешён ТОЛЬКО при email_verified=true (анти-account-takeover).
var ErrEmailNotVerified = errors.New("auth: email пользователя не подтверждён провайдером")

// ErrEmailEmpty возвращается, когда после нормализации email пользователя пуст.
var ErrEmailEmpty = errors.New("auth: email пользователя пуст после нормализации")

func canonicalEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// resolveUser реализует чистую доменную логику привязки OAuth-профиля к пользователю платформы.
//
// Алгоритм (политика §3, email = личность):
//  1. email_verified=false → ошибка ErrEmailNotVerified; ничего не создаём (анти-takeover).
//  2. FindUserByEmail: пользователь найден → UpsertIdentity, возвращаем существующего.
//  3. FindUserByEmail: не найден → CreateUser + UpsertIdentity, возвращаем нового.
//
// Метод не зависит от Postgres или сети — Repository инъектируется (мок в тестах).
func (s *Service) resolveUser(ctx context.Context, p Profile) (*User, error) {
	// Шаг 1: проверка email_verified ОБЯЗАТЕЛЬНА (анти-account-takeover, §3).
	// Матч по email разрешён ТОЛЬКО при email_verified=true.
	if !p.EmailVerified {
		return nil, ErrEmailNotVerified
	}
	p.Email = canonicalEmail(p.Email)
	if p.Email == "" {
		return nil, ErrEmailEmpty
	}

	// Шаг 2: поиск существующего пользователя по email (email = личность, §3).
	existing, err := s.repo.FindUserByEmail(ctx, p.Email)
	if err != nil {
		return nil, fmt.Errorf("auth: поиск пользователя по email: %w", err)
	}

	if existing != nil {
		// Пользователь найден → обновляем/создаём связь провайдер↔пользователь.
		// CreateUser не вызываем — пользователь уже существует.
		if err := s.repo.UpsertIdentity(ctx, p.Provider, p.ProviderSub, existing.ID); err != nil {
			return nil, fmt.Errorf("auth: upsert identity (существующий): %w", err)
		}
		return existing, nil
	}

	// Шаг 3: пользователь не найден → регистрируем нового.
	newUser, err := s.repo.CreateUser(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("auth: создание пользователя: %w", err)
	}

	if err := s.repo.UpsertIdentity(ctx, p.Provider, p.ProviderSub, newUser.ID); err != nil {
		return nil, fmt.Errorf("auth: upsert identity (новый): %w", err)
	}

	return newUser, nil
}
