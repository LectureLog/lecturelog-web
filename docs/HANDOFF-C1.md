# HANDOFF — Волна C1 (доменные модули). Точка возобновления.

> Первым прочитай этот файл, затем `docs/HANDOFF-NEXT-ORCHESTRATOR.md` (правила/грабли),
> `docs/WORKFLOW.md`, `docs/TASKS.md` (§C1), `docs/plans/2026-06-22-platform-design.md`.
> Роль ведущей сессии — **ОРКЕСТРАТОР**: код руками НЕ пишет (даже мелкий фикс из ревью —
> через LOOP-исполнителя; см. память feedback-orchestrator-no-coding). Механика
> (gofmt, git, ворота) — сам.

## База волны
- `integration` = `3b306c4` (после C0 + C1-lecture + **C1-upload** + **C1-sync** +
  **C1-devstack** + **C1-hub** + docs `11bbd48` + **C1-reader БЭКЕНД A/B1/B2**). origin синхронен по
  C0 (2ef594f..0507824) + e750cb7; C1-атомы накапливаются в integration (PR/push в main — в конце волны).
- Дерево ЧИСТО, ворота ЗЕЛЁНЫЕ (build/vet/test/gofmt, gen-check exit 0; integration-БД-тесты hub
  прогнаны реально 82с), worktree-ов нет. Безопасная граница.

## ⏸️ ТОЧКА ВОЗОБНОВЛЕНИЯ (2026-06-26, 2): остались ТОЛЬКО reader-UI (C) и reader-проводка (D)
ВЫПОЛНЕНО в этой сессии (2026-06-26): **C1-devstack** (merge ddc0b6d), **C1-hub** (merge 0ce083f,
независимо проверен — безопасность чистая), docs (11bbd48), и **БЭКЕНД C1-reader** — под-атомы A/B1/B2
(merge 3b306c4): пакет `internal/s3` (локальный presigned-GET + GetObject поверх MinIO, нов. зав-ть
`minio-go/v7`), `internal/reader/structure.go` (схема structure.json + парсер + фикстуры),
`internal/reader` сервис доступа (матрица прав + presign 24ч + goldmark БЕЗ unsafe-HTML, нов. зав-ть
`goldmark`). Ворота зелёные, s3-integration-тест компилируется за тегом (реальный MinIO — на e2e-GATE).

ОСТАЛОСЬ по C1-reader (ПЕРВОЕ ДЕЙСТВИЕ нового чата): под-атомы **C** (UI) и **D** (проводка) — см.
раздел «C1-reader: ОСТАВШИЕСЯ под-атомы C и D» ниже. С завершением D витрина `/hub` получит рабочую
ссылку `/lectures/{id}/read` (сейчас 404). Полный план reader — в истории Plan-агента; контракты бэкенда
зафиксированы ниже.

### 🚧 C1-reader: ОСТАВШИЕСЯ под-атомы C и D
**Готовые контракты бэкенда (НЕ менять без нужды):**
- `reader.Service.Load(ctx, lectureID, viewerID string)(reader.ReaderView, error)` — viewerID=="" аноним.
  Ошибки: `reader.ErrNotFound` (404/несуществует/нет прав — НЕ раскрывать), `reader.ErrNotReady`
  (владельцу, лекция не ready), `reader.ErrCoreUnavailable` (structure.json не прочитан — мягкий 502).
- `reader.NewService(repo LectureRepo, store ObjectStore, presign Presigner, md Renderer, ttl)` —
  интерфейсы объявлены в `internal/reader/reader.go` (consumer-owned). `s3.Client` удовлетворяет
  ObjectStore (GetObject) и Presigner (PresignGet); `reader.MarkdownRenderer` (goldmark) — Renderer;
  адаптер поверх `db.LectureDB` (FindByID→`reader.LectureMeta`) — LectureRepo. ttl = `cfg.PresignedTTL` (24ч).
- `reader.ReaderView{LectureID,Title,SourceTitle,SourceKind,Duration,IsOwner,Sections[]ViewSection}`;
  `ViewSection{Number"01",Title,Subtopics}`; `ViewSubtopic{Number"1.1",Title,ContentHTML(санитизирован
  goldmark — можно templ.Raw),Media*ViewMedia,SlideURLs[]string}`; `ViewMedia{Kind,Start,End,URL}` (URL presigned).

