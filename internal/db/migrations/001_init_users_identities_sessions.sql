-- 001: базовые типы, перечисления и идентичность пользователей платформы.

CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- gen_random_uuid() на PG < 13 (страховка)

-- Перечисления лекций (нативные enum — строгая типизация, §3).
CREATE TYPE lecture_status      AS ENUM ('processing', 'ready', 'failed');
CREATE TYPE lecture_visibility  AS ENUM ('private', 'public');
CREATE TYPE lecture_source_kind AS ENUM ('audio', 'video', 'video_url');

-- users: наш пользователь, независим от провайдера; email = личность (§3).
CREATE TABLE users (
    user_id    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email      text        NOT NULL UNIQUE,             -- канонический email, матч входа по нему
    name       text,                                    -- отображаемое имя из OAuth
    avatar_url text,                                    -- аватар из OAuth
    created_at timestamptz NOT NULL DEFAULT now()
);

-- identities: связь провайдер↔наш user. Email здесь НЕ хранится (политика §3).
CREATE TABLE identities (
    provider     text        NOT NULL,                  -- напр. 'google'
    provider_sub text        NOT NULL,                  -- стабильный ID от провайдера
    user_id      uuid        NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_sub)                -- уникальность (provider, provider_sub) = §3
);
CREATE INDEX idx_identities_user_id ON identities (user_id);

-- sessions: серверные сессии в Postgres (кука session_id → лукап, §3).
CREATE TABLE sessions (
    session_id uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);  -- чистка протухших

---- create above / drop below ----

DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS identities;
DROP TABLE IF EXISTS users;
DROP TYPE IF EXISTS lecture_source_kind;
DROP TYPE IF EXISTS lecture_visibility;
DROP TYPE IF EXISTS lecture_status;
-- pgcrypto не дропаем (может использоваться другими).
