-- 005: расширение перечисления lecture_visibility значением 'unlisted' («По ссылке»).

ALTER TYPE lecture_visibility ADD VALUE IF NOT EXISTS 'unlisted';

---- create above / drop below ----

-- Удаление значения из ENUM в PostgreSQL не поддерживается напрямую.
SELECT 1;
