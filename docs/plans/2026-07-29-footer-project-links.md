# Ссылки на репозитории и Telegram разработчика в футере (Design + Implementation Plan)

**Goal:** В футере (виден на всех страницах) появляются ссылки на два репозитория проекта и на Telegram разработчика. Заодно в проекте впервые вводится безопасный паттерн внешней ссылки, чтобы следующая такая ссылка не забыла `rel`.

**Scope:** Только футер. Секция на лендинге, страница `/about`, ссылки в шапке, шильдики версии/лицензии — сознательно вне scope.

**Architecture:** Изменение чисто презентационное, внутри пакета `internal/web`. URL'ы — неэкспортируемые константы в новом файле `internal/web/links.go` (не в `internal/config`: значения статические и не зависят от окружения, а пакет `web` осознанно не зависит от `config`, см. комментарий в `web.go:1-5`). Разметка — `templ footer()` в `layout.templ`. Стили — новые `.ll-*` классы в `assets/tailwind.css` без единого цвета (наследуют существующий `.ll-footer-link`), поэтому запрет на хардкод цветов из `tailwind.css:30` не задевается.

**Tech Stack:** Go 1.25, chi, templ, Tailwind v4 CSS-first (standalone CLI в `bin/tailwindcss`).

**Значения ссылок:**

| Подпись | URL |
|---|---|
| `lecturelog-core` | `https://github.com/LectureLog/lecturelog-core` |
| `lecturelog-web` | `https://github.com/LectureLog/lecturelog-web` |
| только иконка, без подписи | `https://t.me/fus1ond` |

Telegram по решению владельца — **только иконка**, минималистично. Следствие, которое нельзя терять: иконка помечена `aria-hidden`, поэтому у такой ссылки не остаётся видимого текста и доступное имя обязано жить в `aria-label` (иначе скринридер прочитает URL). Плюс область нажатия у голой иконки должна дотягивать до 24×24 (WCAG 2.5.8).

**Команды проекта:**
- `make templ` — регенерация `*_templ.go` (обязательно после правки `.templ`)
- `make tailwind` — сборка `assets/tailwind.css` → `static/css/app.css --minify`
- `make gate` — полная проверка как в CI: генерация + `go build` + `go vet` + `go test ./...`
- `make preview` — визуальный предпросмотр всех страниц на порту 8901
- CI дополнительно требует `git diff --exit-code` после генерации: закоммиченные `*_templ.go` и `app.css` обязаны совпадать с результатом сборки.

---

## Два осознанных отклонения от текущих конвенций

**1. Брендовые иконки заливкой, а не штрихом.** Все существующие иконки проекта — контурные (`fill="none" stroke="currentColor" stroke-width="2"`, см. `icons.templ`). Логотипы GitHub и Telegram спроектированы как заливка; попытка обвести их штрихом на 16px даёт визуальную кашу. Поэтому `iconGithub` и `iconTelegram` — единственные иконки с `fill="currentColor"` без `stroke`. В `icons.templ` это фиксируется комментарием, чтобы отклонение не выглядело недосмотром и не копировалось на обычные иконки.

**2. Подпись «Витрина публичных лекций» сокращается до «Витрина».** В футере становится четыре ссылки вместо одной; длинная подпись переполняет строку. Маршрут `/hub` не меняется.

---

## Task 1: Константы URL

**Files:**
- Create: `internal/web/links.go`

Неэкспортируемые константы `repoCoreURL`, `repoWebURL`, `devTelegramURL` с комментариями на русском. Неэкспортируемые сознательно: за пределами пакета они не нужны, а тесты живут во внешнем пакете `web_test` и должны утверждать фактическое ожидаемое значение URL литералом, а не сверять константу с самой собой.

**Verify:** `go build ./...`

---

## Task 2: Иконки GitHub и Telegram

**Files:**
- Modify: `internal/web/icons.templ`

Добавить `templ iconGithub()` и `templ iconTelegram()`: `viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"`, без `stroke`. Размер задаётся из CSS, не атрибутами (как у остальных иконок).

