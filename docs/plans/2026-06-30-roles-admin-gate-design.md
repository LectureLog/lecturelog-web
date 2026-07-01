# Модель ролей / админ-гейт — дизайн-план

> Статус: дизайн согласован с пользователем через грилл-сессию 2026-06-30.
> Это ДИЗАЙН, не implementation-plan. План реализации (TDD task-by-task) пишется отдельно
> на основе этого документа.

## Зачем эта задача

Параллельно делается фича «YouTube cookies UI» (`docs/plans/2026-06-30-youtube-cookies-ui.md`):
загрузка `cookies.txt` через веб с проксированием в core. Cookies в ядре — **глобальный
singleton** (одна строка на всё ядро, не per-user). Страницу `/settings` (cookies, а в
будущем — API-ключи ядра) должен видеть и менять **только администратор**.

В web сейчас **нет модели ролей**: `auth.User` (`internal/auth/auth.go:33`) содержит только
`ID, Email, Name, AvatarURL`. Поэтому роли выделены в отдельную задачу — эту.

**Масштаб (подтверждён пользователем):** сценарий — 1 администратор, но конфиг сразу
делается списком email для break-glass/переезда владельца. Все email из списка равноправны.
Цель — через веб задавать глобальные настройки ядра (cookies, API-ключи). Полноценный RBAC
НЕ планируется → YAGNI: никакой таблицы ролей, никакой колонки в БД.

---

## Принятые решения (итог грилла)

| Ветка | Решение |
|------|---------|
| A. Механизм гейта | `ADMIN_EMAILS` (allowlist через запятую) в **опциональном** блоке `config.go`. IsAdmin вычисляется на каждый запрос: канонический platform email ∈ allowlist. Пустой список = fail-closed + warning в лог при старте. |
| A0. Email-identity | Админство НЕ завязано на Google/provider/sub/domain. Только `auth.User.Email`, нормализация `trim + lower`, без Gmail-specific правил. Будущие провайдеры должны давать verified email. |
| A1. Миграция email | Добавить миграцию канонизации существующих `users.email`. Перед `UPDATE` проверить пустые/дубли после `lower(trim(email))`; при конфликте миграция падает с понятной ошибкой. Ролей в БД всё ещё нет. |
| B. RequireAdmin | Серверный middleware на группе `/settings*`. Аноним на боевом mount идёт через `RequireAuth`; залогиненный не-админ: обычный запрос → 302 `/lectures`; htmx → `HX-Redirect: /lectures` (200). Не дыра — настоящий серверный гейт. |
| C. Проброс в шаблоны | Хелпер `web.NewLayoutData(ctx, title)` тянет CSRFToken + IsAdmin + SettingsAvailable из контекста (убивает footgun). Вход в админ-раздел — иконка-шестерёнка в шапке, видна только админу и только когда реальные `/settings*` routes смонтированы. |
| D. cookies_invalid | Карточки лекций НЕ ветвим по роли. Единый нейтральный текст для всех: **«Cookies YouTube устарели — обратитесь к администратору»**. Сигнатура `mapErrorCode` НЕ меняется. |
| E. Проактивный сигнал | Out of scope (MVP). Возраст cookies виден через `updated_at` на `/settings` (это даёт план cookies-UI). Активный пуш/баннер — не делаем. |
| F. Граница задач | План ролей владеет механизмом + строкой `cookies_invalid` + финальным монтированием `/settings*` под RequireAuth+RequireAdmin. RequireAuth-only `/settings` допустим только локально в рабочей ветке, не в `dev`. |
| G. Settings security | Весь `/settings*` под admin gate, без CSRF-exempt. Mutating settings actions логируют `user.ID`, email, action и результат без содержимого секретов. |

---

## A. Механизм админ-гейта: ADMIN_EMAILS

**Конфиг (`internal/config/config.go`):**
- Новое поле `Config.AdminEmails []string`.
- Читается в **опциональном** блоке `Load` (паттерн `CORE_MINIO_USE_SSL`, НЕ в `required`-срезе).
  Пустой `ADMIN_EMAILS` = админов нет = **fail-closed** (сервер стартует, `/settings` закрыт
  для всех). Сервер НЕ должен падать при отсутствии ключа.
- При старте, если `len(cfg.AdminEmails)==0`, `cmd/server` пишет warning в лог: админов нет,
  `/settings` будет недоступен всем.
