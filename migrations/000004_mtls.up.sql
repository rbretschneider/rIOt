-- mTLS: CA storage, device certificates, and bootstrap keys

CREATE TABLE ca_config (
    id          INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    ca_cert_pem TEXT NOT NULL,
    ca_key_pem  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE device_certs (
    id            BIGSERIAL PRIMARY KEY,
    device_id     TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    serial_number TEXT NOT NULL UNIQUE,
    cert_pem      TEXT NOT NULL,
    not_before    TIMESTAMPTZ NOT NULL,
    not_after     TIMESTAMPTZ NOT NULL,
    revoked       BOOLEAN NOT NULL DEFAULT false,
    revoked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE bootstrap_keys (
    key_hash       TEXT PRIMARY KEY,
    label          TEXT NOT NULL DEFAULT '',
    used           BOOLEAN NOT NULL DEFAULT false,
    used_by_device TEXT REFERENCES devices(id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL
);
