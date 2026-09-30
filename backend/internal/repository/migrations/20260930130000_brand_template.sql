-- +goose Up
-- Brand Templates (F-23, UC-9, M3, spec/architecture/er.md §2.4): a named set of Brand / Safety
-- Constraints saved from a Project and applied to new Projects / Episodes. A template is not tied to
-- any Project or Series (there is no Organization entity); applying one COPIES its rows into
-- brand_constraint, so the Project's constraints and the template change independently and a
-- template update never propagates. brand_template_constraint mirrors brand_constraint
-- (value is {"text": "..."}); its order is the insertion order (rowid).
CREATE TABLE brand_template (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE brand_template_constraint (
    id                TEXT PRIMARY KEY,
    template_id       TEXT NOT NULL REFERENCES brand_template (id) ON DELETE CASCADE,
    type              TEXT NOT NULL CHECK (type IN (
        'brand_guideline', 'forbidden_expression', 'required_disclaimer', 'product_claim',
        'logo_rule', 'character_restriction', 'asset_rights', 'platform_rule')),
    enforcement_level TEXT NOT NULL DEFAULT 'lock' CHECK (enforcement_level IN ('lock', 'advisory')),
    value             TEXT NOT NULL CHECK (json_valid(value))
);
CREATE INDEX idx_brand_template_constraint_template ON brand_template_constraint (template_id);

-- +goose Down
DROP TABLE brand_template_constraint;
DROP TABLE brand_template;
