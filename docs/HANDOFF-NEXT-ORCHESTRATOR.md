# Промпт/брифинг для СЛЕДУЮЩЕГО оркестрирующего вызова LectureLog

> Скопируй текст ниже (раздел «ПРОМПТ») как стартовое сообщение новому оркестратору,
> либо просто открой этот файл первым. Он фиксирует состояние, правила и «грабли»,
> на которые мы уже наступили — чтобы не наступать снова.

---

## ПРОМПТ (вставлять новому вызову)

Ты — ОРКЕСТРАТОР разработки LectureLog по `docs/WORKFLOW.md`. Код руками НЕ пишешь:
планируешь, запускаешь сабагентов (планировщик/исполнитель/приёмщик/code-reviewer/docs),
читаешь вердикты, ведёшь ветки/worktree. Не сваливайся в исполнителя/архитектора.

ПЕРВЫМ делом прочитай: `docs/HANDOFF-C0.md` (состояние волны C0 — ЗАВЕРШЕНА),
этот файл целиком, `docs/WORKFLOW.md`, `docs/TASKS.md`, `docs/plans/2026-06-22-platform-design.md`.

СОСТОЯНИЕ: волна C0 (фундамент) ЗАВЕРШЕНА — все 4 атома (config, db, web, auth) приняты
(ACCEPT=COMPLETE + REVIEW=APPROVE) и смержены в ЛОКАЛЬНУЮ `integration` (коммит ce3e498+).
Ворота зелёные, дерево чисто, worktree-ы убраны. origin НЕ обновлён.

ПЕРВЫЕ ДЕЙСТВИЯ (закрыть хвост C0):
1. **DOCS C0-auth** — отдельным docs-субагентом обнови README про `internal/auth` + `cmd/server`
   (этот шаг НЕ был сделан из-за лимитов; контекст изменений — в HANDOFF-C0.md раздел «C0-auth — итог»).
2. **Конец волны C0** — по решению владельца: push `integration` на origin + ОДИН PR
   (integration → ... — уточни у владельца целевую ветку/смысл PR; B1 был PR в integration,
   но теперь атомы уже в integration, значит PR, вероятно, integration→main в конце волны.
   СПРОСИ владельца перед push — это первый публичный push волны).
3. **Затем волна C1** — доменные модули (5 ∥-задач: upload, lecture, hub, reader, sync).
   Vertical slices, у каждой свой прототип в `design/prototypes/`. См. TASKS.md §C1 и долги C1 ниже.

ПОЛИТИКА ВЛАДЕЛЬЦА (НЕ переоткрывать):
- Мержи готовые атомы в ЛОКАЛЬНУЮ integration СРАЗУ (--no-ff, без PR на каждый атом).
  Push + PR — в КОНЦЕ волны. (Это сменило раннюю политику «PR на каждый атом» из B1.)
- Веди автономно; зови владельца в конце волны ИЛИ останавливайся ЗАРАНЕЕ при лимитах.
- ЭКОНОМИЯ ТОКЕНОВ — модель сабагента под задачу: планирование — Plan-агент (дефолт/opus ок),
  кодирование/приёмка/docs — Sonnet (opus для рутинного кода ИЗБЫТОЧЕН). code-reviewer — свой.
- Мелкие механические правки (gofmt, go mod tidy, git-операции) оркестратор делает САМ,
  не плодя сабагента. Осмысленные правки кода — через исполнителя (LOOP).

ЛИМИТЫ: команда `cswap --status` (shell) показывает 5h/7d окна тарифа. Следи; останавливайся
заранее (владелец просил «довести до ~95% когда сабагент закончит, потом фиксировать»).
Прайм-кэш 5 мин — длинные паузы дороже. `/usage` как shell НЕ работает (это CLI-команда).

---

## КАК ВЕДЁТСЯ АТОМ (8 шагов, обкатано на 4 атомах C0)
PLAN(Plan-агент→PLAN.md в worktree, в git exclude, НЕ коммитить) → ISOLATE(git worktree
.worktrees/<задача>, ветка node/<задача> от integration) → BUILD(исполнитель Sonnet, TDD) →
ACCEPT(приёмщик Sonnet, ≠исполнитель, 2 слоя: ворота + смысл; вердикт COMPLETE|INCOMPLETE) →
REVIEW(superpowers:code-reviewer) → LOOP(замечания→исполнителю, лимит 3 круга) →
MERGE(--no-ff в локальную integration) → DOCS(отдельный docs-субагент, правило владельца).

ОРКЕСТРАТОР ВСЕГДА перепроверяет ворота САМ фактически (не верит отчёту сабагента) перед ACCEPT
и перед мержем: `make gate` (для web/auth) или `go build/vet/test`; проверяет ЧИСТОТУ ДЕРЕВА после
gate, gofmt, детерминизм генерации, что артефакты/бинари не закоммичены.

---

## «ГРАБЛИ», на которые мы УЖЕ наступили (проверяй каждый раз!)

1. **LSP основного дерева даёт ЛОЖНЫЕ ошибки про worktree-код.** Файлы атома живут только в
   `.worktrees/<задача>`; LSP/диагностика основного дерева показывает «undefined: X»,
   «missing go.sum entry», «not used», «This file is within module .worktrees/...». ЭТО АРТЕФАКТ —
   игнорируй. Истина = фактическая сборка В worktree (`cd .worktrees/<задача> && go build ./...`).
   Каждый раз проверяй в worktree, не верь diagnostics-блоку.

