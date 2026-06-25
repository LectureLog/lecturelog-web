package web_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LectureLog/lecturelog-web/internal/web"
	"github.com/go-chi/chi/v5"
)

// renderLayout — вспомогательная функция: рендерит Layout в строку.
func renderLayout(t *testing.T, title string) string {
	t.Helper()
	var b bytes.Buffer
	data := web.LayoutData{Title: title}
	err := web.Layout(data, nil).Render(context.Background(), &b)
	if err != nil {
		t.Fatalf("Layout.Render: %v", err)
	}
	return b.String()
}

// renderUploadPage — вспомогательная функция: рендерит UploadPage в строку.
func renderUploadPage(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	data := web.LayoutData{Title: "Новый конспект", CSRFToken: "tok"}
	err := web.UploadPage(data).Render(context.Background(), &b)
	if err != nil {
		t.Fatalf("UploadPage.Render: %v", err)
	}
	return b.String()
}

// TestLayout_DocType проверяет DOCTYPE и базовые атрибуты корневого элемента.
func TestLayout_DocType(t *testing.T) {
	html := renderLayout(t, "Тест")

	// templ генерирует строчный <!doctype html> — это корректно по HTML5
	if !strings.Contains(strings.ToLower(html), "<!doctype html>") {
		t.Error("ожидается DOCTYPE html")
	}
	if !strings.Contains(html, `data-theme="light"`) {
		t.Error("ожидается дефолтная тема light на корневом элементе")
	}
	if !strings.Contains(html, `lang="ru"`) {
		t.Error("ожидается lang=ru на html")
	}
}

// TestLayout_Title проверяет что заголовок вкладки совпадает с LayoutData.Title.
func TestLayout_Title(t *testing.T) {
	html := renderLayout(t, "ЛекчурЛог")

	if !strings.Contains(html, "<title>ЛекчурЛог</title>") {
		t.Error("ожидается <title>ЛекчурЛог</title>")
	}
}

// TestLayout_StaticAssets проверяет наличие ссылок на статические ресурсы.
func TestLayout_StaticAssets(t *testing.T) {
	html := renderLayout(t, "Тест")

	if !strings.Contains(html, "/static/css/app.css") {
		t.Error("ожидается ссылка на /static/css/app.css")
	}
	if !strings.Contains(html, "/static/vendor/htmx.min.js") {
		t.Error("ожидается ссылка на /static/vendor/htmx.min.js")
	}
}

// TestLayout_GoogleFonts проверяет подключение шрифтов через Google Fonts CDN.
func TestLayout_GoogleFonts(t *testing.T) {
	html := renderLayout(t, "Тест")

	if !strings.Contains(html, "fonts.googleapis.com") {
		t.Error("ожидается preconnect/link к fonts.googleapis.com")
	}
	if !strings.Contains(html, "Source+Serif+4") {
		t.Error("ожидается подключение шрифта Source Serif 4")
	}
	if !strings.Contains(html, "Onest") {
		t.Error("ожидается подключение шрифта Onest")
	}
}

// TestLayout_ThemeScript проверяет наличие инлайн-скрипта анти-FOUC темы.
func TestLayout_ThemeScript(t *testing.T) {
	html := renderLayout(t, "Тест")

	if !strings.Contains(html, "ll_theme") {
		t.Error("ожидается инлайн-скрипт темы с ключом ll_theme в localStorage")
	}
	if !strings.Contains(html, "setAttribute") {
		t.Error("ожидается setAttribute в инлайн-скрипте (установка data-theme)")
	}
}

