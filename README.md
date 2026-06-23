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
| `make build` | `generate` → `go build ./...` |
| `make vet` | `go vet ./...` |
| `make test` | `go test ./...` (юнит + контрактный smoke к замоканному ядру) |
| `make gate` | ворота GATE B: `generate` + build + vet + test |
| `make sync-spec` | обновить вендоренную спеку из репозитория ядра |

Ворота GATE B (слой 1 приёмки платформы):

```bash
go generate ./... && go build ./... && go vet ./... && go test ./...
```

`go generate` обязателен перед сборкой — он создаёт/обновляет `gen.go` из
вендоренной спеки. Контрактный smoke поднимает `httptest`-мок ядра и проверяет
пути, методы, Content-Type, сериализацию тел и маппинг кодов (200/204/400/404/409)
в доменные ошибки — герметично, без сети и соседнего репозитория.

## Статус

- **B1** — `internal/coreclient`: типизированный клиент ядра, HMAC-верификатор
  вебхука, GATE B зелёный.
- **C0-config** — `internal/config`: единый конфиг-слой, fail-fast агрегация
  ошибок окружения, валидация `LECTURELOG_WEBHOOK_SECRET` (долг B1 закрыт).

Впереди — оставшиеся атомы C0 (db, web-каркас, auth) и волна C1 (доменные
модули: upload, lecture, hub, reader, sync). Подробности — в `docs/WORKFLOW.md`
и `docs/TASKS.md`.
