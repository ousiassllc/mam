-- +goose NO TRANSACTION
-- +goose Up
-- A Project can hold an audio file (asset.type = 'audio': WAV, MP3, FLAC, OGG, M4A) so that it can be the audio input of a single-shot
-- generation (Issue #1153, spec/architecture/er.md §4 "アセットの種別"). SQLite cannot alter a CHECK constraint, so asset is rebuilt (SQLite's
-- "generalized ALTER TABLE" procedure). asset is referenced by character / product / location (reference images, ON DELETE SET NULL), series
-- (style_reference_asset_id, ON DELETE SET NULL) and reference_item (ON DELETE CASCADE), so foreign keys are off while the table is swapped:
-- dropping asset with them on would run those actions on every row. The writer pool has one connection, so the PRAGMA holds for the whole
-- migration and is switched back on at the end. The referencing tables name asset by text and are not touched, so their foreign keys find the new
-- table under the old name (the migration test runs foreign_key_check and the ON DELETE actions on data).
PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE asset_new (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    type       TEXT NOT NULL CHECK (type IN ('image', 'video', 'reference', 'audio')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT,
    size_bytes INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO asset_new (id, project_id, type, file_path, mime_type, size_bytes, created_at)
    SELECT id, project_id, type, file_path, mime_type, size_bytes, created_at FROM asset;
DROP TABLE asset;
ALTER TABLE asset_new RENAME TO asset;
CREATE INDEX idx_asset_project ON asset (project_id);

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
-- The audio assets are deleted first, with foreign keys on, so that whatever points at them is cleared (a reference_item row is removed, a
-- reference column set to NULL); the table before this one has no type for a sound. The files stay in the assets directory.
BEGIN;
DELETE FROM asset WHERE type = 'audio';
COMMIT;

PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE asset_old (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    type       TEXT NOT NULL CHECK (type IN ('image', 'video', 'reference')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT,
    size_bytes INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO asset_old (id, project_id, type, file_path, mime_type, size_bytes, created_at)
    SELECT id, project_id, type, file_path, mime_type, size_bytes, created_at FROM asset;
DROP TABLE asset;
ALTER TABLE asset_old RENAME TO asset;
CREATE INDEX idx_asset_project ON asset (project_id);

COMMIT;
PRAGMA foreign_keys = ON;
