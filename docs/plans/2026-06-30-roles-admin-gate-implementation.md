# Roles Admin Gate Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a server-side admin gate for `/settings*` based on optional `ADMIN_EMAILS`, canonical user emails, `IsAdmin` layout state, and neutral `cookies_invalid` user-facing text.

**Architecture:** Keep roles out of the database: admin status is derived on every request by comparing canonical `auth.User.Email` with the normalized `ADMIN_EMAILS` allowlist. `auth.Service` owns allowlist parsing at service construction, global `LoadAdmin` context population, and `RequireAdmin` enforcement; `web.NewLayoutData` consumes context state so handlers cannot forget CSRF or admin status. Existing settings routes must be mounted once, inside `RequireAuth -> RequireAdmin`, when the cookies-UI settings service is present.

**Tech Stack:** Go, chi, templ, gorilla/csrf, pgx/tern migrations, testcontainers for integration migration tests, Makefile targets `templ`, `test`, `gate`, `migrate-test`.

---

## Implementation Notes / Risks

- This worktree currently does **not** contain `internal/settings` or `internal/web/page_settings.templ`; those are introduced by the YouTube cookies UI plan. Do not invent a fake settings UI in this task. If `internal/settings` exists when this plan is executed, mount it inside the protected admin group. If it does not exist, implement all admin infrastructure and leave the actual `settingsSvc.Mount(ar)` wiring for the merge point with cookies-UI Tasks 1-4. Final merge/deploy is not acceptable until `/settings*` routes, once present, live under `RequireAuth -> RequireAdmin`.
- Use a variadic auth option, `auth.WithAdminEmails(...)`, so existing tests and call sites using `auth.NewService(repo, provider, ttl, secure)` continue to compile until they intentionally opt in.
- Email canonicalization is `strings.TrimSpace` + `strings.ToLower`; do not add Gmail-specific rules.
- Migration `004_normalize_user_emails.sql` cannot restore original case/spacing on down migration. Use a documented no-op down section.
- `web.NewLayoutData` imports `internal/auth`. This is acceptable if `auth` still does not import `web`; verify with `go test ./internal/web ./internal/auth`.
- The admin settings gear must not point at an unmounted `/settings` route. `LayoutData`
  carries both `IsAdmin` and `SettingsAvailable`; the gear renders only when both are true.
  The cookies-UI merge must set the settings flag globally when real settings routes are
  mounted.
- Code comments in snippets below are in Russian. Do not add any generated-author attribution to comments, commits, or PR text.

## Task 1: Parse `ADMIN_EMAILS` In Config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/load_test.go`

**Step 1: Write failing tests**

Add these tests to `internal/config/load_test.go` near optional-key tests:

```go
func TestLoad_AdminEmailsMissingIsOptional(t *testing.T) {
	env := fullEnv()
	delete(env, "ADMIN_EMAILS")

	cfg, err := Load(makeGetenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.AdminEmails) != 0 {
		t.Fatalf("AdminEmails = %#v, ожидается пустой список", cfg.AdminEmails)
	}
}

func TestLoad_AdminEmailsCanonicalizesList(t *testing.T) {
	env := fullEnv()
	env["ADMIN_EMAILS"] = " Admin@Example.COM, second@example.com "

	cfg, err := Load(makeGetenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"admin@example.com", "second@example.com"}
	if !slices.Equal(cfg.AdminEmails, want) {
		t.Fatalf("AdminEmails = %#v, ожидается %#v", cfg.AdminEmails, want)
	}
}

func TestLoad_AdminEmailsDropsEmptyItems(t *testing.T) {
	env := fullEnv()
	env["ADMIN_EMAILS"] = "admin@example.com,, ,owner@example.com,"

	cfg, err := Load(makeGetenv(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"admin@example.com", "owner@example.com"}
	if !slices.Equal(cfg.AdminEmails, want) {
		t.Fatalf("AdminEmails = %#v, ожидается %#v", cfg.AdminEmails, want)
	}
}
```

Add `slices` to the test imports.

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/config -run 'TestLoad_AdminEmails' -v
```

Expected: FAIL to compile because `Config.AdminEmails` does not exist.

**Step 3: Implement minimal config support**

In `internal/config/config.go`:

- Add `AdminEmails []string` to `Config`.
- In optional parsing, read `ADMIN_EMAILS` after the other optional keys or before TTL parsing.
- Use a small helper so the behavior is easy to test:

```go
func parseAdminEmails(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	emails := make([]string, 0, len(parts))
	for _, part := range parts {
		email := strings.ToLower(strings.TrimSpace(part))
		if email == "" {
			continue
		}
		emails = append(emails, email)
	}
	return emails
}
```

Call `cfg.AdminEmails = parseAdminEmails(getenv("ADMIN_EMAILS"))`.

**Step 4: Run tests to verify pass**

Run:

```bash
go test ./internal/config -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/load_test.go
git commit -m "feat(auth): read admin email allowlist"
```

## Task 2: Canonicalize OAuth Email In `resolveUser`

**Files:**
- Modify: `internal/auth/login.go`
- Modify: `internal/auth/login_test.go`

**Step 1: Extend test mock to capture arguments**

In `internal/auth/login_test.go`, add fields to `mockRepository`:

```go
findEmail      string
createdProfile Profile
```

Update methods:

```go
func (m *mockRepository) FindUserByEmail(_ context.Context, email string) (*User, error) {
	m.findCalled++
	m.findEmail = email
	return m.findUserResult, m.findUserErr
}