**Под-атом C — UI читалки** (`internal/web`, БЕЗ изменения cmd/server):
- `internal/web/page_reader.templ` + сгенерированный `_templ.go`: `web.ReaderVM` (shape согласуй с
  маппингом ReaderView→VM в хендлере D) + компоненты по прототипу `design/prototypes/Конспект.dc.html`
  (sidebar+TOC, doc-head, section→subtopic→player/slide/callout). `ContentHTML` через `templ.Raw`
  (инвариант: уже санитизирован goldmark в B2 — впиши комментарий).
- `internal/web/static/js/reader.js` (scroll-spy/progress, TOC nav, players preload=metadata, lightbox,
  search, export) — проверь, что `js/*` уже в `//go:embed` (internal/web/static.go).
- `internal/web/assets/tailwind.css` → классы читалки (токены, без хардкода) → `make tailwind`.
- ГРАБЛЯ детерминизма: templ ТОЛЬКО из каталога пакета (`make templ`), app.css после `make web-gen`,
  финальный коммит после генерации, `make gen-check` ОБЯЗАТЕЛЕН.

**Под-атом D — проводка + handlers + Export** (`cmd/server`, `internal/reader/handlers.go`, `internal/coreclient`):
- `cmd/server/main.go`: построить `s3.New(cfg.CoreMinIO...)`, `reader.NewService(...)`, адаптер
  `readerRepo` (FindByID поверх db.LectureDB → reader.LectureMeta), `var _ reader.LectureRepo=...`.
  Монтаж `GET /read/{id}` (+`/read/{id}/export`) **ВНЕ группы RequireAuth** (под LoadSession; доступ —
  внутри сервиса). НЕ под RequireAuth — иначе аноним не прочитает public.
- `internal/reader/handlers.go` + handlers_test.go (httptest): handleRead (viewerID из auth.UserFromContext
  или ""; Load; ErrNotFound→404, ErrNotReady→экран «обрабатывается», ErrCoreUnavailable→**мягкий 502**,
  не http.Error-стектрейс; иначе маппинг ReaderView→web.ReaderVM + web.ReaderPage). handleExport (presigned ZIP).
- `internal/coreclient/client.go`: `GetResultURL(ctx, taskID, filename)(string,error)` поверх уже
  сгенерированного `GetTaskResultUrlApiV1TasksTaskIdResultUrlGetWithResponse` (тип `ResultUrlResponse`) — Export ZIP.
- Финальные ворота: `make web-gen && make gen-check && go build/vet/test`; gofmt; go mod tidy.

**КРИТИЧНЫЙ РИСК reader (НЕ блокер атома, блокер e2e):** эндпоинта `structure.json` в ядре ПОКА НЕТ
(§10.4 отложено), per-artifact presign в coreclient нет → платформа презайнит ЛОКАЛЬНО (internal/s3) и
читает `results/<core_task_id>/structure.json` напрямую из MinIO. JSON-теги structure.json — мой контракт
из §6, МОГУТ разойтись с реальной сериализацией ядра → сверить при появлении эталона. Реальный e2e reader
заблокирован ядром. Долги: имя маршрута (/read/{id} — уточнить); browser-reachable MinIO endpoint
(возможно отдельный публичный endpoint в config); error_code-каталог экранов.

### ⚠️ НОВАЯ СХЕМА РОЛЕЙ + ГРАБЛЯ КОММИТОВ CODEX (распоряжение владельца 2026-06-26)
Владелец уточнил схему: **Codex билдит И КОММИТИТ сам → ОТДЕЛЬНЫЙ агент проверяет код → оркестратор
ТОЛЬКО решает мердж** (не гоняет ворота вместо проверяющего; механический коммит/мердж — за оркестратором).
- **ГРАБЛЯ (важно!):** песочница Codex (`-s workspace-write`) делает `.git` READ-ONLY by design —
  Codex НЕ МОЖЕТ закоммитить НИ в worktree, НИ даже в свежем клоне (проверено: `index.lock: Read-only
  file system`). writable_roots это НЕ снимают (жёсткий guard на `.git`). Единственный путь для
  Codex-самокоммита — ОТКЛЮЧИТЬ песочницу: `codex exec ... -s danger-full-access` ИЛИ
  `--dangerously-bypass-approvals-and-sandbox` (среда уже в контейнере). Но здешний авто-классификатор
  это БЛОКИРУЕТ, пока в правах нет разрешающего правила.
