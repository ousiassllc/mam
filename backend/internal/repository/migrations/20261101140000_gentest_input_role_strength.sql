-- +goose Up
-- The reference images of an image model take a role besides the whole picture (face, style, edit) and a strength
-- (spec/architecture/gentest/er-gentest.md). strength is NULL for the default (and for a first / last frame, which has none).
-- SQLite cannot alter a CHECK constraint, so gentest_input is rebuilt; no table references it.
CREATE TABLE gentest_input_new (
    id         TEXT PRIMARY KEY,
    batch_id   TEXT NOT NULL REFERENCES gentest_batch (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('reference', 'first_frame', 'last_frame', 'face', 'style', 'edit')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    origin     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(origin)),
    position   INTEGER NOT NULL DEFAULT 0,
    strength   REAL CHECK (strength IS NULL OR (strength >= 0 AND strength <= 1))
);
INSERT INTO gentest_input_new (id, batch_id, role, file_path, mime_type, size_bytes, origin, position)
    SELECT id, batch_id, role, file_path, mime_type, size_bytes, origin, position FROM gentest_input;
DROP TABLE gentest_input;
ALTER TABLE gentest_input_new RENAME TO gentest_input;
CREATE INDEX idx_gentest_input_batch ON gentest_input (batch_id, position);

-- +goose Down
-- A face / style / edit reference goes back to the whole picture: the old table knows only those roles.
CREATE TABLE gentest_input_old (
    id         TEXT PRIMARY KEY,
    batch_id   TEXT NOT NULL REFERENCES gentest_batch (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('reference', 'first_frame', 'last_frame')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    origin     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(origin)),
    position   INTEGER NOT NULL DEFAULT 0
);
INSERT INTO gentest_input_old (id, batch_id, role, file_path, mime_type, size_bytes, origin, position)
    SELECT id, batch_id, CASE WHEN role IN ('face', 'style', 'edit') THEN 'reference' ELSE role END, file_path, mime_type, size_bytes, origin, position
    FROM gentest_input;
DROP TABLE gentest_input;
ALTER TABLE gentest_input_old RENAME TO gentest_input;
CREATE INDEX idx_gentest_input_batch ON gentest_input (batch_id, position);