- Парсинг: `strings.Split(raw, ",")`, для каждого элемента `strings.TrimSpace` +
  `strings.ToLower`, пустые отбрасываются. Результат — нормализованный срез.
- Все email из allowlist равноправны. Нет owner/superadmin.

**Хелпер сравнения:** канонический email пользователя сравнивается с allowlist
**case-insensitive + trimmed** (`strings.EqualFold` после trim, либо сравнение
нормализованных значений). Это вынести в `auth` (см. ниже).

**Источник identity:** только платформенный `auth.User.Email`. Не использовать
provider-specific данные (`provider`, `provider_sub`, Google domain/hosted domain и т.п.).
Нормализация provider-agnostic: `strings.TrimSpace` + `strings.ToLower`. Gmail-специфичные
правила (`+alias`, удаление точек) НЕ применять.

**Тесты** (`internal/config/load_test.go` — образец):
- `ADMIN_EMAILS` отсутствует → `AdminEmails` пуст, ошибки нет.
- `ADMIN_EMAILS="a@x.com, B@Y.com"` → `["a@x.com","b@y.com"]` (trim + lower).
- Пустые элементы (`"a@x.com,,"`) отбрасываются.

---

## A0. Канонический email пользователя

Админ-гейт опирается на `auth.User.Email`, поэтому сам email должен быть каноническим на
границе входа в систему.

**`auth.resolveUser`:**
- После проверки `EmailVerified=true` нормализует `p.Email` через `strings.TrimSpace` +
  `strings.ToLower`.
- Если после trim email пустой → возвращает отдельную ошибку auth до обращения к repository;
  пользователя и сессию не создаём.
- `FindUserByEmail` и `CreateUser` получают уже канонический email. Для `CreateUser` можно
  заменить `p.Email` в копии `Profile`, чтобы адаптер БД не знал о нормализации.
- Это требование не завязано на Google. Будущие провайдеры должны поставлять verified email
  перед созданием/поиском пользователя.

**Миграция БД:**
- Добавить новую миграцию после `003_lectures_hub_index.sql`.
- Перед обновлением проверить:
  - нет строк, где `lower(trim(email)) = ''`;
  - нет групп `lower(trim(email))`, которые дают больше одного пользователя.
- Если проблема найдена, миграция должна упасть с понятным `RAISE EXCEPTION`, а не молча
  склеить пользователей.
- Затем выполнить `UPDATE users SET email = lower(trim(email)) WHERE email <> lower(trim(email));`.

**Тесты:**
- `resolveUser` ищет/создаёт по lowercase+trim email.
- пустой после trim email → ошибка, `FindUserByEmail/CreateUser` не вызываются.
- миграция на нормальных данных сохраняет/нормализует email; конфликт дублей покрыть
  SQL/integration-тестом, если существующая миграционная тест-инфраструктура позволяет без
  чрезмерного усложнения.

---

## A′ + C. IsAdmin в контексте — ГЛОБАЛЬНЫЙ middleware (критично)

**Почему глобальный, а не внутри RequireAdmin:** шестерёнка видна на ВСЕХ страницах
(`/lectures`, `/upload`, `/hub`, `/read/{id}`), а RequireAdmin исполняется только на
`/settings`. Если IsAdmin кладётся внутри RequireAdmin — на остальных страницах
`NewLayoutData` увидит `IsAdmin=false` и шестерёнка молча не отрисуется нигде, кроме
настроек. Это тот же footgun, что мы убрали для LayoutData. Поэтому:

**Новый глобальный middleware `auth.Service.LoadAdmin`** (или расширение текущей цепочки):
- Монтируется в `WithGlobalMiddleware` **сразу после `LoadSession`** (нужен уже загруженный
  `*User`).
- Читает `UserFromContext`. Если user != nil и `user.Email ∈ allowlist` → кладёт `IsAdmin=true`
  в контекст. Иначе — IsAdmin отсутствует/false.
- Allowlist инъектируется в `auth.Service` при создании из `cfg.AdminEmails`. Технически
  предпочтителен option (`auth.WithAdminEmails(...)`), чтобы не размножать позиционный `nil`
  по тестам. Сам `auth.Service` владеет проверкой.

**Хелперы в `internal/auth` (по образцу `UserFromContext`, `auth.go:127`):**
- `IsAdminFromContext(ctx) bool` — читает флаг из контекста.
- Внутренний `isAdminEmail(email) bool` на `Service` — нормализованная проверка по allowlist.
- Новый `contextKey ctxKeyIsAdmin`.

