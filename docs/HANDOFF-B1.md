# HANDOFF — Волна B / задача B1 (coreclient). Пауза до сброса лимитов.

> Этот файл — точка возобновления в ЧИСТОМ чате. Прочитай его первым.
> Роль ведущей сессии — **ОРКЕСТРАТОР** (см. `docs/WORKFLOW.md`): код руками НЕ
> пишет, запускает агентов, читает вердикты, ведёт ветки. Не сваливайся в роль
> исполнителя/архитектора.

## Где мы в state-машине атома B1

Атом из 8 шагов (`docs/WORKFLOW.md` → «Шаблон одной задачи»):

```
1. PLAN     ✅ сделано  → PLAN.md в worktree (в git exclude, НЕ коммитить)
2. ISOLATE  ✅ сделано  → worktree .worktrees/B1-coreclient, ветка node/B1-coreclient от integration
3. BUILD    ✅ сделано  → код+тесты, ворота ЗЕЛЁНЫЕ (проверено оркестратором фактически)
4. ACCEPT   ⏳ СЛЕДУЮЩИЙ ШАГ → приёмка (general-purpose, НЕ исполнитель): ворота + смысл по плану
5. REVIEW   ⬜ не начато → superpowers:code-reviewer: качество/стандарты/контракт
6. LOOP     ⬜ при замечаниях → возврат исполнителю, тот же worktree, лимит 3 круга
7. MERGE    ⬜ зелено → авто-мерж в integration. ВЛАДЕЛЕЦ ПРОЦЕССА выбрал автономию
            «до MERGE»: ОСТАНОВИТЬСЯ перед авто-мержем и спросить человека.
8. DOCS     ⬜ отдельным субагентом (правило пользователя)
```

## Автономия (решение владельца процесса)
Веду атом автономно до MERGE; **стоп перед авто-мержем в integration** для решения
человека. Ранний стоп — только если LOOP застрял на 3 кругах.

## Ключевые РЕШЕНИЯ брейншторма (НЕ переоткрывать, заложены в код)

1. **HMAC — расхождение доков с фактическим контрактом ядра, РАЗРЕШЕНО:**
   - TASKS.md B1 и дизайн-документ §2 (строка ~305) пишут «HMAC-подпись ИСХОДЯЩИХ
     запросов к ядру» — это **НЕВЕРНО**.
   - Проверено по коду ядра (`lecturelog-core/lecturelog/api/routes.py`): ядро
     **НЕ проверяет инбаунд-подпись** на POST /tasks, POST /uploads, DELETE. HMAC
     живёт ТОЛЬКО на исходящем вебхуке ядра (`infrastructure/webhook/http_notifier.py`):
     заголовок `X-Webhook-Signature` = HMAC-SHA256(hex) от БАЙТОВ тела с ключом
     `LECTURELOG_WEBHOOK_SECRET`.
   - **В B1:** coreclient НЕ подписывает исходящие. HMAC = только ВЕРИФИКАТОР
     входящего вебхука (`VerifyWebhookSignature`, constant-time `hmac.Equal`) + тип
     тела `WebhookPayload{task_id,status,error,error_code}`.
   - **Долг (для DOCS-шага):** актуализировать формулировки в `docs/TASKS.md` и
     `docs/plans/2026-06-22-platform-design.md` (§2, строка ~305), чтобы следующая
     волна не наступила на это снова.

2. **Граница B1:** только верификатор-функция + тип тела вебхука. HTTP-handler
   приёма вебхука и матч по `core_task_id` → задача **C1-sync** (вне scope B1).

3. **Smoke GATE B:** httptest.Server-мок ядра по схемам openapi (герметично, в CI).
   Live-тест против настоящего ядра в B1 НЕ требуется.

4. **Toolchain:** oapi-codegen через tool-директиву `go.mod` (Go 1.25, без tools.go),
   режим types+client (ClientWithResponses), спека VENDORED в
   `internal/coreclient/openapi.json`.

5. **Факт по контракту:** POST /api/v1/tasks = `multipart/form-data`
   (поля audio/video/s3_key/video_url/media/slides/no_slides); POST /api/v1/uploads
   = `application/json`. Имя Go-модуля: `github.com/LectureLog/lecturelog-web`.

## Что построил исполнитель (BUILD), подтверждено фактическим прогоном

Файлы в worktree (закоммичены в node/B1-coreclient):
- Сгенерировано: `internal/coreclient/gen.go` (types + ClientWithResponses)
- Руками: `go.mod`, `go.sum`, `Makefile`, `.gitignore`,
  `internal/coreclient/{openapi.json, oapi-codegen.yaml, generate.go, config.go,
  client.go, client_test.go, webhook.go, webhook_test.go}`,
  `scripts/normalize_openapi.py`
