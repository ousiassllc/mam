-- +goose Up
-- Production Analytics (spec/spec.md §28): the events the MVP Success Metrics need and no other table
-- holds. A row is written each time the user does a hand operation of the production (selects or rejects a
-- Take, edits a Shot Spec, directs a regeneration, ...), see spec/spec.md §28.3 for the kinds.
--   override  the operation contradicts what the AI had decided (selects another Take than the AI Pick,
--             rejects the AI Pick, edits the AI's Shot Spec / Rough Cut, asks again for a Shot the Judge passed,
--             requests changes to a checkpoint)
--   kind      'switch_to_director' is the takeover of an AUTO Project. It is not a generation operation
--             and is not counted as a Human Intervention; the AUTO Completion Rate reads it.
-- shot_id / take_id are NULL when the operation is about the Project (a checkpoint, the Rough Cut, the mode).
-- The rows stay when their Shot or Take is deleted (only the Project owns them).
CREATE TABLE human_intervention (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    shot_id    TEXT REFERENCES shot (id) ON DELETE SET NULL,
    take_id    TEXT REFERENCES take (id) ON DELETE SET NULL,
    kind       TEXT NOT NULL CHECK (kind IN (
        'generate', 'regenerate', 'repair', 'select_take', 'reject_take', 'partial_take', 'edit_shot_spec',
        'lock_change', 'edit_timeline', 'request_changes', 'switch_to_director')),
    override   INTEGER NOT NULL DEFAULT 0 CHECK (override IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_human_intervention_project ON human_intervention (project_id, created_at);
CREATE INDEX idx_human_intervention_shot ON human_intervention (shot_id);

-- +goose Down
DROP TABLE human_intervention;
