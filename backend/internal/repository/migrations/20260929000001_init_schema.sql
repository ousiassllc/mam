-- +goose Up
-- M1 schema (spec/architecture/er.md §3).
-- M2 tables/columns (series, note*, branch*, brand_constraint, *.branch_id, *.series_id,
-- *.origin_id, project.episode_number, prompt_version.note_refs) are added by the M2 migrations.
-- JSON columns are TEXT guarded by json_valid(); timestamps are UTC ISO-8601 TEXT.

CREATE TABLE project (
    id                   TEXT PRIMARY KEY,
    state                TEXT NOT NULL DEFAULT 'DRAFT' CHECK (state IN (
        'DRAFT', 'PLANNING', 'VISUAL_DESIGN', 'AWAITING_APPROVAL', 'GENERATING', 'JUDGING',
        'REPAIRING', 'EDITING', 'EDIT_JUDGING', 'AUDIO', 'FINAL_QC', 'READY', 'EXPORTED')),
    mode                 TEXT NOT NULL CHECK (mode IN ('auto', 'director')),
    confirmation_level   TEXT NOT NULL CHECK (confirmation_level IN ('full_auto', 'key_check', 'custom_check')),
    production_template  TEXT NOT NULL CHECK (production_template IN ('product_cm', 'drama', 'anime', 'sns_short')),
    quality_policy       TEXT NOT NULL CHECK (quality_policy IN ('fast', 'balanced', 'max_quality')),
    project_budget_ceiling NUMERIC,
    shot_budget_ceiling    NUMERIC,
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_project_state ON project (state);

CREATE TABLE brief (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL UNIQUE REFERENCES project (id) ON DELETE CASCADE,
    content    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(content)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE asset (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    type       TEXT NOT NULL CHECK (type IN ('image', 'video', 'reference')),
    file_path  TEXT NOT NULL,
    mime_type  TEXT,
    size_bytes INTEGER,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_asset_project ON asset (project_id);

CREATE TABLE chat_message (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    role                TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    kind                TEXT NOT NULL CHECK (kind IN (
        'text', 'question', 'approval', 'keyframe', 'take', 'progress', 'escalation')),
    content             TEXT NOT NULL DEFAULT '',
    reply_to_message_id TEXT REFERENCES chat_message (id),
    ref_type            TEXT,
    ref_id              TEXT,
    approval_status     TEXT,
    attachment_asset_ids TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(attachment_asset_ids)),
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    CHECK (role = 'assistant' OR kind = 'text'),
    CHECK (
        (kind = 'approval' AND approval_status IS NOT NULL AND approval_status IN ('pending', 'approved', 'changes_requested'))
        OR (kind <> 'approval' AND approval_status IS NULL))
);
CREATE INDEX idx_chat_message_project ON chat_message (project_id, created_at);
CREATE INDEX idx_chat_message_reply ON chat_message (reply_to_message_id);

-- chat_message is append-only; only approval_status may change (on response).
-- +goose StatementBegin
CREATE TRIGGER chat_message_append_only
BEFORE UPDATE OF id, project_id, role, kind, content, reply_to_message_id, ref_type, ref_id,
    attachment_asset_ids, created_at ON chat_message
BEGIN
    SELECT RAISE(ABORT, 'chat_message is append-only');
END;
-- +goose StatementEnd

CREATE TABLE character (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    name                    TEXT NOT NULL,
    face_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    body_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    hairstyle               TEXT,
    wardrobe                TEXT,
    voice                   TEXT,
    personality             TEXT
);
CREATE INDEX idx_character_project ON character (project_id);

CREATE TABLE product (
    id                          TEXT PRIMARY KEY,
    project_id                  TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    name                        TEXT NOT NULL,
    geometry_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    label_reference_asset_id    TEXT REFERENCES asset (id) ON DELETE SET NULL,
    material                    TEXT,
    orientation                 TEXT,
    condition                   TEXT
);
CREATE INDEX idx_product_project ON product (project_id);

CREATE TABLE location (
    id                       TEXT PRIMARY KEY,
    project_id               TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    name                     TEXT NOT NULL,
    layout_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    time                     TEXT,
    lighting_state           TEXT,
    weather                  TEXT
);
CREATE INDEX idx_location_project ON location (project_id);

CREATE TABLE script_version (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    content        TEXT NOT NULL,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, version_number)
);

CREATE TABLE scene (
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
CREATE INDEX idx_scene_project_order ON scene (project_id, order_index);

CREATE TABLE shot (
    id               TEXT PRIMARY KEY,
    scene_id         TEXT NOT NULL REFERENCES scene (id) ON DELETE CASCADE,
    order_index      INTEGER NOT NULL,
    objective        TEXT,
    duration_seconds REAL,
    shot_spec        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(shot_spec)),
    state            TEXT NOT NULL DEFAULT 'PLANNING' CHECK (state IN (
        'PLANNING', 'GENERATING', 'JUDGING', 'REPAIRING', 'EDITING')),
    selected_take_id TEXT REFERENCES take (id) ON DELETE SET NULL
);
CREATE INDEX idx_shot_scene_order ON shot (scene_id, order_index);

CREATE TABLE prompt_version (
    id                   TEXT PRIMARY KEY,
    shot_id              TEXT NOT NULL REFERENCES shot (id) ON DELETE CASCADE,
    version_number       INTEGER NOT NULL,
    shot_spec_snapshot   TEXT NOT NULL CHECK (json_valid(shot_spec_snapshot)),
    model_adapter_output TEXT CHECK (model_adapter_output IS NULL OR json_valid(model_adapter_output)),
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (shot_id, version_number)
);
CREATE INDEX idx_prompt_version_shot ON prompt_version (shot_id);

CREATE TABLE take (
    id                TEXT PRIMARY KEY,
    prompt_version_id TEXT NOT NULL REFERENCES prompt_version (id) ON DELETE CASCADE,
    model_name        TEXT NOT NULL,
    model_parameters  TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(model_parameters)),
    file_path         TEXT,
    status            TEXT NOT NULL DEFAULT 'candidate' CHECK (status IN (
        'candidate', 'ai_pick', 'selected', 'rejected')),
    scores            TEXT CHECK (scores IS NULL OR json_valid(scores)),
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_take_prompt_version ON take (prompt_version_id);
CREATE INDEX idx_take_status ON take (status);

-- judge_result.take_id is intentionally not UNIQUE: every Judge run is kept as history.
CREATE TABLE judge_result (
    id                 TEXT PRIMARY KEY,
    take_id            TEXT NOT NULL REFERENCES take (id) ON DELETE CASCADE,
    result             TEXT NOT NULL CHECK (result IN ('pass', 'fail')),
    overall_confidence REAL,
    issues             TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(issues)),
    metrics            TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(metrics)),
    preserve           TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(preserve)),
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_judge_result_take ON judge_result (take_id, created_at);

