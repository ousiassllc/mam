-- +goose Up
-- Persistent background jobs (generation / judge / repair). Persisting them lets the app
-- find unfinished work after an abnormal termination (non-functional.md §3 クラッシュ復旧).
CREATE TABLE job (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('generate', 'judge', 'repair')),
    payload     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    status      TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    attempts    INTEGER NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at  TEXT,
    finished_at TEXT
);
CREATE INDEX idx_job_status ON job (status, created_at);

-- +goose Down
DROP TABLE job;
