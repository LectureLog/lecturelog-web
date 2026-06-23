# Makefile платформы LectureLog (web).
# Цели обёрнуты вокруг стандартного go-тулчейна и генерации coreclient из спеки ядра.

.PHONY: generate build vet test gate sync-spec

# Генерация coreclient: нормализация спеки (3.1.0 -> 3.0-nullable) + oapi-codegen.
# Сам процесс описан в //go:generate директивах internal/coreclient/generate.go.
generate:
	go generate ./...

# Сборка всего модуля. Перед сборкой обязательна генерация gen.go из спеки.
build: generate
	go build ./...

# Статический анализ.
vet:
	go vet ./...

# Прогон тестов (юнит + контрактный smoke к замоканному ядру).
test:
	go test ./...

# GATE B / слой 1 приёмки платформы: генерация + сборка + vet + тесты.
gate: generate
	go build ./... && go vet ./... && go test ./...

# Обновить вендоренную копию спеки ядра из соседнего репозитория core.
# ВАЖНО: после sync-spec обязательно `make generate` и ревью diff в internal/coreclient/gen.go,
# чтобы поймать рассинхрон контракта.
sync-spec:
	cp ../lecturelog-core/docs/openapi.json internal/coreclient/openapi.json