func (m *mockRepository) CreateUser(_ context.Context, p Profile) (*User, error) {
	m.createCalled++
	m.createdProfile = p
	return m.createUserResult, m.createUserErr
}
```

Existing tests should still pass after only this mock change.

**Step 2: Write failing tests**

Add:

```go
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
```

**Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/auth -run 'TestResolveUser_(CanonicalizesEmail|EmptyCanonicalEmail)' -v
```

Expected: FAIL because email is not normalized and `ErrEmailEmpty` is undefined.

**Step 4: Implement canonicalization**

In `internal/auth/login.go`:

- Import `strings`.
- Add:

```go
var ErrEmailEmpty = errors.New("auth: email пользователя пуст после нормализации")

func canonicalEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
```

- In `resolveUser`, immediately after `EmailVerified` check:

```go
p.Email = canonicalEmail(p.Email)
if p.Email == "" {
	return nil, ErrEmailEmpty
}
```

Do not normalize `ProviderSub`, `Name`, or `AvatarURL`.

**Step 5: Run tests**

Run:

```bash
go test ./internal/auth -run TestResolveUser -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/auth/login.go internal/auth/login_test.go
git commit -m "feat(auth): canonicalize oauth email"
```

## Task 3: Add Email Normalization Migration

**Files:**
- Create: `internal/db/migrations/004_normalize_user_emails.sql`
- Modify: `internal/db/migrate_integration_test.go`

**Step 1: Add integration test helper**

In `internal/db/migrate_integration_test.go`, add imports:

```go
import (
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
)
```

Factor the container startup currently inline in `TestMigrateIntegration` into this helper near the top of the file:

```go
func newTestPostgresPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("lecturelogtest"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("testcontainers: не удалось запустить Postgres: %v", err)
	}
	t.Cleanup(func() {
		if terr := testcontainers.TerminateContainer(pgContainer); terr != nil {
			t.Logf("TerminateContainer: %v", terr)
		}
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}

	pool, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}
```

Then change `TestMigrateIntegration` to use:

```go
pool := newTestPostgresPool(t, ctx)
```

and remove the duplicated container setup plus the old `defer pool.Close()` from that test.

Add a second helper:

```go
func migrateToVersionForTest(ctx context.Context, t *testing.T, pool *pgxpool.Pool, version int32) {
	t.Helper()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()

	migrator, err := migrate.NewMigrator(ctx, conn.Conn(), "public.schema_version")
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}

	subFS, err := migrationsSubFS()
	if err != nil {
		t.Fatalf("migrationsSubFS: %v", err)
	}
	if err := migrator.LoadMigrations(subFS); err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if err := migrator.MigrateTo(ctx, version); err != nil {
		t.Fatalf("MigrateTo(%d): %v", version, err)
	}
}
```

**Step 2: Write failing migration tests**

Add two integration tests. Do not try to run these in default `go test ./...`.

```go
func TestMigrateIntegration_NormalizesExistingUserEmails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool := newTestPostgresPool(t, ctx)

	migrateToVersionForTest(ctx, t, pool, 3)

	_, err := pool.Exec(ctx, `
		INSERT INTO users (email, name)
		VALUES (' Admin@Example.COM ', 'Admin')
	`)
	if err != nil {
		t.Fatalf("insert dirty user: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var email string
	err = pool.QueryRow(ctx, `SELECT email FROM users WHERE name = 'Admin'`).Scan(&email)
	if err != nil {
		t.Fatalf("select email: %v", err)
	}
	if email != "admin@example.com" {
		t.Fatalf("email = %q, ожидается admin@example.com", email)
	}
}

func TestMigrateIntegration_FailsOnDuplicateCanonicalEmails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool := newTestPostgresPool(t, ctx)

	migrateToVersionForTest(ctx, t, pool, 3)

	_, err := pool.Exec(ctx, `
		INSERT INTO users (email, name)
		VALUES ('Admin@Example.COM', 'Admin 1'), (' admin@example.com ', 'Admin 2')
	`)
	if err != nil {
		t.Fatalf("insert duplicate users: %v", err)
	}

	err = Migrate(ctx, pool)
	if err == nil {
		t.Fatal("Migrate должен упасть на дублях после canonicalization")
	}
	if !strings.Contains(err.Error(), "duplicate users.email after canonicalization") {
		t.Fatalf("ошибка = %v, ожидается понятное сообщение про дубли", err)
	}
}
```

