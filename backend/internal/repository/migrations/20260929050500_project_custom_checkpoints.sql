-- +goose Up
-- Confirmation checkpoints chosen for CUSTOM CHECK (F-03, spec/spec.md §4.4): a JSON array of
-- checkpoint names (domain.Checkpoint). Ignored for FULL AUTO and KEY CHECK.
ALTER TABLE project ADD COLUMN custom_checkpoints TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(custom_checkpoints));

-- +goose Down
ALTER TABLE project DROP COLUMN custom_checkpoints;
