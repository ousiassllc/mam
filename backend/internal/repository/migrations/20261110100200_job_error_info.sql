-- +goose Up
-- The failure of a job as the common error (Issue #1010, spec/architecture/errors.md): the JSON of apperr.Record (code, arguments, detail with the secrets
-- hidden, error id). "error" keeps the sentence the job queue always wrote. '' for a job that did not fail, and for the jobs that failed before this column
-- existed (they have the sentence only).
ALTER TABLE job ADD COLUMN error_info TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE job DROP COLUMN error_info;
