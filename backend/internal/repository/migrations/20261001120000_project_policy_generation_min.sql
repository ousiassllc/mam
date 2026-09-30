-- +goose Up
-- max_generation_seconds matches the API and domain.MinGenerationSeconds (10). SQLite cannot
-- alter a CHECK, so the table is rebuilt. Rows already stored below 10 are raised to 10.
UPDATE project_policy
SET max_generation_seconds = 10
WHERE max_generation_seconds IS NOT NULL AND max_generation_seconds < 10;

CREATE TABLE project_policy_new (
    project_id             TEXT PRIMARY KEY REFERENCES project (id) ON DELETE CASCADE,
    max_retries            INTEGER CHECK (max_retries IS NULL OR max_retries BETWEEN 0 AND 10),
    max_generation_seconds INTEGER CHECK (max_generation_seconds IS NULL OR max_generation_seconds BETWEEN 10 AND 3600),
    min_acceptance_score   INTEGER CHECK (min_acceptance_score IS NULL OR min_acceptance_score BETWEEN 0 AND 100),
    min_axis_score         INTEGER CHECK (min_axis_score IS NULL OR min_axis_score BETWEEN 0 AND 100)
);
INSERT INTO project_policy_new (project_id, max_retries, max_generation_seconds, min_acceptance_score, min_axis_score)
SELECT project_id, max_retries, max_generation_seconds, min_acceptance_score, min_axis_score FROM project_policy;
DROP TABLE project_policy;
ALTER TABLE project_policy_new RENAME TO project_policy;

-- +goose Down
CREATE TABLE project_policy_old (
    project_id             TEXT PRIMARY KEY REFERENCES project (id) ON DELETE CASCADE,
    max_retries            INTEGER CHECK (max_retries IS NULL OR max_retries BETWEEN 0 AND 10),
    max_generation_seconds INTEGER CHECK (max_generation_seconds IS NULL OR max_generation_seconds BETWEEN 1 AND 3600),
    min_acceptance_score   INTEGER CHECK (min_acceptance_score IS NULL OR min_acceptance_score BETWEEN 0 AND 100),
    min_axis_score         INTEGER CHECK (min_axis_score IS NULL OR min_axis_score BETWEEN 0 AND 100)
);
INSERT INTO project_policy_old (project_id, max_retries, max_generation_seconds, min_acceptance_score, min_axis_score)
SELECT project_id, max_retries, max_generation_seconds, min_acceptance_score, min_axis_score FROM project_policy;
DROP TABLE project_policy;
ALTER TABLE project_policy_old RENAME TO project_policy;
