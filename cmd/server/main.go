// Пакет server — точка входа платформы LectureLog (web-хаб).
// Загружает конфигурацию, подключается к Postgres, инициализирует Auth-сервис
// и запускает HTTP-сервер с chi-роутером.
package main

import (
	"context"
	"crypto/rand"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/csrf"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/LectureLog/lecturelog-web/internal/auth"
	"github.com/LectureLog/lecturelog-web/internal/config"
	"github.com/LectureLog/lecturelog-web/internal/coreclient"
	"github.com/LectureLog/lecturelog-web/internal/db"
	"github.com/LectureLog/lecturelog-web/internal/lecture"
	"github.com/LectureLog/lecturelog-web/internal/upload"
	"github.com/LectureLog/lecturelog-web/internal/web"

	"github.com/go-chi/chi/v5"
)

// dbAdapter реализует auth.Repository поверх db.UserDB и db.SessionDB.
// db не знает про auth (нет импорта auth→db), auth не знает про pgx (нет импорта db→pgx).
// Связка происходит здесь, в cmd/server.
type dbAdapter struct {
	users    *db.UserDB
	sessions *db.SessionDB
}

func (a *dbAdapter) FindUserByEmail(ctx context.Context, email string) (*auth.User, error) {
	row, err := a.users.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &auth.User{
		ID:        row.UserID,
		Email:     row.Email,
		Name:      row.Name,
		AvatarURL: row.AvatarURL,
	}, nil
}

func (a *dbAdapter) FindUserByID(ctx context.Context, userID string) (*auth.User, error) {
	row, err := a.users.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &auth.User{
		ID:        row.UserID,
		Email:     row.Email,
		Name:      row.Name,
		AvatarURL: row.AvatarURL,
	}, nil
}

func (a *dbAdapter) CreateUser(ctx context.Context, p auth.Profile) (*auth.User, error) {
	row, err := a.users.CreateUser(ctx, p.Email, p.Name, p.AvatarURL)
	if err != nil {
		return nil, err
	}
	return &auth.User{
		ID:        row.UserID,
		Email:     row.Email,
		Name:      row.Name,
		AvatarURL: row.AvatarURL,
	}, nil
}

func (a *dbAdapter) UpsertIdentity(ctx context.Context, provider, providerSub, userID string) error {
	return a.users.UpsertIdentity(ctx, provider, providerSub, userID)
}

