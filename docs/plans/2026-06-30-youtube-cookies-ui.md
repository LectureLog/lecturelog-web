# YouTube cookies — веб-интерфейс (Implementation Plan)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Дать пользователю загрузить `cookies.txt` для YouTube через веб и видеть статус («загружены/дата» или «не загружены»); web хранит cookies только транзитом и проксирует их в core API.

**Architecture:** Новый сервис `internal/settings` (или расширение существующего) с тремя действиями: страница настроек (GET), загрузка cookies (POST multipart → проксирование в core `PUT /api/v1/youtube/cookies`), статус (GET → core `GET /api/v1/youtube/cookies`). Сам файл cookies в web не сохраняется — читается из multipart-запроса и стримится в core. Метод coreclient добавляется поверх сгенерированного OpenAPI-клиента (для multipart — вручную, как уже сделано для `/tasks`). UI — templ-страница со статусом и формой, обновление статуса через htmx-свап после загрузки.

**Tech Stack:** Go, chi router, templ, htmx, oapi-codegen (вендоренная спека ядра), сгенерированный coreclient.

**Зависимость (core):** core-план `lecturelog-core/docs/plans/2026-06-30-youtube-cookies.md` ВЫПОЛНЕН и в `dev` (проверено: эндпоинты `GET/PUT/DELETE /api/v1/youtube/cookies`, `CookieStatusResponse {exists, size, updated_at?}`, PUT отдаёт 200/400/413/422; обновлённая `openapi.json` — 28 КБ, содержит `youtube/cookies`).

**Зависимость (роли/админ-гейт) — ОТДЕЛЬНЫЙ план.** Cookies в ядре — глобальный singleton (одна строка на всё ядро), поэтому страница настроек по решению пользователя должна быть видна/доступна ТОЛЬКО админу. В web сейчас НЕТ модели ролей (`auth.User` = `{ID, Email, Name, AvatarURL}`). Админ-гейт (`ADMIN_EMAILS` в config, `RequireAdmin` middleware, `IsAdmin` в контексте/`LayoutData`, шестерёнка в шапке, единый текст `cookies_invalid`) проектируется ОТДЕЛЬНЫМ планом `docs/plans/2026-06-30-roles-admin-gate-design.md`. В этом плане соответствующие места помечены как **[зависит от плана ролей]** и НЕ реализуются здесь, чтобы не дублировать при параллельной разработке. RequireAuth-only `/settings` допустим только как локальное промежуточное состояние в рабочей ветке; в `dev` и prod мержить/выкатывать только финальное состояние с `RequireAuth+RequireAdmin`.

**Команды проекта:**
- Тесты: `go test ./internal/...`
- Сборка: `go build ./...`
- Генерация coreclient (проверено в Makefile): `make sync-spec` (копирует `../lecturelog-core/docs/openapi.json` → `internal/coreclient/openapi.json`), затем `make generate` (= `go generate ./...`). После — ревью diff в `internal/coreclient/gen.go` на рассинхрон контракта.

---

## Task 1: Обновить вендоренную OpenAPI-спеку и перегенерировать клиент

**Files:**
- Modify: `internal/coreclient/openapi.json` (копия из core)
- Regenerate: `internal/coreclient/*.gen.go`

**Step 1: Скопировать свежую спеку и перегенерировать**

Цели в Makefile (проверено): `sync-spec` (строка ~114) копирует спеку, `generate` (строка ~87) запускает `go generate ./...`. Выполни:

```bash
make sync-spec   # cp ../lecturelog-core/docs/openapi.json internal/coreclient/openapi.json
make generate    # go generate ./...
```

> Текущая вендоренная спека УСТАРЕЛА (24 КБ, без `youtube/cookies`); свежая из core — 28 КБ. После `make generate` обязательно ревью diff в `internal/coreclient/gen.go`.

**Step 2: Убедиться, что клиент содержит новые операции**

Run: `grep -rn "YoutubeCookies\|youtube/cookies" internal/coreclient/*.gen.go`
Expected: появились типы/методы для PUT/GET/DELETE `/youtube/cookies`.

**Step 3: Сборка**

Run: `go build ./...`
Expected: успешно.

**Step 4: Commit**

```bash
git add internal/coreclient/openapi.json internal/coreclient/*.gen.go
git commit -m "chore(coreclient): перегенерация под /youtube/cookies"
```

> NB: `internal/coreclient/openapi.normalized.json` — промежуточный артефакт нормализации (см. `generate.go`), в git НЕ коммитится. Не добавляй его в `git add` (он не отслеживается; `gen-check` его не проверяет).

