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
| Задача | PLAN | ISOLATE | BUILD | ACCEPT | REVIEW | LOOP | MERGE(PR) | DOCS |
|---|---|---|---|---|---|---|---|---|
| C0-config | ✅ | ✅ node/C0-config | ✅ | ✅ COMPLETE | ✅ APPROVE | ✅ 1 круг (gofmt) | ⏳ PR, стоп | ✅ |
| C0-db     | — | — | — | — | — | — | — | — |
| C0-web    | — | — | — | — | — | — | — | — |
| C0-auth   | — | — | — | — | — | — | — | — |

MERGE: НЕ авто-мерж. На каждом готовом атоме — PR (node/<задача> → integration)
через gh с функциональным описанием, СТОП, зову человека принять.

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
- СЛЕДУЮЩИЙ ШАГ: push node/C0-config на origin + PR → integration, СТОП, человек
  принимает PR. После приёма — синхронизировать локальную integration, убрать
  worktree, начать C0-db (зависит от config: DSN из PLATFORM_DB_DSN).

## Артефакты
- Worktree C0-config: `.worktrees/C0-config`, ветка `node/C0-config` от integration.
- План задачи: `<worktree>/PLAN.md` (в git exclude — НЕ коммитить).
- Контракт ядра (read-only): `/home/krivonosov/projects/lecturelog-core/docs/{openapi.json,api-contract.md}`.
