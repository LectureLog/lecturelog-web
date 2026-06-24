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

## Точка входа `cmd/server`

Единственный бинарь платформы. Цепочка инициализации:

```
config.Load → db.New + db.Migrate → dbAdapter → auth.NewService → web.NewRouter → ListenAndServe
```

**`dbAdapter`** — адаптер из `cmd/server`, реализует `auth.Repository` поверх
`db.UserDB` и `db.SessionDB`. Связка намеренно живёт здесь: `internal/db` не
импортирует `internal/auth`, `internal/auth` не знает про `pgx`. Корректность
проверяется compile-time: `var _ auth.Repository = (*dbAdapter)(nil)`.

**`web.NewRouter`** принимает вариативные опции — `web.WithGlobalMiddleware` и
`web.WithMount`. Пакет `internal/web` не зависит от `internal/auth`; сервис
инъектируется через опции:

```go
web.NewRouter(
    web.WithGlobalMiddleware(authSvc.LoadSession, csrfMiddleware),
    web.WithMount(func(r chi.Router) { authSvc.Mount(r) }),
)
```

**Адрес** задаётся через `PLATFORM_ADDR` (дефолт `:8080`).

**Известное ограничение (технический долг).** CSRF-ключ генерируется через
`crypto/rand` при каждом старте сервера. При рестарте все ранее выданные CSRF-токены
инвалидируются. Для прод-стабильности необходимо вынести ключ в env-переменную
(например, `PLATFORM_CSRF_KEY`).

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

Впереди — волна C1 (доменные модули: upload, lecture, hub, reader, sync).
Подробности — в `docs/WORKFLOW.md` и `docs/TASKS.md`.
