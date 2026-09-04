CREATE TABLE IF NOT EXISTS external_identities (
    id             BIGSERIAL PRIMARY KEY,
    issuer         TEXT NOT NULL,
    subject        TEXT NOT NULL,
    email          TEXT,
    email_verified BOOLEAN,
    first_login_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT external_identities_issuer_subject_key UNIQUE (issuer, subject)
);
