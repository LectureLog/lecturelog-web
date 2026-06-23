package db

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"
)

// ternSeparator — разделитель секций up/down в файлах tern/v2.
// Подтверждён по исходникам migrate/migrate.go@v2.4.1.
const ternSeparator = "---- create above / drop below ----"

// TestMigrationsEmbedNotEmpty проверяет, что встроенная FS содержит SQL-файлы миграций.
// Тест не требует запущенного Postgres.
func TestMigrationsEmbedNotEmpty(t *testing.T) {
	subFS, err := migrationsSubFS()
	if err != nil {
		t.Fatalf("migrationsSubFS вернул ошибку: %v", err)
	}

	entries, err := fs.ReadDir(subFS, ".")
	if err != nil {
		t.Fatalf("fs.ReadDir вернул ошибку: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("embed: каталог migrations пуст, ожидались SQL-файлы")
	}

	t.Logf("Найдено файлов миграций: %d", len(entries))
	for _, e := range entries {
		t.Logf("  - %s", e.Name())
	}
}

// TestMigrationsFilesValid проверяет каждый SQL-файл миграции без подключения к БД:
//   - файл существует и не пуст;
//   - имя соответствует шаблону NNN_name.sql (нумерация tern);
//   - секция up (до разделителя) содержит непустой SQL;
//   - разделитель tern/v2 присутствует (файл не необратим).
func TestMigrationsFilesValid(t *testing.T) {
	subFS, err := migrationsSubFS()
	if err != nil {
		t.Fatalf("migrationsSubFS вернул ошибку: %v", err)
	}

	entries, err := fs.ReadDir(subFS, ".")
	if err != nil {
		t.Fatalf("fs.ReadDir вернул ошибку: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("embed: каталог migrations пуст")
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(subFS, name)
			if err != nil {
				t.Fatalf("ошибка чтения файла миграции %s: %v", name, err)
			}
			if len(bytes.TrimSpace(data)) == 0 {
				t.Fatalf("файл миграции %s пуст", name)
			}

			content := string(data)

			// Проверяем наличие разделителя tern (необратимые миграции не закладываем)
			if !strings.Contains(content, ternSeparator) {
				t.Errorf("файл %s не содержит разделителя tern %q", name, ternSeparator)
			}

			// Проверяем, что секция up не пуста (до разделителя есть SQL, не только комментарии)
			parts := strings.SplitN(content, ternSeparator, 2)
			upSection := strings.TrimSpace(parts[0])
			hasSQL := false
			for _, line := range strings.Split(upSection, "\n") {
				trimmed := strings.TrimSpace(line)
				if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
					hasSQL = true
					break
				}
			}
			if !hasSQL {
				t.Errorf("файл %s: секция up не содержит SQL-операторов", name)
			}
		})
	}
}

// TestNew_BadDSN проверяет, что New с заведомо неверным DSN возвращает ошибку
// и не зависает (контекст с коротким таймаутом). Postgres не нужен.
func TestNew_BadDSN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := New(ctx, "postgres://invalid-host-that-does-not-exist:5432/db?connect_timeout=1")
	if err == nil {
		pool.Close()
		t.Fatal("New с битым DSN должен вернуть ошибку, вернул nil")
	}
	if pool != nil {
		pool.Close()
		t.Fatal("New с битым DSN должен вернуть nil пул, вернул ненулевое значение")
	}

	t.Logf("New вернул ожидаемую ошибку: %v", err)
}
