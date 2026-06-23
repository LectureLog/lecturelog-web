package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// staticFiles — встроенная файловая система статики.
// Содержит: static/css/app.css (Tailwind), static/vendor/htmx.min.js.
//
//go:embed static
var staticFiles embed.FS

// staticFS возвращает sub-FS начиная с "static" для хендлера /static/*.
func staticFS() (http.FileSystem, error) {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}
