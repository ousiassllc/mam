-- +goose Up
-- The AI Video Judge returns a Repair Strategy next to its issues (spec/spec.md §12.3, F-14): the
-- recommended action and its instructions, as JSON. NULL means the Judge recommended nothing
-- (a passing Take).
ALTER TABLE judge_result ADD COLUMN repair_strategy TEXT CHECK (repair_strategy IS NULL OR json_valid(repair_strategy));

-- +goose Down
ALTER TABLE judge_result DROP COLUMN repair_strategy;