- **ЧТО НУЖНО (одноразово, делает ВЛАДЕЛЕЦ — агент не может, self-modification guard):** добавить в
  `.claude/settings.local.json` правило `{"permissions":{"allow":["Bash(codex exec:*)"]}}` (или через
  `/permissions`). ПОСЛЕ этого Codex запускать с `--dangerously-bypass-approvals-and-sandbox` → он
  коммитит сам пофазно. Конфиг `~/.codex/config.toml` уже имеет `[sandbox_workspace_write] network_access=true`
  и `[search] enabled=true`.
- **ПОКА правила НЕТ** (как в этой сессии): Codex билдит в worktree (sandbox ON, ворота гонит сам, но НЕ
  коммитит) → оркестратор САМ перепроверяет ворота + пофазно коммитит (механика) → ОТДЕЛЬНЫЙ агент
  (Claude, ≠ Codex-движок) проверяет код и даёт вердикт COMPLETE/INCOMPLETE → оркестратор решает мердж.
  Это рабочий компромисс, волна так и велась (devstack/hub/reader-be).
- **Грабля worktree+tailwind:** в worktree нет `bin/` (gitignored, 120МБ tailwindcss). Перед BUILD с
  генерацией CSS — симлинк `ln -s /root/lecturelog-web/bin .worktrees/<atom>/bin` (в коммит не идёт),
  удалять перед `git worktree remove`.

### ✅ ВЫПОЛНЕН АТОМ C1-devstack (dev-experience, merge ddc0b6d, 2026-06-26)
Цель — запуск в одну команду без ручного `source .env`. Небольшой самодостаточный атом. Спека ниже —
историческая (реализовано: godotenv в main, `.env.example`, `docker-compose.yml` Postgres16+MinIO+init,
make `up`/`down`/`dev`, раздел README «Локальный запуск», `.env` в .gitignore).
Объём (BUILD через Codex по этой спеке):
1. **godotenv-загрузчик в `cmd/server/main.go`**: ПЕРЕД `config.Load` подгружать `.env` из корня, если
   файл есть (например `github.com/joho/godotenv` → `_ = godotenv.Load()`; молча игнорировать
   отсутствие файла — реальные env-переменные имеют приоритет). `go get` зависимости, `go mod tidy`.
2. **`.env.example`** в корне: все ОБЯЗАТЕЛЬНЫЕ переменные с комментариями и плейсхолдерами —
   GOOGLE_CLIENT_ID/SECRET, PLATFORM_CALLBACK_URL=http://localhost:8080/auth/callback,
   LECTURELOG_WEBHOOK_SECRET, PLATFORM_DB_DSN (на локальный compose-Postgres),
   CORE_API_BASE_URL, CORE_MINIO_ENDPOINT/ACCESS_KEY/SECRET_KEY/BUCKET/USE_SSL; опц.
   PLATFORM_ADDR/PRESIGNED_TTL/SESSION_TTL/PLATFORM_SECURE. `.env` — в .gitignore (НЕ коммитить).
3. **`docker-compose.yml`**: сервис `postgres` (16, healthcheck, том, креды под DSN из .env.example)
   и `minio` (+ `minio/mc` init-контейнер, создающий BUCKET и политику). Порты наружу
   (5432, 9000/9001). DSN/endpoint в .env.example должны указывать на эти сервисы.
4. **Makefile**: цели `up` (docker compose up -d), `down`, `dev` (up + `set -a; . ./.env; set +a;
   go run ./cmd/server`). Прокомментировать.
5. **README**: раздел «Локальный запуск» — `cp .env.example .env` → заполнить Google OAuth →
   `make up` → `make dev` → http://localhost:8080. Уточнить, что ядро (CORE_API_BASE_URL) для
   ПОЛНОГО цикла загрузки нужно отдельно (compose поднимает только Postgres+MinIO платформы).
Ворота: `go build/vet/test`, `go mod tidy` (чистый diff), gofmt. Тесты герметичны (godotenv.Load()
без файла не падает). Долг: мок-ядро в compose — отдельно (для e2e GATE C1). НЕ блокер.
Грабля: миграции применяются автоматически при старте (db.Migrate) — отдельная команда не нужна.

