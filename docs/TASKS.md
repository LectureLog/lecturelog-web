# Задачи платформы LectureLog Web (Волны B, C0, C1)

Контекст для агентов. **КАК** работать — `docs/WORKFLOW.md` (роли, гейты, петля).
**ПОЧЕМУ** так — `docs/plans/2026-06-22-platform-design.md` (ссылки на §N ниже).
Стек: Go, SSR-монолит — chi + templ + htmx + Tailwind. Модули — vertical slices
(§9). Дизайн-система — «Читальный зал» (дизайн-пакет `design/`).

Контракт ядра, который потребляем: `lecturelog-core/docs/openapi.json` +
`api-contract.md`. **Волна B/C начинается только после GATE A** (контракт ядра
стабилен, см. `lecturelog-core/docs/TASKS-core.md`).

---

## ВОЛНА B — мост к ядру (1 задача, последовательно)

### B1 — coreclient из openapi
- Сгенерировать типизированный Go-клиент из `lecturelog-core/docs/openapi.json`
  (`oapi-codegen`). Встроить генерацию в сборку (`go generate` / Makefile).
- Обёртка `coreclient/`: HMAC-подпись исходящих (§2, общий секрет с ядром),
  конфиг endpoints ядра из `.env`.
- Методы под наши сценарии: создать задачу (`POST /tasks` с s3_key/video_url),
  presigned-PUT (`POST /uploads`), статус (`GET /tasks/{id}`), удалить
  (`DELETE /tasks/{id}` — из A1).
- **GATE B:** клиент компилируется (= контракт валиден) + контрактный smoke к
  замоканному/живому ядру. Это автоловец рассинхрона — раньше написания модулей.

---

## ВОЛНА C0 — фундамент (последовательно, блокеры всего)

### C0-db — схема и миграции (§3)
- Таблицы: `users` (email UNIQUE NOT NULL), `identities` (provider,
  provider_sub, user_id; email НЕ хранить — см. политику email=личность §3),
  `sessions` (session_id, user_id, expires_at), `lectures` (полная схема из §3).
- Индекс на `lectures.core_task_id` (матч вебхука, §7).
- pgx + миграционный инструмент.

### C0-config — .env-контур (§9)
- OAuth-секреты, HMAC-ключ (общий с ядром), endpoints/креды MinIO ядра,
  PLATFORM_CALLBACK_URL, параметры сессии/TTL presigned (24ч).

### C0-web — каркас «Читальный зал» (§1, дизайн-пакет)
- templ + Tailwind, токены из `design/tokens.css`, общий layout (sticky-шапка,
  тема светлая/тёмная), htmx-хелперы, статика. По `design/STYLE_GUIDE.md`.

### C0-auth — вход и доступ (§4) ← БЛОКЕР защищённых роутов
- Google OAuth-флоу (**state безусловно**, §4).
- Сессии в Postgres (кука httponly/secure/**SameSite=Lax**).
- Логика входа по email (§3: матч по `users.email`, только при
  `email_verified=true`; разный email = новый юзер).
- **CSRF-токен** на мутирующих формах (synchronizer, htmx-заголовок, middleware).
- Middleware прав: пускает анонимов на public-роуты, требует сессию на остальном
  (§4 матрица).

**GATE C0:** миграции применяются; layout рендерится в обеих темах; вход через
Google работает; middleware пускает аноним на public и режет на приватном;
CSRF/state срабатывают.

---

## ВОЛНА C1 — доменные модули (5 задач ∥, vertical slices)

### C1-upload — загрузка (§5)
- Два режима: файл (presigned-PUT в MinIO ядра) / YouTube-ссылка (video_url).
- Файл: запомнить выданный s3_key (привязка к юзеру) → confirm со **сверкой**
  s3_key → создать лекцию (после заливки!) + задачу в ядре. Несовпадение → 403.
- Ссылка: сразу лекция + задача с video_url.
- Форма: переключатель режима, опц. PDF (оба режима), тумблер «извлекать слайды»
  (дефолт ВКЛ, гасится при PDF), title предзаполнен+редактируем.
- Карточка рождается ТОЛЬКО после успешной заливки.

### C1-lecture — модель и ЛК (§3, §8)
- ЛК «Мои лекции», карточки со статусом, переименование (только title, inline).
- Удаление (hard): `DELETE /tasks/{id}` ядру → потом своя строка (§8 порядок).
- Retry на failed (из s3_key в окне 7д / video_url) — UX по §11.4 минимально.
- Переключение visibility private/public (публиковать только ready).

### C1-hub — витрина (§8, §4)
- `lectures WHERE visibility=public ORDER BY published_at DESC`, автор (имя+аватар).
- Открыт анонимам (воронка, §4). Поиск/список по дизайну.

### C1-reader — читалка (§6) «Читальный зал»
- Грузит `structure.json` ядра (из A3) → рендер дерева; content_md→html (goldmark).
- presigned-пачка на 24ч при открытии (свежая каждый раз, после проверки прав).
- Видео `preload="metadata"` (превью браузером). Export ZIP — presigned ~60с.
- Сайдбар-оглавление (scroll-spy), плееры, слайды+лайтбокс, callout, поиск, тема.

### C1-sync — синхронизация с ядром (§7)
- Приём вебхука (HMAC-проверка), матч лекции по core_task_id.
- Запись статуса **conditional update «только из processing»** (§7, анти-гонка).
- Поллинг-прокси `GET /tasks/{id}` для htmx-карточки, **10с**, только при
  открытой вкладке; fallback-дописывание статуса при потерянном вебхуке.
- Хук под email-уведомление в обработчике терминального статуса (MVP не шлёт).

**GATE C1:** сквозной e2e — загрузка → задача в ядре → вебхук → ready → чтение
конспекта → публикация → виден в хабе анониму → удаление убирает отовсюду.

---

## Ворота платформы (слой 1 приёмки)
```
go build ./... && go vet ./... && go test ./...
```
(+ `templ generate` и сборка Tailwind перед build; `oapi-codegen` для B1.)

## НЕ делать сейчас (отложено — §11 дизайн-документа)
Каталог error_code, email-отправка, retry-UX сверх минимума, импорт MD/TeX, теги,
счётчики, soft-delete, слияние аккаунтов, биллинг. Это будущие волны.