---

## Task 2: Методы coreclient — статус и загрузка cookies

**Files:**
- Modify: `internal/coreclient/client.go`
- Test: `internal/coreclient/client_test.go` (или новый `cookies_test.go`)

**Step 1: Написать падающий тест**

> ⚠️ ВАЖНО — стиль тестов проекта (проверено в `internal/coreclient/client_test.go`): тесты НЕ поднимают «голый» `httptest.NewServer` на каждый кейс. Есть герметичный `mockCore` (`newMockCore(t)`) с `http.NewServeMux()` и хелпер `newTestClient(t, m.srv.URL)`, который строит клиент через `New(Config{BaseURL: srvURL})`. Используй стандартную библиотеку (`testing`, ручные `if got != want { t.Errorf(...) }`) — `require`/`testify` в пакете НЕ используется (в `client_test.go` его нет). Добавь обработчик `/api/v1/youtube/cookies` в `newMockCore` (GET/PUT/DELETE) с захватом полей в структуру `mockCore` (как `lastTaskForm`/`lastTaskCT`), и тесты в `cookies_test.go` собирай через `newTestClient`.

Эскиз обработчика в `mockCore` (добавить в `newMockCore`, поля захвата — в структуру `mockCore`):

```go
// GET/PUT/DELETE /api/v1/youtube/cookies
mux.HandleFunc("/api/v1/youtube/cookies", func(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        writeJSON(w, http.StatusOK, CookieStatusResponse{Exists: true, Size: 120})
    case http.MethodPut:
        m.lastCookieCT = r.Header.Get("Content-Type")
        if err := r.ParseMultipartForm(1 << 20); err != nil {
            http.Error(w, "bad multipart", http.StatusBadRequest)
            return
        }
        f, _, err := r.FormFile("file")
        if err != nil {
            http.Error(w, "no file", http.StatusBadRequest)
            return
        }
        m.lastCookieBody, _ = io.ReadAll(f)
        writeJSON(w, http.StatusOK, CookieStatusResponse{Exists: true, Size: len(m.lastCookieBody)})
    case http.MethodDelete:
        w.WriteHeader(http.StatusNoContent)
    }
})
```

```go
// internal/coreclient/cookies_test.go (стиль stdlib, как client_test.go)
func TestGetYouTubeCookieStatus(t *testing.T) {
    m := newMockCore(t)
    defer m.srv.Close()
    c := newTestClient(t, m.srv.URL)
    st, err := c.GetYouTubeCookieStatus(context.Background())
    if err != nil {
        t.Fatalf("неожиданная ошибка: %v", err)
    }
    if !st.Exists || st.Size != 120 {
        t.Errorf("статус: got %+v", st)
    }
}

func TestPutYouTubeCookies(t *testing.T) {
    m := newMockCore(t)
    defer m.srv.Close()
    c := newTestClient(t, m.srv.URL)
    st, err := c.PutYouTubeCookies(context.Background(), []byte("abc"))
    if err != nil {
        t.Fatalf("неожиданная ошибка: %v", err)
    }
    if !strings.HasPrefix(m.lastCookieCT, "multipart/form-data") {
        t.Errorf("ожидался multipart, получили %q", m.lastCookieCT)
    }
    if string(m.lastCookieBody) != "abc" {
        t.Errorf("тело cookies: got %q", m.lastCookieBody)
    }
    if !st.Exists {
        t.Errorf("ожидался exists=true")
    }
}
```

> Тип `CookieStatusResponse` появится в сгенерированном клиенте после Task 1 (это схема ответа ядра). Сверь точное имя поля по `*.gen.go`.

**Step 2: Запустить — падает**

Run: `go test ./internal/coreclient/ -run YouTubeCookie -v`
Expected: FAIL — методов нет.

**Step 3: Реализация**

В `internal/coreclient/client.go`:

```go
// CookieStatus — статус YouTube-cookies в ядре (без содержимого).
type CookieStatus struct {
    Exists    bool
    Size      int
    UpdatedAt string // RFC3339 или пусто
}

// GetYouTubeCookieStatus запрашивает статус cookies (GET /youtube/cookies).
func (c *CoreClient) GetYouTubeCookieStatus(ctx context.Context) (CookieStatus, error) {
    resp, err := c.api.GetYoutubeCookiesApiV1YoutubeCookiesGetWithResponse(ctx)
    if err != nil {
        return CookieStatus{}, fmt.Errorf("coreclient: статус cookies: %w", err)
    }
    if resp.JSON200 != nil {
        s := CookieStatus{Exists: resp.JSON200.Exists, Size: resp.JSON200.Size}
        if resp.JSON200.UpdatedAt != nil {
            s.UpdatedAt = resp.JSON200.UpdatedAt.Format(time.RFC3339)
        }
        return s, nil
    }
    return CookieStatus{}, fmt.Errorf("coreclient: неожиданный код на статус cookies: %d", resp.StatusCode())
}
```