### ⚙️ ОКРУЖЕНИЕ НЕСТАБИЛЬНО (важно!)
Среда теряет тулчейны посреди сессии. На старте НЕ было Go — ставил вручную (tar в /usr/local/go,
PATH через /etc/profile.d/go.sh, НО Bash-инструмент не-login → PATH задавать инлайн
`export PATH=$PATH:/usr/local/go/bin:/root/go/bin`). Node тоже пропадал → runtime codex-плагина
(`.mjs`) молча падал `node: command not found`, Codex ничего не коммитил. Node v22 ставил в /usr/local.
ЕСЛИ Codex вернулся пустым/без коммитов — ПЕРВЫМ делом `which go && which node`, переустанови.

### ⚠️ ГРАБЛЯ Codex-обёртки: детач + зависание на stdin
`codex:codex-rescue` (особенно с run_in_background) часто ДЕТАЧИТ реальный codex-job и возвращает
«Задача передана в фоновый режим» — job может зависнуть/не закоммитить. `codex exec` напрямую в фоне
ВИСНЕТ на чтении stdin (`Reading additional input from stdin...`, 0% CPU) — ОБЯЗАТЕЛЬНО `</dev/null`.
Надёжный путь для LOOP-фиксов: `codex exec -C <worktree> -m gpt-5.5 -c model_reasoning_effort=medium
-s workspace-write --skip-git-repo-check "$(cat prompt)" </dev/null`. В sandbox codex `.git` бывает
read-only → codex НЕ закоммитит; тогда оркестратор сам прогоняет ворота и коммитит правки (механика).
ВСЕГДА перепроверяй процесс: жив ли (ps), пишет ли файлы (mtime), есть ли коммит — НЕ верь «отправлено».

## Порядок задач C1 (РЕШЕНО оркестратором)
Заявлено «5 ∥-задач», но реальные зависимости делают их НЕ полностью параллельными:
`lecture` — фактический блокер-основа (его модель/данные потребляют upload/hub/reader/sync).
```
C1-lecture → модель lectures + ЛК «Мои лекции». БЛОКЕР-основа. ПЕРВЫЙ. ✅ ЗАВЕРШЁН.
C1-upload  → форма загрузки (файл presigned-PUT / YouTube), сверка s3_key, confirm→лекция+задача.
             Зависит от lecture (создаёт строки) + coreclient (задача в ядро). ВТОРОЙ (объёмный).
C1-sync    → приём вебхука (HMAC), conditional update статуса, поллинг-прокси 10с.
             Зависит от lecture-таблицы (через db, НЕ через пакет lecture) + coreclient.
C1-hub     → витрина публичных (visibility=public ORDER BY published_at). Открыт анонимам.
             Зависит от lecture-данных. Относительно небольшой.
C1-reader  → читалка «Читальный зал»: structure.json, presigned-пачка 24ч, рендер. Зависит
             от lecture (права) + coreclient + s3. САМЫЙ ОБЪЁМНЫЙ.
```
hub и reader друг от друга НЕ зависят — их МОЖНО распараллелить (владелец просил
распараллеливать где возможно), но взвесь токены (7d-окно было на 88%) и merge-конфликты.

## State-машина волны C1
| Задача | PLAN | ISOLATE | BUILD | ACCEPT | REVIEW | LOOP | MERGE | DOCS |
|---|---|---|---|---|---|---|---|---|
| C1-lecture | ✅ | ✅ | ✅ | ✅ COMPLETE | ✅ APPROVE | ✅ 2 фикса (security) | ✅ в integration (48f55b3) | ✅ (b452a96) |
| C1-upload  | ✅ | ✅ | ✅ 7/7 | ✅ COMPLETE | ✅ (2 MAJOR ui) | ✅ guard/htmx фиксы | ✅ в integration (df35653) | ✅ (db3ec68) |
| C1-sync    | ✅ | ✅ | ✅ | ✅ COMPLETE | ✅ (2 MAJOR) | ✅ MaxBytes+статус фиксы | ✅ в integration (2a96b75) | ✅ (db3ec68) |
| C1-devstack| ✅ | ✅ | ✅ | ✅ COMPLETE | — | — | ✅ в integration (ddc0b6d) | ✅ (этот коммит) |
| C1-hub     | ✅ | ✅ | ✅ | ✅ COMPLETE | ✅ | ✅ | ✅ в integration (0ce083f) | ✅ (этот коммит) |
| C1-reader (бэкенд A/B1/B2) | ✅ | ✅ | ✅ | самопроверка матрицы доступа | — | — | ✅ в integration (3b306c4) | ✅ (этот коммит) |
| C1-reader (UI=C + проводка=D) | ✅ (в плане) | ⏳ | ⏳ | | | | | |

