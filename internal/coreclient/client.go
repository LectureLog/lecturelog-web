package coreclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"strconv"
)

// ErrTaskNotFound возвращается, когда ядро отвечает 404 на запрос статуса задачи.
var ErrTaskNotFound = errors.New("coreclient: задача не найдена")

// UploadResult — доменный результат presigned-PUT (POST /uploads).
type UploadResult struct {
	Key       string // ключ объекта в S3 (начинается с uploads/)
	URL       string // presigned PUT-URL для прямой загрузки контента
	ExpiresIn int    // срок жизни URL в секундах
}

// CreateTaskParams — параметры создания задачи (POST /tasks, multipart/form-data).
// Должен быть задан РОВНО ОДИН источник: S3Key ИЛИ VideoURL. Сценарии B1
// покрывают именно эти два источника (audio/video-файлы напрямую — вне scope).
type CreateTaskParams struct {
	S3Key    string // источник: ключ уже загруженного объекта (uploads/...)
	VideoURL string // источник: URL видео (напр. ссылка YouTube)
	Media    string // опционально: "audio" | "video" (по умолчанию на стороне ядра — audio)
	NoSlides bool   // опционально: отключить извлечение слайдов
}

// CreateUpload запрашивает у ядра presigned-PUT URL для загрузки контента.
// Тело запроса — application/json {filename}.
func (c *CoreClient) CreateUpload(ctx context.Context, filename string) (UploadResult, error) {
	resp, err := c.api.CreateUploadUrlApiV1UploadsPostWithResponse(ctx, UploadUrlRequest{Filename: filename})
	if err != nil {
		return UploadResult{}, fmt.Errorf("coreclient: запрос presigned-URL: %w", err)
	}

	if resp.JSON200 != nil {
		return UploadResult{
			Key:       resp.JSON200.Key,
			URL:       resp.JSON200.Url,
			ExpiresIn: resp.JSON200.ExpiresIn,
		}, nil
	}

	// Ошибочные ветки: detail из ErrorResponse (400/409) либо общий код.
	if resp.JSON400 != nil {
		return UploadResult{}, fmt.Errorf("coreclient: presigned-URL отклонён (400): %s", resp.JSON400.Detail)
	}
	if resp.JSON409 != nil {
		return UploadResult{}, fmt.Errorf("coreclient: presigned-URL недоступен (409): %s", resp.JSON409.Detail)
	}
	return UploadResult{}, fmt.Errorf("coreclient: неожиданный код от ядра на /uploads: %d", resp.StatusCode())
}

// CreateTask создаёт задачу обработки в ядре. Тело — multipart/form-data,
// которое собирается здесь вручную (oapi-codegen для multipart даёт только
// сырой …WithBodyWithResponse). Возвращает идентификатор задачи.
func (c *CoreClient) CreateTask(ctx context.Context, params CreateTaskParams) (string, error) {
	// Лёгкая клиентская защита: ровно один источник. Финальную валидацию
	// (формат s3_key, наличие "..") делает ядро (ответит 400).
	hasS3 := params.S3Key != ""
	hasURL := params.VideoURL != ""
	if hasS3 == hasURL {
		return "", fmt.Errorf("coreclient: нужен ровно один источник (s3_key ИЛИ video_url)")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if hasS3 {
		if err := w.WriteField("s3_key", params.S3Key); err != nil {
			return "", fmt.Errorf("coreclient: запись s3_key: %w", err)
		}
	} else {
		if err := w.WriteField("video_url", params.VideoURL); err != nil {
			return "", fmt.Errorf("coreclient: запись video_url: %w", err)
		}
	}
	if params.Media != "" {
		if err := w.WriteField("media", params.Media); err != nil {
			return "", fmt.Errorf("coreclient: запись media: %w", err)
		}
	}
	if params.NoSlides {
		if err := w.WriteField("no_slides", strconv.FormatBool(params.NoSlides)); err != nil {
			return "", fmt.Errorf("coreclient: запись no_slides: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("coreclient: закрытие multipart: %w", err)
	}

	// FormDataContentType() содержит boundary — передаётся как Content-Type.
	resp, err := c.api.CreateTaskApiV1TasksPostWithBodyWithResponse(ctx, w.FormDataContentType(), &buf)
	if err != nil {
		return "", fmt.Errorf("coreclient: создание задачи: %w", err)
	}

	if resp.JSON200 != nil {
		return resp.JSON200.TaskId, nil
	}
	if resp.JSON400 != nil {
		return "", fmt.Errorf("coreclient: задача отклонена (400): %s", resp.JSON400.Detail)
	}
	return "", fmt.Errorf("coreclient: неожиданный код от ядра на /tasks: %d", resp.StatusCode())
}

// TaskStatus — доменное отражение TaskStatusResponse.
// Error/ErrorCode/Stage/ResultPath — указатели (nullable в контракте).
type TaskStatus struct {
	TaskID      string
	Stage       *string
	ProgressPct int
	Error       *string
	ErrorCode   *string
	ResultPath  *string
}

// GetTaskStatus возвращает статус задачи. На 404 возвращает ErrTaskNotFound.
func (c *CoreClient) GetTaskStatus(ctx context.Context, taskID string) (TaskStatus, error) {
	resp, err := c.api.GetTaskStatusApiV1TasksTaskIdGetWithResponse(ctx, taskID)
	if err != nil {
		return TaskStatus{}, fmt.Errorf("coreclient: запрос статуса задачи: %w", err)
	}

	if resp.JSON200 != nil {
		s := resp.JSON200
		return TaskStatus{
			TaskID:      s.TaskId,
			Stage:       s.Stage,
			ProgressPct: s.ProgressPct,
			Error:       s.Error,
			ErrorCode:   s.ErrorCode,
			ResultPath:  s.ResultPath,
		}, nil
	}
	if resp.JSON404 != nil {
		return TaskStatus{}, ErrTaskNotFound
	}
	return TaskStatus{}, fmt.Errorf("coreclient: неожиданный код от ядра на статусе: %d", resp.StatusCode())
}

// DeleteTask удаляет задачу. Контракт идемпотентен: успех — 204 (в т.ч. на
// повторном удалении уже удалённой задачи) -> nil.
func (c *CoreClient) DeleteTask(ctx context.Context, taskID string) error {
	resp, err := c.api.DeleteTaskApiV1TasksTaskIdDeleteWithResponse(ctx, taskID)
	if err != nil {
		return fmt.Errorf("coreclient: удаление задачи: %w", err)
	}

	if resp.StatusCode() == 204 {
		return nil
	}
	return fmt.Errorf("coreclient: неожиданный код от ядра на удалении: %d", resp.StatusCode())
}
