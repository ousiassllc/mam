-- +goose NO TRANSACTION
-- +goose Up
-- Reversible Decisions (F-28, spec/spec.md §29.4): the Branch tables and the Branch-scoped World State
-- version series the restore endpoints of spec/api/endpoints.md §2.4 address (spec/architecture/er.md).
--
-- world_state_version gets a nullable branch_id (NULL = the trunk) and UNIQUE (project_id, branch_id,
-- version_number). SQLite cannot change a UNIQUE constraint, so the table is rebuilt (the "12 steps"
-- of https://sqlite.org/lang_altertable.html#otheralter). Foreign keys are switched off for the
-- rebuild, otherwise dropping the old table would cascade into state_diff (and into the tables that
-- reference a world_state_version and are added by other migrations); this is why the file runs
-- outside goose's transaction and opens its own.
-- NULLs are distinct in a UNIQUE constraint, so the trunk gets a partial unique index of its own.
PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE branch (
    id                          TEXT PRIMARY KEY,
    project_id                  TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    branch_point_type           TEXT NOT NULL CHECK (branch_point_type IN ('scene', 'shot', 'frame')),
    branch_point_scene_id       TEXT REFERENCES scene (id) ON DELETE SET NULL,
    branch_point_shot_id        TEXT REFERENCES shot (id) ON DELETE SET NULL,
    branch_point_offset_seconds REAL,
    name                        TEXT NOT NULL,
    world_state_version_id      TEXT REFERENCES world_state_version (id) ON DELETE SET NULL,
    created_at                  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_branch_project ON branch (project_id);

CREATE TABLE branch_version (
    id                     TEXT PRIMARY KEY,
    branch_id              TEXT NOT NULL REFERENCES branch (id) ON DELETE CASCADE,
    version_number         INTEGER NOT NULL,
    name                   TEXT NOT NULL,
    world_state_version_id TEXT REFERENCES world_state_version (id) ON DELETE SET NULL,
    created_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (branch_id, version_number)
);

CREATE TABLE world_state_version_new (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    branch_id      TEXT REFERENCES branch (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    state          TEXT NOT NULL CHECK (json_valid(state)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, branch_id, version_number)
);
INSERT INTO world_state_version_new (id, project_id, branch_id, version_number, state, created_at)
    SELECT id, project_id, NULL, version_number, state, created_at FROM world_state_version;
DROP TABLE world_state_version;
ALTER TABLE world_state_version_new RENAME TO world_state_version;
CREATE INDEX idx_world_state_version_project ON world_state_version (project_id, branch_id, version_number);
CREATE UNIQUE INDEX uq_world_state_version_trunk ON world_state_version (project_id, version_number) WHERE branch_id IS NULL;

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE world_state_version_old (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    state          TEXT NOT NULL CHECK (json_valid(state)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, version_number)
);
INSERT INTO world_state_version_old (id, project_id, version_number, state, created_at)
    SELECT id, project_id, version_number, state, created_at FROM world_state_version WHERE branch_id IS NULL;
DROP TABLE world_state_version;
ALTER TABLE world_state_version_old RENAME TO world_state_version;
CREATE INDEX idx_world_state_version_project ON world_state_version (project_id, version_number);
DROP TABLE branch_version;
DROP TABLE branch;

COMMIT;
PRAGMA foreign_keys = ON;
