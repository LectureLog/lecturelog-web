# LectureLog Web — веб-платформа «Читальный зал»

Веб-платформа LectureLog: загрузка лекций, отслеживание обработки, чтение
конспектов и публичная витрина. Один из трёх репозиториев проекта:

- **core** (`lecturelog-core`) — ядро обработки: принимает задачи, обрабатывает
  медиа, владеет своим MinIO и таблицей задач. Источник правды по контракту.
- **web** (`lecturelog-web`, этот репозиторий) — платформа: пользователи, права,
  загрузка, читалка, витрина. Ходит в ядро только по HTTP, в БД ядра не лезет.
- **docs** — общая документация процесса.

Платформа общается с ядром через типизированный Go-клиент, сгенерированный из
OpenAPI-контракта ядра.

## Локальный запуск

```bash
cp .env.example .env
# Заполните GOOGLE_CLIENT_ID и GOOGLE_CLIENT_SECRET из Google Cloud Console.
make up
make dev
```

После запуска откройте http://localhost:8080.

`make up` поднимает только Postgres и MinIO платформы. Для полного цикла
загрузки отдельно запустите ядро и укажите его адрес в `CORE_API_BASE_URL` без
`/api/v1`. Миграции БД применяются автоматически при старте сервера через
`db.Migrate`, отдельная команда не нужна.

## Архитектура контракта

Главный инвариант (см. `docs/WORKFLOW.md`): `openapi.json` ядра — **производный
от кода ядра** (FastAPI), а не пишется руками. Платформа генерирует клиент из
этого файла через `oapi-codegen`. Отсюда жёсткий порядок изменений:

```
код ядра → регенерация openapi.json → coreclient платформы (oapi-codegen) → код платформы
```

Поэтому ядро патчится первым, платформа — после стабилизации контракта. Если
ядро отдало несовместимый контракт, клиент платформы перестанет компилироваться
до написания доменных модулей — это главный автоловец рассинхрона (GATE B).

## Пакет `internal/coreclient`

Типизированный HTTP-клиент к ядру: доменная обёртка `CoreClient` поверх
машинно-сгенерированного `ClientWithResponses`. Пакет лежит в `internal/`, т.к.
это деталь реализации платформы, а не публичный API.

Доменные методы (`internal/coreclient/client.go`):

- `CreateUpload(ctx, filename)` — запрос presigned-PUT URL (`POST /uploads`,
  тело `application/json` `{filename}`); возвращает `{Key, URL, ExpiresIn}`.
- `CreateTask(ctx, params)` — создание задачи (`POST /tasks`,
  **multipart/form-data**); ровно один источник — `S3Key` или `VideoURL`, плюс
  опциональные `Media` и `NoSlides`. multipart-тело собирается вручную, т.к.
  oapi-codegen для multipart даёт только сырой `…WithBodyWithResponse`.
- `GetTaskStatus(ctx, taskID)` — статус задачи (`GET /tasks/{id}`); на 404
  возвращает `ErrTaskNotFound`. Nullable-поля (`Stage`, `Error`, `ErrorCode`,
  `ResultPath`) отражены указателями.
- `DeleteTask(ctx, taskID)` — удаление задачи (`DELETE /tasks/{id}`,
  идемпотентно: 204 → `nil`, в т.ч. при повторном удалении).

Верификатор входящего вебхука (`internal/coreclient/webhook.go`):

- `VerifyWebhookSignature(body, signatureHex, secret) bool` — constant-time
  проверка подписи через `hmac.Equal`.
- `WebhookPayload` — тип тела вебхука `{task_id, status, error, error_code}`
  (`error`/`error_code` — указатели: `nil` = JSON `null`).

## HMAC: только на вебхуке ядро → платформа

Подпись HMAC существует **только на исходящем вебхуке ядра** (направление
ядро → платформа):

- **Ядро само подписывает** свой callback: заголовок `X-Webhook-Signature` =
  HMAC-SHA256 (hex) от **байтов тела** запроса с ключом
  `LECTURELOG_WEBHOOK_SECRET`.
- **Платформа верифицирует** эту подпись через `VerifyWebhookSignature`
  (constant-time, от тех же байтов, что пришли по HTTP).

Исходящие запросы платформа → ядро (`POST /uploads`, `POST /tasks`,
`GET`/`DELETE /tasks/{id}`) **НЕ подписываются**: ядро их подпись не проверяет.
Защита исходящего направления — сетевой контур и общие секреты, а не подпись
каждого запроса.

> HTTP-приём вебхука (endpoint, чтение тела, матч лекции по `core_task_id`) — вне
> B1; это задача C1-sync. B1 даёт только чистый верификатор и тип тела.

## Пакет `internal/db`

Общий слой подключения к Postgres платформы и применения миграций схемы.

**Подключение.** Точка входа — `db.New(ctx, dsn) (*pgxpool.Pool, error)`: создаёт
`pgxpool`, проверяет доступность базы через `Ping`, возвращает пул готовым к
использованию. DSN передаётся строкой (из `config.Config.PlatformDBDSN`); пакет
`db` не импортирует `internal/config`.

**Миграции.** Движок — `jackc/tern/v2` (pgx-родной, без сторонних CLI-зависимостей).
Файлы `*.sql` встроены в бинарь через `//go:embed migrations/*.sql`. Применение:

```go
db.Migrate(ctx, pool) error
```

Вызов идемпотентен: повторный запуск при уже применённых миграциях ничего не меняет.
Таблица версий (`schema_version`) управляется tern автоматически.

**Схема (таблицы):**

