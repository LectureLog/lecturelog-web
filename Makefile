# Makefile платформы LectureLog (web).
# Цели обёрнуты вокруг стандартного go-тулчейна и генерации coreclient из спеки ядра.

.PHONY: generate build vet test gate sync-spec migrate-test templ tailwind-bin tailwind web-gen gen-check up up-stub down dev watch prod prod-down

# ─── Toolchain web-слоя ────────────────────────────────────────────────────────

# Версия Tailwind CLI (standalone, без node). Менять здесь при обновлении.
TAILWIND_VERSION := 4.1.10
TAILWIND_BIN := ./bin/tailwindcss

# Генерация templ-компонентов (через tool-директиву go.mod).
# Запускаем из каталога пакета, чтобы FileName в *_templ.go был коротким ("layout.templ"),
# совпадая с выводом go generate ./... → однозначный детерминированный артефакт.
templ:
	cd internal/web && go tool templ generate

# Скачивание Tailwind standalone CLI в ./bin/ (gitignored).
# Бинарь нужен только при пересборке CSS; сам app.css коммитится.
tailwind-bin:
	@mkdir -p ./bin
	@if [ ! -f $(TAILWIND_BIN) ]; then \
		echo "Скачиваю Tailwind CLI v$(TAILWIND_VERSION)..."; \
		curl -fsSL \
			"https://github.com/tailwindlabs/tailwindcss/releases/download/v$(TAILWIND_VERSION)/tailwindcss-linux-x64" \
			-o $(TAILWIND_BIN); \
		chmod +x $(TAILWIND_BIN); \
		echo "Tailwind CLI скачан: $(TAILWIND_BIN)"; \
	else \
		echo "Tailwind CLI уже есть: $(TAILWIND_BIN)"; \
	fi

# Сборка CSS: токены + Tailwind utility-классы → static/css/app.css.
# Tailwind v4 использует CSS-first конфиг (@theme в tailwind.css), не tailwind.config.js.
# Запускается только при пересборке; собранный app.css коммитится.
tailwind: tailwind-bin
	$(TAILWIND_BIN) \
		-i internal/web/assets/tailwind.css \
		-o internal/web/static/css/app.css \
		--minify

# web-gen: полная генерация web-слоя (templ + Tailwind).
web-gen: templ tailwind

# gen-check: проверяет, что генерация детерминирована (артефакты не изменились).
# Используется в CI / gate для проверки синхронности исходников и артефактов.
gen-check: web-gen
	git diff --exit-code -- internal/web/

# ─── Стандартные цели ──────────────────────────────────────────────────────────

# Поднять только локальный Postgres платформы. Ядро и его MinIO запускаются отдельно.
up:
	docker compose up -d postgres

# Поднять Postgres и MinIO-заглушку ядра для разработки без реального ядра.
up-stub:
	docker compose --profile stub-minio up -d

# Остановить локальные сервисы платформы.
down:
	docker compose down

# Запустить сервер; .env подгружается точкой входа автоматически.
dev: up
	go run ./cmd/server

# Горячая перезагрузка Go-кода. Если air не установлен, запускается через go run
# без добавления инструмента разработки в go.mod. templ и CSS генерируются вручную.
watch:
	@if command -v air >/dev/null 2>&1; then \
		air; \
	else \
		go run github.com/air-verse/air@latest; \
	fi

# Собрать и поднять production-окружение одной командой: web + Postgres.
prod:
	docker compose -f docker-compose.prod.yml up -d --build

# Остановить production-окружение.
prod-down:
	docker compose -f docker-compose.prod.yml down

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

# GATE C0: web-генерация (templ+tailwind) + сборка + vet + тесты.
gate: web-gen generate
	go build ./... && go vet ./... && go test ./...

# GATE C0: интеграционная проверка применения DDL-миграций против реального Postgres
# (поднимается через testcontainers; требует запущенного Docker).
migrate-test:
	go test -tags=integration ./internal/db/...

# Обновить вендоренную копию спеки ядра из соседнего репозитория core.
# ВАЖНО: после sync-spec обязательно `make generate` и ревью diff в internal/coreclient/gen.go,
# чтобы поймать рассинхрон контракта.
sync-spec:
	cp ../lecturelog-core/docs/openapi.json internal/coreclient/openapi.json
