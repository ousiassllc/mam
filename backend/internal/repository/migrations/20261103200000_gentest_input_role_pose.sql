-- +goose Up
-- A reference of an image model can be a pose reference (role "pose": only the body pose and camera angle of a pose chart or mannequin are
-- taken from the picture; spec/architecture/gentest/er-gentest.md). SQLite cannot alter a CHECK constraint, so gentest_input is rebuilt;
-- no table references it. The list also allows "mask", the inpaint mask of a batch, which is stored beside the references.
CREATE TABLE gentest_input_new (
    id         TEXT PRIMARY KEY,
    batch_id   TEXT NOT NULL REFERENCES gentest_batch (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('reference', 'first_frame', 'last_frame', 'face', 'style', 'edit', 'mask', 'pose')),
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
-- A pose input goes back to the whole picture (the table before this one allows the mask of the inpaint migration, 20261102120000).
CREATE TABLE gentest_input_old (
    id         TEXT PRIMARY KEY,
    batch_id   TEXT NOT NULL REFERENCES gentest_batch (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('reference', 'first_frame', 'last_frame', 'face', 'style', 'edit', 'mask')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    origin     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(origin)),
    position   INTEGER NOT NULL DEFAULT 0,
    strength   REAL CHECK (strength IS NULL OR (strength >= 0 AND strength <= 1))
);
INSERT INTO gentest_input_old (id, batch_id, role, file_path, mime_type, size_bytes, origin, position, strength)
    SELECT id, batch_id, CASE WHEN role = 'pose' THEN 'reference' ELSE role END, file_path, mime_type, size_bytes, origin, position, strength
    FROM gentest_input;
DROP TABLE gentest_input;
ALTER TABLE gentest_input_old RENAME TO gentest_input;
CREATE INDEX idx_gentest_input_batch ON gentest_input (batch_id, position);
