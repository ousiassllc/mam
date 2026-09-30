-- +goose NO TRANSACTION
-- +goose Up
-- Branch (F-17, spec/architecture/er.md §4 "Branch"): another development of the story that starts at
-- a Scene / Shot / Frame and keeps its own World State and derived tree.
--   branch          the Branch Point (scene / shot / frame with offset) and the Snapshot of the World State
--   branch_version  every change of the Branch itself (name, World State reference), append-only
--   scene / world_state_version / edit_timeline_version / audio_timeline_version
--                   branch_id (NULL = the trunk); the UNIQUE constraint includes the Branch, so every
--                   Branch numbers its own Scenes and versions. NULLs are distinct in a UNIQUE index, so
--                   the trunk is covered by COALESCE(branch_id, '').
--   character / product / location / export
--                   branch_id (NULL = the trunk): the values the Branch changes never touch the trunk.
-- SQLite cannot change a UNIQUE constraint, so the four tables above are rebuilt (SQLite's "generalized
-- ALTER TABLE" procedure). They are referenced by shot, state_diff and editor_judge_result, so foreign keys
-- are off while the tables are swapped: dropping a table with them on would run the ON DELETE CASCADE of
-- the referencing rows. The writer pool has one connection, so the PRAGMA holds for the whole migration and
-- is switched back on at the end.
PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE branch (
    id                          TEXT PRIMARY KEY,
    project_id                  TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    branch_point_type           TEXT NOT NULL CHECK (branch_point_type IN ('scene', 'shot', 'frame')),
    branch_point_scene_id       TEXT REFERENCES scene (id) ON DELETE CASCADE,
    branch_point_shot_id        TEXT REFERENCES shot (id) ON DELETE CASCADE,
    branch_point_offset_seconds REAL CHECK (branch_point_offset_seconds IS NULL OR branch_point_offset_seconds >= 0),
    name                        TEXT NOT NULL,
    world_state_version_id      TEXT REFERENCES world_state_version (id) ON DELETE SET NULL,
    created_at                  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (
        (branch_point_type = 'scene' AND branch_point_scene_id IS NOT NULL AND branch_point_shot_id IS NULL AND branch_point_offset_seconds IS NULL)
        OR (branch_point_type = 'shot' AND branch_point_scene_id IS NULL AND branch_point_shot_id IS NOT NULL AND branch_point_offset_seconds IS NULL)
        OR (branch_point_type = 'frame' AND branch_point_scene_id IS NULL AND branch_point_shot_id IS NOT NULL AND branch_point_offset_seconds IS NOT NULL)
    )
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
CREATE INDEX idx_branch_version_branch ON branch_version (branch_id, version_number);

CREATE TABLE scene_new (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    branch_id   TEXT REFERENCES branch (id) ON DELETE CASCADE,
    order_index INTEGER NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    location_id TEXT REFERENCES location (id) ON DELETE SET NULL,
    time        TEXT,
    objective   TEXT,
    state       TEXT NOT NULL DEFAULT 'PLANNING'
);
INSERT INTO scene_new (id, project_id, order_index, title, location_id, time, objective, state)
    SELECT id, project_id, order_index, title, location_id, time, objective, state FROM scene;
DROP TABLE scene;
ALTER TABLE scene_new RENAME TO scene;
CREATE UNIQUE INDEX idx_scene_project_branch_order ON scene (project_id, COALESCE(branch_id, ''), order_index);

CREATE TABLE world_state_version_new (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    branch_id      TEXT REFERENCES branch (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    state          TEXT NOT NULL CHECK (json_valid(state)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO world_state_version_new (id, project_id, version_number, state, created_at)
    SELECT id, project_id, version_number, state, created_at FROM world_state_version;
DROP TABLE world_state_version;
ALTER TABLE world_state_version_new RENAME TO world_state_version;
CREATE UNIQUE INDEX idx_world_state_version_project ON world_state_version (project_id, COALESCE(branch_id, ''), version_number);

CREATE TABLE edit_timeline_version_new (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    branch_id      TEXT REFERENCES branch (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    timeline       TEXT NOT NULL CHECK (json_valid(timeline)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO edit_timeline_version_new (id, project_id, version_number, timeline, created_at)
    SELECT id, project_id, version_number, timeline, created_at FROM edit_timeline_version;
DROP TABLE edit_timeline_version;
ALTER TABLE edit_timeline_version_new RENAME TO edit_timeline_version;
CREATE UNIQUE INDEX idx_edit_timeline_project ON edit_timeline_version (project_id, COALESCE(branch_id, ''), version_number);

CREATE TABLE audio_timeline_version_new (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    branch_id      TEXT REFERENCES branch (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    timeline       TEXT NOT NULL CHECK (json_valid(timeline)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO audio_timeline_version_new (id, project_id, version_number, timeline, created_at)
    SELECT id, project_id, version_number, timeline, created_at FROM audio_timeline_version;
DROP TABLE audio_timeline_version;
ALTER TABLE audio_timeline_version_new RENAME TO audio_timeline_version;
CREATE UNIQUE INDEX idx_audio_timeline_project ON audio_timeline_version (project_id, COALESCE(branch_id, ''), version_number);

ALTER TABLE character ADD COLUMN branch_id TEXT REFERENCES branch (id) ON DELETE CASCADE;
DROP INDEX IF EXISTS idx_character_project;
CREATE INDEX idx_character_project ON character (project_id, branch_id);

ALTER TABLE product ADD COLUMN branch_id TEXT REFERENCES branch (id) ON DELETE CASCADE;
DROP INDEX IF EXISTS idx_product_project;
CREATE INDEX idx_product_project ON product (project_id, branch_id);

ALTER TABLE location ADD COLUMN branch_id TEXT REFERENCES branch (id) ON DELETE CASCADE;
DROP INDEX IF EXISTS idx_location_project;
CREATE INDEX idx_location_project ON location (project_id, branch_id);

ALTER TABLE export ADD COLUMN branch_id TEXT REFERENCES branch (id) ON DELETE CASCADE;
DROP INDEX IF EXISTS idx_export_project;
CREATE INDEX idx_export_project ON export (project_id, branch_id);

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
-- Deleting the Branches first, with foreign keys on, cascades to their Scenes, Shots, Takes, entities and versions.
BEGIN;
DELETE FROM branch;
COMMIT;

PRAGMA foreign_keys = OFF;
BEGIN;

DROP INDEX idx_export_project;
ALTER TABLE export DROP COLUMN branch_id;
CREATE INDEX idx_export_project ON export (project_id);

DROP INDEX idx_location_project;
ALTER TABLE location DROP COLUMN branch_id;
CREATE INDEX idx_location_project ON location (project_id);

DROP INDEX idx_product_project;
ALTER TABLE product DROP COLUMN branch_id;
CREATE INDEX idx_product_project ON product (project_id);

DROP INDEX idx_character_project;
ALTER TABLE character DROP COLUMN branch_id;
CREATE INDEX idx_character_project ON character (project_id);

CREATE TABLE audio_timeline_version_old (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    timeline       TEXT NOT NULL CHECK (json_valid(timeline)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, version_number)
);
INSERT INTO audio_timeline_version_old (id, project_id, version_number, timeline, created_at)
    SELECT id, project_id, version_number, timeline, created_at FROM audio_timeline_version;
DROP TABLE audio_timeline_version;
ALTER TABLE audio_timeline_version_old RENAME TO audio_timeline_version;
CREATE INDEX idx_audio_timeline_project ON audio_timeline_version (project_id, version_number);

CREATE TABLE edit_timeline_version_old (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    timeline       TEXT NOT NULL CHECK (json_valid(timeline)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, version_number)
);
INSERT INTO edit_timeline_version_old (id, project_id, version_number, timeline, created_at)
    SELECT id, project_id, version_number, timeline, created_at FROM edit_timeline_version;
DROP TABLE edit_timeline_version;
ALTER TABLE edit_timeline_version_old RENAME TO edit_timeline_version;
CREATE INDEX idx_edit_timeline_project ON edit_timeline_version (project_id, version_number);

CREATE TABLE world_state_version_old (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    state          TEXT NOT NULL CHECK (json_valid(state)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, version_number)
);
INSERT INTO world_state_version_old (id, project_id, version_number, state, created_at)
    SELECT id, project_id, version_number, state, created_at FROM world_state_version;
DROP TABLE world_state_version;
ALTER TABLE world_state_version_old RENAME TO world_state_version;
CREATE INDEX idx_world_state_version_project ON world_state_version (project_id, version_number);

CREATE TABLE scene_old (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    order_index INTEGER NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    location_id TEXT REFERENCES location (id) ON DELETE SET NULL,
    time        TEXT,
    objective   TEXT,
    state       TEXT NOT NULL DEFAULT 'PLANNING',
    UNIQUE (project_id, order_index)
);
INSERT INTO scene_old (id, project_id, order_index, title, location_id, time, objective, state)
    SELECT id, project_id, order_index, title, location_id, time, objective, state FROM scene;
DROP TABLE scene;
ALTER TABLE scene_old RENAME TO scene;
CREATE INDEX idx_scene_project_order ON scene (project_id, order_index);

DROP TABLE branch_version;
DROP TABLE branch;

COMMIT;
PRAGMA foreign_keys = ON;
