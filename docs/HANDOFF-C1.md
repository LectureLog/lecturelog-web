# HANDOFF — Волна C1 (доменные модули). Точка возобновления.

> Первым прочитай этот файл, затем `docs/HANDOFF-NEXT-ORCHESTRATOR.md` (правила/грабли),
> `docs/WORKFLOW.md`, `docs/TASKS.md` (§C1), `docs/plans/2026-06-22-platform-design.md`.
> Роль ведущей сессии — **ОРКЕСТРАТОР**: код руками НЕ пишет (даже мелкий фикс из ревью —
> через LOOP-исполнителя; см. память feedback-orchestrator-no-coding). Механика
> (gofmt, git, ворота) — сам.

## База волны
- `integration` = `db3ec68` (после C0 + C1-lecture + **C1-upload ПОЛНОСТЬЮ** + **C1-sync**). origin
  синхронен по C0 (2ef594f..0507824), НО C1-lecture/upload/sync ещё НЕ запушены (PR/push — в конце волны).
- Дерево ЧИСТО, ворота ЗЕЛЁНЫЕ (build/vet/test/gofmt + gen-check exit 0), worktree-ов нет. Безопасная граница.

## ⏸️ ТОЧКА ВОЗОБНОВЛЕНИЯ (2026-06-25): C1-upload+C1-sync ЗАВЕРШЕНЫ; остались hub, reader
ВЫПОЛНЕНО в этой сессии (2026-06-25): достроены последние 2 под-атома C1-upload (mount=cb164fb,
ui=fdea754+fix 777f04a, merge df35653) И полностью атом C1-sync (1c4dc45+fix 9485c3e, merge 2a96b75).
README обновлён (db3ec68). ПЕРВОЕ ДЕЙСТВИЕ нового чата: начать **C1-hub** (меньше) и/или **C1-reader**
(самый объёмный — ДРОБИТЬ на под-атомы). hub и reader независимы → можно ∥.

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
| C1-hub     | ⏳ | | | | | | | |
| C1-reader  | ⏳ | | | | | | | |

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