// TestLayout_ThemeToggle проверяет наличие тумблера темы (иконочная кнопка).
func TestLayout_ThemeToggle(t *testing.T) {
	html := renderLayout(t, "Тест")

	if !strings.Contains(html, `aria-label="Сменить тему"`) {
		t.Error("ожидается кнопка тумблера темы с aria-label=\"Сменить тему\"")
	}
	// Кнопка содержит иконки луны и солнца
	if !strings.Contains(html, "ll-icon-moon") {
		t.Error("ожидается иконка луны (ll-icon-moon) для тумблера темы")
	}
	if !strings.Contains(html, "ll-icon-sun") {
		t.Error("ожидается иконка солнца (ll-icon-sun) для тумблера темы")
	}
}

// TestLayout_Header проверяет sticky-шапку и brand-mark.
func TestLayout_Header(t *testing.T) {
	html := renderLayout(t, "LectureLog")

	if !strings.Contains(html, "ll-topbar") {
		t.Error("ожидается шапка с классом ll-topbar")
	}
	if !strings.Contains(html, "ll-brand-mark") {
		t.Error("ожидается brand-mark (логотип)")
	}
	if !strings.Contains(html, "LectureLog") {
		t.Error("ожидается название LectureLog в шапке")
	}
}

// TestLayout_HtmxHeaders проверяет наличие hx-headers на body (хук CSRF для C0-auth).
func TestLayout_HtmxHeaders(t *testing.T) {
	html := renderLayout(t, "Тест")

	if !strings.Contains(html, "hx-headers") {
		t.Error("ожидается hx-headers на body (место под CSRF-токен C0-auth)")
	}
}