### C1-devstack — пофазные коммиты (merge ddc0b6d)
| коммит | фаза |
|---|---|
| 4a63219 | godotenv-загрузка `.env` в `cmd/server` (молча игнорирует отсутствие файла) |
| 99988f7 | `docker-compose.yml` (Postgres 16 + MinIO + minio-init создаёт bucket) + `.env.example` |
| 85c7c3c | Makefile-цели `up`/`down`/`dev` + раздел README «Локальный запуск» |

### C1-hub — пофазные коммиты (merge 0ce083f)
| коммит | фаза |
|---|---|
| c14f5d9 | индекс `idx_lectures_public_published_at` (миграция `003_lectures_hub_index.sql`) |
| b9a4fc9 | `db.LectureDB.ListPublic` (lectures⋈users, visibility='public' ORDER BY published_at DESC) |
| 8b15a4d | доменный `hub.Service` + `Repository` (зеркало `internal/lecture`) |
| 5fe2c37 | страница `/hub` (templ `HubPage`/`HubCard`/`HubEmpty` + handler) |
| 764d586 | монтаж витрины в `cmd/server` (`hubRepo`-адаптер, `GET /hub` для анонимов) |

### C1-upload — разбивка на 7 под-атомов (5 смержено, 2 осталось)
| # | под-атом | что | статус |
|---|---|---|---|
| 1 | upload-wiring | реальный coreclient в main + coreTasksAdapter (замена noopCoreTasks) + db.CreateLecture(+CoreTaskID) | ✅ merge 775b160 |
| 2 | upload-validate | internal/upload/validate.go — DetectMedia/ValidateFileMeta(+MIME)/ValidateYouTubeURL | ✅ merge cbcfb2d (+MIME-фикс из REVIEW) |
| 3 | upload-token | internal/upload/token.go — HMAC stateless токен pending-upload (JSON-payload, media привязан) | ✅ merge 73067b1 (+JSON-фикс) |
| 4 | upload-service | internal/upload/service.go — PrepareFileUpload/ConfirmFileUpload/CreateYouTube; порядок CreateTask→CreateLecture | ✅ merge dc3a818 (+media-binding фикс) |
| 5 | upload-handlers | internal/upload/handlers.go — Mount + presign/confirm/youtube, errors.Is→401/403/422, HX-Redirect | ✅ merge bbe2082 (+extract_slides-инверсия фикс) |
| 6 | **upload-ui** | internal/web/page_upload.templ — форма по прототипу design/prototypes/Загрузка.dc.html (сегмент режима, file/url, PDF, тумблер слайдов, title); JS: presign-fetch→прямой PUT в MinIO→confirm; GET /upload-страница. **ГРАБЛИ детерминизма templ/app.css — make gen-check ОБЯЗАТЕЛЬНО** | ⏳ ОСТАЛОСЬ |
| 7 | **upload-mount** | cmd/server/main.go — адаптер uploadRepo (upload.Repository поверх db.LectureDB), сгенерить uploadSignKey (32б rand, как csrfKey), upload.NewService(core, repo, signer, cfg.PresignedTTL), смонтировать под RequireAuth-группой (рядом с lectureSvc.Mount). **Закрывает BLOCKER из REVIEW handlers: «routes не подключены в main»** | ⏳ ОСТАЛОСЬ |

### Контракты пакета internal/upload (для UI и mount — НЕ менять без нужды)
- `upload.Service`: `NewService(core Core, repo Repository, signer *Signer, uploadTTL time.Duration)`.
  - `Core` интерфейс: `CreateUpload(ctx, filename)(coreclient.UploadResult,error)`, `CreateTask(ctx,coreclient.CreateTaskParams)(string,error)` — `*coreclient.CoreClient` удовлетворяет.
  - `Repository` интерфейс: `CreateLecture(ctx, upload.CreateLectureParams)(lectureID string,error)` — нужен АДАПТЕР поверх db.LectureDB (vmount-под-атом). upload.CreateLectureParams{OwnerID,Title,SourceKind,S3Key,VideoURL,CoreTaskID}.
  - `Signer`: `NewSigner(key []byte)`, `Sign(userID,s3Key,media,ttl)`, `Verify(token,userID,s3Key)(media,err)`.
