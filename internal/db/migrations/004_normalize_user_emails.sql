-- 004: канонизация users.email перед введением ADMIN_EMAILS.

DO $$
DECLARE
    duplicate_email text;
BEGIN
    IF EXISTS (
        SELECT 1
        FROM users
        WHERE lower(btrim(email)) = ''
    ) THEN
        RAISE EXCEPTION '004_normalize_user_emails: users.email contains empty values after trim';
    END IF;

    SELECT canonical_email
    INTO duplicate_email
    FROM (
        SELECT lower(btrim(email)) AS canonical_email, count(*) AS users_count
        FROM users
        GROUP BY lower(btrim(email))
        HAVING count(*) > 1
        ORDER BY canonical_email
        LIMIT 1
    ) duplicates;

    IF duplicate_email IS NOT NULL THEN
        RAISE EXCEPTION '004_normalize_user_emails: duplicate users.email after canonicalization: %', duplicate_email;
    END IF;

    UPDATE users
    SET email = lower(btrim(email))
    WHERE email <> lower(btrim(email));
END $$;

---- create above / drop below ----

-- Нормализация необратима: исходный регистр и пробелы не восстановить.
SELECT 1;
