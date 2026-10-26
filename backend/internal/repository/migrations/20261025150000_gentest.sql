-- +goose Up
-- The generation test screen (spec/architecture/gentest/er-gentest.md): batches of generations that belong to no Project,
-- one row per result, the inputs they used, the prompts that were used, and where a result was imported to. None of
-- these tables references a Project table: ids of Projects and assets are plain TEXT, so deleting a Project never
-- touches the history and an exported Project bundle does not carry it (bundle.unbundledTables). The files are
-- below <data dir>/gentest/, not in the assets directory.
CREATE TABLE gentest_batch (
    id             TEXT PRIMARY KEY,
    media          TEXT NOT NULL CHECK (media IN ('image', 'video')),
    job_id         TEXT NOT NULL DEFAULT '',
    -- aspect_ratio, resolution, duration_seconds, count (the results per model x prompt)
    params         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params)),
    -- the sum of the priced estimates in USD, and the estimate as it was confirmed (breakdown per model)
    estimated_cost REAL NOT NULL DEFAULT 0,
    estimate       TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(estimate)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at    TEXT
);
CREATE INDEX idx_gentest_batch_job ON gentest_batch (job_id);

-- The images a batch was given: copied into the gentest directory, so the history survives the deletion of the Project or
-- the history result they came from. origin says where they came from (upload, project asset, reference item, history result).
CREATE TABLE gentest_input (
    id         TEXT PRIMARY KEY,
    batch_id   TEXT NOT NULL REFERENCES gentest_batch (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('reference', 'first_frame', 'last_frame')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    origin     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(origin)),
    position   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_gentest_input_batch ON gentest_input (batch_id, position);

-- One generation. model is the canonical identifier (a call never falls back, so it is the model that made the file),
-- api_model the vendor's identifier. prompt is what the user wrote, sent_prompt what was sent (the negative prompt is
-- folded into it when the model has no field for it: negative_mode). seed is NULL for a model that takes none;
-- seed_sent says the seed went to the vendor. cost_basis: priced (estimated_cost in USD), free (a local model: 0),
-- unknown (no price: estimated_cost is NULL).
CREATE TABLE gentest_result (
    id             TEXT PRIMARY KEY,
    batch_id       TEXT NOT NULL REFERENCES gentest_batch (id) ON DELETE CASCADE,
    position       INTEGER NOT NULL DEFAULT 0,
    media          TEXT NOT NULL CHECK (media IN ('image', 'video')),
    model          TEXT NOT NULL,
    api_model      TEXT NOT NULL DEFAULT '',
    prompt         TEXT NOT NULL,
    negative_prompt TEXT NOT NULL DEFAULT '',
    sent_prompt    TEXT NOT NULL DEFAULT '',
    negative_mode  TEXT NOT NULL DEFAULT 'none' CHECK (negative_mode IN ('none', 'field', 'folded')),
    params         TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params)),
    used_inputs    TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(used_inputs)),
    seed           INTEGER,
    seed_sent      INTEGER NOT NULL DEFAULT 0 CHECK (seed_sent IN (0, 1)),
    status         TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    error          TEXT NOT NULL DEFAULT '',
    file_path      TEXT NOT NULL DEFAULT '',
    mime_type      TEXT NOT NULL DEFAULT '',
    size_bytes     INTEGER NOT NULL DEFAULT 0,
    cost_basis     TEXT NOT NULL DEFAULT 'unknown' CHECK (cost_basis IN ('priced', 'free', 'unknown')),
    estimated_cost REAL,
    actual_cost    REAL,
    favorite       INTEGER NOT NULL DEFAULT 0 CHECK (favorite IN (0, 1)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at    TEXT
);
CREATE INDEX idx_gentest_result_created ON gentest_result (created_at DESC, id DESC);
CREATE INDEX idx_gentest_result_batch ON gentest_result (batch_id, position);
CREATE INDEX idx_gentest_result_model ON gentest_result (model);
CREATE INDEX idx_gentest_result_favorite ON gentest_result (favorite) WHERE favorite = 1;
CREATE INDEX idx_gentest_result_status ON gentest_result (status);

-- The prompts that were used, once per (prompt, negative prompt). No reference from the results: deleting results
-- never deletes a prompt, deleting a prompt never touches a result.
CREATE TABLE gentest_prompt (
    id              TEXT PRIMARY KEY,
    prompt          TEXT NOT NULL,
    negative_prompt TEXT NOT NULL DEFAULT '',
    use_count       INTEGER NOT NULL DEFAULT 1 CHECK (use_count >= 1),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_used_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (prompt, negative_prompt)
);
CREATE INDEX idx_gentest_prompt_used ON gentest_prompt (last_used_at DESC);

-- Where a result was imported to. project_id, asset_id and reference_item_id are plain TEXT, not foreign keys: the
-- history must not depend on the Project (a deleted Project leaves a record of where the image went).
CREATE TABLE gentest_import (
    id                TEXT PRIMARY KEY,
    result_id         TEXT NOT NULL REFERENCES gentest_result (id) ON DELETE CASCADE,
    project_id        TEXT NOT NULL,
    asset_id          TEXT NOT NULL,
    reference_item_id TEXT,
    -- as: reference | asset, plus kind / name / tags and, when it was assigned to a slot, the target, id and slot
    target            TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(target)),
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_gentest_import_result ON gentest_import (result_id);

-- +goose Down
DROP TABLE gentest_import;
DROP TABLE gentest_prompt;
DROP TABLE gentest_result;
DROP TABLE gentest_input;
DROP TABLE gentest_batch;
