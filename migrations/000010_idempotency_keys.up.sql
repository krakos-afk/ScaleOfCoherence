-- Idempotency keys for mutating item requests. A client that retries with the
-- same key (and the same request body) gets the original response replayed
-- instead of a second write. Keys are scoped per (org, user) so one caller's
-- keys can never collide with or observe another's.
--
-- Rows are created when a request starts (status_code NULL = in progress) and
-- updated with the response when it finishes. The app deletes finished and
-- abandoned rows older than 24h; nothing here needs a permanent history.
CREATE TABLE idempotency_keys (
    org_id        UUID        NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key           TEXT        NOT NULL,
    request_hash  TEXT        NOT NULL,
    status_code   INT,
    response_body BYTEA,
    content_type  TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    PRIMARY KEY (org_id, user_id, key)
);

-- Cleanup scans by age.
CREATE INDEX idx_idempotency_keys_created_at ON idempotency_keys (created_at);