- Промежуточный (в .gitignore, НЕ коммитится): `internal/coreclient/openapi.normalized.json`

Доменные методы обёртки `CoreClient`: `CreateUpload` (JSON), `CreateTask`
(multipart, s3_key и video_url), `GetTaskStatus` (`ErrTaskNotFound` на 404),
`DeleteTask` (204→nil, идемпотентно). Опция `WithHTTPDoer`.

Отклонения от плана (легитимные):
- **Нормализация спеки 3.1.0 ПОНАДОБИЛАСЬ:** oapi-codegen v2.7.1 падает на
  `anyOf[type,null]`. Скрипт `scripts/normalize_openapi.py` схлопывает 3.1-nullable
  → 3.0-nullable и понижает версию до 3.0.3. Встроен в `go generate` двумя
  директивами (нормализация → oapi-codegen), чтобы `go generate ./...` был
  самодостаточен.
- Переименования из-за коллизий со сгенерированными именами: обёртка `CoreClient`
  (а не Client), опция `WithHTTPDoer` (а не WithHTTPClient).

### Доказательство зелёных ворот (прогон оркестратора в worktree)
```
cd .worktrees/B1-coreclient
go generate ./...  → exit 0, git status чист (детерминизм генерации)
go build ./...     → exit 0
go vet ./...       → exit 0
go test ./...      → ok internal/coreclient (16 тестов PASS)
```

### Ложная диагностика (НЕ баг)
IDE-диагностика показала ошибки `Client redeclared`, `undefined:
VerifyWebhookSignature` и т.п. Это артефакт ОСНОВНОГО рабочего каталога: файлов
B1 там нет (они только в worktree), LSP видел ручные файлы без `gen.go`. Реальная
сборка в worktree зелёная. Игнорировать.

## КАК ПРОДОЛЖИТЬ (для нового чата)

1. Перечитай этот файл, `docs/WORKFLOW.md`, `PLAN.md` (в worktree).
2. **Шаг 4 ACCEPT:** запусти приёмщика (general-purpose, ОБЯЗАТЕЛЬНО не тот же,
   что строил) в worktree `.worktrees/B1-coreclient`. Двухслойно:
   - слой 1 (ворота, бинарно): `go generate ./... && go build ./... && go vet ./...
     && go test ./...`
   - слой 2 (смысл по PLAN.md + инварианты дизайна, особенно HMAC-решение выше).
   Вердикт: COMPLETE | INCOMPLETE + список пробелов.
3. **Шаг 5 REVIEW:** `superpowers:code-reviewer` — качество, баги, стандарты
   (CLAUDE.md: русские комменты, без авторства Claude), соответствие контракту.
4. **Шаг 6 LOOP:** замечания → исполнителю в тот же worktree (можно продолжить
   агента исполнителя по его agentId, если сессия та же; в новом чате — новый
   исполнитель с этим контекстом). Лимит 3 круга на проверку.
5. **Шаг 7 MERGE:** при COMPLETE+APPROVE — **остановись и спроси человека** перед
   авто-мержем node/B1-coreclient → integration. Перед мержем: добавить
   `.worktrees/` в `.git/info/exclude` основного дерева (сейчас оно untracked).
6. **Шаг 8 DOCS:** отдельным субагентом — обновить README/доки И закрыть долг п.1
   (формулировки HMAC в TASKS.md и дизайн-документе §2).

## Артефакты и пути
- Worktree: `/home/krivonosov/projects/lecturelog-web/.worktrees/B1-coreclient`
- Ветка задачи: `node/B1-coreclient` (от `integration`)
- План: `<worktree>/PLAN.md` (в git exclude — НЕ коммитить)
- Контракт ядра (read-only): `/home/krivonosov/projects/lecturelog-core/docs/{openapi.json, api-contract.md}`
- agentId исполнителя (для продолжения в ТОЙ ЖЕ сессии): `adb7fd2e9400f3402`
  (в новом чате недоступен — стартуй нового исполнителя с контекстом отсюда).

## Git-состояние на момент паузы
- `integration`: коммит `0f27a97 docs: планы и задачи волн B/C0/C1` (+ этот HANDOFF
  ещё не закоммичен на момент записи).
- `node/B1-coreclient`: коммиты исполнителя по фазам B1 (код+тесты).
- Основное дерево: чисто, кроме untracked `.worktrees/` и нового `docs/HANDOFF-B1.md`.