**Тесты:** middleware кладёт IsAdmin=true для email из allowlist; false для не-allowlist;
false для анонима (user=nil); сравнение нечувствительно к регистру/пробелам.

---

## B. RequireAdmin middleware

**Новый `auth.Service.RequireAdmin`** (по образцу `RequireAuth`, `middleware.go:46`):
- Читает `IsAdminFromContext`. Если true → `next`.
- Если флага нет/false, дополнительно берёт `UserFromContext` и сверяет `user.Email` с
  allowlist сервиса. Это делает security gate независимым от случайно забытого `LoadAdmin`.
  `LoadAdmin` всё равно нужен для шестерёнки в layout.
- Иначе (аутентифицирован, но не админ; или direct-вызов middleware анонимом):
  - htmx (`HX-Request: true`) → заголовок `HX-Redirect: /lectures`, статус 200.
  - обычный запрос → `http.Redirect(w, r, "/lectures", http.StatusFound)`.
- **НЕ редиректить на `/auth/login`** — не-админ уже аутентифицирован, это дало бы петлю.

**Монтирование (`cmd/server/main.go`):** группа `/settings*` оборачивается
`RequireAuth` → затем `RequireAdmin` (порядок важен: сначала «залогинен», потом «админ»).
Это **единственное** место монтирования `/settings`.

**Фактическое поведение на боевом mount:**
- аноним, обычный браузер → `RequireAuth` отдаёт 302 `/auth/login`;
- аноним, htmx → `RequireAuth` отдаёт 401 (существующее поведение);
- залогиненный не-админ → `RequireAdmin` отдаёт 302 `/lectures`;
- залогиненный не-админ, htmx → `RequireAdmin` отдаёт 200 + `HX-Redirect: /lectures`;
- админ → проходит.

**Тест-матрица (repo TDD-heavy, тесты назвать в impl-плане):**
- админ → проходит (next вызван).
- не-админ, обычный браузер → 302 `Location: /lectures`.
- не-админ, htmx → 200 + `HX-Redirect: /lectures`.
- direct-вызов `RequireAdmin` анонимом → fail-closed: не вызывает next, редиректит как
  не-админа.
- композиционный тест в `internal/auth/middleware_test.go` на минимальном `chi.Router`:
  `LoadSession -> LoadAdmin -> RequireAuth -> RequireAdmin -> GET /settings`, чтобы поймать
  порядок middleware без тяжёлого `cmd/server` с БД/OAuth/core.

---

## C. NewLayoutData + шестерёнка в шапке

**Хелпер `web.NewLayoutData(ctx context.Context, title string) LayoutData`:**
- Внутри: `CSRFToken: web.CSRFTokenFromContext(ctx)`, `IsAdmin: auth.IsAdminFromContext(ctx)`,
  `SettingsAvailable: web.SettingsAvailableFromContext(ctx)`, `Title: title`.
- ⚠️ Зависимость направления импорта: `web` импортирует `auth` для `IsAdminFromContext`.
  Проверить, что нет цикла `auth → web`. Если цикл есть — IsAdmin прокидывать через
  отдельный геттер в `web` (свой `web.IsAdminFromContext`, который middleware из auth
  наполняет) ИЛИ хелпер положить в пакет, который импортирует оба. Решить на этапе
  реализации; цикла быть не должно (auth сейчас не импортирует web).

**`LayoutData` (`internal/web/layout.templ:6`):**
- добавить поле `IsAdmin bool`;
- добавить поле `SettingsAvailable bool`, чтобы roles-ветка без cookies-UI не показывала
  админу битую ссылку на несмонтированный `/settings`.

**Settings-флаг (`internal/web/settings.go`):**
- `web.WithSettingsAvailable(ctx)` кладёт в контекст признак, что реальные settings routes
  смонтированы в этом сервере;
- `web.SettingsAvailableFromContext(ctx) bool` возвращает false по умолчанию.
- Когда cookies-UI добавит `internal/settings` и защищённый mount `/settings*`, `cmd/server`
  должен выставлять этот флаг глобальным middleware для всех страниц, чтобы шестерёнка была
  видна админу на `/hub`, `/read/{id}`, `/lectures`, `/upload`.

**`header` (`layout.templ:61`):** в `ll-top-actions`, ПЕРЕД `@themeToggle()`, добавить
`if data.IsAdmin && data.SettingsAvailable { @adminGearLink() }` — иконка-шестерёнка
`<a href="/settings">` в стиле `ll-icon-btn` (как themeToggle). Иконку шестерёнки добавить
рядом с `iconMoon`/`iconSun`. Ссылка должна иметь `aria-label="Настройки"` и
`title="Настройки"`.