func (a *dbAdapter) CreateSession(ctx context.Context, userID string, expiresAt time.Time) (*auth.Session, error) {
	row, err := a.sessions.CreateSession(ctx, userID, expiresAt)
	if err != nil {
		return nil, err
	}
	return &auth.Session{
		ID:        row.SessionID,
		UserID:    row.UserID,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (a *dbAdapter) GetSession(ctx context.Context, sessionID string) (*auth.Session, error) {
	row, err := a.sessions.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &auth.Session{
		ID:        row.SessionID,
		UserID:    row.UserID,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (a *dbAdapter) DeleteSession(ctx context.Context, sessionID string) error {
	return a.sessions.DeleteSession(ctx, sessionID)
}

func main() {
	ctx := context.Background()

	// Загружаем конфигурацию из переменных окружения
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("config.Load: %v", err)
	}

	core, err := coreclient.New(cfg.CoreClient())
	if err != nil {
		log.Fatalf("coreclient.New: %v", err)
	}

	// Подключаемся к Postgres и применяем миграции
	pool, err := db.New(ctx, cfg.PlatformDBDSN)
	if err != nil {
		log.Fatalf("db.New: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("db.Migrate: %v", err)
	}

	// Инициализируем адаптер репозитория
	repo := &dbAdapter{
		users:    &db.UserDB{Pool: pool},
		sessions: &db.SessionDB{Pool: pool},
	}

	// Определяем, работаем ли в prod-режиме (куки Secure)
	// В prod рекомендуется передать PLATFORM_SECURE=true через env.
	// По умолчанию false (локальная разработка по http).
	secureCookies := os.Getenv("PLATFORM_SECURE") == "true"

	// Настраиваем Google OAuth 2.0
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.OAuth.ClientID,
		ClientSecret: cfg.OAuth.ClientSecret,
		RedirectURL:  cfg.OAuth.CallbackURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
	// Реальный Google userinfo endpoint
	provider := auth.NewGoogleProvider(oauthCfg, "https://www.googleapis.com/oauth2/v3/userinfo")

	// Создаём auth.Service
	authSvc := auth.NewService(repo, provider, cfg.SessionTTL, secureCookies)

	// Инициализируем lecture.Service с адаптерами БД и ядра.
	lectureSvc := lecture.NewService(
		&lectureRepo{lectures: &db.LectureDB{Pool: pool}},
		&coreTasksAdapter{core: core},
	)

	// CSRF-ключ генерируется на старте (32 случайных байта).
	// ДОЛГ: для прод-стабильности вынести в env (PLATFORM_CSRF_KEY) — рестарт инвалидирует токены.
	// Решение оркестратора: internal/config НЕ модифицируем, беты хватает.
	csrfKey := make([]byte, 32)
	if _, err := rand.Read(csrfKey); err != nil {
		log.Fatalf("генерация CSRF-ключа: %v", err)
	}

	// Ключ подписи presigned-токенов загрузки генерируется на старте (32 случайных байта).
	// ДОЛГ: рестарт сервера инвалидирует незавершённые presign-токены;
	// стабильный ключ из env (PLATFORM_UPLOAD_SIGN_KEY) — долг (как у csrfKey).
	uploadSignKey := make([]byte, 32)
	if _, err := rand.Read(uploadSignKey); err != nil {
		log.Fatalf("генерация ключа подписи upload: %v", err)
	}

	// Создаём signer для подписи и верификации presigned-токенов загрузки
	signer := upload.NewSigner(uploadSignKey)

	// Создаём upload.Service: обрабатывает presign / confirm / youtube
	uploadSvc := upload.NewService(core, &uploadRepo{lectures: &db.LectureDB{Pool: pool}}, signer, cfg.PresignedTTL)

	// gorilla/csrf middleware: токен из контекста (csrf.Token(r)) → templ-формы через hx-headers.
	// X-CSRF-Token — заголовок для htmx (hx-headers={"X-CSRF-Token": "..."}).
	csrfMiddleware := csrf.Protect(
		csrfKey,
		csrf.Secure(secureCookies),
		csrf.RequestHeader("X-CSRF-Token"),
	)

	// csrf-инжектор: добавляет CSRF-токен в контекст запроса после csrfMiddleware.
	// Хендлеры лекций читают токен через web.CSRFTokenFromContext(r.Context())
	// и передают в LayoutData.CSRFToken для hx-headers.
	csrfInjector := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := web.WithCSRFToken(r.Context(), csrf.Token(r))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	// Собираем chi-роутер с auth-middleware и маршрутами
	handler := web.NewRouter(
		web.WithGlobalMiddleware(
			authSvc.LoadSession, // читает сессию → *User в контекст
			csrfMiddleware,      // CSRF-защита мутирующих маршрутов
			csrfInjector,        // кладёт csrf.Token(r) в контекст для layout
		),
		web.WithMount(func(r chi.Router) {
			authSvc.Mount(r) // GET /auth/login, GET /auth/callback, POST /auth/logout
		}),
		web.WithMount(func(r chi.Router) {
			// Группа под RequireAuth: только аутентифицированные пользователи
			r.Group(func(pr chi.Router) {
				pr.Use(authSvc.RequireAuth)
				lectureSvc.Mount(pr) // GET /lectures, POST /lectures/{id}/*
				uploadSvc.Mount(pr)  // POST /upload/presign, /upload/confirm, /upload/youtube
			})
		}),
	)

	addr := envOr("PLATFORM_ADDR", ":8080")
	log.Printf("LectureLog web запускается на %s (secure=%v)", addr, secureCookies)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("ListenAndServe: %v", err)
	}
}

// envOr возвращает значение переменной окружения key или def если переменная не задана.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ─── Адаптер для lecture.Repository ─────────────────────────────────────────

// lectureRepo реализует lecture.Repository поверх db.LectureDB.
// db не знает про lecture (нет импорта lecture→db), lecture не знает про pgx.
// Связка происходит здесь, в cmd/server.
type lectureRepo struct {
	lectures *db.LectureDB
}

func (r *lectureRepo) ListByOwner(ctx context.Context, ownerID string) ([]lecture.Lecture, error) {
	rows, err := r.lectures.ListByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	result := make([]lecture.Lecture, len(rows))
	for i, row := range rows {
		result[i] = rowToLecture(row)
	}
	return result, nil
}

func (r *lectureRepo) FindByID(ctx context.Context, lectureID string) (*lecture.Lecture, error) {
	row, err := r.lectures.FindByID(ctx, lectureID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	lec := rowToLecture(*row)
	return &lec, nil
}

func (r *lectureRepo) Rename(ctx context.Context, lectureID, ownerID, title string) (int64, error) {
	return r.lectures.Rename(ctx, lectureID, ownerID, title)
}

func (r *lectureRepo) SetVisibility(ctx context.Context, lectureID, ownerID, visibility string) (int64, error) {
	return r.lectures.SetVisibility(ctx, lectureID, ownerID, visibility)
}

func (r *lectureRepo) Delete(ctx context.Context, lectureID, ownerID string) (int64, error) {
	return r.lectures.Delete(ctx, lectureID, ownerID)
}

func (r *lectureRepo) SetCoreTaskProcessing(ctx context.Context, lectureID, ownerID, coreTaskID string) (int64, error) {
	return r.lectures.SetCoreTaskProcessing(ctx, lectureID, ownerID, coreTaskID)
}

// rowToLecture маппит db.LectureRow → lecture.Lecture.
func rowToLecture(row db.LectureRow) lecture.Lecture {
	return lecture.Lecture{
		ID:          row.LectureID,
		OwnerID:     row.OwnerID,
		CoreTaskID:  row.CoreTaskID,
		Status:      lecture.Status(row.Status),
		ErrorCode:   row.ErrorCode,
		Visibility:  lecture.Visibility(row.Visibility),
		SourceKind:  row.SourceKind,
		S3Key:       row.S3Key,
		VideoURL:    row.VideoURL,
		Title:       row.Title,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		PublishedAt: row.PublishedAt,
	}
}

// ─── Адаптер для lecture.CoreTasks ──────────────────────────────────────────

// coreTasksAdapter реализует lecture.CoreTasks поверх coreclient.CoreClient.
type coreTasksAdapter struct {
	core *coreclient.CoreClient
}

func (a *coreTasksAdapter) DeleteTask(ctx context.Context, coreTaskID string) error {
	return a.core.DeleteTask(ctx, coreTaskID)
}

func (a *coreTasksAdapter) CreateTask(ctx context.Context, p lecture.CreateTaskParams) (string, error) {
	return a.core.CreateTask(ctx, coreclient.CreateTaskParams{
		S3Key:    p.S3Key,
		VideoURL: p.VideoURL,
		Media:    p.Media,
	})
}

// _ — проверка на этапе компиляции: lectureRepo реализует lecture.Repository.
var _ lecture.Repository = (*lectureRepo)(nil)

// _ — проверка на этапе компиляции: coreTasksAdapter реализует lecture.CoreTasks.
var _ lecture.CoreTasks = (*coreTasksAdapter)(nil)

// _ — проверка на этапе компиляции: dbAdapter реализует auth.Repository.
var _ auth.Repository = (*dbAdapter)(nil)

// ─── Адаптер для upload.Repository ──────────────────────────────────────────

// uploadRepo реализует upload.Repository поверх db.LectureDB.
// db не знает про upload (нет импорта upload→db), upload не знает про pgx.
// Связка происходит здесь, в cmd/server.
type uploadRepo struct {
	lectures *db.LectureDB
}

// CreateLecture создаёт запись лекции в БД и возвращает её ID.
func (r *uploadRepo) CreateLecture(ctx context.Context, p upload.CreateLectureParams) (string, error) {
	row, err := r.lectures.CreateLecture(ctx, db.CreateLectureParams{
		OwnerID:    p.OwnerID,
		Title:      p.Title,
		SourceKind: p.SourceKind,
		CoreTaskID: p.CoreTaskID,
		S3Key:      p.S3Key,
		VideoURL:   p.VideoURL,
	})
	if err != nil {
		return "", err
	}
	return row.LectureID, nil
}

// _ — проверка на этапе компиляции: uploadRepo реализует upload.Repository.
var _ upload.Repository = (*uploadRepo)(nil)