**Step 3: Run tests to verify failure**

Run:

```bash
go test -tags=integration ./internal/db/... -run 'TestMigrateIntegration_(NormalizesExistingUserEmails|FailsOnDuplicateCanonicalEmails)' -v
```

Expected: FAIL because migration 004 does not exist or version 4 does not change emails.

**Step 4: Create migration**

Create `internal/db/migrations/004_normalize_user_emails.sql`:

```sql
-- 004: канонизация users.email перед введением ADMIN_EMAILS.

DO $$
DECLARE
    duplicate_email text;
BEGIN
    IF EXISTS (
        SELECT 1
        FROM users
        WHERE lower(btrim(email)) = ''
    ) THEN
        RAISE EXCEPTION '004_normalize_user_emails: users.email contains empty values after trim';
    END IF;

    SELECT canonical_email
    INTO duplicate_email
    FROM (
        SELECT lower(btrim(email)) AS canonical_email, count(*) AS users_count
        FROM users
        GROUP BY lower(btrim(email))
        HAVING count(*) > 1
        ORDER BY canonical_email
        LIMIT 1
    ) duplicates;

    IF duplicate_email IS NOT NULL THEN
        RAISE EXCEPTION '004_normalize_user_emails: duplicate users.email after canonicalization: %', duplicate_email;
    END IF;

    UPDATE users
    SET email = lower(btrim(email))
    WHERE email <> lower(btrim(email));
END $$;

---- create above / drop below ----

-- Нормализация необратима: исходный регистр и пробелы не восстановить.
SELECT 1;
```

**Step 5: Run migration tests**

Run:

```bash
go test -tags=integration ./internal/db/... -v
```

Expected: PASS if Docker/testcontainers is available. If Docker is unavailable, record the skip reason and still run default tests later.

**Step 6: Commit**

```bash
git add internal/db/migrations/004_normalize_user_emails.sql internal/db/migrate_integration_test.go
git commit -m "feat(db): normalize stored user emails"
```

## Task 4: Add Admin Context And `LoadAdmin`

**Files:**
- Modify: `internal/auth/auth.go`
- Modify: `internal/auth/middleware.go`
- Modify: `internal/auth/middleware_test.go`

**Step 1: Write failing tests**

In `internal/auth/middleware_test.go`, add tests:

```go
func TestIsAdminFromContext_DefaultFalse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if IsAdminFromContext(req.Context()) {
		t.Fatal("IsAdminFromContext должен вернуть false для пустого контекста")
	}
}

func TestLoadAdmin_AdminUserSetsContextFlag(t *testing.T) {
	user := &User{ID: "u-1", Email: " Admin@Example.COM "}
	sess := &Session{ID: "sess-1", UserID: "u-1", ExpiresAt: time.Now().Add(time.Hour)}
	mock := &mockRepository{getSessionResult: sess, findUserResult: user}
	svc := NewService(mock, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))

	var isAdmin bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAdmin = IsAdminFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := svc.LoadSession(svc.LoadAdmin(next))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-1"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", w.Code)
	}
	if !isAdmin {
		t.Fatal("LoadAdmin должен положить IsAdmin=true для email из allowlist")
	}
}

func TestLoadAdmin_NonAdminAndAnonStayFalse(t *testing.T) {
	t.Run("non-admin", func(t *testing.T) {
		user := &User{ID: "u-1", Email: "user@example.com"}
		sess := &Session{ID: "sess-1", UserID: "u-1", ExpiresAt: time.Now().Add(time.Hour)}
		mock := &mockRepository{getSessionResult: sess, findUserResult: user}
		svc := NewService(mock, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))

		var isAdmin bool
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isAdmin = IsAdminFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-1"})
		w := httptest.NewRecorder()
		svc.LoadSession(svc.LoadAdmin(next)).ServeHTTP(w, req)

		if isAdmin {
			t.Fatal("LoadAdmin не должен ставить IsAdmin=true для не-админа")
		}
	})

	t.Run("anon", func(t *testing.T) {
		svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))

		var isAdmin bool
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isAdmin = IsAdminFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		svc.LoadSession(svc.LoadAdmin(next)).ServeHTTP(w, req)

		if isAdmin {
			t.Fatal("LoadAdmin не должен ставить IsAdmin=true для анонима")
		}
	})
}
```

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/auth -run 'Test(IsAdminFromContext|LoadAdmin)' -v
```

Expected: FAIL because `WithAdminEmails`, `LoadAdmin`, and `IsAdminFromContext` do not exist.

**Step 3: Implement auth option and context helpers**

In `internal/auth/auth.go`:

- Import `strings` if `canonicalEmail` is moved here. Prefer a single package-private `canonicalEmail` helper shared by `login.go` and admin checks.
- Add `adminEmails map[string]struct{}` to `Service`.
- Add:

```go
type Option func(*Service)