**Все 5 call-site Layout переводятся на `NewLayoutData(ctx, title)`** (grep подтверждён):
1. `internal/reader/handlers.go:47` (`/read/{id}`, доступен анонимам — IsAdmin корректно false).
2. `internal/lecture/handlers.go:54` (`/lectures`).
3. `internal/hub/handlers.go:34` (`/hub`, **анонимный** — user может быть nil, IsAdmin=false;
   проверить, что хелпер это терпит).
4. `cmd/server/main.go:287` (`/upload`).
5. `internal/web/page_settings.templ` (страница настроек — создаётся планом cookies-UI;
   если к моменту реализации ролей её ещё нет, перевод на NewLayoutData делает план cookies-UI
   при создании страницы — см. F).

Статический `page_demo.templ:6` не трогаем (нет хендлера/контекста).

**Замечание по публичным страницам:** `/hub` и `/read/{id}` доступны анонимам, но если запрос
пришёл с валидной сессией админа и settings routes включены, шестерёнка должна показываться и
там. Для гостя, не-админа или сервера без settings routes ссылка скрыта.

**Тесты:** рендер Layout с `IsAdmin=true && SettingsAvailable=true` содержит ссылку на
`/settings`; с `IsAdmin=false` или `SettingsAvailable=false` — нет.

---

## D. cookies_invalid — единый текст, без ветвления по роли

Решение пользователя: **карточки лекций не трогать ветвлением по роли.**

- В оба `mapErrorCode` (`internal/lecture/handlers.go:239` и
  `internal/syncsvc/handlers.go:193`) добавить кейс:
  ```go
  case "cookies_invalid":
      return "Cookies YouTube устарели — обратитесь к администратору"
  ```
- **Сигнатура `mapErrorCode(code)` НЕ меняется.** IsAdmin в `lecture`/`syncsvc` хендлеры
  пробрасывать НЕ нужно. Ветка D — минимальная.
- Дубль двух `mapErrorCode` НЕ устраняем (они и так не идентичны: lecture=3 кейса,
  syncsvc=6; расхождение, вероятно, осознанное — syncsvc видит коды поллинга
  `rate_limit`/`bad_input`/`internal`). Унификация — отдельный рефактор-PR при желании,
  вне этой задачи.

**Тесты:** `mapErrorCode("cookies_invalid")` в обоих пакетах → ожидаемая строка.

**Это закрывает пункт #5 плана cookies-UI** (показ `cookies_invalid`): текст пишется здесь,
план cookies-UI switch не трогает.

---

## E. Известное ограничение (зафиксировано)

Нет проактивного сигнала «cookies протухли». Админ узнаёт только увидев свою упавшую
YouTube-лекцию с текстом из D. На `/settings` виден возраст cookies (`updated_at` — даёт
план cookies-UI). Активный баннер/пуш — **out of scope MVP**. Возможное развитие позже:
баннер для админа при свежих `cookies_invalid` по всем лекциям (нужен новый метод
репозитория) — отдельной задачей.

Существующая login-сессия НЕ инвалидируется при изменении `ADMIN_EMAILS`. Это нормально:
конфиг читается на старте, после рестарта `LoadAdmin/RequireAdmin` пересчитают доступ по
новому allowlist. Пользователь останется залогинен, но админский доступ пропадёт.

---

## F. Граница задач (роли ↔ cookies-UI) — во избежание коллизий

**План ролей (этот) ВЛАДЕЕТ:**
- `ADMIN_EMAILS` в config + парсинг.
- Канонизация email в `auth.resolveUser`.
- Миграция существующих `users.email` к `lower(trim(email))` с fail-fast на пустые/дубли.
- `auth`: allowlist в Service, `LoadAdmin` middleware, `RequireAdmin`, `IsAdminFromContext`.
- Глобальное монтирование `LoadAdmin` после `LoadSession` в `main.go`.
- **Монтирование `/settings*` под `RequireAuth` → `RequireAdmin`** (единственная точка).
- `web.NewLayoutData`, поле `LayoutData.IsAdmin`, шестерёнка в `header`, перевод 4 текущих
  call-site (reader/lecture/hub/upload) на NewLayoutData.
- Строка `cookies_invalid` в обоих `mapErrorCode`.