| Таблица | Роль |
|---|---|
| `users` | Пользователи платформы; `email` UNIQUE NOT NULL; PK — UUID |
| `identities` | OAuth-аккаунты; составной PK `(provider, provider_sub)`, FK→`users`; email не хранится |
| `sessions` | Сессии; `session_id` UUID PK, FK→`users`, `expires_at` |
| `lectures` | Лекции; статус/видимость/тип источника — нативные PostgreSQL enum; поля `core_task_id`, `s3_key`, `video_url`, `published_at` и др. |

UUID генерится базой (`gen_random_uuid()`, расширение `pgcrypto`).

Частичный индекс `idx_lectures_core_task_id` на `lectures.core_task_id` (WHERE NOT
NULL) — обеспечивает быстрый матч входящего вебхука по идентификатору задачи ядра.

Частичный индекс `idx_lectures_public_published_at` на `lectures.published_at DESC`
(WHERE visibility='public', миграция `003_lectures_hub_index.sql`) — ускоряет выдачу
публичной витрины `/hub`.

**Data-access лекций (`db.LectureDB`).** Тип `LectureDB` предоставляет операции
над таблицей `lectures`; не содержит доменной логики (порядок вызовов ядро→БД —
в пакете `lecture`).

Тип строки — `db.LectureRow`; nullable текстовые поля (`core_task_id`, `s3_key`,
`video_url`, `error_code`) возвращаются через `COALESCE` как пустая строка;
`published_at` — `*time.Time`.

| Метод | Сигнатура | Описание |
|---|---|---|
| `ListByOwner` | `(ctx, ownerID) ([]LectureRow, error)` | Лекции владельца, updated\_at DESC; возвращает пустой срез (не nil) |
| `FindByID` | `(ctx, lectureID) (*LectureRow, error)` | По PK; (nil, nil) если не найдена |
| `Rename` | `(ctx, lectureID, ownerID, title) (int64, error)` | Обновляет title + updated\_at; фильтр owner\_id; возвращает affected |
| `SetVisibility` | `(ctx, lectureID, ownerID, visibility) (int64, error)` | public: WHERE status='ready', устанавливает published\_at; private: published\_at не обнуляется |
| `Delete` | `(ctx, lectureID, ownerID) (int64, error)` | Hard-delete; вызывается только после `core.DeleteTask` (гарантия домена) |
| `SetCoreTaskProcessing` | `(ctx, lectureID, ownerID, coreTaskID) (int64, error)` | Переводит failed→processing, обновляет core\_task\_id, очищает error\_code; WHERE status='failed' |
| `UpdateStatusConditional` | `(ctx, coreTaskID, status, errorCode) (int64, error)` | Анти-гонка: обновляет статус только из processing (WHERE status='processing'); потребитель — C1-sync |
| `ListPublic` | `(ctx, limit) ([]PublicLectureRow, error)` | Витрина: публичные лекции (lectures⋈users) с автором, WHERE visibility='public', ORDER BY published\_at DESC; потребитель — `hub` |

`UpdateStatusConditional` — единственный метод, который потребляет C1-sync
(вебхук / поллинг), а не пакет `lecture`; его назначение — атомарно принять
результат ядра без гонки с параллельным retry.

**Тесты.** Дефолтный `go test ./...` не требует Postgres: проверяет встроенность
embed-файлов, синтаксическую корректность SQL и отказ `New` на заведомо битом DSN.
Интеграционный тест (тег `integration`) поднимает Postgres через `testcontainers`,
применяет миграции и проверяет идемпотентность. Команда:

```bash
make migrate-test   # go test -tags=integration ./internal/db/...  (требует Docker)
```

## Пакет `internal/config`

Единая точка чтения и валидации конфигурации приложения из окружения. Пакет
используется при старте сервера; ни один компонент платформы не обращается к
`os.Getenv` напрямую.

Сигнатура точки входа:

```go
func Load(getenv func(string) string) (*Config, error)
```

Геттер окружения инъектируется, а не захватывается из `os` — это делает функцию
детерминированной и тривиально тестируемой без манипуляций с реальным окружением
процесса. Новых зависимостей пакет не вносит (stdlib-only).

**Fail-fast с агрегацией.** При отсутствии любого обязательного ключа `Load`
возвращает **одну** ошибку со списком **всех** недостающих ключей — не падает на
первом. Заданное, но непарсируемое значение опционального ключа тоже является
ошибкой (не молчаливый дефолт).

**Валидация `LECTURELOG_WEBHOOK_SECRET`** вынесена сюда (закрытие долга B1):
пустой или незаданный секрет — обязательная ошибка при загрузке конфигурации.
Прежде эта проверка отсутствовала, что позволяло принять поддельную подпись при
пустом секрете.

**Фабрика `(*Config).CoreClient()`** возвращает готовую `coreclient.Config` без
дублирования полей. `CORE_API_BASE_URL` передаётся без суффикса `/api/v1` —
пути, сгенерированные `oapi-codegen`, уже несут этот префикс.

### Таблица env-ключей

