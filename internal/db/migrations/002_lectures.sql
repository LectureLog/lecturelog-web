-- 002: лекции — тонкая обёртка над задачей ядра (§3).

CREATE TABLE lectures (
    lecture_id   uuid                 PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     uuid                 NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    core_task_id text,                                  -- текущая задача ядра; при retry перезаписывается (null до создания)
    status       lecture_status       NOT NULL DEFAULT 'processing',
    error_code   text,                                  -- код ошибки ядра при failed (null иначе)
    visibility   lecture_visibility   NOT NULL DEFAULT 'private',
    source_kind  lecture_source_kind  NOT NULL,
    s3_key       text,                                  -- исходник в uploads/ для retry (файл); null для video_url
    video_url    text,                                  -- источник для retry (YouTube); null для файла
    title        text                 NOT NULL,         -- редактируемый, предзаполнен
    created_at   timestamptz          NOT NULL DEFAULT now(),
    updated_at   timestamptz          NOT NULL DEFAULT now(),
    published_at timestamptz                            -- null = не опубликована
);

-- Индекс матча вебхука ядра по core_task_id (§7). Частичный — NULL не индексируем.
CREATE INDEX idx_lectures_core_task_id ON lectures (core_task_id)
    WHERE core_task_id IS NOT NULL;

---- create above / drop below ----

DROP TABLE IF EXISTS lectures;
