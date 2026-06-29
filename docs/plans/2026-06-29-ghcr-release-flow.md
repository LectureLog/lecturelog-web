# GHCR Release Flow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Настроить для `lecturelog-web` и `lecturelog-core` единый dev/stable процесс: ветка `dev` публикует Docker image `:dev`, git-теги `v*` публикуют `:latest` и версионный тег, VPS обновляется через `docker compose pull && docker compose up -d`.

**Architecture:** Каждый репозиторий сам собирает и публикует свой GHCR-образ через GitHub Actions. Production/VPS compose-файлы больше не собирают код на сервере, а тянут готовые images с тегом из переменной окружения. Документация фиксирует: `dev` — быстрый канал проверки, `main` — стабильный код, `latest` — Docker-тег, релиз — git tag.

**Tech Stack:** GitHub Actions, Docker Buildx, GHCR, Docker Compose v2, Go 1.25, Python 3.12, pytest, ruff.

---

### Task 1: Зафиксировать deploy-модель в документации

**Files:**
- Modify: `lecturelog-web/docs/WORKFLOW.md`
- Modify: `lecturelog-web/README.md`
- Modify: `lecturelog-core/README.md`

**Step 1:** Обновить раздел веток: заменить модель `integration -> main` на `dev -> main -> tag`.

**Step 2:** Описать каналы образов:
- `:dev` собирается при push в `dev`;
- `:latest` и `:vX.Y.Z` собираются при push git tag `v*`;
- `latest` не является git-веткой.

**Step 3:** Добавить короткие команды:
```bash
docker compose pull
docker compose up -d
```

**Step 4:** Проверить, что README не обещает сборку на VPS как основной сценарий.

### Task 2: Добавить web workflow публикации образов

**Files:**
- Create: `lecturelog-web/.github/workflows/docker-publish.yml`

**Step 1:** Создать workflow на `push.branches: [dev]`, `push.tags: ["v*"]`, `workflow_dispatch`.

**Step 2:** Перед сборкой запустить gate web:
```bash
go generate ./...
go build ./...
go vet ./...
go test ./...
```

**Step 3:** Публиковать `ghcr.io/lecturelog/lecturelog-web:dev` для ветки `dev`.

**Step 4:** Публиковать `ghcr.io/lecturelog/lecturelog-web:latest` и `ghcr.io/lecturelog/lecturelog-web:<tag>` для git tag `v*`.

**Step 5:** Для tag-событий создать GitHub Release с generated notes.

### Task 3: Добавить core workflow публикации образов

**Files:**
- Create: `lecturelog-core/.github/workflows/docker-publish.yml`
- Keep: `lecturelog-core/.github/workflows/ci.yml`

**Step 1:** Создать workflow на `push.branches: [dev]`, `push.tags: ["v*"]`, `workflow_dispatch`.

**Step 2:** Перед сборкой запустить core-проверки:
```bash
ruff check .
ruff format --check .
pytest -q
python scripts/export_openapi.py
git diff --exit-code docs/openapi.json
```

**Step 3:** Публиковать `ghcr.io/lecturelog/lecturelog-core:dev` для ветки `dev`.

**Step 4:** Публиковать `ghcr.io/lecturelog/lecturelog-core:latest` и `ghcr.io/lecturelog/lecturelog-core:<tag>` для git tag `v*`.

**Step 5:** Для tag-событий создать GitHub Release с generated notes.

### Task 4: Добавить VPS deploy templates на готовых images

**Files:**
- Create: `lecturelog-web/deploy/compose.vps.yml`
- Create: `lecturelog-web/deploy/env.web.example`
- Create: `lecturelog-core/deploy/compose.vps.yml`
- Create: `lecturelog-core/deploy/env.core.example`

**Step 1:** В web compose использовать:
```yaml
image: ghcr.io/lecturelog/lecturelog-web:${LECTURELOG_WEB_IMAGE_TAG:-dev}
```

**Step 2:** В core compose использовать:
```yaml
image: ghcr.io/lecturelog/lecturelog-core:${LECTURELOG_CORE_IMAGE_TAG:-dev}
```

**Step 3:** Оставить Postgres/MinIO сервисы рядом с приложениями, но не использовать `build:`.

**Step 4:** Пробросить только локальные порты `127.0.0.1`, чтобы публичный доступ шёл через reverse proxy.

### Task 5: Проверить результат

**Files:**
- All changed files in both repos.

**Step 1:** Проверить YAML парсинг workflows и compose через Python `yaml.safe_load`.

**Step 2:** Проверить docker compose config:
```bash
docker compose -f deploy/compose.vps.yml config
```

**Step 3:** Запустить web tests:
```bash
GOCACHE=/tmp/go-build go test ./...
```

**Step 4:** Запустить core tests:
```bash
.venv/bin/pytest -q
```

**Step 5:** Просмотреть diff обоих репозиториев и убедиться, что нет секретов и нет упоминаний авторства ассистента.
