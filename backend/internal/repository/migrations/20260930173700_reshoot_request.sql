-- +goose Up
-- Editor → Reshoot Loop (F-19, spec/spec.md §17.2): an additional Shot the Editor Judge asked the
-- Director for when the material is not enough. The Shot is an ordinary shot row; this table keeps
-- why it exists and where in the cut it goes, so that once it has a Selected Take it is put into
-- the Edit Timeline of the Branch that asked (status 'applied') and the Retry Policy can count the
-- rounds (every request-additional-shot call is one round of the Project / Branch).
CREATE TABLE reshoot_request (
    id                               TEXT PRIMARY KEY,
    project_id                       TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    branch_id                        TEXT REFERENCES branch (id) ON DELETE CASCADE,
    edit_timeline_version_id         TEXT NOT NULL REFERENCES edit_timeline_version (id) ON DELETE CASCADE,
    round                            INTEGER NOT NULL CHECK (round >= 1),
    shot_id                          TEXT NOT NULL REFERENCES shot (id) ON DELETE CASCADE,
    after_shot_id                    TEXT REFERENCES shot (id) ON DELETE SET NULL,
    kind                             TEXT NOT NULL CHECK (kind IN ('insert', 'reaction')),
    objective                        TEXT NOT NULL,
    duration_seconds                 REAL NOT NULL CHECK (duration_seconds > 0),
    status                           TEXT NOT NULL DEFAULT 'requested' CHECK (status IN ('requested', 'applied')),
    applied_edit_timeline_version_id TEXT REFERENCES edit_timeline_version (id) ON DELETE SET NULL,
    created_at                       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_reshoot_request_project ON reshoot_request (project_id, branch_id, round);
CREATE INDEX idx_reshoot_request_status ON reshoot_request (status);

-- +goose Down
DROP INDEX idx_reshoot_request_status;
DROP INDEX idx_reshoot_request_project;
DROP TABLE reshoot_request;