> ⚠️ КРИТИЧНО — НЕ собирай PUT вручную через `http.NewRequestWithContext(c.baseURL+...)`. Проверено: `CoreClient` (`config.go:24`) хранит ТОЛЬКО `api ClientWithResponsesInterface` — **полей `c.baseURL` и сырого `http.Client` НЕ существует**. BaseURL живёт внутри сгенерированного `ClientWithResponses`. Образец `CreateTask` (`client.go:64`) собирает multipart `bytes.Buffer` сам, но ОТПРАВЛЯЕТ его через сгенерированный `...WithBodyWithResponse(ctx, contentType, body)` — повтори ровно этот путь. После Task 1 oapi-codegen сгенерирует метод вида `PutYoutubeCookiesApiV1YoutubeCookiesPutWithBodyWithResponse(ctx, contentType, body)` — точное имя возьми из `*.gen.go`.

```go
// PutYouTubeCookies загружает cookies.txt в ядро multipart-запросом (поле file).
// Тело собирается вручную (multipart), но отправляется через сгенерированный
// …WithBodyWithResponse — ровно как CreateTask (client.go:64), без сырого http.
func (c *CoreClient) PutYouTubeCookies(ctx context.Context, content []byte) (CookieStatus, error) {
    var buf bytes.Buffer
    mw := multipart.NewWriter(&buf)
    fw, err := mw.CreateFormFile("file", "cookies.txt")
    if err != nil {
        return CookieStatus{}, fmt.Errorf("coreclient: multipart cookies: %w", err)
    }
    if _, err := fw.Write(content); err != nil {
        return CookieStatus{}, fmt.Errorf("coreclient: запись cookies: %w", err)
    }
    if err := mw.Close(); err != nil {
        return CookieStatus{}, fmt.Errorf("coreclient: закрытие multipart: %w", err)
    }

    // FormDataContentType() содержит boundary — как в CreateTask (client.go:99-100).
    resp, err := c.api.PutYoutubeCookiesApiV1YoutubeCookiesPutWithBodyWithResponse(
        ctx, mw.FormDataContentType(), &buf,
    )
    if err != nil {
        return CookieStatus{}, fmt.Errorf("coreclient: загрузка cookies: %w", err)
    }
    if resp.JSON200 != nil {
        st := CookieStatus{Exists: resp.JSON200.Exists, Size: resp.JSON200.Size}
        if resp.JSON200.UpdatedAt != nil {
            st.UpdatedAt = resp.JSON200.UpdatedAt.Format(time.RFC3339)
        }
        return st, nil
    }
    // Ядро отдаёт 400 (некорректный формат) и 413 (слишком большой) — оба ErrorResponse.
    // Имена JSON400/JSON413 зависят от того, что сгенерировал oapi-codegen для PUT
    // (в openapi описаны responses 400/413/422) — сверь по *.gen.go.
    if resp.JSON400 != nil {
        return CookieStatus{}, fmt.Errorf("coreclient: cookies отклонены (400): %s", resp.JSON400.Detail)
    }
    if resp.JSON413 != nil {
        return CookieStatus{}, fmt.Errorf("coreclient: cookies слишком большие (413): %s", resp.JSON413.Detail)
    }
    return CookieStatus{}, fmt.Errorf("coreclient: неожиданный код на загрузку cookies: %d", resp.StatusCode())
}
```

> Если oapi-codegen НЕ сгенерировал типизированные `JSON400/JSON413` для PUT (бывает, когда схема ответа — generic `HTTPValidationError`/`ErrorResponse`) — ориентируйся на `resp.StatusCode()` и `resp.Body`. Сверь по факту в `*.gen.go` после Task 1; форма обработки ошибок — как в `CreateUpload` (client.go:52-58), где разбираются `JSON400`/`JSON409`.

**Step 4: Запустить — проходит**

Run: `go test ./internal/coreclient/ -run YouTubeCookie -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/coreclient/client.go internal/coreclient/cookies_test.go
git commit -m "feat(coreclient): статус и загрузка YouTube-cookies"
```

---

