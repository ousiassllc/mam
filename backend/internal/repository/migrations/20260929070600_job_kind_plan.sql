-- +goose Up
-- Adds the 'plan' job kind: the planning stages of the AUTO pipeline (Producer -> Writer ->
-- Director -> Storyboard) run as a persisted background job so an interrupted run is resumed
-- after an abnormal termination. SQLite cannot alter a CHECK constraint, so the table is rebuilt.
CREATE TABLE job_new (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('generate', 'judge', 'repair', 'plan')),
    payload     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    status      TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    attempts    INTEGER NOT NULL DEFAULT 0,
    error       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    started_at  TEXT,
    finished_at TEXT
);
INSERT INTO job_new (id, kind, payload, status, attempts, error, created_at, started_at, finished_at)
    SELECT id, kind, payload, status, attempts, error, created_at, started_at, finished_at FROM job;
DROP TABLE job;
ALTER TABLE job_new RENAME TO job;
CREATE INDEX idx_job_status ON job (status, created_at);

-- +goose Down
DELETE FROM job WHERE kind = 'plan';
CREATE TABLE job_old (
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
INSERT INTO job_old (id, kind, payload, status, attempts, error, created_at, started_at, finished_at)
    SELECT id, kind, payload, status, attempts, error, created_at, started_at, finished_at FROM job;
DROP TABLE job;
ALTER TABLE job_old RENAME TO job;
CREATE INDEX idx_job_status ON job (status, created_at);
