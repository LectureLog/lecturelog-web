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
| C0-web    | ✅ | ✅ | ✅ | ✅ COMPLETE | ✅ APPROVE | ✅ 2 круга (детерминизм templ; чистка config+focus) | ✅ в integration (08cdb4c) | ✅ |
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

## ДИЗАЙН-ПАКЕТ ВНЕСЁН В РЕПО (коммит 154cfea, в integration)
Каталог `design/` теперь в репозитории (источник — прототип дизайнера из
Downloads/«Сбор нового проекта»):
- `design/tokens.css` — токены «Читальный зал»: светлая/тёмная (атрибут
  `data-theme="dark"` на корне), фон #f4f2eb, поверхность #fbf9f3, чернила #26241e,
  вечнозелёный акцент #2f5a4c; шрифты Source Serif 4 (контент) + Onest (интерфейс,
  Google Fonts); радиусы r-card/slide/btn/pill/track. **Переносить ПЕРВЫМ** в Tailwind-тему.
- `design/STYLE_GUIDE.md` — принципы (монохром+1 акцент, серив для чтения, hairline,
  колонка 65ch, моторика 0.14–0.22s), типограф-шкала, сетка (шапка 58px sticky,
  двухколоночник max-1180px, сайдбар 312px, контент max-768px, брейкпоинты 980/520),
  компоненты, §7 состояния новых страниц (upload/processing/library/empty), §8 тех-правила
  (видимость НЕ завязывать на анимацию; tabular-nums; a11y).
- `design/README.md` — handoff читалки + модель данных конспекта (ложится на structure.json ядра).
- `design/prototypes/*.dc.html` — hi-fi РЕФЕРЕНСЫ (Конспект 718стр, Хаб, Загрузка,
  Общее, Книги-галерея, Логотип). **НЕ продакшн-код** — переносить в templ+Tailwind, не копировать HTML.
- Соответствие экранов: Конспект→C1-reader(§6), Хаб→C1-hub(§8), Загрузка→C1-upload(§5).
  Для C0-web базовый каркас (шапка+layout+тема) — смотреть «Общее.dc.html» и STYLE_GUIDE §4.

### C0-web — итог атома (смержен в integration 08cdb4c)
- `internal/web`: chi+templ+htmx+Tailwind v4 (CSS-first). templ — tool-директива go.mod
  (`go generate ./...`, `*_templ.go` коммитятся). Tailwind — standalone CLI без node
  (Makefile `tailwind-bin` → ./bin/ gitignored; `app.css` коммитится). htmx вендорён
  (static/vendor/htmx.min.js v1.9.12, go:embed). Layout «Читальный зал»: sticky-шапка
  58px, тема data-theme (дефолт light в разметке, анти-FOUC инлайн-скрипт, тумблер),
  токены design/tokens.css → internal/web/assets/tokens.css → Tailwind @theme. web.NewRouter
  (chi): /static/* (embed) + / (демо). cmd/server НЕ создан (граница — C0-auth/интеграция).
- Ворота web: `make gate` (templ+tailwind+build/vet/test), `make gen-check` (детерминизм,
  git diff --exit-code), `make web-gen`. go test проходит БЕЗ node/Tailwind/интернета (артефакты в репо).
- ACCEPT COMPLETE, REVIEW APPROVE. 2 LOOP-круга: (1) детерминизм генерации templ — было
  два несогласованных способа (из корня vs из пакета) → грязное дерево после gate; канон =
  генерация из каталога пакета, `make templ` приведён к нему (e4befd5). (2) чистка: удалён
  мёртвый tailwind.config.js (v4 CSS-first → `@source` в tailwind.css), focus-стили a11y (7013617).
- Коммиты пофазные с реальным diff.

## ДОЛГИ для C1 (из REVIEW C0-web — не блокеры, зафиксировать)
1. **FileServer отдаёт листинг каталогов** (/static/css/, /static/vendor/). Для прод-беты
   обернуть FileSystem, возвращающий 404 на директории. → C0-auth/интеграция.
2. **tabular-nums** (§8) — применить на числовых элементах (время/проценты/даты) в C1
   (reader/lecture). В C0 числовых нет.
3. **max-width**: базовый layout = 1240px (из прототипа Общее.dc.html — корректно для
   каркаса приложения). Читалка C1-reader использует 1180px (из Конспект.dc.html — свой
   прототип). НЕ дефект, просто разные прототипы для разных экранов.
4. **Tailwind purge**: добавлен `@source "../"` — при C1 (utility-классы в .templ) проверить,
   что нужные классы не вырезаются и попадают в app.css.

## СЛЕДУЮЩИЙ ШАГ: C0-auth (ПОСЛЕДНИЙ атом C0, блокер защищённых роутов)
Зависит от db (users/identities/sessions) + config (OAuth-секреты, PLATFORM_CALLBACK_URL) +
web (layout для страниц входа/ошибок, hx-headers-хук под CSRF уже заложен в layout).
- Google OAuth-флоу (state БЕЗУСЛОВНО, §4). Сессии в Postgres (кука httponly/secure/SameSite=Lax).
- Логика входа по email (§3: матч users.email только при email_verified=true; разный email = новый юзер).
- CSRF synchronizer-токен на мутирующих формах (templ кладёт, htmx шлёт заголовком — место
  hx-headers в layout готово; middleware сверяет). Готовый middleware (gorilla/csrf или nosurf).
- Middleware прав: аноним на public-роуты, сессия на остальном (§4 матрица).
- Вероятно появится cmd/server (точка входа: config.Load → db.New/Migrate → web.NewRouter +
  auth-роуты+middleware) — здесь граница C0-web «cmd позже» закрывается. Уточнить в плане:
  заводить полноценный cmd/server или минимальный bootstrap.
- GATE C0 (полный): миграции применяются; layout в обеих темах; вход через Google работает;
  middleware пускает аноним на public и режет приватное; CSRF/state срабатывают.
- ПОСЛЕ C0-auth → КОНЕЦ ВОЛНЫ C0: push integration на origin + PR (политика владельца).
- Независим от config/db (чистая презентация). Worktree node/C0-web от integration.
- Ворота C0-web: + `templ generate` и сборка Tailwind ПЕРЕД go build. Учесть в плане
  toolchain (templ как tool-директива go.mod? как Makefile-цель?).
- ПОСЛЕ C0-web → C0-auth (последний, блокер защищённых роутов; зависит от db+config).
- Затем КОНЕЦ ВОЛНЫ C0: push integration на origin + PR (новая политика).

## Артефакты
- Worktree C0-config: `.worktrees/C0-config`, ветка `node/C0-config` от integration.
- План задачи: `<worktree>/PLAN.md` (в git exclude — НЕ коммитить).
- Контракт ядра (read-only): `/home/krivonosov/projects/lecturelog-core/docs/{openapi.json,api-contract.md}`.
