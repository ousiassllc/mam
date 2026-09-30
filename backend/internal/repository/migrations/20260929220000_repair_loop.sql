-- +goose Up
-- Repair / Retake Loop (F-14, spec/spec.md §13).
--
-- judge_result.scores: the 0-100 score of every evaluation axis of the Production Template, when the
-- Judge gave them. The Retry Policy's Minimum acceptance score is applied to them. NULL: no scores.
ALTER TABLE judge_result ADD COLUMN scores TEXT CHECK (scores IS NULL OR json_valid(scores));

-- The Jev shadow run (spec/architecture/model-adapters.md §4.5): whenever the Repair Agent (the Reasoning
-- LLM) chooses a strategy, Jev is asked the same question in the background. Nothing here changes what
-- the Repair Agent does; the rows only measure whether Jev agrees with it, how sure Jev is, how long it
-- takes and what it costs. error is set (and the other columns are empty) when the call failed.
CREATE TABLE repair_shadow (
    id               TEXT PRIMARY KEY,
    repair_action_id TEXT NOT NULL REFERENCES repair_action (id) ON DELETE CASCADE,
    model            TEXT NOT NULL,
    jev_choice       TEXT NOT NULL DEFAULT '',
    confidence       REAL,
    probabilities    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(probabilities)),
    decided_action   TEXT NOT NULL,
    agreed           INTEGER NOT NULL DEFAULT 0 CHECK (agreed IN (0, 1)),
    latency_ms       INTEGER NOT NULL DEFAULT 0,
    input_tokens     INTEGER NOT NULL DEFAULT 0,
    output_tokens    INTEGER NOT NULL DEFAULT 0,
    cost_usd         REAL NOT NULL DEFAULT 0,
    error            TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_repair_shadow_action ON repair_shadow (repair_action_id);

-- +goose Down
DROP TABLE repair_shadow;
ALTER TABLE judge_result DROP COLUMN scores;
