-- +goose Up
-- Issue #1115 (spec/spec.md §4.3): AUTO starts the generation by itself, with no approval card. generation_launch is the claim of a stage of a Shot
-- (never started twice, across looks and restarts); it no longer stands for an approval, so message_id (NOT NULL, FK to generation_approval) goes.
-- SQLite cannot drop a NOT NULL column with a foreign key, so the table is rebuilt; every claim is kept. generation_approval stays as a read-only record
-- of the cards of v0.1.34: the Orchestrator reads the scope of a declined card, and nothing writes it any more.
CREATE TABLE generation_launch_new (
    shot_id    TEXT NOT NULL REFERENCES shot (id) ON DELETE CASCADE,
    stage      TEXT NOT NULL CHECK (stage IN ('reference_pack', 'generate')),
    job_id     TEXT,
    outcome    TEXT NOT NULL DEFAULT 'started' CHECK (outcome IN ('started', 'budget', 'failed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (shot_id, stage)
);
INSERT INTO generation_launch_new (shot_id, stage, job_id, outcome, created_at)
    SELECT shot_id, stage, job_id, outcome, created_at FROM generation_launch;
DROP TABLE generation_launch;
ALTER TABLE generation_launch_new RENAME TO generation_launch;

-- +goose Down
-- A claim has no approval card to point at any more, so the claims are not kept: they are runtime state, and the Orchestrator of the older version
-- asks for its approval again before it starts anything.
DROP TABLE generation_launch;
CREATE TABLE generation_launch (
    shot_id    TEXT NOT NULL REFERENCES shot (id) ON DELETE CASCADE,
    stage      TEXT NOT NULL CHECK (stage IN ('reference_pack', 'generate')),
    message_id TEXT NOT NULL REFERENCES generation_approval (message_id) ON DELETE CASCADE,
    job_id     TEXT,
    outcome    TEXT NOT NULL DEFAULT 'started' CHECK (outcome IN ('started', 'budget', 'failed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (shot_id, stage)
);
