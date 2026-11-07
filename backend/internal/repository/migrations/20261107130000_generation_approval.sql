-- +goose Up
-- The generation approval of the AUTO pipeline (Issue #1108, spec/spec.md §4.3, spec/architecture/er.md §4): before any Reference Pack, Keyframe or
-- Video is made, the user approves what will be made and what it is estimated to cost. generation_approval is what the approval card of
-- chat_message (ref_type = 'generation') stands for: the scope that was shown (a JSON list of the planned Shots with their estimates). generation_launch
-- is what the Orchestrator started after it: one row per Shot and stage, taken before the start so that the same stage of a Shot is never started twice,
-- whatever the number of times the approval is looked at and across restarts.
CREATE TABLE generation_approval (
    message_id       TEXT PRIMARY KEY REFERENCES chat_message (id) ON DELETE CASCADE,
    project_id       TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    fingerprint      TEXT NOT NULL,
    scope            TEXT NOT NULL CHECK (json_valid(scope)),
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_generation_approval_project ON generation_approval (project_id);

CREATE TABLE generation_launch (
    shot_id    TEXT NOT NULL REFERENCES shot (id) ON DELETE CASCADE,
    stage      TEXT NOT NULL CHECK (stage IN ('reference_pack', 'generate')),
    message_id TEXT NOT NULL REFERENCES generation_approval (message_id) ON DELETE CASCADE,
    job_id     TEXT,
    outcome    TEXT NOT NULL DEFAULT 'started' CHECK (outcome IN ('started', 'budget', 'failed')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (shot_id, stage)
);

-- +goose Down
DROP TABLE generation_launch;
DROP TABLE generation_approval;
