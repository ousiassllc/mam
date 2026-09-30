-- +goose Up
-- Final QC (F-26, spec/spec.md §22 FINAL_QC): every run is kept as history, so the findings of a FAIL
-- stay visible after the Project went back to EDITING / AUDIO and the next run has passed.
CREATE TABLE final_qc_result (
    id                        TEXT PRIMARY KEY,
    project_id                TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    audio_timeline_version_id TEXT NOT NULL REFERENCES audio_timeline_version (id) ON DELETE CASCADE,
    result                    TEXT NOT NULL CHECK (result IN ('pass', 'fail')),
    findings                  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(findings)),
    created_at                TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_final_qc_result_project ON final_qc_result (project_id, created_at);

-- +goose Down
DROP INDEX idx_final_qc_result_project;
DROP TABLE final_qc_result;