| Ключ | Назначение | Обяз. | Дефолт |
|---|---|---|---|
| `GOOGLE_CLIENT_ID` | Google OAuth client id | да | — |
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret | да | — |
| `PLATFORM_CALLBACK_URL` | OAuth callback URL | да | — |
| `LECTURELOG_WEBHOOK_SECRET` | HMAC-секрет вебхука (общий с ядром) | да | — |
| `PLATFORM_DB_DSN` | DSN Postgres платформы (pgx) | да | — |
| `CORE_API_BASE_URL` | Базовый URL ядра (без `/api/v1`) | да | — |
| `CORE_MINIO_ENDPOINT` | MinIO ядра endpoint | да | — |
| `CORE_MINIO_ACCESS_KEY` | MinIO ядра access key | да | — |
| `CORE_MINIO_SECRET_KEY` | MinIO ядра secret key | да | — |
| `CORE_MINIO_BUCKET` | MinIO ядра bucket | да | — |
| `CORE_MINIO_USE_SSL` | MinIO use SSL | нет | `false` |
| `PRESIGNED_TTL` | TTL presigned-пачки | нет | `24h` |
| `SESSION_TTL` | TTL сессии | нет | `720h` |
| `PLATFORM_SECURE` | Флаг `Secure` для кук (`true` в prod/HTTPS) | нет | `false` |
| `PLATFORM_ADDR` | Адрес и порт HTTP-сервера | нет | `:8080` |

## Пакет `internal/web`

Каркас SSR-презентации «Читальный зал» — общий слой рендеринга и роутинга для
всех будущих страниц платформы. Доменные страницы (читалка, хаб, загрузка) — C1.