func WithAdminEmails(emails []string) Option {
	return func(s *Service) {
		s.adminEmails = make(map[string]struct{}, len(emails))
		for _, email := range emails {
			canonical := canonicalEmail(email)
			if canonical == "" {
				continue
			}
			s.adminEmails[canonical] = struct{}{}
		}
	}
}
```

- Change constructor to variadic options:

```go
func NewService(repo Repository, provider OAuthProvider, sessionTTL time.Duration, secure bool, opts ...Option) *Service {
	s := &Service{repo: repo, provider: provider, sessionTTL: sessionTTL, secure: secure, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
```

- Add context key and helpers:

```go
const ctxKeyIsAdmin contextKey = "auth_is_admin"

func IsAdminFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyIsAdmin).(bool)
	return v
}

func withAdmin(r *http.Request, isAdmin bool) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeyIsAdmin, isAdmin))
}

func (s *Service) isAdminEmail(email string) bool {
	if len(s.adminEmails) == 0 {
		return false
	}
	_, ok := s.adminEmails[canonicalEmail(email)]
	return ok
}
```

If `canonicalEmail` currently lives in `login.go`, keep it package-private and reusable from `auth.go`.

**Step 4: Implement `LoadAdmin`**

In `internal/auth/middleware.go`, add:

```go
func (s *Service) LoadAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, withAdmin(r, s.isAdminEmail(user.Email)))
	})
}
```

**Step 5: Run tests**

Run:

```bash
go test ./internal/auth -run 'Test(IsAdminFromContext|LoadAdmin|ResolveUser)' -v
```

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/auth/auth.go internal/auth/middleware.go internal/auth/middleware_test.go internal/auth/login.go
git commit -m "feat(auth): load admin flag into request context"
```

## Task 5: Add `RequireAdmin` Middleware

**Files:**
- Modify: `internal/auth/middleware.go`
- Modify: `internal/auth/middleware_test.go`

**Step 1: Write failing tests**

Add tests to `internal/auth/middleware_test.go`:

```go
func TestRequireAdmin_AdminPasses(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req = withUser(req, &User{ID: "u-admin", Email: "admin@example.com"})

	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	svc.RequireAdmin(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", w.Code)
	}
	if !nextCalled {
		t.Fatal("RequireAdmin должен вызвать next для админа")
	}
}

func TestRequireAdmin_NonAdminRedirectsToLectures(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req = withUser(req, &User{ID: "u-user", Email: "user@example.com"})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next не должен вызываться для не-админа")
	})

	w := httptest.NewRecorder()
	svc.RequireAdmin(next).ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("код = %d, ожидается 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/lectures" {
		t.Fatalf("Location = %q, ожидается /lectures", loc)
	}
}

func TestRequireAdmin_NonAdminHtmxRedirectHeader(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.Header.Set("HX-Request", "true")
	req = withUser(req, &User{ID: "u-user", Email: "user@example.com"})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next не должен вызываться для не-админа")
	})

	w := httptest.NewRecorder()
	svc.RequireAdmin(next).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидается 200", w.Code)
	}
	if got := w.Header().Get("HX-Redirect"); got != "/lectures" {
		t.Fatalf("HX-Redirect = %q, ожидается /lectures", got)
	}
}

func TestRequireAdmin_AnonDirectCallFailsClosed(t *testing.T) {
	svc := NewService(&mockRepository{}, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next не должен вызываться без пользователя")
	})

	w := httptest.NewRecorder()
	svc.RequireAdmin(next).ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("код = %d, ожидается 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/lectures" {
		t.Fatalf("Location = %q, ожидается /lectures", loc)
	}
}
```

Add a composition test:

```go
func TestAdminGateComposition_LoadSessionLoadAdminRequireAuthRequireAdmin(t *testing.T) {
	user := &User{ID: "u-admin", Email: "admin@example.com"}
	sess := &Session{ID: "sess-admin", UserID: "u-admin", ExpiresAt: time.Now().Add(time.Hour)}
	mock := &mockRepository{getSessionResult: sess, findUserResult: user}
	svc := NewService(mock, nil, time.Hour, false, WithAdminEmails([]string{"admin@example.com"}))

	r := chi.NewRouter()
	r.Use(svc.LoadSession)
	r.Use(svc.LoadAdmin)
	r.Group(func(pr chi.Router) {
		pr.Use(svc.RequireAuth)
		pr.Use(svc.RequireAdmin)
		pr.Get("/settings", func(w http.ResponseWriter, r *http.Request) {
			if !IsAdminFromContext(r.Context()) {
				t.Fatal("IsAdmin должен быть true внутри защищённого settings-хендлера")
			}
			w.WriteHeader(http.StatusNoContent)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "sess-admin"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("код = %d, ожидается 204", w.Code)
	}
}
```