## Task 3: Сервис настроек — хендлеры статуса и загрузки

**Files:**
- Create: `internal/settings/service.go`
- Create: `internal/settings/handlers.go`
- Test: `internal/settings/handlers_test.go`

**Step 1: Написать падающий тест**

Фейк core-интерфейса (как в `internal/upload` через интерфейс `Core`). Проверь:
- `GET /settings/cookies/status` рендерит «загружены»/«не загружены» (htmx-фрагмент)
- `POST /settings/cookies` с multipart `file` → вызывает `PutYouTubeCookies`, при успехе отдаёт обновлённый фрагмент статуса
- ошибка core (400 от ядра) → понятное сообщение пользователю
- неавторизованный → 401 (паттерн `auth.UserFromContext`, как в `upload/handlers.go:22`)

```go
// internal/settings/handlers_test.go — эскиз
type fakeCore struct {
    status coreclient.CookieStatus
    putErr error
}
func (f *fakeCore) GetYouTubeCookieStatus(ctx context.Context) (coreclient.CookieStatus, error) {
    return f.status, nil
}
func (f *fakeCore) PutYouTubeCookies(ctx context.Context, b []byte) (coreclient.CookieStatus, error) {
    if f.putErr != nil { return coreclient.CookieStatus{}, f.putErr }
    return coreclient.CookieStatus{Exists: true, Size: len(b)}, nil
}
```

**Step 2: Запустить — падает**

Run: `go test ./internal/settings/ -v`
Expected: FAIL — пакета нет.

**Step 3: Реализация**

`internal/settings/service.go`:

```go
package settings

import (
    "context"

    "github.com/LectureLog/lecturelog-web/internal/coreclient"
)

// Core — нужная сервису часть API ядра (для тестируемости через фейк).
type Core interface {
    GetYouTubeCookieStatus(ctx context.Context) (coreclient.CookieStatus, error)
    PutYouTubeCookies(ctx context.Context, content []byte) (coreclient.CookieStatus, error)
}

type Service struct {
    core Core
}

func NewService(core Core) *Service {
    return &Service{core: core}
}
```

`internal/settings/handlers.go`:

```go
package settings

import (
    "net/http"

    "github.com/LectureLog/lecturelog-web/internal/auth"
    "github.com/go-chi/chi/v5"
)

// Лимит на загружаемый cookies.txt (ядро тоже лимитирует 1 МБ).
const maxCookieBytes = 1 << 20

func (s *Service) Mount(r chi.Router) {
    r.Get("/settings", s.handlePage)
    r.Get("/settings/cookies/status", s.handleStatus)
    r.Post("/settings/cookies", s.handleUpload)
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
    if auth.UserFromContext(r.Context()) == nil {
        http.Error(w, "требуется авторизация", http.StatusUnauthorized)
        return
    }
    st, err := s.core.GetYouTubeCookieStatus(r.Context())
    if err != nil {
        http.Error(w, "ядро недоступно", http.StatusBadGateway)
        return
    }
    // рендер htmx-фрагмента статуса (templ из Task 4)
    _ = CookieStatusFragment(st).Render(r.Context(), w)
}

func (s *Service) handleUpload(w http.ResponseWriter, r *http.Request) {
    if auth.UserFromContext(r.Context()) == nil {
        http.Error(w, "требуется авторизация", http.StatusUnauthorized)
        return
    }
    if err := r.ParseMultipartForm(maxCookieBytes); err != nil {
        http.Error(w, "файл слишком большой или некорректен", http.StatusBadRequest)
        return
    }
    file, _, err := r.FormFile("file")
    if err != nil {
        http.Error(w, "выберите файл cookies.txt", http.StatusBadRequest)
        return
    }
    defer file.Close()
    content, err := io.ReadAll(io.LimitReader(file, maxCookieBytes+1))
    if err != nil || len(content) > maxCookieBytes {
        http.Error(w, "файл слишком большой", http.StatusBadRequest)
        return
    }
    st, err := s.core.PutYouTubeCookies(r.Context(), content)
    if err != nil {
        // 400 от ядра (кривой формат) → понятное сообщение
        http.Error(w, "не удалось сохранить cookies: проверьте формат (Netscape cookies.txt)", http.StatusBadRequest)
        return
    }
    _ = CookieStatusFragment(st).Render(r.Context(), w)
}
```

> `handlePage` рендерит полную страницу (Task 4). Импорты (`io` и т.д.) добавь по факту. Сверь обработку ошибок ядра со стилем `internal/upload` и `internal/reader` (там есть `writeMessage`/маппинг статусов — переиспользуй подход).

