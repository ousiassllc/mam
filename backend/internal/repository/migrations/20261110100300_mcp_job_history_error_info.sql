-- +goose Up
-- The failure of a job as the common error (Issue #1010, spec/architecture/errors.md): the JSON of apperr.Record, as the job table keeps it
-- (error_info), so that get_job and list_jobs have it after the job table row is gone. '' for a job that did not fail or whose failure was not kept.
ALTER TABLE mcp_job_history ADD COLUMN error_info TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE mcp_job_history DROP COLUMN error_info;
