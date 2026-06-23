# HANDOFF — Волна C0 (фундамент платформы). Точка возобновления.

> Первым прочитай этот файл, затем `docs/WORKFLOW.md`, `docs/TASKS.md` (§C0),
> `docs/plans/2026-06-22-platform-design.md` (§3 db, §4 auth, §9 раскладка).
> Роль ведущей сессии — **ОРКЕСТРАТОР**: код руками НЕ пишет, запускает агентов,
> читает вердикты, ведёт ветки/worktree-ы. Решение владельца (2026-06-23):
> оркеструю автономно, зову человека в конце волны (приём PR) или останавливаюсь
> заранее при приближении лимитов тарифа.

## База волны
- `integration` = `2ef594f` (PR #1 B1 смержен на origin, локальная подтянута).
  Содержит готовый `internal/coreclient/` (gen.go, client, webhook-верификатор),
  `go.mod` (module `github.com/LectureLog/lecturelog-web`, go 1.25, tool-директива
  oapi-codegen), `Makefile`, `README.md`, `scripts/normalize_openapi.py`.
- Воркати B1 убран, ветка `node/B1-coreclient` удалена.

## Порядок задач C0 (РЕШЕНО: строго последовательно)
Зависимости (§9): доменные модули → общие слои; auth.middleware — общий блокер.
```
C0-config  → общий слой internal/config, ни от кого не зависит. Закрывает долг B1
             (fail-fast на пустом LECTURELOG_WEBHOOK_SECRET). ПЕРВЫЙ.
C0-db      → общий слой internal/db (pgx+миграции). Зависит от config (DSN). ВТОРОЙ.
C0-web     → общий слой internal/web (templ+Tailwind+layout «Читальный зал»).
             Презентация, независим. ТРЕТИЙ.
C0-auth    → доменный модуль auth (OAuth+сессии+CSRF/state+middleware).
             Зависит от db (users/identities/sessions) + config (OAuth). ПОСЛЕДНИЙ.
```
Последовательно — дешевле по токенам, нет merge-конфликтов в go.mod, проще приёмка.
Соответствует «последовательно внутри C0» в WORKFLOW.

## Долги B1, релевантные C0
1. **C0-config ОБЯЗАН падать на старте при пустом `LECTURELOG_WEBHOOK_SECRET`.**
   `VerifyWebhookSignature` при пустом секрете может вернуть true → молчаливая
   дыра в C1-sync. Валидация секрета — в config, НЕ в coreclient.
2. NB: `internal/coreclient/config.go` УЖЕ есть (endpoints ядра для coreclient).
   C0-config (`internal/config/`) — конфиг ВСЕГО приложения (.env): OAuth, HMAC,
   DB DSN, MinIO ядра, callback-URL, TTL сессии/presigned. НЕ дублировать
   coreclient.Config — интегрировать (config строит/питает coreclient).

## Ворота C0 (слой 1 приёмки, из TASKS.md)
```
go build ./... && go vet ./... && go test ./...
```
(+ для C0-web: templ generate и сборка Tailwind перед build.)
GATE C0: миграции применяются; layout рендерится в обеих темах; вход через Google
работает; middleware пускает аноним на public и режет приватное; CSRF/state срабатывают.

## State-машина волны
| Задача | PLAN | ISOLATE | BUILD | ACCEPT | REVIEW | LOOP | MERGE | DOCS |
|---|---|---|---|---|---|---|---|---|
| C0-config | ✅ | ✅ | ✅ | ✅ COMPLETE | ✅ APPROVE | ✅ 1 круг (gofmt) | ✅ в integration (bb819d3) | ✅ |
| C0-db     | ✅ | ✅ | ✅ | ✅ COMPLETE | ✅ APPROVE | ✅ 1 круг (go mod tidy) | ✅ в integration (00025c7) | ✅ |
| C0-web    | — | — | — | — | — | — | — | — |
| C0-auth   | — | — | — | — | — | — | — | — |

## ИЗМЕНЕНИЕ MERGE-ПОЛИТИКИ (решение владельца 2026-06-23)
Атомы C0 **мержатся в локальную integration сразу** (--no-ff, без PR на каждый).
В КОНЦЕ волны C0: push ветки integration на origin + ОДИН PR integration→... (или
по договорённости). НЕ создавать PR на каждый атом. Worktree атома убирается после мержа.

### C0-config — итог атома (готов к PR)
- Ветка `node/C0-config` от integration. Пакет `internal/config`:
  `Load(getenv func(string) string) (*Config, error)` — чистая, stdlib-only,
  агрегированный fail-fast по всем недостающим обязательным; отдельная проверка
  пустого `LECTURELOG_WEBHOOK_SECRET` (долг B1 закрыт в config, не в coreclient);
  фабрика `(*Config).CoreClient()` без дублирования; дефолты PRESIGNED_TTL=24h,
  SESSION_TTL=720h, CORE_MINIO_USE_SSL=false.
- ACCEPT: COMPLETE (ворота build/vet/test exit 0, go.mod/coreclient не тронуты,
  10 тестов PASS). REVIEW: CHANGES REQUESTED → единственный фикс gofmt load_test.go
  применён (коммит 97dc077) → APPROVE. DOCS: README обновлён (коммит a709705).
- Наблюдение (не блокер): коммиты фаз 3–6 пустые (--allow-empty) — TDD-гранулярность
  нарушена, реализация в одном коммите фазы 2; функционально полно, тесты red→green
  существуют и проходят. Учесть для следующих исполнителей: коммитить пофазно реально.
### C0-db — итог атома (смержен в integration)
- `internal/db`: миграции tern/v2 (embed), `db.New(ctx,dsn) *pgxpool.Pool` (ping),
  `db.Migrate(ctx,pool)`. Таблицы users/identities/sessions/lectures строго §3,
  нативные enum, gen_random_uuid, частичный idx_lectures_core_task_id (§7).
  db НЕ импортирует config (DSN строкой). Решение оркестратора: индексы hub/owner
  отданы в C1 (только core_task_id здесь).
- ACCEPT COMPLETE (дефолтные ворота без Postgres зелёные; интеграционный тест за
  тегом `integration` РЕАЛЬНО прошёл через testcontainers — Docker есть в окружении).
  REVIEW APPROVE. Замечание ревью (go mod tidy: прямые зависимости были // indirect)
  закрыто коммитом 189e6a7. DOCS: README обновлён (e3a41ab).
- Коммиты пофазные с реальным diff (долг C0-config по гранулярности исправлен).
- GATE-команда миграций: `make migrate-test` (== `go test -tags=integration ./internal/db/...`), требует Docker.

## ПАУЗА (2026-06-23, ~22:40): остановка по лимитам
5h-окно тарифа на 86% (сброс ~23:20). Остановился ПОСЛЕ мержа C0-db, ДО старта
C0-web — чтобы не упереться в лимит посреди атома. integration = 00025c7 (config+db),
ветки атомов убраны, дерево чисто, ворота зелёные. Origin НЕ обновлён (push — в конце
волны по новой политике).

## СЛЕДУЮЩИЙ ШАГ (после сброса лимитов): C0-web
- Каркас «Читальный зал»: templ + Tailwind, токены design/tokens.css, общий layout
  (sticky-шапка, светлая/тёмная тема), htmx-хелперы, статика. По design/STYLE_GUIDE.md
  (дизайн-пакет — найти/проверить наличие каталога design/ перед планированием).
- Независим от config/db (чистая презентация). Worktree node/C0-web от integration.
- Ворота C0-web: + `templ generate` и сборка Tailwind ПЕРЕД go build. Учесть в плане
  toolchain (templ как tool-директива go.mod? как Makefile-цель?).
- ПОСЛЕ C0-web → C0-auth (последний, блокер защищённых роутов; зависит от db+config).
- Затем КОНЕЦ ВОЛНЫ C0: push integration на origin + PR (новая политика).

## Артефакты
- Worktree C0-config: `.worktrees/C0-config`, ветка `node/C0-config` от integration.
- План задачи: `<worktree>/PLAN.md` (в git exclude — НЕ коммитить).
- Контракт ядра (read-only): `/home/krivonosov/projects/lecturelog-core/docs/{openapi.json,api-contract.md}`.