**Стек.** Роутер — [chi](https://github.com/go-chi/chi); HTML-компоненты —
[templ](https://templ.guide) (Go-шаблоны с типизацией, генерация `*_templ.go`);
интерактивность — [htmx](https://htmx.org) v1.9.12; стили — Tailwind CSS v4
(CSS-first, standalone CLI без node).

**Toolchain.**

- **templ** прописан как tool-директива `go.mod` (как oapi-codegen): никаких
  `tools.go`, никакого глобального бинаря. Генерация — `go generate ./...`
  (директива в `internal/web/generate.go`). Сгенерированные `*_templ.go`
  коммитятся в репозиторий.
- **Tailwind** — standalone CLI-бинарь (без Node.js), скачивается целью
  `make tailwind-bin` в `./bin/` (gitignored). Собранный
  `internal/web/static/css/app.css` коммитится в репозиторий — `go test`
  не требует наличия Tailwind-бинаря.
- **htmx** вендорён: `internal/web/static/vendor/htmx.min.js`, отдаётся
  через `//go:embed` (не CDN).

**Layout «Читальный зал».** templ-компонент `Layout(data, actions)`:

- `<html data-theme>` — атрибут определяет тему (дефолт `light` в разметке,
  не завязан на JS).
- Sticky-шапка 58px: brand-mark, название, слот `actions`, тумблер темы.
- `<head>`: Google Fonts (Source Serif 4 + Onest), `/static/css/app.css`, htmx.
- Инлайн-скрипт (до first paint) читает `localStorage` и проставляет
  `data-theme` на `<html>` — анти-FOUC без вспышки дефолтной темы.

**Дизайн-токены.** Источник — `design/tokens.css` (пакет `design/`, style-guide
платформы). Токены скопированы в `internal/web/assets/tokens.css` и подключены
к Tailwind через директиву `@theme` (`var(--token)`); тёмная тема —
переопределение переменных под `[data-theme="dark"]`.

**Страница «Мои лекции» (`page_lectures.templ`).** Добавлена в C1-lecture.

- `LectureCardVM` — view-модель карточки (ID, Title, Status, StatusLabel,
  Visibility, SourceKind, CanPublish, CanRetry, ErrorText, UpdatedAt); маппинг
  `lecture.Lecture → LectureCardVM` выполняет `lecture/handlers.go`.
- `LecturesPage(data LayoutData, vms []LectureCardVM)` — полная страница в базовом
  Layout; пустое состояние — `LecturesEmpty`.
- `LectureCard(vm LectureCardVM)` — карточка; используется и в полных ответах
  (GET /lectures), и в htmx-партиалах (rename / visibility / retry).
- `LectureTitleInline(vm)` — inline-форма переименования (POST на rename).
- `LectureVisibilityToggle(vm)` — тумблер видимости (POST на visibility).

**CSRF в hx-headers (закрытие долга C0-web).** Добавлен `internal/web/csrf.go`:

```go
func WithCSRFToken(ctx context.Context, token string) context.Context
func CSRFTokenFromContext(ctx context.Context) string
```

`LayoutData.CSRFToken` передаётся в `Layout` и выводится в `hx-headers` шапки
(`{"X-CSRF-Token": "<токен>"}`), отчего все htmx-запросы страницы автоматически
несут CSRF-токен. `cmd/server` инжектирует токен в контекст через
`csrfInjector`-middleware после `gorilla/csrf`.

**Роутер.** `web.NewRouter()` (chi) монтирует:

- `/static/*` — embed-статика (CSS, htmx, vendor-файлы);
- `/` — демо-страница (проверка layout).

`cmd/server` смонтирован в C0-auth — см. раздел ниже.

**Тесты.** Рендер `Layout` в `bytes.Buffer` + `httptest`-проверка роутера.
Ни браузера, ни Node.js, ни Tailwind-бинаря не требуется — артефакты
(`*_templ.go`, `app.css`) коммитятся.

## Пакет `internal/auth`

Доменный модуль аутентификации платформы: Google OAuth 2.0, серверные сессии в
Postgres, CSRF-защита и middleware прав.

**Ключевые типы:**

| Тип | Роль |
|---|---|
| `Profile` | Данные пользователя от OAuth-провайдера (sub, email, email_verified, name, picture) |
| `User` | Доменный пользователь платформы (UUID, email, имя, аватар) |
| `Session` | Серверная сессия (UUID, UserID, ExpiresAt) |
| `Service` | Центральный сервис — содержит бизнес-логику входа, сессий и middleware |
| `Repository` | Интерфейс доступа к данным (мокируется в тестах) |
| `OAuthProvider` | Интерфейс OAuth-провайдера (мокируется через httptest) |

**Создание сервиса:**

```go
auth.NewService(repo Repository, provider OAuthProvider, sessionTTL time.Duration, secure bool) *Service
```

`secure=true` выставляет флаг `Secure` на всех куках — использовать в prod (HTTPS).

**OAuth flow (Google OAuth 2.0).** Профиль берётся из userinfo endpoint (не из
`id_token`). State-параметр генерируется через `crypto/rand` безусловно; сверка —
constant-time (`hmac.Equal`-эквивалент) — защищает OAuth round-trip от CSRF.
`NewGoogleProvider(cfg *oauth2.Config, userinfoURL string) OAuthProvider` позволяет
подменять endpoint в тестах на `httptest`-сервер.

**Куки:**

| Кука | TTL | Назначение |
|---|---|---|
| `ll_session` | из `config.SessionTTL` | HttpOnly, Secure, SameSite=Lax; session_id сессии |
| `ll_oauth_state` | 600 секунд | Одноразовая; OAuth state (анти-CSRF при login) |

**Логика входа (`resolveUser`).** Алгоритм «email = личность»:

1. `email_verified=false` → `ErrEmailNotVerified`, ничего не создаётся (guard
   против account takeover).
2. Матч по `users.email` — найден: `UpsertIdentity`, возвращается существующий.
3. Не найден: `CreateUser` + `UpsertIdentity`, возвращается новый.

**HTTP-хендлеры и роутинг:**

- `HandleLogin` — генерирует state, устанавливает `ll_oauth_state`, редиректит
  на `AuthCodeURL` провайдера.
- `HandleCallback` — сверяет state, обменивает код на профиль через
  `OAuthProvider.Exchange`, вызывает `resolveUser`, создаёт сессию, выдаёт
  `ll_session`.
- `HandleLogout` — удаляет сессию из БД, сбрасывает `ll_session`.
- `Mount(r chi.Router)` — монтирует все три маршрута в chi-роутер
  (`GET /auth/login`, `GET /auth/callback`, `POST /auth/logout`).

**Middleware:**

- `LoadSession` — читает `ll_session`, валидирует через `Repository.GetSession`
  (фильтрация `expires_at > now()` на стороне Postgres), кладёт `*User` в контекст
  через `UserFromContext`. Анонимные запросы пропускает.
- `RequireAuth` — блокирует анонимов: обычный запрос → `302 /auth/login`, htmx
  (`HX-Request: true`) → `401` (htmx не обрабатывает редирект как навигацию).

**CSRF.** `gorilla/csrf` монтируется в `cmd/server` глобально (ключ 32 байта,
`csrf.RequestHeader("X-CSRF-Token")`). Под htmx токен передаётся через
`hx-headers={"X-CSRF-Token": "..."}` — место в layout заложено в C0-web.

**Тестируемость.** Интерфейсы `Repository` и `OAuthProvider` позволяют тестировать
без Postgres и реальных запросов к Google. Дефолтный `go test ./...` проходит
герметично: OAuth-тест через `httptest`, `resolveUser` — на репозиторий-моке.

## Пакет `internal/lecture`

Доменный модуль «Мои лекции»: хранит бизнес-логику управления лекциями
пользователя и монтирует HTTP-хендлеры ЛК.

**Ключевые типы:**

| Тип | Роль |
|---|---|
| `Lecture` | Доменная лекция (ID, OwnerID, CoreTaskID, Status, Visibility, SourceKind, S3Key, VideoURL, Title, …) |
| `Status` | Enum статуса обработки: `processing` / `ready` / `failed` |
| `Visibility` | Enum видимости: `private` / `public` |
| `Repository` | Интерфейс data-access (владеет пакет lecture → адаптер живёт в cmd/server) |
| `CoreTasks` | Интерфейс к ядру: `DeleteTask` + `CreateTask` (мокабельность без coreclient) |
| `CreateTaskParams` | Параметры создания задачи: S3Key, VideoURL, Media |
| `Service` | Центральный сервис; содержит repo и core |

**Конструктор:**

```go
lecture.NewService(repo Repository, core CoreTasks) *Service
```

**Методы Service:**

| Метод | Сигнатура | Описание |
|---|---|---|
| `List` | `(ctx, ownerID) ([]Lecture, error)` | Список лекций владельца (updated\_at DESC) |
| `Rename` | `(ctx, lectureID, ownerID, title) (Lecture, error)` | Trim + валидация ≤200 симв.; возвращает свежую запись |
| `SetVisibility` | `(ctx, lectureID, ownerID, vis Visibility) (Lecture, error)` | Публикация только готовых (ready); снятие — в любой момент |
| `Delete` | `(ctx, lectureID, ownerID) error` | Сначала `core.DeleteTask`, затем удаление строки; ошибка ядра прерывает операцию |
| `Retry` | `(ctx, lectureID, ownerID) (Lecture, error)` | Повторная обработка только failed-лекций с источником |

**Бизнес-правила:**

- **Публикация** (`SetVisibility → public`): разрешена только при `status=ready`; иначе `ErrNotReady`.
- **Retry**: разрешён только при `status=failed` и наличии `S3Key` или `VideoURL`; иначе `ErrNotFailed` / `ErrNoRetrySource`. Алгоритм: `CreateTask` в ядре → `SetCoreTaskProcessing` в БД (failed→processing).
- **Delete**: owner-проверка выполняется **до** обращения к ядру — нельзя удалить чужую задачу. Если ядро вернуло ошибку — строка в БД не трогается.
- **Анти-перебор ID**: во всех мутациях (Rename/SetVisibility/Delete/Retry) несуществующая и чужая лекция возвращают одинаковый `ErrNotFound` — исключает оракул чужих UUID.

**Доменные ошибки:**

| Константа | Значение |
|---|---|
| `ErrNotFound` | Лекция не найдена или нет прав |
| `ErrNotReady` | Публикация доступна только для ready |
| `ErrNotFailed` | Retry доступен только для failed |
| `ErrNoRetrySource` | Нет источника для повтора |

**HTTP-хендлеры (`Service.Mount`):**

```
GET    /lectures                 — страница «Мои лекции» (полный рендер)
POST   /lectures/{id}/rename     — переименование (htmx partial: карточка)
POST   /lectures/{id}/visibility — переключение видимости (htmx partial: карточка)
POST   /lectures/{id}/retry      — повторная обработка (htmx partial: карточка)
DELETE /lectures/{id}            — удаление (пустой 200, htmx удаляет DOM-узел)
```

Все маршруты работают под `auth.RequireAuth` (монтируется в `cmd/server`).
CSRF-токен передаётся через `hx-headers={"X-CSRF-Token": "..."}` — Layout
берёт значение из `web.LayoutData.CSRFToken`, которое хендлер заполняет через
`web.CSRFTokenFromContext`.

## Пакет `internal/upload`

Доменный модуль загрузки лекций: валидация файла, получение presigned-URL у ядра,
HMAC-токен незавершённой загрузки, создание задачи и строки лекции.

**Ключевые типы:**

| Тип | Роль |
|---|---|
| `Service` | Центральный сервис; содержит `Core`, `Repository`, `Signer` |
| `Core` | Интерфейс к ядру: `CreateUpload` + `CreateTask` |
| `Repository` | Интерфейс БД: `CreateLecture` |
| `Signer` | HMAC-подписчик/верификатор stateless-токена незавершённой загрузки |
| `PrepareResult` | Ответ presign: `Token`, `PutURL`, `S3Key`, `Media`, `Title`, `ExpiresIn` |
| `ConfirmInput` | Форма подтверждения: `Token`, `S3Key`, `Title`, `HasPDF`, `ExtractSlides` |
| `YouTubeInput` | Форма YouTube: `URL`, `Title`, `HasPDF`, `ExtractSlides` |
| `CreateLectureParams` | Параметры новой строки лекции в БД |

**Конструктор:**

```go
upload.NewService(core Core, repo Repository, signer *Signer, uploadTTL time.Duration) *Service
```

**Методы Service:**

| Метод | Описание |
|---|---|
| `PrepareFileUpload(ctx, userID, filename, size, mime)` | Валидирует мета-данные файла, запрашивает у ядра presigned-PUT URL, возвращает HMAC-токен |
| `ConfirmFileUpload(ctx, userID, ConfirmInput)` | Верифицирует токен, создаёт задачу в ядре, сохраняет лекцию в БД |
| `CreateYouTube(ctx, userID, YouTubeInput)` | Валидирует YouTube-URL, создаёт задачу в ядре, сохраняет лекцию в БД |

**HTTP-хендлеры (`Service.Mount`):**

```
GET  /upload              — страница формы загрузки (templ, режим Файл/Ссылка,
                            drop-зона, опции has_pdf / extract_slides, заголовок)
POST /upload/presign      — JSON {filename, size, mime} → {token, put_url, s3_key,
                            media, title, expires_in}
POST /upload/confirm      — form {token, s3_key, title, has_pdf, extract_slides}
                            → задача в ядре + строка лекции; HX-Redirect: /lectures
POST /upload/youtube      — form {url, title, has_pdf, extract_slides}
                            → то же для YouTube-URL; HX-Redirect: /lectures
```

`GET /upload` смонтирован напрямую в `cmd/server` под `RequireAuth`; три мутирующих
маршрута — через `uploadSvc.Mount`.

**Клиентский поток загрузки файла** (`internal/web/static/js/upload.js`):

1. JS отправляет `POST /upload/presign` с `{filename, size, mime}`.
2. Получает `{token, put_url, s3_key, …}`, выполняет прямой `PUT` файла в MinIO
   по `put_url` (минуя сервер платформы).
3. Отправляет `POST /upload/confirm` с `{token, s3_key, title, …}`.
4. Сервер отвечает `HX-Redirect: /lectures` — htmx выполняет полную навигацию.

Guard от двойного сабмита блокирует повторную отправку до завершения потока.
CSRF-токен передаётся через `data`-атрибут формы.

**HMAC-токен (`Signer`).** Stateless-токен связывает userID + s3_key + media +
срок действия. Кодируется как `base64url(payload).base64url(hmac-sha256)`.
Верификация: constant-time `hmac.Equal`, проверка владельца, проверка срока.

**Валидация (`validate.go`):**

- Поддерживаемые расширения: `.mp4`, `.mov`, `.mkv`, `.webm`, `.avi` (video);
  `.mp3`, `.wav`, `.m4a`, `.aac`, `.ogg`, `.flac` (audio).
- MIME от клиента — недоверенный; пустой MIME допускается.
- Лимит размера файла: 5 ГБ (бета-потолок).
- YouTube-URL: только `youtube.com`, `www.youtube.com`, `youtu.be`, `m.youtube.com`.

**Доменные ошибки:**

| Константа | Значение |
|---|---|
| `ErrUnsupportedMedia` | Неподдерживаемый тип медиа |
| `ErrEmptyFile` | Пустой файл (size ≤ 0) |
| `ErrTooLarge` | Файл превышает лимит |
| `ErrEmptyFilename` | Пустое имя файла |
| `ErrMediaMismatch` | MIME-тип не совпадает с расширением файла |
| `ErrInvalidURL` | Некорректная или не-YouTube ссылка |
| `ErrForbidden` | Токен не принадлежит пользователю или истёк |

**Известные ограничения (технический долг):**

- PDF-слайды (`has_pdf`) пока не передаются в ядро — проброс файла отдельный
  атом. `has_pdf=true` только включает `no_slides=true` в задаче ядра (отключает
  извлечение слайдов из видео) и гасит тумблер `extract_slides`.
- Прямые (не-YouTube) медиа-URL не поддержаны.
- `uploadSignKey` генерируется при каждом старте — рестарт сервера инвалидирует
  все незавершённые presign-токены (приемлемо для беты).

## Пакет `internal/syncsvc`

Доменный модуль синхронизации статуса лекции с ядром: приём подписанного вебхука
и поллинг-прокси статуса задачи.

**Ключевые типы:**

| Тип | Роль |
|---|---|
| `Service` | Центральный сервис; содержит `Repository`, `CoreStatus`, `webhookSecret` |
| `Repository` | Узкий порт БД: `UpdateStatusConditional` + `FindByID` |
| `CoreStatus` | Узкий порт ядра: `GetTaskStatus` |
| `TaskProgress` | Статус задачи ядра: `Stage`, `ProgressPct`, `Status`, `ErrorCode` |
| `LectureView` | Минимальный срез лекции для карточки и проверки владельца |

**Конструктор:**

```go
syncsvc.NewService(repo Repository, core CoreStatus, webhookSecret string) *Service
```

**HTTP-хендлеры:**

```
POST /webhooks/core          — приём вебхука ядра (ПУБЛИЧНЫЙ, без сессии;
                               защищён HMAC X-Webhook-Signature, CSRF-exempt)
GET  /lectures/{id}/status   — поллинг-прокси (под RequireAuth, htmx ~10с):
                               возвращает htmx-фрагмент карточки лекции
```

**`HandleWebhook` (`POST /webhooks/core`):**

- Тело ограничено `MaxBytesReader` 1 МБ.
- Подпись: `coreclient.VerifyWebhookSignature(body, X-Webhook-Signature, secret)` —
  constant-time HMAC-SHA256. Неверная подпись → `401 Unauthorized`.
- Тело: JSON `{task_id, status, error, error_code}` (`coreclient.WebhookPayload`).
- Допустимые статусы: `processing`, `ready`, `failed`; иное → `400 Bad Request`.
- Обновление в БД: `UpdateStatusConditional` (анти-гонка — только из `processing`),
  идемпотентно.
- CSRF-exempt: маршрут `POST /webhooks/core` выведен из-под `gorilla/csrf` через
  обёртку `csrfExempt` в `cmd/server`.

**`HandlePollStatus` (`GET /lectures/{id}/status`):**

- Требует авторизации (`RequireAuth`); проверяет владельца лекции.
- Если статус нетерминальный и задача есть в ядре — запрашивает `GetTaskStatus`.
- **Fallback-запись:** при переходе задачи в терминальный статус (по ответу
  ядра) выполняет `UpdateStatusConditional` в БД (resilience к потере вебхука).
- При недоступности ядра — мягкая деградация: возвращает карточку с последним
  известным статусом из БД.
- Ответ: `text/html; charset=utf-8` — htmx-фрагмент (`web.LectureCard`).

**Маппинг `error_code` → русская метка** реализован в `lectureToVM`:

| Код ядра | Отображение |
|---|---|
| `rate_limit` | Превышен лимит обработки |
| `bad_input` | Некорректный источник |
| `internal` | Внутренняя ошибка обработки |
| `processing_error` | Ошибка обработки |
| `download_error` | Ошибка загрузки |
| `transcription_error` | Ошибка распознавания речи |

## Пакет `internal/hub`

Доменный модуль публичной витрины: список опубликованных лекций, открытый
анонимным посетителям. Зеркалит структуру `internal/lecture` (Service + Repository
+ handlers).

**Ключевые типы:**

| Тип | Роль |
|---|---|
| `PublicLecture` | Публичная лекция витрины с автором (ID, Title, SourceKind, PublishedAt, AuthorName, AuthorAvatarURL) |
| `Repository` | Узкий порт БД: `ListPublic(ctx, limit)` — адаптер поверх `db.LectureDB` живёт в `cmd/server` |
| `Service` | Центральный сервис; хранит repo и потолок выдачи |

**Конструктор:**

```go
hub.NewService(repo Repository, limit int) *Service
```

`limit <= 0` заменяется значением по умолчанию (`defaultLimit = 200`).

**HTTP-хендлер (`Service.Mount`):**

```
GET /hub   — публичная витрина лекций (visibility='public', ORDER BY
             published_at DESC); ОТКРЫТА анонимам, без RequireAuth
```

Хендлер рендерит `web.HubPage` (карточки `web.HubCardVM`: автор, тип источника,
дата публикации, ссылка в читалку `/lectures/{id}/read`).

**Известные ограничения (технический долг):**

- `/hub` пока отдельная страница, а не лендинг `/`.
- Серверной пагинации нет — выдача ограничена потолком `limit=200`.
- Ссылка карточки в читалку `/lectures/{id}/read` ждёт атома C1-reader (до него — 404).

## Точка входа `cmd/server`

Единственный бинарь платформы. Цепочка инициализации:

```
config.Load → coreclient.New → db.New + db.Migrate → dbAdapter → auth.NewService
  → lecture.NewService (coreTasksAdapter) → upload.NewService (uploadRepo, Signer)
  → syncsvc.NewService (syncRepo, coreStatusAdapter) → web.NewRouter → ListenAndServe
```

**`dbAdapter`** — адаптер из `cmd/server`, реализует `auth.Repository` поверх
`db.UserDB` и `db.SessionDB`. Связка намеренно живёт здесь: `internal/db` не
импортирует `internal/auth`, `internal/auth` не знает про `pgx`. Корректность
проверяется compile-time: `var _ auth.Repository = (*dbAdapter)(nil)`.

**`coreTasksAdapter`** — реализует `lecture.CoreTasks` поверх `*coreclient.CoreClient`
(`DeleteTask`, `CreateTask`). Проверяется compile-time.

**`uploadRepo`** — реализует `upload.Repository` поверх `db.LectureDB`
(`CreateLecture`). Проверяется compile-time.

**`syncRepo`** — реализует `syncsvc.Repository` поверх `db.LectureDB`
(`UpdateStatusConditional`, `FindByID`). Проверяется compile-time.

**`coreStatusAdapter`** — реализует `syncsvc.CoreStatus` поверх
`*coreclient.CoreClient` (`GetTaskStatus`). Проверяется compile-time.

**`hubRepo`** — реализует `hub.Repository` поверх `db.LectureDB` (`ListPublic`).
Проверяется compile-time. Витрина `/hub` монтируется вне `RequireAuth`-группы.

Единственный экземпляр `*coreclient.CoreClient` (создаётся через `coreclient.New`)
и единственный экземпляр `*db.LectureDB` разделяются между всеми адаптерами.

**`web.NewRouter`** принимает вариативные опции — `web.WithGlobalMiddleware` и
`web.WithMount`. Пакет `internal/web` не зависит от `internal/auth`; сервисы
инъектируются через опции:

```go
web.NewRouter(
    web.WithGlobalMiddleware(
        authSvc.LoadSession,
        csrfExempt("/webhooks/core", csrfMiddleware), // вебхук ядра выведен из-под CSRF
        csrfInjector,
    ),
    web.WithMount(func(r chi.Router) { r.Post("/webhooks/core", syncSvc.HandleWebhook) }),
    web.WithMount(func(r chi.Router) { authSvc.Mount(r) }),
    web.WithMount(func(r chi.Router) { hubSvc.Mount(r) }), // GET /hub — анонимам
    web.WithMount(func(r chi.Router) {
        r.Group(func(pr chi.Router) {
            pr.Use(authSvc.RequireAuth)
            lectureSvc.Mount(pr)          // GET /lectures, POST /lectures/{id}/*
            pr.Get("/upload", ...)        // страница формы загрузки
            uploadSvc.Mount(pr)           // POST /upload/presign|confirm|youtube
            pr.Get("/lectures/{id}/status", syncSvc.HandlePollStatus)
        })
    }),
)
```

**Адрес** задаётся через `PLATFORM_ADDR` (дефолт `:8080`).

**Известные ограничения (технический долг).**

- CSRF-ключ и ключ подписи presign-токенов (`uploadSignKey`) генерируются через
  `crypto/rand` при каждом старте сервера. При рестарте все ранее выданные
  CSRF-токены и незавершённые presign-токены инвалидируются. Для прод-стабильности
  необходимо вынести ключи в env-переменные (`PLATFORM_CSRF_KEY`,
  `PLATFORM_UPLOAD_SIGN_KEY`).
- Мягкий экран 502/503 при недоступности ядра (для `confirm` и `youtube`) —
  не реализован (долг §8).

## Генерация клиента

```bash
go generate ./...
```

Шаги генерации (`internal/coreclient/generate.go`):

1. **Нормализация спеки** `scripts/normalize_openapi.py` — схлопывает
   3.1.0-конструкцию nullable (`anyOf:[{...},{type:null}]`) в 3.0-совместимый
   `nullable: true`, т.к. oapi-codegen v2.7.x не понимает 3.1.0-nullable
   напрямую. Промежуточный `openapi.normalized.json` в репозиторий не коммитится.
2. **oapi-codegen** (режим `types + client`) → `internal/coreclient/gen.go`
   (коммитится в репозиторий).

Вендоренная копия спеки ядра — `internal/coreclient/openapi.json`. Обновление из
соседнего репозитория ядра:

```bash
make sync-spec   # cp ../lecturelog-core/docs/openapi.json -> internal/coreclient/openapi.json
```

После `make sync-spec` обязательны `make generate` и ревью diff в `gen.go` —
чтобы поймать рассинхрон контракта.

Версии прибиты: **Go 1.25**, **oapi-codegen v2.7.1** (через tool-директиву
`go.mod`, без `tools.go`).

## Сборка и тесты

Цели `Makefile`:

| Цель | Действие |
|---|---|
| `make generate` | нормализация спеки + oapi-codegen |
| `make web-gen` | `go generate ./...` для пакета `internal/web` (templ) |
| `make tailwind-bin` | скачать Tailwind standalone CLI в `./bin/` |
| `make build` | `generate` → `go build ./...` |
| `make vet` | `go vet ./...` |
| `make test` | `go test ./...` (юнит + контрактный smoke к замоканному ядру) |
| `make gate` | полные ворота: templ-генерация + Tailwind-сборка + build + vet + test |
| `make gen-check` | проверка детерминизма генерации (`git diff --exit-code`) |
| `make sync-spec` | обновить вендоренную спеку из репозитория ядра |
| `make migrate-test` | интеграционная проверка миграций (требует Docker) |

**Ворота GATE B** (слой 1 приёмки платформы, coreclient/config/db):

```bash
go generate ./... && go build ./... && go vet ./... && go test ./...
```

`go generate` обязателен перед сборкой — он создаёт/обновляет `gen.go` из
вендоренной спеки. Контрактный smoke поднимает `httptest`-мок ядра и проверяет
пути, методы, Content-Type, сериализацию тел и маппинг кодов (200/204/400/404/409)
в доменные ошибки — герметично, без сети и соседнего репозитория.

**Ворота C0-web** (`make gate`): templ-генерация + сборка Tailwind + `go build/vet/test`.
`go test` проходит без Node.js, Tailwind-бинаря и браузера — все артефакты
(`*_templ.go`, `app.css`) коммитятся в репозиторий.

`make gen-check` — CI-цель, проверяет детерминизм генерации: запускает `go generate`
и падает, если `git diff --exit-code` обнаруживает изменения.

Дефолтные ворота не требуют ни Docker, ни Postgres — все интеграционные тесты
изолированы тегом `integration` и запускаются только через `make migrate-test`.

## Статус

- **B1** — `internal/coreclient`: типизированный клиент ядра, HMAC-верификатор
  вебхука, GATE B зелёный.
- **C0-config** — `internal/config`: единый конфиг-слой, fail-fast агрегация
  ошибок окружения, валидация `LECTURELOG_WEBHOOK_SECRET` (долг B1 закрыт).
- **C0-db** — `internal/db`: схема и миграции Postgres платформы (tern + embed),
  таблицы users/identities/sessions/lectures, GATE B зелёный, интеграционный тест
  за `integration`-тегом.
- **C0-web** — `internal/web`: каркас «Читальный зал» завершён. chi-роутер,
  templ-layout, htmx, Tailwind v4, дизайн-токены из `design/`, анти-FOUC,
  тумблер темы, web.NewRouter со статикой и демо-страницей, `make gate` зелёный.
- **C0-auth** — `internal/auth` + `cmd/server`: волна C0 (фундамент) завершена.
  Google OAuth 2.0, серверные сессии в Postgres, CSRF (`gorilla/csrf`), middleware
  `LoadSession`/`RequireAuth`, точка входа `cmd/server` с полной цепочкой
  инициализации, compile-time проверка `dbAdapter`. `make gate` зелёный.

**Волна C1 — активна.** Завершённые атомы:

- **C1-lecture** — `internal/lecture` + расширение `internal/db` и `internal/web`:
  доменный модуль «Мои лекции» готов. Типы `Lecture`/`Status`/`Visibility`,
  интерфейсы `Repository` и `CoreTasks`, `Service` с методами
  List/Rename/SetVisibility/Delete/Retry, все бизнес-правила (owner-проверка до
  ядра, порядок delete, условия публикации/retry), HTTP-хендлеры ЛК под
  `auth.RequireAuth`, страница `LecturesPage` + `LectureCard`, CSRF через
  hx-headers. `make gate` зелёный.

- **C1-upload** — `internal/upload` + проводка в `cmd/server`: загрузка лекций
  полностью готова. Страница формы (`GET /upload`), presign-поток (`POST
  /upload/presign`), подтверждение (`POST /upload/confirm`), YouTube (`POST
  /upload/youtube`), клиентский JS-поток (presign → прямой PUT в MinIO → confirm),
  HMAC stateless-токен `Signer`, валидация медиа. `cmd/server` подключает
  реальный `*coreclient.CoreClient` через `coreTasksAdapter` и `coreStatusAdapter` —
  задачи реально уходят в ядро. `make gate` зелёный.

- **C1-sync** — `internal/syncsvc` + проводка в `cmd/server`: синхронизация
  статуса лекции с ядром готова. Вебхук `POST /webhooks/core` (HMAC-подпись,
  CSRF-exempt, conditional update, идемпотентность), поллинг-прокси `GET
  /lectures/{id}/status` (htmx ~10с, fallback-запись терминального статуса,
  мягкая деградация при недоступности ядра). `make gate` зелёный.

  **Известные ограничения (технический долг C1):**
  - PDF-слайды (`has_pdf`) не передаются в ядро — проброс файла отдельный атом.
  - Прямые (не-YouTube) медиа-URL не поддержаны.
  - `uploadSignKey` и CSRF-ключ генерируются на старте — рестарт инвалидирует
    незавершённые presign-токены (приемлемо для беты).
  - Мягкий экран 502/503 при недоступности ядра — долг (§8).

- **C1-devstack** — dev-experience: локальный запуск в одну команду. godotenv
  подгружает `.env` в `cmd/server` перед `config.Load`; `.env.example`,
  `docker-compose.yml` (Postgres 16 + MinIO + minio-init создаёт bucket),
  Makefile-цели `up`/`down`/`dev`. См. раздел «Локальный запуск».

- **C1-hub** — `internal/hub` + индекс `003` + страница: публичная витрина
  `GET /hub` (visibility='public' ORDER BY published_at DESC с автором), открыта
  анонимам. `db.LectureDB.ListPublic` (lectures⋈users), частичный индекс
  `idx_lectures_public_published_at`. `make gate` зелёный.

  Следующий и последний атом волны C1: **reader** (страница чтения конспекта;
  до него ссылка карточки витрины `/lectures/{id}/read` отдаёт 404).

Подробности — в `docs/WORKFLOW.md` и `docs/TASKS.md`.
