-- +goose Up
-- ComfyUI workflows saved in mam (spec/architecture/comfy/comfy-workflows.md): one global table, usable by every Project.
-- ui_json is the editor's own format (to open the workflow again in the node screen), api_json the API format
-- (what is run). assignment_json holds the input assignment, the output node and the frame / video settings.
-- version counts the edits of the graph or the assignment; a Take made by the workflow records comfy:<id>@v<version>
-- in take.model_name. endpoint is the registered ComfyUI endpoint that runs it.
CREATE TABLE comfy_workflow (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL COLLATE NOCASE UNIQUE CHECK (length(name) > 0),
    kind            TEXT NOT NULL CHECK (kind IN ('image', 'video')),
    endpoint        TEXT NOT NULL DEFAULT 'bundled-comfyui' CHECK (length(endpoint) > 0),
    ui_json         TEXT NOT NULL DEFAULT '' ,
    api_json        TEXT NOT NULL CHECK (json_valid(api_json)),
    assignment_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(assignment_json)),
    version         INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Every version of a workflow with what ran: an old Take names comfy:<id>@v<N>, and an old version can still be run.
CREATE TABLE comfy_workflow_version (
    workflow_id     TEXT NOT NULL REFERENCES comfy_workflow(id) ON DELETE CASCADE,
    version         INTEGER NOT NULL CHECK (version >= 1),
    api_json        TEXT NOT NULL CHECK (json_valid(api_json)),
    assignment_json TEXT NOT NULL CHECK (json_valid(assignment_json)),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (workflow_id, version)
);

-- +goose Down
DROP TABLE comfy_workflow_version;
DROP TABLE comfy_workflow;