CREATE TABLE repair_action (
    id                TEXT PRIMARY KEY,
    judge_result_id   TEXT NOT NULL REFERENCES judge_result (id) ON DELETE CASCADE,
    action_type       TEXT NOT NULL,
    instructions      TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(instructions)),
    resulting_take_id TEXT REFERENCES take (id) ON DELETE SET NULL,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_repair_action_judge_result ON repair_action (judge_result_id);

CREATE TABLE world_state_version (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    state          TEXT NOT NULL CHECK (json_valid(state)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, version_number)
);
CREATE INDEX idx_world_state_version_project ON world_state_version (project_id, version_number);

CREATE TABLE state_diff (
    id                     TEXT PRIMARY KEY,
    shot_id                TEXT NOT NULL REFERENCES shot (id) ON DELETE CASCADE,
    take_id                TEXT NOT NULL REFERENCES take (id) ON DELETE CASCADE,
    diff                   TEXT NOT NULL CHECK (json_valid(diff)),
    world_state_version_id TEXT NOT NULL REFERENCES world_state_version (id) ON DELETE CASCADE,
    applied_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_state_diff_shot ON state_diff (shot_id);

CREATE TABLE edit_timeline_version (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    timeline       TEXT NOT NULL CHECK (json_valid(timeline)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, version_number)
);
CREATE INDEX idx_edit_timeline_project ON edit_timeline_version (project_id, version_number);

CREATE TABLE audio_timeline_version (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    timeline       TEXT NOT NULL CHECK (json_valid(timeline)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, version_number)
);
CREATE INDEX idx_audio_timeline_project ON audio_timeline_version (project_id, version_number);

CREATE TABLE editor_judge_result (
    id                       TEXT PRIMARY KEY,
    edit_timeline_version_id TEXT NOT NULL REFERENCES edit_timeline_version (id) ON DELETE CASCADE,
    result                   TEXT NOT NULL CHECK (result IN ('pass', 're_edit', 'need_shot')),
    findings                 TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(findings)),
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_editor_judge_result_timeline ON editor_judge_result (edit_timeline_version_id);

CREATE TABLE export (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    file_path    TEXT NOT NULL,
    resolution   TEXT NOT NULL,
    aspect_ratio TEXT NOT NULL,
    codec        TEXT NOT NULL,
    exported_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_export_project ON export (project_id);

CREATE TABLE lock (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    target_type    TEXT NOT NULL,
    target_id      TEXT NOT NULL,
    parameter_name TEXT NOT NULL,
    mode           TEXT NOT NULL CHECK (mode IN ('auto', 'manual', 'lock')),
    value          TEXT CHECK (value IS NULL OR json_valid(value)),
    UNIQUE (project_id, target_type, target_id, parameter_name)
);
CREATE INDEX idx_lock_project_target ON lock (project_id, target_type, target_id);

CREATE TABLE cost_ledger_entry (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    shot_id        TEXT REFERENCES shot (id) ON DELETE SET NULL,
    take_id        TEXT REFERENCES take (id) ON DELETE SET NULL,
    provider       TEXT NOT NULL,
    estimated_cost NUMERIC NOT NULL DEFAULT 0,
    actual_cost    NUMERIC,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_cost_ledger_project ON cost_ledger_entry (project_id);

-- +goose Down
DROP TABLE cost_ledger_entry;
DROP TABLE lock;
DROP TABLE export;
DROP TABLE editor_judge_result;
DROP TABLE audio_timeline_version;
DROP TABLE edit_timeline_version;
DROP TABLE state_diff;
DROP TABLE world_state_version;
DROP TABLE repair_action;
DROP TABLE judge_result;
DROP TABLE take;
DROP TABLE prompt_version;
DROP TABLE shot;
DROP TABLE scene;
DROP TABLE script_version;
DROP TABLE location;
DROP TABLE product;
DROP TABLE character;
DROP TRIGGER chat_message_append_only;
DROP TABLE chat_message;
DROP TABLE asset;
DROP TABLE brief;
DROP TABLE project;