2. **Детерминизм генерации (templ + Tailwind) — ловушка №1.**
   - templ: `go generate ./...` (из каталога пакета) и `go tool templ generate` (из корня) дают
     РАЗНЫЙ `FileName` в `*_templ.go` → грязное дерево после gate. КАНОН: генерация из каталога
     пакета (Makefile `templ:` делает `cd internal/web && go tool templ generate`). Проверяй:
     после `make gate` и `make gen-check` дерево ОБЯЗАНО быть чистым (`git status` пусто).
   - Tailwind purge (`@source "../"`) сканирует utility-классы в .templ; набор классов меняется
     между правками → `app.css` ДРЕЙФУЕТ. Закоммиченный `static/css/app.css` ДОЛЖЕН быть в ФИНАЛЕ
     (после `make web-gen`), иначе gate его меняет. Это дважды кусало (C0-web, C0-auth).
   - `make gen-check` = `web-gen && git diff --exit-code` — твой детектор. Гоняй ПЕРЕД ACCEPT.

3. **Пустые коммиты `--allow-empty` (C0-config) — НЕ допускать.** Первый исполнитель схлопнул TDD-фазы
   в один коммит + пустышки. ТРЕБУЙ в промпте исполнителя: пофазные коммиты с РЕАЛЬНЫМ diff,
   red→green. Приёмщик/ревью это ловят. На C0-db/web/auth уже соблюдено — держи планку.

4. **gofmt НЕ входит в `go build/vet/test`.** Дважды файлы были не-gofmt-clean и прошли gate
   (C0-config login_test, C0-auth login_test+main.go). ВСЕГДА проверяй `gofmt -l internal/ cmd/`
   отдельно перед ACCEPT. Фикс — оркестратор сам `gofmt -w` (механика).

5. **go.mod indirect/tidy.** Новые прямые зависимости иногда помечаются `// indirect` или go.sum
   рассинхронен (C0-db). Проверяй `go mod tidy` (пустой diff) сам перед мержем.

6. **Артефакты/бинари не коммитить.** `/bin/` (Tailwind CLI 120МБ, gitignored), `/server` (бинарь
   cmd/server, gitignored). Проверяй `git ls-files | grep -E 'bin/|^server'` = пусто.
   КОММИТИТЬ нужно: `*_templ.go`, `static/css/app.css`, `static/vendor/htmx.min.js` (для чистой
   сборки без node/templ-бинаря/интернета).

7. **Сабагент-исполнитель иногда выходит с интерактивным вопросом «merge/PR?»** — ИГНОРИРУЙ его
   (мержит оркестратор по политике). В промпте исполнителя пиши «НЕ интерактивные вопросы — доделай и отчитайся».

8. **Тесты без сети/Postgres в дефолте.** Интеграционные (БД) — за `//go:build integration`
   (testcontainers, Docker ЕСТЬ в окружении, реально поднимается ~5-25с). OAuth — httptest, не
   реальный Google. Дефолтный `go test ./...` ОБЯЗАН быть зелёным без Docker/сети. GATE-команда
   БД: `make migrate-test` или `go test -tags=integration ./internal/db/...`.

9. **SendMessage недоступен** как инструмент — продолжить того же сабагента по agentId нельзя
   в новом чате; для LOOP запускай нового исполнителя с исчерпывающим контекстом дефекта.

---

## КЛЮЧЕВЫЕ ФАКТЫ ПРОЕКТА (чтобы не перечитывать всё)
- Module: `github.com/LectureLog/lecturelog-web`, Go 1.25. Стек: chi+templ+htmx+Tailwind v4 (CSS-first), pgx/v5, tern (миграции).
- Слои `internal/`: coreclient (B1, клиент ядра + верификатор вебхука), config (env+fail-fast секрета),
  db (схема+миграции+CRUD users/sessions), web (каркас «Читальный зал»), auth (OAuth+сессии+CSRF+middleware).
- `cmd/server/main.go` — точка входа (config→db→auth→web→Serve).
- Дизайн-пакет в `design/` (tokens.css, STYLE_GUIDE.md, prototypes/*.dc.html) — вход для C1.
  Прототипы: Конспект→C1-reader, Хаб→C1-hub, Загрузка→C1-upload. Прототипы РАСХОДЯТСЯ по деталям
  (Общее.dc.html=1240px каркас, Конспект.dc.html=1180px читалка) — это норма, разные экраны.
- HMAC только на ВЕБХУКЕ ядро→платформа (платформа верифицирует); исходящие НЕ подписываются (долг доков B1 закрыт).
- Env: C0-config валидирует обязательные (GOOGLE_CLIENT_ID/SECRET, PLATFORM_CALLBACK_URL,
  LECTURELOG_WEBHOOK_SECRET, PLATFORM_DB_DSN, CORE_API_BASE_URL без /api/v1, CORE_MINIO_*).
  Опц: PRESIGNED_TTL=24h, SESSION_TTL=720h. Для cmd/server: PLATFORM_SECURE="true" в проде.

## ВОЛНА C1 (следующая после хвоста C0) — TASKS.md §C1
5 vertical-slices ∥: upload (§5), lecture (§3/§8), hub (§8), reader (§6), sync (§7).
GATE C1 = сквозной e2e. Долги, заложенные для C1: см. HANDOFF-C0.md (валидация upload до ядра,
индексы hub/owner в lectures, FileServer листинг, tabular-nums, канонизация email, чистка сессий).
Параллелизм C1 возможен (независимые модули), но взвесь токены/merge-конфликты — config/db/web/auth
теперь общие, доменные модули их потребляют.
