-- 003: индекс под витрину публичных лекций (§8 hub). Долг C1 из HANDOFF-C0.
CREATE INDEX idx_lectures_public_published_at
    ON lectures (published_at DESC)
    WHERE visibility = 'public';

---- create above / drop below ----

DROP INDEX IF EXISTS idx_lectures_public_published_at;
