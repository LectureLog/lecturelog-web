package coreclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"
)

// ErrTaskNotFound возвращается, когда ядро отвечает 404 на запрос статуса задачи.
var ErrTaskNotFound = errors.New("coreclient: задача не найдена")

// ErrResultURLEmpty возвращается, когда ядро вернуло успешный ответ без ссылки на результат.
var ErrResultURLEmpty = errors.New("coreclient: ядро вернуло пустую ссылку результата")

// ErrCookiesBadFormat — ядро отклонило cookies из-за неверного формата (400).
var ErrCookiesBadFormat = errors.New("coreclient: cookies отклонены: неверный формат")

// ErrCookiesTooLarge — ядро отклонило cookies из-за размера (413).
var ErrCookiesTooLarge = errors.New("coreclient: cookies слишком большие")

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
	S3Key         string    // источник: ключ уже загруженного объекта (uploads/...)
	VideoURL      string    // источник: URL видео (напр. ссылка YouTube)
	Media         string    // опционально: "audio" | "video" (по умолчанию на стороне ядра — audio)
	NoSlides      bool      // опционально: отключить извлечение слайдов
	SlidesName    string    // имя приложенного PDF/PPTX
	SlidesContent io.Reader // содержимое приложенного PDF/PPTX
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
	if (params.SlidesContent == nil) != (params.SlidesName == "") {
		return "", fmt.Errorf("coreclient: slides требуют одновременно имя и содержимое")
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
	if params.SlidesContent != nil {
		part, err := w.CreateFormFile("slides", params.SlidesName)
		if err != nil {
			return "", fmt.Errorf("coreclient: создание части slides: %w", err)
		}
		if _, err := io.Copy(part, params.SlidesContent); err != nil {
			return "", fmt.Errorf("coreclient: запись slides: %w", err)
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

// GetResultURL возвращает временную ссылку на артефакт результата задачи.
func (c *CoreClient) GetResultURL(ctx context.Context, taskID, filename string) (string, error) {
	var params *GetTaskResultUrlApiV1TasksTaskIdResultUrlGetParams
	if filename != "" {
		params = &GetTaskResultUrlApiV1TasksTaskIdResultUrlGetParams{Filename: &filename}
	}
	resp, err := c.api.GetTaskResultUrlApiV1TasksTaskIdResultUrlGetWithResponse(ctx, taskID, params)
	if err != nil {
		return "", fmt.Errorf("coreclient: запрос ссылки результата: %w", err)
	}
	if resp.JSON200 != nil {
		if resp.JSON200.Url == "" {
			return "", ErrResultURLEmpty
		}
		return resp.JSON200.Url, nil
	}
	if resp.StatusCode() == 404 {
		return "", ErrTaskNotFound
	}
	return "", fmt.Errorf("coreclient: неожиданный код от ядра на ссылке результата: %d", resp.StatusCode())
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

// CookieStatus — доменный статус YouTube-cookies в ядре (без содержимого).
type CookieStatus struct {
	Exists    bool
	Size      int
	UpdatedAt string // RFC3339 или пусто
}

// GetYouTubeCookieStatus запрашивает статус cookies (GET /youtube/cookies).
func (c *CoreClient) GetYouTubeCookieStatus(ctx context.Context) (CookieStatus, error) {
	resp, err := c.api.GetYoutubeCookiesApiV1YoutubeCookiesGetWithResponse(ctx)
	if err != nil {
		return CookieStatus{}, fmt.Errorf("coreclient: статус cookies: %w", err)
	}
	if resp.JSON200 != nil {
		return cookieStatusFrom(resp.JSON200), nil
	}
	return CookieStatus{}, fmt.Errorf("coreclient: неожиданный код на статус cookies: %d", resp.StatusCode())
}

// PutYouTubeCookies загружает cookies.txt в ядро multipart-запросом (поле file).
// Тело собирается вручную (multipart), но отправляется через сгенерированный
// …WithBodyWithResponse — ровно как CreateTask, без сырого http.
func (c *CoreClient) PutYouTubeCookies(ctx context.Context, content []byte) (CookieStatus, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "cookies.txt")
	if err != nil {
		return CookieStatus{}, fmt.Errorf("coreclient: multipart cookies: %w", err)
	}
	if _, err := fw.Write(content); err != nil {
		return CookieStatus{}, fmt.Errorf("coreclient: запись cookies: %w", err)
	}
	if err := mw.Close(); err != nil {
		return CookieStatus{}, fmt.Errorf("coreclient: закрытие multipart: %w", err)
	}

	resp, err := c.api.PutYoutubeCookiesApiV1YoutubeCookiesPutWithBodyWithResponse(
		ctx, mw.FormDataContentType(), &buf,
	)
	if err != nil {
		return CookieStatus{}, fmt.Errorf("coreclient: загрузка cookies: %w", err)
	}
	if resp.JSON200 != nil {
		return cookieStatusFrom(resp.JSON200), nil
	}
	switch resp.StatusCode() {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return CookieStatus{}, ErrCookiesBadFormat
	case http.StatusRequestEntityTooLarge:
		return CookieStatus{}, ErrCookiesTooLarge
	}
	return CookieStatus{}, fmt.Errorf("coreclient: неожиданный код на загрузку cookies: %d", resp.StatusCode())
}

// DeleteYouTubeCookies удаляет cookies из ядра (DELETE /youtube/cookies).
// Ядро отдаёт 204 без тела — возвращаем пустой статус (exists=false).
func (c *CoreClient) DeleteYouTubeCookies(ctx context.Context) (CookieStatus, error) {
	resp, err := c.api.DeleteYoutubeCookiesApiV1YoutubeCookiesDeleteWithResponse(ctx)
	if err != nil {
		return CookieStatus{}, fmt.Errorf("coreclient: удаление cookies: %w", err)
	}
	if resp.StatusCode() == http.StatusNoContent || resp.StatusCode() == http.StatusOK {
		return CookieStatus{Exists: false}, nil
	}
	return CookieStatus{}, fmt.Errorf("coreclient: неожиданный код на удаление cookies: %d", resp.StatusCode())
}

// cookieStatusFrom маппит сгенерированную схему в доменный CookieStatus.
func cookieStatusFrom(r *CookieStatusResponse) CookieStatus {
	s := CookieStatus{Exists: r.Exists, Size: r.Size}
	if r.UpdatedAt != nil {
		s.UpdatedAt = r.UpdatedAt.Format(time.RFC3339)
	}
	return s
}
