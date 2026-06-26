// Пакет server — точка входа платформы LectureLog (web-хаб).
// Загружает конфигурацию, подключается к Postgres, инициализирует Auth-сервис
// и запускает HTTP-сервер с chi-роутером.
package main

import (
	"context"
	"crypto/rand"
	"errors"
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
	"github.com/LectureLog/lecturelog-web/internal/hub"
	"github.com/LectureLog/lecturelog-web/internal/lecture"
	"github.com/LectureLog/lecturelog-web/internal/syncsvc"
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

	// Единый экземпляр db.LectureDB используется обоими адаптерами (lectureRepo и uploadRepo)
	lectureDB := &db.LectureDB{Pool: pool}

	// Инициализируем lecture.Service с адаптерами БД и ядра.
	lectureSvc := lecture.NewService(
		&lectureRepo{lectures: lectureDB},
		&coreTasksAdapter{core: core},
	)
	hubSvc := hub.NewService(&hubRepo{lectures: lectureDB}, 0)

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
	upRepo := &uploadRepo{lectures: lectureDB}
	uploadSvc := upload.NewService(core, upRepo, signer, cfg.PresignedTTL)

	// ─── C1-sync wiring ───
	syncSvc := syncsvc.NewService(
		&syncRepo{lectures: lectureDB},
		&coreStatusAdapter{core: core},
		cfg.WebhookSecret,
	)

	// gorilla/csrf middleware: токен из контекста (csrf.Token(r)) → templ-формы через hx-headers.
	// X-CSRF-Token — заголовок для htmx (hx-headers={"X-CSRF-Token": "..."}).
	csrfMiddleware := csrf.Protect(
		csrfKey,
		csrf.Secure(secureCookies),
		csrf.RequestHeader("X-CSRF-Token"),
	)

	// C1-sync: внешний вебхук ядра подписан HMAC, но не имеет CSRF-cookie.
	csrfExempt := func(path string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			protected := mw(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == path {
					next.ServeHTTP(w, r)
					return
				}
				protected.ServeHTTP(w, r)
			})
		}
	}

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
			authSvc.LoadSession,                          // читает сессию → *User в контекст
			csrfExempt("/webhooks/core", csrfMiddleware), // CSRF-защита мутирующих маршрутов, кроме HMAC-вебхука
			csrfInjector,                                 // кладёт csrf.Token(r) в контекст для layout
		),
		web.WithMount(func(r chi.Router) {
			r.Post("/webhooks/core", syncSvc.HandleWebhook)
		}),
		web.WithMount(func(r chi.Router) {
			authSvc.Mount(r) // GET /auth/login, GET /auth/callback, POST /auth/logout
		}),
		web.WithMount(func(r chi.Router) {
			hubSvc.Mount(r) // GET /hub доступен анонимным посетителям
		}),
		web.WithMount(func(r chi.Router) {
			// Группа под RequireAuth: только аутентифицированные пользователи
			r.Group(func(pr chi.Router) {
				pr.Use(authSvc.RequireAuth)
				lectureSvc.Mount(pr) // GET /lectures, POST /lectures/{id}/*
				// C1-upload-ui: страница формы загрузки (GET) — рендер templ под RequireAuth.
				pr.Get("/upload", func(w http.ResponseWriter, r *http.Request) {
					user := auth.UserFromContext(r.Context())
					if user == nil {
						http.Error(w, "unauthorized", http.StatusUnauthorized)
						return
					}
					token := web.CSRFTokenFromContext(r.Context())
					data := web.LayoutData{Title: "Новый конспект", CSRFToken: token}
					if err := web.UploadPage(data).Render(r.Context(), w); err != nil {
						http.Error(w, "render upload page", http.StatusInternalServerError)
						return
					}
				})
				uploadSvc.Mount(pr) // POST /upload/presign, /upload/confirm, /upload/youtube
				// C1-sync: поллинг-прокси статуса лекции (htmx ~10с).
				pr.Get("/lectures/{id}/status", syncSvc.HandlePollStatus)
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

// ─── Адаптер для hub.Repository ─────────────────────────────────────────────

// hubRepo реализует hub.Repository поверх db.LectureDB.
type hubRepo struct {
	lectures *db.LectureDB
}

func (r *hubRepo) ListPublic(ctx context.Context, limit int) ([]hub.PublicLecture, error) {
	rows, err := r.lectures.ListPublic(ctx, limit)
	if err != nil {
		return nil, err
	}
	result := make([]hub.PublicLecture, len(rows))
	for i, row := range rows {
		result[i] = hub.PublicLecture{
			ID:              row.LectureID,
			OwnerID:         row.OwnerID,
			Title:           row.Title,
			SourceKind:      row.SourceKind,
			PublishedAt:     row.PublishedAt,
			AuthorName:      row.AuthorName,
			AuthorAvatarURL: row.AuthorAvatarURL,
		}
	}
	return result, nil
}

// _ — проверка на этапе компиляции: hubRepo реализует hub.Repository.
var _ hub.Repository = (*hubRepo)(nil)

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

// ─── C1-sync wiring ────────────────────────────────────────────────────────

// syncRepo реализует syncsvc.Repository поверх db.LectureDB.
type syncRepo struct {
	lectures *db.LectureDB
}

func (r *syncRepo) UpdateStatusConditional(ctx context.Context, coreTaskID, status, errorCode string) (int64, error) {
	return r.lectures.UpdateStatusConditional(ctx, coreTaskID, status, errorCode)
}

func (r *syncRepo) FindByID(ctx context.Context, lectureID string) (*syncsvc.LectureView, error) {
	row, err := r.lectures.FindByID(ctx, lectureID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return rowToLectureView(*row), nil
}

func rowToLectureView(row db.LectureRow) *syncsvc.LectureView {
	return &syncsvc.LectureView{
		ID:         row.LectureID,
		OwnerID:    row.OwnerID,
		CoreTaskID: row.CoreTaskID,
		Status:     row.Status,
		ErrorCode:  row.ErrorCode,
		Title:      row.Title,
		SourceKind: row.SourceKind,
		Visibility: row.Visibility,
		UpdatedAt:  row.UpdatedAt,
	}
}

// coreStatusAdapter реализует syncsvc.CoreStatus поверх coreclient.CoreClient.
type coreStatusAdapter struct {
	core *coreclient.CoreClient
}

func (a *coreStatusAdapter) GetTaskStatus(ctx context.Context, coreTaskID string) (*syncsvc.TaskProgress, error) {
	status, err := a.core.GetTaskStatus(ctx, coreTaskID)
	if err != nil {
		if errors.Is(err, coreclient.ErrTaskNotFound) {
			return nil, syncsvc.ErrTaskNotFound
		}
		return nil, err
	}
	return taskStatusToProgress(status), nil
}

func taskStatusToProgress(status coreclient.TaskStatus) *syncsvc.TaskProgress {
	progress := &syncsvc.TaskProgress{
		ProgressPct: status.ProgressPct,
		Status:      "processing",
	}
	if status.Stage != nil {
		progress.Stage = *status.Stage
	}
	if status.ErrorCode != nil {
		progress.ErrorCode = *status.ErrorCode
	}

	if status.ErrorCode != nil || status.Error != nil {
		progress.Status = "failed"
		return progress
	}
	if status.ResultPath != nil {
		progress.Status = "ready"
	}
	return progress
}

// _ — проверка на этапе компиляции: syncRepo реализует syncsvc.Repository.
var _ syncsvc.Repository = (*syncRepo)(nil)

// _ — проверка на этапе компиляции: coreStatusAdapter реализует syncsvc.CoreStatus.
var _ syncsvc.CoreStatus = (*coreStatusAdapter)(nil)
