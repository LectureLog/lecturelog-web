package web

// Внешние ссылки проекта: репозитории и контакт разработчика.
//
// Держим здесь, а не в internal/config: значения статические и не зависят от
// окружения, а пакет web осознанно не зависит от config (см. web.go).
// Неэкспортируемые — за пределами пакета они не нужны, а тесты живут во внешнем
// пакете web_test и утверждают ожидаемый URL литералом.
const (
	// repoCoreURL — репозиторий ядра обработки лекций.
	repoCoreURL = "https://github.com/LectureLog/lecturelog-core"
	// repoWebURL — репозиторий этого веб-приложения.
	repoWebURL = "https://github.com/LectureLog/lecturelog-web"
	// devTelegramURL — Telegram разработчика.
	devTelegramURL = "https://t.me/fus1ond"
)
