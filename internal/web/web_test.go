package web_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LectureLog/lecturelog-web/internal/web"
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