// TestRouter_DemoPage проверяет GET / через httptest.
func TestRouter_DemoPage(t *testing.T) {
	router := web.NewRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET / = %d, ожидается 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-theme`) {
		t.Error("GET / должен содержать data-theme в ответе")
	}
	if !strings.Contains(body, "ll-topbar") {
		t.Error("GET / должен содержать шапку ll-topbar")
	}
}

// TestRouter_StaticCSS проверяет отдачу статического CSS через /static/*.
func TestRouter_StaticCSS(t *testing.T) {
	router := web.NewRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /static/css/app.css = %d, ожидается 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type для app.css = %q, ожидается text/css", ct)
	}
}

// TestRouter_StaticHtmx проверяет отдачу htmx через /static/vendor/*.
func TestRouter_StaticHtmx(t *testing.T) {
	router := web.NewRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/vendor/htmx.min.js", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /static/vendor/htmx.min.js = %d, ожидается 200", rec.Code)
	}
}

// TestRouter_WithMount проверяет монтирование дополнительных маршрутов через WithMount.
func TestRouter_WithMount(t *testing.T) {
	router := web.NewRouter(
		web.WithMount(func(r chi.Router) {
			r.Get("/test-mounted", func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusTeapot) // 418 как маркер
			})
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test-mounted", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Errorf("WithMount: /test-mounted = %d, ожидается 418", rec.Code)
	}
}

// TestRouter_WithGlobalMiddleware проверяет применение глобального middleware.
func TestRouter_WithGlobalMiddleware(t *testing.T) {
	const headerName = "X-Test-MW"
	const headerValue = "applied"

	router := web.NewRouter(
		web.WithGlobalMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(headerName, headerValue)
				next.ServeHTTP(w, r)
			})
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(rec, req)

	if rec.Header().Get(headerName) != headerValue {
		t.Errorf("WithGlobalMiddleware: заголовок %q = %q, ожидается %q",
			headerName, rec.Header().Get(headerName), headerValue)
	}
}

// TestRouter_ExistingRoutesUnchanged проверяет, что опции не ломают существующие маршруты.
func TestRouter_ExistingRoutesUnchanged(t *testing.T) {
	// С опциями — существующие маршруты должны работать
	router := web.NewRouter(
		web.WithGlobalMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		}),
	)

	// GET / должен по-прежнему работать
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET / с опциями = %d, ожидается 200", rec.Code)
	}

	// Статика должна работать
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil)
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("GET /static/css/app.css с опциями = %d, ожидается 200", rec2.Code)
	}
}

// ─── Тесты CSRF в layout ─────────────────────────────────────────────────────

// TestLayout_CSRFHeader_Empty проверяет, что пустой токен даёт hx-headers="{}".
// Обратная совместимость: существующие тесты используют пустую LayoutData.
func TestLayout_CSRFHeader_Empty(t *testing.T) {
	html := renderLayout(t, "Тест")

	// Пустой токен → hx-headers="{}" (совместимость с существующими тестами)
	if !strings.Contains(html, `hx-headers="{}"`) {
		t.Error("пустой CSRF-токен: ожидается hx-headers=\"{}\"")
	}
}

// ─── Тесты страницы загрузки ────────────────────────────────────────────────

func TestUploadPage_Segments(t *testing.T) {
	html := renderUploadPage(t)

	if !strings.Contains(html, `data-mode="file"`) || !strings.Contains(html, ">Файл<") {
		t.Error("ожидается сегмент режима Файл")
	}
	if !strings.Contains(html, `data-mode="url"`) || !strings.Contains(html, ">Ссылка<") {
		t.Error("ожидается сегмент режима Ссылка")
	}
}

func TestUploadPage_DropZone(t *testing.T) {
	html := renderUploadPage(t)

	if !strings.Contains(html, `class="ll-upload-drop"`) {
		t.Error("ожидается drop-зона ll-upload-drop")
	}
	if !strings.Contains(html, `type="file"`) {
		t.Error("ожидается input type=file")
	}
	if !strings.Contains(html, "Выбрать файл") {
		t.Error("ожидается кнопка выбора файла")
	}
}

func TestUploadPage_YouTubeForm(t *testing.T) {
	html := renderUploadPage(t)

	if !strings.Contains(html, `hx-post="/upload/youtube"`) {
		t.Error("ожидается htmx-форма POST /upload/youtube")
	}
	if !strings.Contains(html, `name="url"`) {
		t.Error("ожидается поле url")
	}
}

func TestUploadPage_ExtractToggle(t *testing.T) {
	html := renderUploadPage(t)

	if !strings.Contains(html, `name="extract_slides"`) {
		t.Error("ожидается тумблер extract_slides")
	}
	if !strings.Contains(html, `name="has_pdf"`) {
		t.Error("ожидается чекбокс has_pdf")
	}
}

func TestUploadPage_CSRFData(t *testing.T) {
	html := renderUploadPage(t)

	if !strings.Contains(html, `id="ll-upload"`) {
		t.Error("ожидается корневой #ll-upload")
	}
	if !strings.Contains(html, `data-csrf="tok"`) {
		t.Error("ожидается CSRF-токен в data-csrf")
	}
}

func TestUploadPage_ScriptTag(t *testing.T) {
	html := renderUploadPage(t)

	if !strings.Contains(html, `<script src="/static/js/upload.js" defer></script>`) {
		t.Error("ожидается подключение /static/js/upload.js")
	}
}

// TestLayout_CSRFHeader_WithToken проверяет, что непустой токен попадает в hx-headers.
func TestLayout_CSRFHeader_WithToken(t *testing.T) {
	var b bytes.Buffer
	data := web.LayoutData{Title: "Тест", CSRFToken: "test-csrf-token-123"}
	err := web.Layout(data, nil).Render(context.Background(), &b)
	if err != nil {
		t.Fatalf("Layout.Render с CSRF-токеном: %v", err)
	}
	html := b.String()

	// Токен должен появиться в hx-headers
	if !strings.Contains(html, "test-csrf-token-123") {
		t.Error("CSRF-токен не попал в hx-headers")
	}
	if !strings.Contains(html, "X-CSRF-Token") {
		t.Error("ожидается заголовок X-CSRF-Token в hx-headers")
	}
}

// ─── Тесты страницы лекций ────────────────────────────────────────────────────

// renderLecturesPage — вспомогательная функция: рендерит LecturesPage в строку.
func renderLecturesPage(t *testing.T, vms []web.LectureCardVM) string {
	t.Helper()
	var b bytes.Buffer
	data := web.LayoutData{Title: "Мои лекции"}
	err := web.LecturesPage(data, vms).Render(context.Background(), &b)
	if err != nil {
		t.Fatalf("LecturesPage.Render: %v", err)
	}
	return b.String()
}

// renderLectureCard — вспомогательная функция: рендерит LectureCard в строку.
func renderLectureCard(t *testing.T, vm web.LectureCardVM) string {
	t.Helper()
	var b bytes.Buffer
	err := web.LectureCard(vm).Render(context.Background(), &b)
	if err != nil {
		t.Fatalf("LectureCard.Render: %v", err)
	}
	return b.String()
}

// TestLecturesPage_Empty проверяет пустое состояние страницы лекций.
func TestLecturesPage_Empty(t *testing.T) {
	html := renderLecturesPage(t, nil)

	// Пустое состояние должно содержать заголовок и подсказку
	if !strings.Contains(html, "ll-empty") {
		t.Error("пустое состояние: ожидается класс ll-empty")
	}
	// Должна быть ссылка на будущий /upload
	if !strings.Contains(html, "/upload") {
		t.Error("пустое состояние: ожидается ссылка на /upload")
	}
}

// TestLecturesPage_WithCards проверяет, что страница рендерит карточки.
func TestLecturesPage_WithCards(t *testing.T) {
	vms := []web.LectureCardVM{
		{
			ID:          "lec-1",
			Title:       "Алгебра",
			Status:      "ready",
			StatusLabel: "Готово",
			Visibility:  "private",
			SourceKind:  "audio",
			CanPublish:  true,
			UpdatedAt:   "24 июня 2026",
		},
	}
	html := renderLecturesPage(t, vms)

	if !strings.Contains(html, "Алгебра") {
		t.Error("ожидается заголовок лекции 'Алгебра'")
	}
	if !strings.Contains(html, "ll-lec-grid") {
		t.Error("ожидается сетка карточек ll-lec-grid")
	}
}

// TestLectureCard_Status проверяет бейдж статуса и tabular-nums на мете.
func TestLectureCard_Status(t *testing.T) {
	vm := web.LectureCardVM{
		ID:          "lec-1",
		Title:       "Физика квантовая",
		Status:      "processing",
		StatusLabel: "Обработка",
		Visibility:  "private",
		SourceKind:  "video",
		UpdatedAt:   "24 июн. 2026",
	}
	html := renderLectureCard(t, vm)

	// Должен быть бейдж статуса
	if !strings.Contains(html, "Обработка") {
		t.Error("ожидается статус 'Обработка' в карточке")
	}
	// tabular-nums для дат/мета
	if !strings.Contains(html, "ll-lec-meta") {
		t.Error("ожидается класс ll-lec-meta для мета-информации (tabular-nums)")
	}
	// Заголовок через ll-lec-title
	if !strings.Contains(html, "ll-lec-title") {
		t.Error("ожидается класс ll-lec-title для заголовка лекции")
	}
}

// TestLectureCard_Failed проверяет наличие кнопки retry для failed-лекции.
func TestLectureCard_Failed(t *testing.T) {
	vm := web.LectureCardVM{
		ID:          "lec-2",
		Title:       "Лекция с ошибкой",
		Status:      "failed",
		StatusLabel: "Ошибка",
		Visibility:  "private",
		SourceKind:  "audio",
		CanRetry:    true,
		ErrorText:   "Ошибка обработки",
		UpdatedAt:   "24 июн. 2026",
	}
	html := renderLectureCard(t, vm)

	// Кнопка retry для failed
	if !strings.Contains(html, "/retry") {
		t.Error("ожидается htmx-кнопка retry для failed-лекции")
	}
	// Текст ошибки
	if !strings.Contains(html, "Ошибка обработки") {
		t.Error("ожидается текст ошибки в карточке")
	}
}