**Step 4: Запустить — проходит**

Run: `go test ./internal/settings/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/settings/
git commit -m "feat(settings): хендлеры статуса и загрузки YouTube-cookies"
```

---

## Task 4: templ-страница и фрагмент статуса

**Files:**
- Create: `internal/web/page_settings.templ`
- Modify: `internal/settings/handlers.go` (подключить рендер `web.SettingsPage`)
- Regenerate: `*_templ.go` (через `templ generate` — см. как в проекте, `internal/web/generate.go`)

**Step 1: templ-разметка**

`internal/web/page_settings.templ` — страница настроек с блоком «YouTube cookies»:
- индикатор статуса (templ-компонент `CookieStatusFragment`, принимает статус): «Cookies загружены · обновлены <дата>» либо «Cookies не загружены»
- форма `POST /settings/cookies` `enctype=multipart/form-data` с `<input type="file" name="file" accept=".txt">` и кнопкой; htmx: `hx-post="/settings/cookies" hx-encoding="multipart/form-data" hx-target="#cookie-status" hx-swap="outerHTML"`
- подсказка: «Экспортируй cookies.txt расширением браузера (Netscape format)»
- ⚠️ CSRF — проверено: в `page_upload.templ:78` токен идёт НЕ скрытым полем, а через **`hx-headers={ `{"X-CSRF-Token":"` + data.CSRFToken + `"}` }`** на форме (мутирующий htmx-POST). Глобальный `csrfHeaders` в `layout.templ:51` уже кладёт токен на `<body>`, но форма `/upload/youtube` дублирует его на себе — повтори ровно этот паттерн для `/settings/cookies`. НЕ изобретай скрытый `<input type="hidden" name="csrf">` — middleware ждёт заголовок `X-CSRF-Token`.

> Грабля из памяти проекта `[[project-htmx-corrupted-csrf]]`: форма YouTube падала на `ErrNoToken`, когда htmx был «битый» (развёрнутые HTML-escape в regex повредили htmx.js). При вёрстке проверь, что токен реально уходит в заголовке (DevTools → Network → запрос на `/settings/cookies` → Request Headers → `X-CSRF-Token`).

> Посмотри `internal/web/page_upload.templ` как образец структуры страницы, layout и подключения CSRF/стилей. `CookieStatusFragment` оберни в `<div id="cookie-status">…</div>`, чтобы htmx-свап заменял именно его.

**Step 2: Сгенерировать templ и собрать**

Run: `templ generate` (или цель из Makefile) затем `go build ./...`
Expected: успешно, типы `web.SettingsPage`/`web.CookieStatusFragment` доступны.

> Если `CookieStatusFragment` логичнее держать в пакете `web` — тогда в `settings/handlers.go` импортируй `web` и вызывай `web.CookieStatusFragment(...)`. Реши по тому, как организованы остальные фрагменты (lectures/reader).

**Step 3: Подключить рендер страницы в `handlePage`**

```go
func (s *Service) handlePage(w http.ResponseWriter, r *http.Request) {
    user := auth.UserFromContext(r.Context())
    if user == nil {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    st, _ := s.core.GetYouTubeCookieStatus(r.Context())
    token := web.CSRFTokenFromContext(r.Context())
    data := web.LayoutData{Title: "Настройки", CSRFToken: token}
    _ = web.SettingsPage(data, st).Render(r.Context(), w)
}
```

> **[зависит от плана ролей]** План ролей (`2026-06-30-roles-admin-gate-design.md`) добавляет в `LayoutData` поле `IsAdmin` и хелпер `web.NewLayoutData(ctx, title)`, который сам тянет `CSRFToken` + `IsAdmin` из контекста. Если на момент реализации этой задачи хелпер уже есть — используй его (`data := web.NewLayoutData(r.Context(), "Настройки")`) вместо ручной сборки `LayoutData{...}`. Если плана ролей ещё нет — ручная сборка выше допустима только локально в промежуточной ветке; перед merge в `dev` settings-страница должна перейти на `NewLayoutData` и admin gate.

**Step 4: Собрать и прогнать тесты пакета**

Run: `go build ./... && go test ./internal/settings/ ./internal/web/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/web/page_settings.templ internal/web/*_templ.go internal/settings/handlers.go
git commit -m "feat(web): страница настроек с загрузкой YouTube-cookies"
```

---

## Task 5: Монтирование в роутер

**Files:**
- Modify: `cmd/server/main.go` (создать `settingsSvc`, смонтировать)

