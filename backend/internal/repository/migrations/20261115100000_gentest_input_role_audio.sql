-- +goose Up
-- The input of a video model can be an audio file (role "audio": the sound a speech-to-video model such as Wan 2.2 S2V makes the clip from; Issue #1153,
-- spec/architecture/gentest/er-gentest.md). SQLite cannot alter a CHECK constraint, so gentest_input is rebuilt; no table references it. The list keeps every
-- role the table had: the inpaint mask, the pose reference and the markup layer included.
CREATE TABLE gentest_input_new (
    id         TEXT PRIMARY KEY,
    batch_id   TEXT NOT NULL REFERENCES gentest_batch (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('reference', 'first_frame', 'last_frame', 'face', 'style', 'edit', 'mask', 'pose', 'markup', 'audio')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    origin     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(origin)),
    position   INTEGER NOT NULL DEFAULT 0,
    strength   REAL CHECK (strength IS NULL OR (strength >= 0 AND strength <= 1))
);
INSERT INTO gentest_input_new (id, batch_id, role, file_path, mime_type, size_bytes, origin, position, strength)
    SELECT id, batch_id, role, file_path, mime_type, size_bytes, origin, position, strength FROM gentest_input;
DROP TABLE gentest_input;
ALTER TABLE gentest_input_new RENAME TO gentest_input;
CREATE INDEX idx_gentest_input_batch ON gentest_input (batch_id, position);

-- +goose Down
-- An audio input is dropped: the table before this one has no role for a sound, and a reference would be a file no image reader can open.
CREATE TABLE gentest_input_old (
    id         TEXT PRIMARY KEY,
    batch_id   TEXT NOT NULL REFERENCES gentest_batch (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('reference', 'first_frame', 'last_frame', 'face', 'style', 'edit', 'mask', 'pose', 'markup')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    origin     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(origin)),
    position   INTEGER NOT NULL DEFAULT 0,
    strength   REAL CHECK (strength IS NULL OR (strength >= 0 AND strength <= 1))
);
INSERT INTO gentest_input_old (id, batch_id, role, file_path, mime_type, size_bytes, origin, position, strength)
    SELECT id, batch_id, role, file_path, mime_type, size_bytes, origin, position, strength FROM gentest_input WHERE role <> 'audio';
DROP TABLE gentest_input;
ALTER TABLE gentest_input_old RENAME TO gentest_input;
CREATE INDEX idx_gentest_input_batch ON gentest_input (batch_id, position);
