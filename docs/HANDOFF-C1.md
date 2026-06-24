# HANDOFF — Волна C1 (доменные модули). Точка возобновления.

> Первым прочитай этот файл, затем `docs/HANDOFF-NEXT-ORCHESTRATOR.md` (правила/грабли),
> `docs/WORKFLOW.md`, `docs/TASKS.md` (§C1), `docs/plans/2026-06-22-platform-design.md`.
> Роль ведущей сессии — **ОРКЕСТРАТОР**: код руками НЕ пишет (даже мелкий фикс из ревью —
> через LOOP-исполнителя; см. память feedback-orchestrator-no-coding). Механика
> (gofmt, git, ворота) — сам.

## База волны
- `integration` = `b452a96` (после C0 + атом C1-lecture + его docs). origin синхронен по C0
  (2ef594f..0507824), НО C1-lecture ещё НЕ запушен (PR/push — в конце волны C1 по политике).

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
| C1-upload  | ⏳ | | | | | | | |
| C1-sync    | ⏳ | | | | | | | |
| C1-hub     | ⏳ | | | | | | | |
| C1-reader  | ⏳ | | | | | | | |

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
Когда **5h-окно тарифа достигает 95%** (`cswap --status`) — закругляй разработку на безопасной
границе (после мержа атома / до старта нового), допиши handoff (этот файл + NEXT-ORCHESTRATOR),
чтобы продолжить в новом чистом чате. Не начинать атом, если ясно, что не закроешь до лимита.