- `Service.Mount(r chi.Router)` регистрирует POST /upload/presign (JSON {filename,size,mime}→{token,put_url,s3_key,media,title,expires_in}), POST /upload/confirm (form token/s3_key/title/has_pdf/extract_slides→HX-Redirect /lectures), POST /upload/youtube (form url/title/has_pdf/extract_slides→HX-Redirect /lectures). GET /upload-страница НЕ в Mount — её добавит UI-под-атом.
- Ошибки upload: ErrForbidden→403; ErrUnsupportedMedia/ErrEmptyFile/ErrTooLarge/ErrEmptyFilename/ErrMediaMismatch/ErrInvalidURL→422.

### Долги C1-upload (НЕ блокеры, на конец атома/волны)
1. **PDF-слайды НЕ уходят в ядро** (coreclient.CreateTaskParams не принимает slides-файл). HasPDF влияет только на no_slides и UI-гашение тумблера. Проброс PDF — отдельный атом (меняет coreclient). Задокументировано в service.go комментарием.
2. **502/503 мягкий экран** при недоступности ядра на confirm/youtube — сейчас 500 (в handlers TODO-комментарий). §8.
3. **Прямые (не-youtube) URL** — ValidateYouTubeURL ограничен youtube-хостами; прямые медиа-URL долг.
4. **maxUploadBytes=5GiB грубая константа** беты (не из env) — вынести в конфиг — долг.
5. **uploadSignKey рестарт-инвалидация** (как csrfKey): рестарт сервера обнулит незавершённые presign-токены. Приемлемо для беты; стабильный ключ из env — долг.
6. **README** (~строка 521) упоминает noopCoreTasks как активное ограничение — устарел после wiring. ПОПРАВИТЬ на шаге DOCS C1-upload (отдельный docs-субагент по правилу владельца).
7. **GATE C1 e2e** (загрузка→ядро→вебхук→ready→чтение) требует C1-sync + C1-reader — за пределами C1-upload.

### Долги C1-hub (НЕ блокеры, на конец атома/волны)
1. **`/hub` не лендинг `/`** — пока отдельная страница; корень `/` остаётся демо-страницей C0-web.
   Сделать `/hub` лендингом — отдельное решение/атом.
2. **Серверной пагинации нет** — выдача ограничена потолком `limit=200` (`hub.defaultLimit`).
   Курсорная/offset-пагинация — долг при росте каталога.
3. **Ссылка в читалку `/lectures/{id}/read`** (карточка витрины) ждёт C1-reader — до него 404.

## C1-lecture — итог атома (смержен 48f55b3, docs b452a96)
- **data-access `internal/db/lectures.go`**: тип `db.LectureRow`; `LectureDB{Pool}`; методы
  ListByOwner (updated_at DESC), FindByID (nil,nil при отсутствии), Rename, SetVisibility
  (public только ready, published_at=now() при публикации, private НЕ обнуляет published_at),
  Delete, SetCoreTaskProcessing (retry: failed→processing), UpdateStatusConditional
  (анти-гонка §7: WHERE core_task_id=? AND status='processing' — потребитель C1-sync,
  пакет lecture его НЕ зовёт), CreateLecture (тест-хелпер). ВСЕ мутации с owner_id в WHERE.
  Integration-тесты за тегом `integration` (testcontainers, 12 тестов реально прошли ~74с).
- **доменный пакет `internal/lecture`**: `Service` + интерфейсы `Repository` и `CoreTasks`
  (мокабельность). Методы List/Rename/SetVisibility/Delete/Retry. Бизнес-правила:
  публиковать только ready (ErrNotReady), retry только failed+источник (ErrNotFailed/
  ErrNoRetrySource), Delete: сначала ядро (CoreTasks.DeleteTask), при ошибке ядра строку
  НЕ трогаем (§8), потом repo.Delete. **owner-проверка ВО ВСЕХ мутациях ДО обращения к
  ядру** (анти-перебор ID — 2 дыры найдены приёмкой/ревью и закрыты: Delete/Retry коммит
  5051f4f, SetVisibility коммит bb7f4a5). Дефолтные юнит-тесты на моках, БЕЗ Postgres.
