-- +goose Up
-- When a generation test result began to run (Issue #1002, spec/architecture/gentest/er-gentest.md): finished_at - started_at is how long the
-- call took, which the estimate of the batch gives as the time a model needs at a resolution and a length. NULL while queued, and for the
-- results that ran before this column existed (they are left out of the estimate: created_at is when the batch was queued, not when the call began).
ALTER TABLE gentest_result ADD COLUMN started_at TEXT;

-- +goose Down
ALTER TABLE gentest_result DROP COLUMN started_at;
