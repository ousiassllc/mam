-- +goose Up
-- Quality/Cost Policy overrides of a Project (F-22, UC-8): Max retries per Shot, Max generation time,
-- Minimum quality thresholds. The Quality Policy and the budget ceilings stay on project. A NULL column
-- means "the default of the Quality Policy"; a Project without a row has no override.
CREATE TABLE project_policy (
    project_id             TEXT PRIMARY KEY REFERENCES project (id) ON DELETE CASCADE,
    max_retries            INTEGER CHECK (max_retries IS NULL OR max_retries BETWEEN 0 AND 10),
    max_generation_seconds INTEGER CHECK (max_generation_seconds IS NULL OR max_generation_seconds BETWEEN 1 AND 3600),
    min_acceptance_score   INTEGER CHECK (min_acceptance_score IS NULL OR min_acceptance_score BETWEEN 0 AND 100),
    min_axis_score         INTEGER CHECK (min_axis_score IS NULL OR min_axis_score BETWEEN 0 AND 100)
);

-- +goose Down
DROP TABLE project_policy;