**Verify:** `make templ`, затем `go build ./...`. Визуальная проверка формы глифов — в Task 5; недостаточно убедиться, что SVG присутствует в HTML, нужно увидеть, что он рисует именно логотип.

---

## Task 3: Обёртка внешней ссылки и новый футер

**Files:**
- Modify: `internal/web/layout.templ`

**Step 1: `templ footerExtLink(href string, label string)`**

```templ
templ footerExtLink(href string, label string) {
	<a class="ll-footer-link ll-footer-link--ext" href={ href } target="_blank" rel="noopener noreferrer">
		{ children... }
		<span>{ label }</span>
	</a>
}
```

Иконка передаётся дочерним блоком. `href={ href }` — выражение, а не строковый литерал: templ прогоняет его через `templ.SafeURL`-санитайзер, посторонние схемы становятся `about:invalid`. `rel="noopener noreferrer"` задаётся здесь и только здесь — это и есть смысл обёртки.

**Step 2: переписать `templ footer()`**

Третий элемент `.ll-footer-inner` (сейчас одиночная `<a href="/hub">`) заменяется на `<nav class="ll-footer-links" aria-label="Ссылки проекта">` с четырьмя пунктами: внутренняя «Витрина» (`/hub`, без иконки и без `target`), затем три внешние через `footerExtLink`.

**Verify:** `make templ`, `go build ./...`

---

## Task 4: Стили

**Files:**
- Modify: `internal/web/assets/tailwind.css`

`.ll-footer-links` — flex, `flex-wrap: wrap`, `align-items: center`, gap. `.ll-footer-link--ext` — `inline-flex`, `align-items: center`, небольшой gap; вложенный `svg` — 1rem × 1rem, `flex-shrink: 0`. Ни одного цвета: hover/цвет наследуются от существующего `.ll-footer-link` (`tailwind.css:1775-1786`).

Проверить и при необходимости поправить мобильный оверрайд `.ll-footer-inner` (`tailwind.css:~2057`), чтобы группа ссылок переносилась и выравнивалась по левому краю вместе с остальным футером.

**Verify:** `make tailwind`; убедиться, что новые классы попали в `static/css/app.css`.

---

## Task 5: Тесты

**Files:**
- Modify: `internal/web/web_test.go`

Тестов футера в проекте сейчас нет вообще — добавляются с нуля, поверх существующих хелперов `renderLayout` / `renderLayoutWithData`.

- `TestFooter_ProjectLinks` — все три URL присутствуют; у ссылок на репозитории есть видимая подпись, у Telegram видимого текста нет, а имя лежит в `aria-label`.
- `TestFooter_LinksHaveAccessibleName` — ни одна ссылка футера не безымянна: есть либо текст, либо `aria-label`.
- `TestFooter_ExternalLinksSecurity` — обход DOM через `golang.org/x/net/html` (уже импортирован в файле): **каждая** ссылка с `target="_blank"` обязана иметь `rel`, содержащий и `noopener`, и `noreferrer`. Тест намеренно сформулирован про все такие ссылки, а не про три конкретные, — он должен ловить регрессию у любой будущей внешней ссылки.
- `TestFooter_ShowcaseLink` — `/hub` не потерялась и не получила `target="_blank"`.
- Проверить футер и для анонима, и для `IsAuthed` — он не зависит от авторизации.

**Verify:** `go test ./internal/web/...`

---

## Task 6: Финальная проверка

**Verify:**
1. `make gate` — зелёный.
2. `git diff --exit-code -- internal/web/` после `make web-gen` — пусто (детерминизм артефактов, требование CI).
3. `make preview` (порт 8901) — открыть страницу, убедиться глазами: иконки рисуют узнаваемые логотипы GitHub и Telegram, строка футера не переполняется, ссылки открываются в новой вкладке. Проверить светлую и тёмную тему и узкий вьюпорт.

## Не-цели

Ссылки на релизы/лицензию, счётчик звёзд GitHub, кнопки «Форкни», аналитика клика по ссылкам, i18n (проект целиком русскоязычный, i18n-слоя нет).
