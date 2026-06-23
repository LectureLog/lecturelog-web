package coreclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockCore поднимает httptest-сервер, имитирующий ядро строго по схемам OpenAPI.
// Сервер герметичен: ни сети наружу, ни обращения к соседнему репо.
// Через поля-замыкания тесты проверяют то, что ушло на сервер.
type mockCore struct {
	srv *httptest.Server

	// Захваченные данные последнего запроса для ассертов в тестах.
	lastUploadFilename string
	lastTaskForm       map[string]string
	lastTaskCT         string
}

func newMockCore(t *testing.T) *mockCore {
	t.Helper()
	m := &mockCore{lastTaskForm: map[string]string{}}

	mux := http.NewServeMux()

	// POST /api/v1/uploads — application/json {filename} -> 200 UploadUrlResponse.
	mux.HandleFunc("/api/v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("ожидался application/json, получили %q", ct)
		}
		var req UploadUrlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		m.lastUploadFilename = req.Filename

		// Спец-имя для ветки 409 (S3_PUBLIC_ENDPOINT не задан).
		if req.Filename == "trigger409.mp4" {
			writeJSON(w, http.StatusConflict, ErrorResponse{Detail: "S3_PUBLIC_ENDPOINT is not configured"})
			return
		}

		writeJSON(w, http.StatusOK, UploadUrlResponse{
			Key:       "uploads/uuid/" + req.Filename,
			Url:       "http://minio/presigned-put",
			ExpiresIn: 86400,
		})
	})

	// POST /api/v1/tasks — multipart/form-data -> 200 CreateTaskResponse.
	mux.HandleFunc("/api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		m.lastTaskCT = r.Header.Get("Content-Type")
		if !strings.HasPrefix(m.lastTaskCT, "multipart/form-data") {
			t.Errorf("ожидался multipart/form-data, получили %q", m.lastTaskCT)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		m.lastTaskForm = map[string]string{}
		for k, v := range r.MultipartForm.Value {
			if len(v) > 0 {
				m.lastTaskForm[k] = v[0]
			}
		}

		// Ветка 400: нет ни одного источника.
		_, hasS3 := m.lastTaskForm["s3_key"]
		_, hasURL := m.lastTaskForm["video_url"]
		if !hasS3 && !hasURL {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Detail: "no source provided"})
			return
		}

		writeJSON(w, http.StatusOK, CreateTaskResponse{TaskId: "task-123"})
	})

	// GET/DELETE /api/v1/tasks/{task_id}.
	mux.HandleFunc("/api/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
		switch r.Method {
		case http.MethodGet:
			if id == "missing" {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Detail: "Task not found"})
				return
			}
			errMsg := "boom"
			errCode := "internal"
			stage := "transcribe"
			resultPath := "export/output/task-123"
			writeJSON(w, http.StatusOK, TaskStatusResponse{
				TaskId:      id,
				Stage:       &stage,
				ProgressPct: 42,
				Error:       &errMsg,
				ErrorCode:   &errCode,
				ResultPath:  &resultPath,
			})
		case http.MethodDelete:
			// 204 без тела; контракт идемпотентен (повторный DELETE тоже 204).
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	})

	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func newTestClient(t *testing.T, srvURL string) *CoreClient {
	t.Helper()
	c, err := New(Config{BaseURL: srvURL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestCreateUpload(t *testing.T) {
	m := newMockCore(t)
	c := newTestClient(t, m.srv.URL)

	res, err := c.CreateUpload(context.Background(), "file.mp4")
	if err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}
	if m.lastUploadFilename != "file.mp4" {
		t.Errorf("на сервер ушёл filename=%q", m.lastUploadFilename)
	}
	if res.Key != "uploads/uuid/file.mp4" || res.URL != "http://minio/presigned-put" || res.ExpiresIn != 86400 {
		t.Errorf("неверный результат: %+v", res)
	}
}

func TestCreateUpload_Conflict(t *testing.T) {
	m := newMockCore(t)
	c := newTestClient(t, m.srv.URL)

	_, err := c.CreateUpload(context.Background(), "trigger409.mp4")
	if err == nil {
		t.Fatal("ожидалась ошибка на 409")
	}
	if !strings.Contains(err.Error(), "S3_PUBLIC_ENDPOINT") {
		t.Errorf("в ошибке нет detail из ErrorResponse: %v", err)
	}
}

func TestCreateTask_S3Key(t *testing.T) {
	m := newMockCore(t)
	c := newTestClient(t, m.srv.URL)

	id, err := c.CreateTask(context.Background(), CreateTaskParams{
		S3Key: "uploads/u/f.mp4",
		Media: "video",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if id != "task-123" {
		t.Errorf("неверный task_id: %q", id)
	}
	if m.lastTaskForm["s3_key"] != "uploads/u/f.mp4" {
		t.Errorf("s3_key не передан: %v", m.lastTaskForm)
	}
	if m.lastTaskForm["media"] != "video" {
		t.Errorf("media не передан: %v", m.lastTaskForm)
	}
}

func TestCreateTask_VideoURL(t *testing.T) {
	m := newMockCore(t)
	c := newTestClient(t, m.srv.URL)

	id, err := c.CreateTask(context.Background(), CreateTaskParams{
		VideoURL: "https://youtu.be/x",
		NoSlides: true,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if id != "task-123" {
		t.Errorf("неверный task_id: %q", id)
	}
	if m.lastTaskForm["video_url"] != "https://youtu.be/x" {
		t.Errorf("video_url не передан: %v", m.lastTaskForm)
	}
	if m.lastTaskForm["no_slides"] != "true" {
		t.Errorf("no_slides не передан: %v", m.lastTaskForm)
	}
}

func TestCreateTask_NoSource(t *testing.T) {
	m := newMockCore(t)
	c := newTestClient(t, m.srv.URL)

	_, err := c.CreateTask(context.Background(), CreateTaskParams{})
	if err == nil {
		t.Fatal("ожидалась ошибка: не задан источник")
	}
}

func TestGetTaskStatus(t *testing.T) {
	m := newMockCore(t)
	c := newTestClient(t, m.srv.URL)

	st, err := c.GetTaskStatus(context.Background(), "task-123")
	if err != nil {
		t.Fatalf("GetTaskStatus: %v", err)
	}
	if st.TaskID != "task-123" || st.ProgressPct != 42 {
		t.Errorf("неверный статус: %+v", st)
	}
	if st.Error == nil || *st.Error != "boom" {
		t.Errorf("ожидали error=boom, получили %v", st.Error)
	}
	if st.ErrorCode == nil || *st.ErrorCode != "internal" {
		t.Errorf("ожидали error_code=internal, получили %v", st.ErrorCode)
	}
}

func TestGetTaskStatus_NotFound(t *testing.T) {
	m := newMockCore(t)
	c := newTestClient(t, m.srv.URL)

	_, err := c.GetTaskStatus(context.Background(), "missing")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("ожидался ErrTaskNotFound, получили %v", err)
	}
}

func TestDeleteTask(t *testing.T) {
	m := newMockCore(t)
	c := newTestClient(t, m.srv.URL)

	// 204 -> nil. Контракт идемпотентен: повторный вызов тоже даёт 204 -> nil.
	if err := c.DeleteTask(context.Background(), "task-123"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if err := c.DeleteTask(context.Background(), "task-123"); err != nil {
		t.Fatalf("повторный DeleteTask (идемпотентность): %v", err)
	}
}
