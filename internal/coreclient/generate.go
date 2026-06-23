package coreclient

// Генерация типов и клиента из вендоренной спеки ядра.
// Запуск: go generate ./internal/coreclient/...
// Шаг 1: нормализация спеки (OpenAPI 3.1.0 nullable через anyOf[type,null] ->
//        3.0-совместимый nullable), см. scripts/normalize_openapi.py.
//        oapi-codegen v2.7.x не понимает 3.1.0-nullable напрямую.
// Шаг 2: генерация типов и клиента из нормализованной спеки.
// openapi.normalized.json — промежуточный артефакт, в репозиторий не коммитится.
//go:generate python3 ../../scripts/normalize_openapi.py openapi.json openapi.normalized.json
//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi.normalized.json
