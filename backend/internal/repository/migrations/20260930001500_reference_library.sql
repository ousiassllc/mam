-- +goose Up
-- Reference Library (spec/spec.md §10.1, M2): the images of asset.type = 'reference' of a Project
-- with a kind, a name and tags, so that the material of a Reference Pack can be found and reused.
-- The Keyframe belongs to one Shot and is not registered.
CREATE TABLE reference_item (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    asset_id       TEXT NOT NULL UNIQUE REFERENCES asset (id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('character', 'product', 'location', 'style', 'lighting')),
    name           TEXT NOT NULL,
    tags           TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags)),
    source         TEXT NOT NULL CHECK (source IN ('generated', 'uploaded')),
    source_shot_id TEXT REFERENCES shot (id) ON DELETE SET NULL,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_reference_item_project ON reference_item (project_id, kind);

-- The Reference Packs made before the Library existed are registered as generated images.
INSERT OR IGNORE INTO reference_item (id, project_id, asset_id, kind, name, tags, source, source_shot_id, created_at)
SELECT lower(hex(randomblob(16))), a.project_id, a.id, json_extract(r.value, '$.kind'),
       substr(COALESCE(NULLIF(trim(json_extract(r.value, '$.name')), ''), json_extract(r.value, '$.kind')), 1, 100),
       '[]', 'generated', s.id, a.created_at
FROM shot s, json_each(s.shot_spec, '$.references') r
JOIN asset a ON a.id = json_extract(r.value, '$.asset_id') AND a.type = 'reference'
WHERE json_extract(r.value, '$.kind') IN ('character', 'product', 'location', 'style', 'lighting');

-- +goose Down
DROP TABLE reference_item;
