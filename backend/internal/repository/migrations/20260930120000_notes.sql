-- +goose Up
-- Notes (spec/spec.md §9.4, F-31, M2): the free-text production memos that Writer / Director / Prompt
-- Planner read as context. Not a Source of Truth (§29.2).
--   note            a memo of a Project, or the canonical memo of a Series (exactly one of project_id /
--                   series_id, like character); origin_id is the Series memo an Episode's memo was copied
--                   from. archived_at set = archived (DELETE never removes the row).
--   note_version    every change of title / content, append-only (version_number is monotonic)
--   note_promotion  the World State promotion proposed for one Project note version (UNIQUE: a version is
--                   proposed once). Its Approval card is chat_message.ref_type = 'note_promotion'.
--   prompt_version.note_refs
--                   the note versions the Prompt Planner was given ([{"note_id", "version_number"}]).
CREATE TABLE note (
    id          TEXT PRIMARY KEY,
    project_id  TEXT REFERENCES project (id) ON DELETE CASCADE,
    series_id   TEXT REFERENCES series (id) ON DELETE CASCADE,
    origin_id   TEXT REFERENCES note (id) ON DELETE SET NULL,
    category    TEXT NOT NULL DEFAULT 'other' CHECK (category IN ('character', 'product', 'location', 'world', 'tone', 'other')),
    pinned      INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
    archived_at TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK ((project_id IS NULL) <> (series_id IS NULL))
);
CREATE INDEX idx_note_project ON note (project_id, archived_at);
CREATE INDEX idx_note_series ON note (series_id, archived_at);
CREATE INDEX idx_note_origin ON note (origin_id);

CREATE TABLE note_version (
    id             TEXT PRIMARY KEY,
    note_id        TEXT NOT NULL REFERENCES note (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    title          TEXT NOT NULL,
    content        TEXT NOT NULL,
    source         TEXT NOT NULL CHECK (source IN ('manual', 'chat')),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (note_id, version_number)
);
CREATE INDEX idx_note_version_note ON note_version (note_id, version_number);

-- note_version is append-only.
-- +goose StatementBegin
CREATE TRIGGER note_version_append_only
BEFORE UPDATE ON note_version
BEGIN
    SELECT RAISE(ABORT, 'note_version is append-only');
END;
-- +goose StatementEnd

CREATE TABLE note_promotion (
    id                     TEXT PRIMARY KEY,
    note_version_id        TEXT NOT NULL UNIQUE REFERENCES note_version (id) ON DELETE CASCADE,
    proposed_diff          TEXT NOT NULL CHECK (json_valid(proposed_diff)),
    locked_conflicts       TEXT NOT NULL DEFAULT '{"changes":[]}' CHECK (json_valid(locked_conflicts)),
    status                 TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'rejected')),
    world_state_version_id TEXT REFERENCES world_state_version (id) ON DELETE SET NULL,
    created_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    resolved_at            TEXT
);
CREATE INDEX idx_note_promotion_status ON note_promotion (status);

ALTER TABLE prompt_version ADD COLUMN note_refs TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(note_refs));

-- +goose Down
ALTER TABLE prompt_version DROP COLUMN note_refs;
DROP TABLE note_promotion;
DROP TRIGGER note_version_append_only;
DROP TABLE note_version;
DROP TABLE note;