Add `github.com/go-chi/chi/v5` to imports.

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/auth -run 'TestRequireAdmin|TestAdminGateComposition' -v
```

Expected: FAIL because `RequireAdmin` does not exist.

**Step 3: Implement `RequireAdmin`**

In `internal/auth/middleware.go`:

```go
func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsAdminFromContext(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}

		user := UserFromContext(r.Context())
		if user != nil && s.isAdminEmail(user.Email) {
			next.ServeHTTP(w, r)
			return
		}

		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/lectures")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/lectures", http.StatusFound)
	})
}
```

**Step 4: Run tests**

Run:

```bash
go test ./internal/auth -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/auth/middleware.go internal/auth/middleware_test.go
git commit -m "feat(auth): require admin for protected routes"
```

## Task 6: Add `NewLayoutData` And Admin Gear Link

**Files:**
- Modify: `internal/web/layout.templ`
- Modify: `internal/web/icons.templ`
- Modify generated: `internal/web/layout_templ.go`
- Modify generated: `internal/web/icons_templ.go`
- Modify: `internal/web/web_test.go`

**Step 1: Write failing layout tests**

In `internal/web/web_test.go`, update helper or add a new helper:

```go
func renderLayoutWithData(t *testing.T, data web.LayoutData) string {
	t.Helper()
	var b bytes.Buffer
	err := web.Layout(data, nil).Render(context.Background(), &b)
	if err != nil {
		t.Fatalf("Layout.Render: %v", err)
	}
	return b.String()
}
```

Add tests:

```go
func TestLayout_AdminGearVisibleForAdminWithSettingsAvailable(t *testing.T) {
	html := renderLayoutWithData(t, web.LayoutData{Title: "Тест", IsAdmin: true, SettingsAvailable: true})

	if !strings.Contains(html, `href="/settings"`) {
		t.Fatal("для админа при доступных настройках ожидается ссылка на /settings")
	}
	if !strings.Contains(html, `aria-label="Настройки"`) {
		t.Fatal("ожидается aria-label для ссылки настроек")
	}
	if !strings.Contains(html, "ll-icon-settings") {
		t.Fatal("ожидается иконка настроек")
	}
}

func TestLayout_AdminGearHiddenForAdminWithoutSettings(t *testing.T) {
	html := renderLayoutWithData(t, web.LayoutData{Title: "Тест", IsAdmin: true})

	if strings.Contains(html, `href="/settings"`) {
		t.Fatal("для админа без доступного settings-роута ссылка на /settings не должна рендериться")
	}
}

func TestLayout_AdminGearHiddenForNonAdminWithSettingsAvailable(t *testing.T) {
	html := renderLayoutWithData(t, web.LayoutData{Title: "Тест", IsAdmin: false, SettingsAvailable: true})

	if strings.Contains(html, `href="/settings"`) {
		t.Fatal("для не-админа ссылка на /settings не должна рендериться даже при доступных настройках")
	}
}

func TestNewLayoutData_DefaultsToContextValues(t *testing.T) {
	ctx := web.WithCSRFToken(context.Background(), "csrf-token")

	data := web.NewLayoutData(ctx, "Тест")

	if data.Title != "Тест" {
		t.Fatalf("Title = %q, ожидается Тест", data.Title)
	}
	if data.CSRFToken != "csrf-token" {
		t.Fatalf("CSRFToken = %q, ожидается csrf-token", data.CSRFToken)
	}
	if data.IsAdmin {
		t.Fatal("IsAdmin должен быть false без admin-флага в контексте")
	}
	if data.SettingsAvailable {
		t.Fatal("SettingsAvailable должен быть false без settings-флага в контексте")
	}
}

func TestNewLayoutData_ReadsSettingsAvailableFromContext(t *testing.T) {
	ctx := web.WithSettingsAvailable(context.Background())

	data := web.NewLayoutData(ctx, "Тест")

	if !data.SettingsAvailable {
		t.Fatal("SettingsAvailable должен быть true при settings-флаге в контексте")
	}
}
```

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/web -run 'TestLayout_AdminGear|TestNewLayoutData' -v
```

Expected: FAIL to compile because `LayoutData.IsAdmin` and `web.NewLayoutData` do not exist.

**Step 3: Implement templ changes**

In `internal/web/layout.templ`:

- Change imports to:

```go
import (
	"context"
	"fmt"

	"github.com/LectureLog/lecturelog-web/internal/auth"
)
```

- Add `IsAdmin bool` and `SettingsAvailable bool` to `LayoutData`.
- Add `internal/web/settings.go` with `WithSettingsAvailable(ctx)` and
  `SettingsAvailableFromContext(ctx) bool`; default is false.
- Add:

```go
// NewLayoutData собирает данные layout из контекста запроса.
func NewLayoutData(ctx context.Context, title string) LayoutData {
	return LayoutData{
		Title:             title,
		CSRFToken:         CSRFTokenFromContext(ctx),
		IsAdmin:           auth.IsAdminFromContext(ctx),
		SettingsAvailable: SettingsAvailableFromContext(ctx),
	}
}
```

- In `header`, before `@themeToggle()`:

```templ
if data.IsAdmin && data.SettingsAvailable {
	@adminGearLink()
}
```

- Add:

```templ
// adminGearLink — вход в админские настройки.
templ adminGearLink() {
	<a class="ll-icon-btn" href="/settings" aria-label="Настройки" title="Настройки">
		@iconSettings()
	</a>
}
```

In `internal/web/icons.templ`, add:

```templ
// iconSettings — иконка шестерёнки для админских настроек.
templ iconSettings() {
	<svg class="ll-icon-settings" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
		<path d="M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5z"></path>
		<path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1A2 2 0 1 1 4.2 17l.1-.1A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.6-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9l-.1-.1A2 2 0 1 1 7 4.2l.1.1A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1-1.6V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1A2 2 0 1 1 19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1a2 2 0 1 1 0 4H21a1.7 1.7 0 0 0-1.6 1z"></path>
	</svg>
}
```

**Step 4: Regenerate templ**

Run:

```bash
make templ
```

Expected: `internal/web/layout_templ.go` and `internal/web/icons_templ.go` update.

**Step 5: Run tests**

Run:

```bash
go test ./internal/web -v
go test ./internal/auth ./internal/web -v
```

Expected: PASS and no import cycle.

**Step 6: Commit**

```bash
git add internal/web/layout.templ internal/web/icons.templ internal/web/layout_templ.go internal/web/icons_templ.go internal/web/web_test.go
git commit -m "feat(web): show settings link for admins"
```

## Task 7: Replace Manual LayoutData Call Sites

**Files:**
- Modify: `internal/reader/handlers.go`
- Modify: `internal/lecture/handlers.go`
- Modify: `internal/hub/handlers.go`
- Modify: `cmd/server/main.go`
- If present: modify `internal/web/page_settings.templ` and generated `internal/web/page_settings_templ.go`

**Step 1: Search current manual call sites**

Run:

```bash
rg -n 'web\.LayoutData\{' internal cmd
```

Expected before implementation: production call sites in reader, lecture, hub, upload in `cmd/server/main.go`, and generated templ files/tests.

**Step 2: Replace production call sites**

Use `web.NewLayoutData(r.Context(), title)` in these exact places:

- `internal/reader/handlers.go`: replace `web.LayoutData{Title: view.Title, CSRFToken: web.CSRFTokenFromContext(r.Context())}` with `web.NewLayoutData(r.Context(), view.Title)`.
- `internal/lecture/handlers.go`: replace manual CSRF extraction and `LayoutData` literal with `data := web.NewLayoutData(r.Context(), "Мои лекции")`.
- `internal/hub/handlers.go`: replace `web.LayoutData{Title: "Хаб"}` with `web.NewLayoutData(r.Context(), "Хаб")`.
- `cmd/server/main.go`: replace `token := ...` and manual upload `LayoutData` with `data := web.NewLayoutData(r.Context(), "Новый конспект")`.
- If `internal/web/page_settings.templ` exists by then, ensure settings handlers render with `web.NewLayoutData(r.Context(), "Настройки")`; do not leave a manual `LayoutData{}` in settings code.

Keep `internal/web/page_demo.templ` unchanged.

**Step 3: Verify grep outcome**

Run:

```bash
rg -n 'web\.LayoutData\{' internal cmd -g '*.go' -g '*.templ'
```

Expected: no production handler call sites remain. Acceptable remaining matches: tests, generated templ artifacts, and `internal/web/page_demo.templ`.

**Step 4: Run focused tests**

Run:

```bash
go test ./internal/reader ./internal/lecture ./internal/hub ./cmd/server -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/reader/handlers.go internal/lecture/handlers.go internal/hub/handlers.go cmd/server/main.go
git add internal/web/page_settings.templ internal/web/page_settings_templ.go 2>/dev/null || true
git commit -m "refactor(web): centralize layout data construction"
```

## Task 8: Wire Admin Allowlist And Settings Gate In `main.go`

**Files:**
- Modify: `cmd/server/main.go`
- If present: modify imports to include `github.com/LectureLog/lecturelog-web/internal/settings`

**Step 1: Verify settings package state**

Run:

```bash
test -d internal/settings && rg -n 'func NewService|func \(.*\) Mount' internal/settings || true
```

Expected in this worktree today: no output because cookies-UI code is not present.

**Step 2: Implement config-to-auth wiring**

In `cmd/server/main.go`:

- After `config.Load`, add:

```go
if len(cfg.AdminEmails) == 0 {
	log.Printf("warning: ADMIN_EMAILS пуст; /settings будет недоступен всем пользователям")
}
```

- Change auth service construction:

```go
authSvc := auth.NewService(
	repo,
	provider,
	cfg.SessionTTL,
	secureCookies,
	auth.WithAdminEmails(cfg.AdminEmails),
)
```

- In `web.WithGlobalMiddleware`, mount `authSvc.LoadAdmin` immediately after `authSvc.LoadSession` and before CSRF:

```go
web.WithGlobalMiddleware(
	authSvc.LoadSession,
	authSvc.LoadAdmin,
	csrfExempt("/webhooks/core", csrfMiddleware),
	csrfInjector,
),
```

**Step 3: Mount settings only when real settings service exists**

If `internal/settings` exists, wire it now:

```go
settingsSvc := settings.NewService(core)
```

Add one protected mount block in `web.NewRouter`:

```go
web.WithMount(func(r chi.Router) {
	r.Group(func(ar chi.Router) {
		ar.Use(authSvc.RequireAuth)
		ar.Use(authSvc.RequireAdmin)
		settingsSvc.Mount(ar) // GET /settings, GET/POST /settings/cookies/*
	})
}),
```

Place this block outside the general authenticated lecture/upload group, or inside it only if the final route chain is still exactly `RequireAuth -> RequireAdmin -> settingsSvc.Mount`. Do not mount `/settings` under bare `RequireAuth`.

Also add a global middleware when the real settings service exists, so every layout-rendering
page can show the admin gear without hard-coding route knowledge in each handler:

```go
settingsAvailable := func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(web.WithSettingsAvailable(r.Context())))
	})
}
```

Mount it in `web.WithGlobalMiddleware` after `authSvc.LoadAdmin` and before CSRF. Do not add
this middleware until real `/settings*` routes are mounted; otherwise the UI links to a 404.

If `internal/settings` does not exist, do not create placeholder routes. Record in the final implementation notes that the merge with cookies-UI must add the protected mount above before shipping settings.

**Step 4: Run compile checks**

Run:

```bash
go test ./cmd/server ./internal/auth -v
go test ./...
```

Expected: PASS.

**Step 5: Verify route/security grep**

Run:

```bash
rg -n 'LoadAdmin|RequireAdmin|ADMIN_EMAILS|settingsSvc|/settings' cmd/server/main.go internal
```

Expected:

- `LoadAdmin` appears in global middleware after `LoadSession`.
- `RequireAdmin` appears in the settings group if settings routes exist.
- No `/settings` route is mounted under bare `RequireAuth`.

**Step 6: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): wire admin allowlist"
```

If settings service was present and mounted:

```bash
git add cmd/server/main.go
git commit -m "feat(server): protect settings routes with admin gate"
```

## Task 9: Add Neutral `cookies_invalid` Text

**Files:**
- Modify: `internal/lecture/handlers.go`
- Modify: `internal/lecture/handlers_test.go`
- Modify: `internal/syncsvc/handlers.go`
- Modify: `internal/syncsvc/handlers_test.go`

**Step 1: Write failing tests**

In `internal/lecture/handlers_test.go`, add:

```go
func TestMapErrorCode_CookiesInvalid(t *testing.T) {
	got := mapErrorCode("cookies_invalid")
	want := "Cookies YouTube устарели — обратитесь к администратору"
	if got != want {
		t.Fatalf("mapErrorCode = %q, ожидается %q", got, want)
	}
}
```

In `internal/syncsvc/handlers_test.go`, add the same test name if no conflict within package:

```go
func TestMapErrorCode_CookiesInvalid(t *testing.T) {
	got := mapErrorCode("cookies_invalid")
	want := "Cookies YouTube устарели — обратитесь к администратору"
	if got != want {
		t.Fatalf("mapErrorCode = %q, ожидается %q", got, want)
	}
}
```

**Step 2: Run tests to verify failure**

Run:

```bash
go test ./internal/lecture ./internal/syncsvc -run TestMapErrorCode_CookiesInvalid -v
```

Expected: FAIL; current output is likely `Ошибка: cookies_invalid`.

**Step 3: Implement both cases**

In both `mapErrorCode` functions:

```go
case "cookies_invalid":
	return "Cookies YouTube устарели — обратитесь к администратору"