- **UI**: `internal/web/page_lectures.templ` (LecturesPage/LectureCard/LectureTitleInline/
  LectureVisibilityToggle/LecturesEmpty, VM `web.LectureCardVM`). Маршрут GET /lectures +
  htmx-мутации rename/visibility/delete/retry под `auth.RequireAuth`. tabular-nums на датах.
- **Долг C0-web ЗАКРЫТ**: CSRF-токен в hx-headers layout (`internal/web/csrf.go`:
  WithCSRFToken/CSRFTokenFromContext; LayoutData.CSRFToken; пустой токен → `{}`).
- **проводка** `cmd/server/main.go`: `lectureRepo` (адаптер Repository поверх db.LectureDB),
  `noopCoreTasks{}` (заглушка — см. долг ниже), монтаж под RequireAuth-группой, csrfInjector.
- 9 коммитов (7 фаз TDD + 2 фикса security). Ворота зелёные, gen-check детерминирован.

## ДОЛГИ C1-lecture (для следующих атомов / конца волны)
1. **РЕАЛЬНЫЙ coreclient (главный долг, для C1-upload/C1-sync):** сейчас в cmd/server стоит
   `noopCoreTasks{}` (DeleteTask→nil; CreateTask→ошибка «обработка временно недоступна»).
   Нужно: инстанцировать `*coreclient.CoreClient` из `cfg.CoreAPIBaseURL` в main; адаптер
   `coreTasksAdapter` (реализует `lecture.CoreTasks` поверх coreclient) — заменит noop БЕЗ
   изменения пакета lecture; реальная retry-логика (source_kind/s3_key/video_url → params).
   Логичнее всего сделать это в C1-upload (он первым реально ходит в ядро).
2. **502/503 мягкий экран** при недоступности ядра (§8) — для удаления/retry/upload.
3. **htmx-error UX**: page_lectures не показывает тост при 401/4xx (RequireAuth под HX-Request
   отдаёт 401). Минимальный тост/обработчик — долг (§11 retry-UX).
4. Из REVIEW (не-блокеры): `isUserError` хрупок (handlers.go ~257) — лучше sentinel
   ErrInvalidTitle + errors.Is; сравнения ошибок через `==` заменить на `errors.Is`
   (handlers.go); `sourceBadgeClass` (page_lectures.templ ~146) всегда muted — различить
   по source_kind; `Service.now` (lecture.go) не используется — удалить/задокументировать.

## РАСПРЕДЕЛЕНИЕ РОЛЕЙ: Codex vs Claude (решение владельца 2026-06-24)
Цель — экономия бюджета Claude (5h/7d). **Codex работает на своём тарифе OpenAI**, его
вызовы НЕ жрут лимиты Claude. Codex доступен (проверено: codex-cli 0.142.0, authenticated,
плагин `codex:`). Вызов — `Agent` с `subagent_type: codex:codex-rescue` ИЛИ скилл `codex:rescue`.
Каждый вызов `Agent` стартует ХОЛОДНЫМ инстансом (свой контекст) → разные вызовы независимы.

**Codex (свой тариф, токеноёмкие шаги):**
- **BUILD** — реализация атома/под-атома по PLAN.md (TDD red→green). Самый дорогой шаг — выносим.
- **REVIEW** — ОТДЕЛЬНЫЙ инстанс Codex (НЕ тот, что кодил!): получает дифф `git diff
  integration..HEAD` + ревью-промпт, независимо оценивает и выдаёт правки. Оркестратор
  передаёт его замечания кодер-инстансу (LOOP) на доработку.
- **LOOP-фиксы** — правки по замечаниям ревью/приёмки (новый узкий инстанс Codex с контекстом дефекта).
- **Диагностика** багов/падений тестов (профиль codex-rescue).

**Claude-сабагенты:**
- **PLAN** (Plan-агент) — архитектура/границы атома, нужен богатый контекст проекта.
- **ACCEPT** (приёмка: ворота + смысл, вердикт COMPLETE|INCOMPLETE) — Sonnet. ОСТАЁТСЯ на
  Claude (решение владельца): две независимые проверки РАЗНЫМИ движками (Codex-ревью +
  Claude-приёмка) надёжнее, чем обе на одном. Приёмщик ≠ кодер ≠ ревьюер.
- **DOCS** (Sonnet) — README/handoff, нужен стиль.

