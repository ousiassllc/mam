-- +goose Up
-- History of the long operations the MCP server started (spec/architecture/mcp.md §6, er-mcp.md): one row per
-- job_id the tools hand out, including the kinds that have no row in `job` (job_<UUIDv7>). It lets get_job
-- answer after a restart and list_jobs search the past. A row is written when the operation starts and
-- updated with what get_job / list_jobs saw (state, message, error); rows older than 30 days, and the oldest
-- beyond 10,000, are deleted (repository/jobhistory).
--
-- id            the job_id: the `job` row's id when queue_job_id is set, otherwise job_<UUIDv7>
-- target        the id the operation works on (shot_id, take_id, ...); '' when it has none
-- project_id    the Project of the target, NULL when it has none. No foreign key: the history outlives the Project
-- queue_job_id  the `job` row behind the operation, NULL when the service queued none
-- state         queued | running | succeeded | failed | cancelled (the states of mcpkit.JobState)
-- error_code    the <code> of the failure's "<code>: <message>" (validation, conflict, internal, ...); '' unless failed
-- error_message the failure's reason as get_job shows it
-- error_id      the correlation id of an `internal` failure, the same value as the error_id attribute of its log line
-- result        JSON object of the result, NULL when there is none
-- finished_at   what the job queue recorded as the end (the same as get_job's finished_at)
-- ended_at      when the end was known: finished_at when there is one, otherwise when mam first saw the end
CREATE TABLE mcp_job_history (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (length(kind) > 0),
    target        TEXT NOT NULL DEFAULT '',
    project_id    TEXT,
    queue_job_id  TEXT,
    state         TEXT NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    message       TEXT NOT NULL DEFAULT '',
    done          INTEGER NOT NULL DEFAULT 0,
    total         INTEGER NOT NULL DEFAULT 0,
    result        TEXT CHECK (result IS NULL OR json_valid(result)),
    error_code    TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    error_id      TEXT,
    started_at    TEXT NOT NULL,
    finished_at   TEXT,
    ended_at      TEXT
);

-- list_jobs (newest first, keyset paging) and the pruning by age and by count walk this index.
CREATE INDEX idx_mcp_job_history_started ON mcp_job_history (started_at DESC, id DESC);
-- The jobs still to be settled (list_jobs looks at them before it answers).
CREATE INDEX idx_mcp_job_history_open ON mcp_job_history (started_at) WHERE state IN ('queued', 'running');

-- +goose Down
DROP INDEX idx_mcp_job_history_open;
DROP INDEX idx_mcp_job_history_started;
DROP TABLE mcp_job_history;