**Step 1: Создать сервис и смонтировать**

Рядом с созданием `uploadSvc` создай:

```go
settingsSvc := settings.NewService(coreClient)
```
(используй тот же `coreClient`, что передаётся в `uploadSvc`; импортируй пакет `settings`)

В финальном состоянии сервис монтируется в группе `/settings*`, которая уже закрыта
`RequireAuth` → `RequireAdmin` планом ролей:

```go
settingsSvc.Mount(ar) // GET /settings, GET /settings/cookies/status, POST /settings/cookies
```

> ⚠️ **[граница с планом ролей — согласовано] Mount-блоком `/settings` владеет ПЛАН РОЛЕЙ, не этот план.** По договорённости (`2026-06-30-roles-admin-gate-design.md`, раздел F) `/settings*` монтируется ОДИН раз — под `RequireAuth` → `RequireAdmin` — и это делает план ролей. Cookies-UI лишь **регистрирует свои роуты внутри уже защищённой группы** через `settingsSvc.Mount(ar)`. При этом после подключения реальных `/settings*` routes сервер должен включить глобальный settings-флаг (`web.WithSettingsAvailable(ctx)`), иначе шестерёнка останется скрытой. Почему: cookies в ядре — глобальный singleton, под голым `RequireAuth` любой залогиненный по прямому URL `/settings` перезатёр бы cookies всем. `RequireAdmin`: обычный запрос → 302 `/lectures`, htmx → `HX-Redirect: /lectures`.
>
> - Если план ролей УЖЕ реализован — НЕ дублируй mount; добавь только `settingsSvc.Mount(ar)` в его группу `/settings*`:
>   ```go
>   // группа уже создана планом ролей: ar.Use(RequireAuth); ar.Use(RequireAdmin)
>   settingsSvc.Mount(ar)
>   ```
> - Если плана ролей ещё НЕТ (cookies-UI стартует раньше) — временно смонтируй под `RequireAuth` с TODO (`// TODO: /settings под RequireAdmin — см. roles-admin-gate, заменить обёртку`) только для локальной разработки. Такое состояние НЕ мержить в `dev` и НЕ выкатывать в prod. Скрытие ссылки (Task 6) — UX-слой, НЕ замена серверного гейта.

**Step 2: Проверка CSRF**

`POST /settings/cookies` — мутирующий, должен проходить CSRF-middleware (он применяется глобально, кроме `/webhooks/core`). Убедись, что форма шлёт CSRF-токен (Task 4). НЕ добавляй `/settings/cookies` в `csrfExempt`.

**Step 3: Сборка и ручная проверка**

Run: `go build ./... && go vet ./...`
Expected: успешно.

> Грабля из памяти проекта: убедись, что htmx не «битый» — форма с CSRF на мутирующем POST падала раньше на ErrNoToken из-за повреждённого htmx. Проверь, что токен реально уходит.

**Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(web): подключение сервиса настроек"
```

---

## Task 6: Навигация на страницу настроек — [ПОЛНОСТЬЮ зависит от плана ролей]

> ⚠️ Исходная формулировка задачи была основана на НЕВЕРНОМ допущении. Проверено в коде: **навигационного меню между разделами в `layout.templ` НЕТ.** Шапка (`header`, `layout.templ:61`) = brand + слот `actions` + `themeToggle`. Ссылок на `/lectures`/`/upload` в layout нет — они зашиты внутри страниц (`page_hub.templ`, `page_lectures.templ`), а `/lectures` и `/upload` передают `actions=nil`. Так что «добавить пункт рядом с /lectures в layout» невозможно — такого места не существует.

**Решение (из грилла, проектируется планом ролей `2026-06-30-roles-admin-gate-design.md`):** вход на `/settings` — иконка-шестерёнка в правой части шапки (`ll-top-actions`, рядом с `themeToggle`), рендерится в `header` ТОЛЬКО если `data.IsAdmin && data.SettingsAvailable`. Это требует поля `LayoutData.IsAdmin`, settings-флага и их проброса во все рендерящие Layout хендлеры — то есть инфраструктуру, которую вводит план ролей.

Актуализация после review: в roles-ветке без cookies-UI реального `/settings` ещё нет, поэтому
шестерёнка рендерится только при `data.IsAdmin && data.SettingsAvailable`. Когда Task 5
монтирует реальные `/settings*` routes под `RequireAuth -> RequireAdmin`, `cmd/server` должен
включить settings-флаг глобальным middleware через `web.WithSettingsAvailable(ctx)`.

**Поэтому:**
- Если план ролей УЖЕ реализован — добавь в `header` (`layout.templ`) условный блок:
  ```go
  if data.IsAdmin && data.SettingsAvailable {
      @settingsLink() // иконка-шестерёнка → href="/settings"
  }
  ```
  рядом с `@themeToggle()`, и новый templ-компонент `settingsLink()` с иконкой (стиль `ll-icon-btn`, как `themeToggle`). Перегенерируй `*_templ.go`.
- Если плана ролей ещё НЕТ — **эту задачу НЕ делай** (нет ни `IsAdmin`, ни админ-гейта). Ветка может временно иметь прямой URL `/settings` под `RequireAuth` только локально; перед merge в `dev` навигационный вход добавляет план ролей вместе с шестерёнкой и серверным гейтом за один проход. Скрывать ссылку без серверного `RequireAdmin` (Task 5) бессмысленно — это не защита.

**Шаги (когда план ролей готов):**

Run: `templ generate && go build ./...` → успешно.

```bash
git add internal/web/layout.templ internal/web/*_templ.go
git commit -m "feat(web): вход в настройки (шестерёнка) для админа"
```

---

## Финал (после всех задач)

1. `go build ./... && go vet ./... && go test ./internal/...` — зелёное.
2. Ручная проверка (опц., через /run или браузер): зайти на `/settings`, загрузить экспортированный cookies.txt, увидеть «Cookies загружены», затем сгенерировать конспект из YouTube-ссылки. Проверь в DevTools, что POST на `/settings/cookies` уходит с заголовком `X-CSRF-Token` (грабля `[[project-htmx-corrupted-csrf]]`).
3. Документация: обновить README/доки web (через отдельного субагента) — раздел про настройку YouTube-cookies.
4. ⚠️ **Перед merge в `dev` и prod-деплоем — закрыть админ-гейт** (план ролей: `RequireAdmin` на `/settings*`). До этого под `RequireAuth` любой залогиненный пишет глобальные cookies ядра — в `dev`/prod так НЕ выкатывать.
5. Деплой — отдельно (GHCR → `docker compose pull && up` на hetzner-fn). Core уже задеплоен (cookies-эндпоинты в `dev`).

**Зависимости задач:** Task 1 (спека) первой; 2 зависит от 1; 3 от 2; 4 от 3; 5 от 3,4. Core-эндпоинты уже существуют (выполнено), `openapi.json` в core обновлена — Task 1 синхронизирует её в web.

**Task 6 и админ-доступ в Task 5 — зависят от ОТДЕЛЬНОГО плана ролей** (`2026-06-30-roles-admin-gate-design.md`): `IsAdmin`, `RequireAdmin`, шестерёнка в шапке, строка `cookies_invalid`. Tasks 1–4 могут идти до плана ролей; RequireAuth-only Task 5 допустим только локально в рабочей ветке. Финальный merge в `dev` и prod-деплой — только после закрытия админ-гейта.

> ⚠️ **Циклическая зависимость двух планов — секвенируй вместе, не «доводи до конца» каждый в изоляции.** Куки-фича требует `RequireAdmin` (из плана ролей); а шестерёнка-вход плана ролей требует, чтобы `/settings` существовал (из куки Tasks 1–4). Порядок: куки T1–4 → инфраструктура ролей (`ADMIN_EMAILS`/`IsAdmin`/`RequireAdmin`/`NewLayoutData`) → админские части обоих (куки T5-гейт, T6-шестерёнка, строка `cookies_invalid`) → merge в `dev`/deploy ОДНИМ готовым состоянием. Ни один план не «done» сам по себе до этого схождения.

---

## ⚠️ Что недопроработано — вызвать доп. агентов до/во время реализации

Эти места в брейншторме проговорены поверхностно. Перед соответствующей задачей запусти отдельного агента для проработки, иначе реализация будет «на глаз».

### 1. Дизайн UI (САМОЕ слабое место — почти не прорабатывали)

Страница настроек и блок cookies описаны **только функционально** (какие поля и htmx-атрибуты). Визуальный дизайн не продуман: расположение блока на странице настроек, состояния (загружены / не загружены / ошибка / в процессе загрузки), внешний вид индикатора статуса, drag-and-drop зоны для файла, сообщения об ошибках.

**Действие перед Task 4:** запустить агента `frontend-design:frontend-design` с контекстом:
- дизайн-система проекта — «Читальный зал» в `design/` (токены + style-guide + прототипы), см. память `[[project-design-package]]`;
- образец существующей страницы — `internal/web/page_upload.templ`;
- нужны макеты: страница `/settings`, блок «YouTube cookies» во всех состояниях, фрагмент статуса для htmx-свапа.
Дизайн согласовать с пользователем (через AskUserQuestion с preview), затем кодировать templ по утверждённому макету.

### 2. ✅ РЕШЕНО (грилл 2026-06-30) — место и видимость «Настроек»

Решено: cookies — глобальная настройка ядра, страница `/settings` видна/доступна ТОЛЬКО админу. Вход — иконка-шестерёнка в шапке (`ll-top-actions`), рендерится только при `data.IsAdmin && data.SettingsAvailable`. Навигационного меню в layout сейчас нет (проверено) — шестерёнка добавляется планом ролей вместе с `LayoutData.IsAdmin` и settings-флагом. Серверный гейт — `RequireAdmin` (Task 5), скрытие ссылки — UX поверх. Подробности — `2026-06-30-roles-admin-gate-design.md`. См. Task 5 и Task 6 (оба помечены `[зависит от плана ролей]`).

### 3. Обработка и показ ошибок ядра

Маппинг ошибок core → сообщения пользователю описан грубо (400 → «проверьте формат»). Не покрыты: 413 (слишком большой файл) отдельным текстом, недоступность ядра (502), таймауты. Стоит свериться с тем, как `internal/reader` и `internal/upload` единообразно отдают ошибки (есть `writeMessage`), и привести к одному стилю.

**Действие:** при Task 3 свериться с существующим стилем ошибок; если разнобой — отдельный агент на унификацию (вне этого плана).

### 4. UX безопасности cookies

Cookies — это полный доступ к YouTube-аккаунту пользователя. В UI стоит явно предупредить, что загружается чувствительный секрет, и, возможно, не использовать личный основной аккаунт. Текст-предупреждение не проработан.

**Действие:** включить в задачу дизайна (п.1) предупреждающий текст; формулировку согласовать с пользователем.

### 5. ✅ РЕШЕНО (грилл 2026-06-30, D-final) — показ кода ошибки `cookies_invalid` отдан плану ролей

Core (Task 5b, выполнен) добавил `error_code == "cookies_invalid"` (yt-dlp «Sign in to confirm you're not a bot» = cookies протухли/не приняты). Поле `error_code` — свободная `string` в схеме задачи, **перегенерация клиента ради него НЕ нужна** (`TaskStatus.ErrorCode *string` в coreclient уже есть; перегенерация в Task 1 — только ради эндпоинтов `/youtube/cookies`).

⚠️ **Исправление допущений исходного плана (проверено в коде):**
- Точка показа — НЕ `internal/reader`, а функция **`mapErrorCode(code string) string`**, которая ДУБЛИРУЕТСЯ в двух местах: `internal/lecture/handlers.go:239` и `internal/syncsvc/handlers.go:193`.
- Эти два `mapErrorCode` **НЕ идентичны**: в `lecture/handlers.go` — 3 кейса (`processing_error`, `download_error`, `transcription_error`); в `syncsvc/handlers.go` — 6 (`rate_limit`, `bad_input`, `internal` + те же 3). Расхождение, возможно, осознанное. «Вынести общий дубль» — НЕ чистый лифт, в рамках cookies НЕ делаем.

**Решение D-final (подтверждено пользователем в грилле ролей):**
- `mapErrorCode` **НЕ ветвится по роли, сигнатура `(code string)` НЕ меняется** (карточки лекций — owner-view, усложнять не нужно; `IsAdmin` в `lecture`/`syncsvc` пробрасывать НЕ требуется).
- Для `cookies_invalid` — **единый нейтральный текст для всех: «Cookies YouTube устарели — обратитесь к администратору»**, добавляется в ОБА `mapErrorCode`.
- **Владелец строки — план ролей** (`2026-06-30-roles-admin-gate-design.md`): он правит оба `switch`. Этот куки-план `mapErrorCode` **НЕ трогает** (один план — один редактор switch, нет конфликта при параллельной разработке).
- Проактивного сигнала «cookies протухли» нет (ветка E=A): админ узнаёт по упавшей **своей** лекции; страница настроек показывает `updated_at` (возраст cookies). Это известное ограничение MVP.

> **Interim до выкатки плана ролей:** пока строка `cookies_invalid` не добавлена в `mapErrorCode`, падение лекции с этим кодом отрендерится дефолтной веткой как «Ошибка: cookies_invalid». Допустимо, т.к. прод-деплой куки-фичи и так связан с планом ролей (см. финал, п.4) — выкатываются вместе.