**Оркестратор (Claude, ведущая сессия):** ведёт конвейер PLAN→BUILD→ACCEPT→REVIEW→LOOP→
MERGE→DOCS; САМ фактически гоняет ворота (build/vet/test/gofmt/gate/gen-check) — не верит
отчётам; мержит; передаёт замечания Codex-ревьюера кодер-инстансу. Код руками НЕ пишет.

> NB по Codex: задавай ему ИСЧЕРПЫВАЮЩИЙ контекст в промпте (холодный старт, проекта не
> знает) — путь worktree, PLAN.md, эталоны стиля, правила (комментарии на русском, без
> авторства AI, TDD пофазно, грабли детерминизма templ/app.css/gofmt, ворота). То же, что
> давалось Sonnet-исполнителю, но Codex ещё меньше «в курсе» соглашений — детализируй сильнее.

## ПРОЦЕСС (обкатано на C1-lecture, держать)
- Атом дробить так, чтобы исполнитель НЕ сидел часами (правило владельца): объёмные атомы
  (upload, reader) бить на под-атомы (data-access+домен отдельно от UI+проводки), каждый —
  самодостаточный коммит-набор с зелёными воротами, отдельный исполнитель. C1-lecture одним
  исполнителем = ~26мин/167k токенов/132 tool-use — это ПОТОЛОК, дальше дробить.
- LOOP-фиксы из ревью/приёмки — ТОЖЕ через сабагента-исполнителя (узкий контекст дефекта),
  НЕ руками оркестратора (память feedback-orchestrator-no-coding).
- Перед ACCEPT и перед MERGE оркестратор САМ фактически гоняет ворота в worktree
  (build/vet/test, gofmt -l, make gate, make gen-check) — не верит отчёту сабагента.
- Грабли — см. HANDOFF-NEXT-ORCHESTRATOR раздел «ГРАБЛИ» (LSP worktree ложит, детерминизм
  templ/app.css, gofmt вне vet, артефакты не коммитить, тесты герметичны/integration за тегом).

## ПРАВИЛО ПАУЗЫ (владелец 2026-06-24, уточнено)
Ориентир — **5h-окно тарифа Claude** (`cswap --status`), НЕ 7d. Работать, пока 5h не достигнет
**95%**. (7d может быть 90-96% — это НЕ повод останавливаться; владелец явно велел смотреть 5h.)
При 5h≈95% — закругляй на безопасной границе (после мержа под-атома / до старта нового), допиши
handoff (этот файл + NEXT-ORCHESTRATOR), продолжишь в чистом чате. Не начинать под-атом, если
ясно, что не закроешь до лимита. ЭТА ПАУЗА (2026-06-24): сделана на 5h≈72% по решению владельца
(не по лимиту) — он остановил работу вручную; граница безопасна (5/7 смержено, дерево чисто).

## CODEX — настройка и грабли (важно для нового оркестратора)
- **Дефолт-модель Codex: gpt-5.5, reasoning effort MEDIUM** (`-m gpt-5.5 -c model_reasoning_effort="medium"`).
  Владелец велел использовать по умолчанию (память feedback-codex-model-default).
- **Сеть в песочнице Codex ВКЛЮЧЕНА** (правка `~/.codex/config.toml` 2026-06-24): добавлены
  `[search] enabled=true` (веб-поиск) и `[sandbox_workspace_write] network_access=true`. Это
  устранило граблю: раньше песочница Codex не давала сокетов → httptest падал «socket: operation
  not permitted» → Codex лез ЧИНИТЬ чужие тесты (auth/coreclient), выходя за scope. ТЕПЕРЬ сеть
  есть, но ВСЁ РАВНО: в промпте Codex-BUILD ЯВНО пиши «трогай ТОЛЬКО пакет X, чужие файлы не
  редактируй; если go test падает вне твоего пакета на socket/read-only — это окружение, не
  дефект». После приёмки ВСЕГДА сверяй `git diff --stat integration..HEAD` — не вышел ли за scope.
  (Память feedback-codex-sandbox-httptest.)
- **Codex иногда НЕ коммитит результат** (оставляет в рабочем дереве) ИЛИ обрыв сети (403/
  ConnectionRefused) теряет незакоммиченное. В промпте требуй «коммить ПОСЛЕ КАЖДОЙ ФАЗЫ, часто».
  Если фикс не закоммичен — оркестратор коммитит САМ пофазно (механика git, не новый сабагент).
  За сессию было 2-3 обрыва сети у Codex-агентов — перезапуск с тем же заданием решал.