**План cookies-UI НЕ трогает:** `mapErrorCode`, механизм гейта, монтирование `/settings`.
Его Task 5 (монтирование `/settings` под RequireAuth) и Task 6 (ссылка в навигации)
**отменяются/делегируются** этому плану. Конкретно:
- Task 5 cookies-UI: убрать монтирование под голым RequireAuth — `/settings` монтирует план
  ролей под RequireAuth+RequireAdmin. Cookies-UI лишь регистрирует свои роуты внутри этой
  группы (`settingsSvc.Mount`) и включает глобальный settings-флаг через
  `web.WithSettingsAvailable`, чтобы шестерёнка начала рендериться.
- Task 6 cookies-UI (ссылка «Настройки» в navbar) — заменяется шестерёнкой из плана ролей.
- При создании `page_settings.templ` cookies-UI использует `web.NewLayoutData` (а не
  ручной `LayoutData{}`).

**Порядок реализации:** план ролей даёт инфраструктуру (гейт + NewLayoutData + группа
`/settings`); план cookies-UI монтирует свои хендлеры в уже готовую защищённую группу.
RequireAuth-only `/settings` допустим только как локальное промежуточное состояние в рабочей
ветке. В `dev`/prod такое состояние НЕ мержить: финальный merge/deploy только после схождения
cookies UI + admin gate, где весь `/settings*` закрыт `RequireAuth+RequireAdmin`.

---

## G. Security/ops acceptance criteria

- Весь префикс `/settings*` (GET page, POST upload, будущий DELETE/PUT) живёт в одной группе
  `RequireAuth -> RequireAdmin`.
- `/settings*` НЕ добавляется в `csrfExempt`. Mutating settings actions должны требовать
  CSRF через общий `hx-headers` из `Layout`/`NewLayoutData`.
- Settings mutations логируются минимально: `user.ID`, канонический email, action, результат,
  размер файла при необходимости. Содержимое cookies/API-ключей/секретов не логировать.
- Залогиненный не-админ при прямом заходе на `/settings` молча уходит на `/lectures`, без flash
  и без отдельной 403-страницы.
- README и операционные env-примеры обновлены: `ADMIN_EMAILS` описан как optional fail-closed
  allowlist; `deploy/env.web.example` содержит пример.
- Compose отдельно пробрасывать не нужно, если используется `env_file`; проверить это в
  `docker-compose.prod.yml` и `deploy/compose.vps.yml`.

---

## Чего сознательно НЕ делаем (YAGNI)

- Таблицы/колонки ролей в БД, RBAC, права-пермишены.
- Owner/superadmin поверх allowlist.
- Gmail-specific нормализацию email (`+alias`, удаление точек).
- Принудительную инвалидизацию сессий при изменении `ADMIN_EMAILS`.
- Флеш-сообщения (нет инфраструктуры; редирект на /lectures достаточен).
- Красивая 403-страница (выбран редирект, не 403).
- Ветвление текста ошибок по роли.
- Проактивные уведомления о протухших cookies.

---

## Сводка touch-points (для impl-плана)

- `internal/config/config.go` (+ `load_test.go`): `ADMIN_EMAILS`.
- `internal/db/migrations/004_normalize_user_emails.sql`: канонизация email с проверками.
- `internal/auth/login.go` (+ `login_test.go`): канонический email, пустой email fail-closed.
- `internal/auth/auth.go`: `IsAdminFromContext`, `ctxKeyIsAdmin`, allowlist в Service.
- `internal/auth/middleware.go` (+ test): `LoadAdmin`, `RequireAdmin`.
- `cmd/server/main.go`: передать `cfg.AdminEmails` в auth; смонтировать `LoadAdmin` глобально;
  warning при пустом allowlist; обернуть `/settings*` в RequireAuth+RequireAdmin; перевести
  upload-хендлер на `NewLayoutData`.
- `internal/web/layout.templ`: поле `IsAdmin`, `NewLayoutData`, шестерёнка в `header`, иконка.
- `internal/reader/handlers.go`, `internal/lecture/handlers.go`, `internal/hub/handlers.go`:
  перевод на `NewLayoutData`.
- `internal/lecture/handlers.go`, `internal/syncsvc/handlers.go`: кейс `cookies_invalid`.
- `README.md`, `deploy/env.web.example`: `ADMIN_EMAILS` и поведение fail-closed.
- Регенерация `*_templ.go` после правки `layout.templ`.