```

Do not change function signatures. Do not branch by admin role.

**Step 4: Run tests**

Run:

```bash
go test ./internal/lecture ./internal/syncsvc -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/lecture/handlers.go internal/lecture/handlers_test.go internal/syncsvc/handlers.go internal/syncsvc/handlers_test.go
git commit -m "feat(lectures): explain expired youtube cookies"
```

## Task 10: Document `ADMIN_EMAILS`

**Files:**
- Modify: `README.md`
- Modify: `deploy/env.web.example`
- Verify only: `docker-compose.prod.yml`
- Verify only: `deploy/compose.vps.yml`

**Step 1: Update env example**

In `deploy/env.web.example`, near OAuth or platform settings, add:

```dotenv
# Администраторы веб-платформы: список verified email через запятую.
# Пустое значение допустимо, но fail-closed: /settings будет недоступен всем.
ADMIN_EMAILS=admin@example.com
```

**Step 2: Update README env table**

In `README.md` env table, add:

```markdown
| `ADMIN_EMAILS` | Allowlist админов для `/settings*`, email через запятую; пусто = fail-closed | нет | пустой список |
```

Also update auth/middleware section:

- Mention `LoadAdmin` after `LoadSession`.
- Mention `RequireAdmin`: admin-only routes, non-admin browser redirect to `/lectures`, htmx `HX-Redirect: /lectures`.
- Update the `auth.NewService` snippet to include optional `auth.WithAdminEmails(cfg.AdminEmails)` without breaking the base signature explanation.

Update `cmd/server` router snippet:

```go
web.WithGlobalMiddleware(
    authSvc.LoadSession,
    authSvc.LoadAdmin,
    csrfExempt("/webhooks/core", csrfMiddleware),
    csrfInjector,
),
```

If settings service exists in the final branch, document that `/settings*` is mounted under `RequireAuth -> RequireAdmin`.

**Step 3: Verify compose env propagation**

Run:

```bash
rg -n 'env_file|ADMIN_EMAILS|CORE_API_BASE_URL|PLATFORM_DB_DSN' docker-compose.prod.yml deploy/compose.vps.yml deploy/env.web.example README.md
```

Expected:

- `docker-compose.prod.yml` uses `env_file: .env`; no compose change required.
- `deploy/compose.vps.yml` uses `env_file: .env`; no compose change required.
- `ADMIN_EMAILS` appears in README and `deploy/env.web.example`.

**Step 4: Commit**

```bash
git add README.md deploy/env.web.example
git commit -m "docs: document admin email allowlist"
```

## Task 11: Final Verification

**Files:**
- No new code unless verification exposes a defect.

**Step 1: Regenerate templ**

Run:

```bash
make templ
```

Expected: no unexpected diff beyond committed generated templ files.

**Step 2: Run unit tests**

Run:

```bash
go test ./internal/config ./internal/auth ./internal/web ./internal/reader ./internal/lecture ./internal/hub ./internal/syncsvc ./cmd/server -v
go test ./...
```

Expected: PASS.

**Step 3: Run migration integration tests**

Run if Docker is available:

```bash
go test -tags=integration ./internal/db/... -v
```

Expected: PASS. If Docker/testcontainers is unavailable, record the exact skip/failure reason in the handoff.

**Step 4: Run generation drift check**

Run:

```bash
make gen-check
```

Expected: PASS with clean `internal/web/` generated artifacts. If `make gen-check` downloads Tailwind and network is unavailable, run `make templ` plus `git diff --exit-code -- internal/web/` and record the limitation.

**Step 5: Security acceptance grep**

Run:

```bash
rg -n 'csrfExempt|/settings|RequireAdmin|LoadAdmin|ADMIN_EMAILS|cookies_invalid' cmd internal README.md deploy/env.web.example
```

Expected:

- No `/settings*` path appears in `csrfExempt`.
- Settings routes, if present, are mounted under `RequireAuth` and `RequireAdmin`.
- `LoadAdmin` is global immediately after `LoadSession`.
- `cookies_invalid` appears in both `internal/lecture/handlers.go` and `internal/syncsvc/handlers.go`.
- `ADMIN_EMAILS` appears in config, main wiring, README, and env example.

**Step 6: Final status**

Run:

```bash
git status --short
```

Expected: clean except for intentional uncommitted work if the user asked not to commit. For normal execution of this plan, each task has its own commit, so final status should be clean.

## Handoff Checklist

- `ADMIN_EMAILS` optional, normalized, empty list fail-closed.
- `auth.resolveUser` canonicalizes verified email before repository calls and rejects empty canonical email.
- Migration 004 normalizes existing `users.email`, fails on empty canonical email, fails on duplicate canonical email.
- `auth.Service` supports `WithAdminEmails`, `LoadAdmin`, `RequireAdmin`, and `IsAdminFromContext`.
- `web.NewLayoutData(ctx, title)` fills `Title`, `CSRFToken`, `IsAdmin`, and
  `SettingsAvailable`.
- Admin gear link renders only for `LayoutData.IsAdmin=true && LayoutData.SettingsAvailable=true`.
- Reader, lecture, hub, upload, and settings page if present use `web.NewLayoutData`.
- `cmd/server` passes `cfg.AdminEmails`, warns when empty, mounts `LoadAdmin` after `LoadSession`, and protects real `/settings*` routes with `RequireAuth -> RequireAdmin`.
- `cookies_invalid` maps to `Cookies YouTube устарели — обратитесь к администратору` in both packages without role branching.
- README and `deploy/env.web.example` document `ADMIN_EMAILS`.
- templ artifacts regenerated and verification commands recorded.
