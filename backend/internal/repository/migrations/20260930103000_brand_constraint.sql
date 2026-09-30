-- +goose Up
-- Safety / Brand Constraints of a Project (F-23, UC-9, spec/architecture/er.md §2.4). They are applied to
-- Prompt generation, the AI Video Judge and Final QC. enforcement_level: 'lock' fails a violating Take or
-- deliverable whatever its score is; 'advisory' only warns. value is {"text": "..."}.
CREATE TABLE brand_constraint (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    type              TEXT NOT NULL CHECK (type IN (
        'brand_guideline', 'forbidden_expression', 'required_disclaimer', 'product_claim',
        'logo_rule', 'character_restriction', 'asset_rights', 'platform_rule')),
    enforcement_level TEXT NOT NULL DEFAULT 'lock' CHECK (enforcement_level IN ('lock', 'advisory')),
    value             TEXT NOT NULL CHECK (json_valid(value)),
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_brand_constraint_project ON brand_constraint (project_id);

-- +goose Down
DROP TABLE brand_constraint;
