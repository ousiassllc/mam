-- +goose NO TRANSACTION
-- +goose Up
-- Series / Episode Memory (F-29, spec/architecture/er.md §4 "Series 正本と Episode の継承"):
--   series          the Series (a set of Episodes) with the canonical Style (text and reference image)
--   series_version  every change of the canon (Characters, Products, Locations, Style) appended as a
--                   full snapshot, never updated (the Series counterpart of world_state_version)
--   project         series_id / episode_number ((series_id, episode_number) UNIQUE), the World State
--                   version the Episode started from and the canon version it inherited
--   character / product / location
--                   project_id becomes nullable; exactly one of project_id (a row of an Episode) and
--                   series_id (the canonical row of the Series) is set; origin_id is the canonical
--                   row an Episode row was copied from.
-- SQLite cannot drop NOT NULL, so the three tables are rebuilt (SQLite's "generalized ALTER TABLE"
-- procedure). location is referenced by scene.location_id, so foreign keys are off while the tables
-- are swapped: dropping location with them on would run scene's ON DELETE SET NULL. The writer pool
-- has one connection, so the PRAGMA holds for the whole migration and is switched back on at the end.
PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE series (
    id                       TEXT PRIMARY KEY,
    name                     TEXT NOT NULL,
    style                    TEXT NOT NULL DEFAULT '',
    style_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE series_version (
    id             TEXT PRIMARY KEY,
    series_id      TEXT NOT NULL REFERENCES series (id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    state          TEXT NOT NULL CHECK (json_valid(state)),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (series_id, version_number)
);

ALTER TABLE project ADD COLUMN series_id TEXT REFERENCES series (id);
ALTER TABLE project ADD COLUMN episode_number INTEGER CHECK (episode_number IS NULL OR episode_number >= 1);
ALTER TABLE project ADD COLUMN inherited_world_state_version_id TEXT REFERENCES world_state_version (id) ON DELETE SET NULL;
ALTER TABLE project ADD COLUMN inherited_series_version_id TEXT REFERENCES series_version (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX idx_project_series ON project (series_id, episode_number);

CREATE TABLE character_new (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT REFERENCES project (id) ON DELETE CASCADE,
    series_id               TEXT REFERENCES series (id) ON DELETE CASCADE,
    origin_id               TEXT REFERENCES character_new (id) ON DELETE SET NULL,
    name                    TEXT NOT NULL,
    face_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    body_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    hairstyle               TEXT,
    wardrobe                TEXT,
    voice                   TEXT,
    personality             TEXT,
    emotion                 TEXT,
    position                TEXT,
    current_action          TEXT,
    CHECK ((project_id IS NULL) <> (series_id IS NULL))
);
INSERT INTO character_new (id, project_id, name, face_reference_asset_id, body_reference_asset_id, hairstyle, wardrobe, voice, personality, emotion, position, current_action)
    SELECT id, project_id, name, face_reference_asset_id, body_reference_asset_id, hairstyle, wardrobe, voice, personality, emotion, position, current_action FROM character;
DROP TABLE character;
ALTER TABLE character_new RENAME TO character;
CREATE INDEX idx_character_project ON character (project_id);
CREATE INDEX idx_character_series ON character (series_id);
CREATE INDEX idx_character_origin ON character (origin_id);

CREATE TABLE product_new (
    id                          TEXT PRIMARY KEY,
    project_id                  TEXT REFERENCES project (id) ON DELETE CASCADE,
    series_id                   TEXT REFERENCES series (id) ON DELETE CASCADE,
    origin_id                   TEXT REFERENCES product_new (id) ON DELETE SET NULL,
    name                        TEXT NOT NULL,
    geometry_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    label_reference_asset_id    TEXT REFERENCES asset (id) ON DELETE SET NULL,
    material                    TEXT,
    orientation                 TEXT,
    condition                   TEXT,
    CHECK ((project_id IS NULL) <> (series_id IS NULL))
);
INSERT INTO product_new (id, project_id, name, geometry_reference_asset_id, label_reference_asset_id, material, orientation, condition)
    SELECT id, project_id, name, geometry_reference_asset_id, label_reference_asset_id, material, orientation, condition FROM product;
DROP TABLE product;
ALTER TABLE product_new RENAME TO product;
CREATE INDEX idx_product_project ON product (project_id);
CREATE INDEX idx_product_series ON product (series_id);
CREATE INDEX idx_product_origin ON product (origin_id);

CREATE TABLE location_new (
    id                        TEXT PRIMARY KEY,
    project_id                TEXT REFERENCES project (id) ON DELETE CASCADE,
    series_id                 TEXT REFERENCES series (id) ON DELETE CASCADE,
    origin_id                 TEXT REFERENCES location_new (id) ON DELETE SET NULL,
    name                      TEXT NOT NULL,
    layout_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    time                      TEXT,
    lighting_state            TEXT,
    weather                   TEXT,
    CHECK ((project_id IS NULL) <> (series_id IS NULL))
);
INSERT INTO location_new (id, project_id, name, layout_reference_asset_id, time, lighting_state, weather)
    SELECT id, project_id, name, layout_reference_asset_id, time, lighting_state, weather FROM location;
DROP TABLE location;
ALTER TABLE location_new RENAME TO location;
CREATE INDEX idx_location_project ON location (project_id);
CREATE INDEX idx_location_series ON location (series_id);
CREATE INDEX idx_location_origin ON location (origin_id);

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN;

DELETE FROM character WHERE project_id IS NULL;
DELETE FROM product WHERE project_id IS NULL;
DELETE FROM location WHERE project_id IS NULL;

CREATE TABLE character_old (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    name                    TEXT NOT NULL,
    face_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    body_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    hairstyle               TEXT,
    wardrobe                TEXT,
    voice                   TEXT,
    personality             TEXT,
    emotion                 TEXT,
    position                TEXT,
    current_action          TEXT
);
INSERT INTO character_old (id, project_id, name, face_reference_asset_id, body_reference_asset_id, hairstyle, wardrobe, voice, personality, emotion, position, current_action)
    SELECT id, project_id, name, face_reference_asset_id, body_reference_asset_id, hairstyle, wardrobe, voice, personality, emotion, position, current_action FROM character;
DROP TABLE character;
ALTER TABLE character_old RENAME TO character;
CREATE INDEX idx_character_project ON character (project_id);

CREATE TABLE product_old (
    id                          TEXT PRIMARY KEY,
    project_id                  TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    name                        TEXT NOT NULL,
    geometry_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    label_reference_asset_id    TEXT REFERENCES asset (id) ON DELETE SET NULL,
    material                    TEXT,
    orientation                 TEXT,
    condition                   TEXT
);
INSERT INTO product_old (id, project_id, name, geometry_reference_asset_id, label_reference_asset_id, material, orientation, condition)
    SELECT id, project_id, name, geometry_reference_asset_id, label_reference_asset_id, material, orientation, condition FROM product;
DROP TABLE product;
ALTER TABLE product_old RENAME TO product;
CREATE INDEX idx_product_project ON product (project_id);

CREATE TABLE location_old (
    id                        TEXT PRIMARY KEY,
    project_id                TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    name                      TEXT NOT NULL,
    layout_reference_asset_id TEXT REFERENCES asset (id) ON DELETE SET NULL,
    time                      TEXT,
    lighting_state            TEXT,
    weather                   TEXT
);
INSERT INTO location_old (id, project_id, name, layout_reference_asset_id, time, lighting_state, weather)
    SELECT id, project_id, name, layout_reference_asset_id, time, lighting_state, weather FROM location;
DROP TABLE location;
ALTER TABLE location_old RENAME TO location;
CREATE INDEX idx_location_project ON location (project_id);

DROP INDEX idx_project_series;
ALTER TABLE project DROP COLUMN inherited_series_version_id;
ALTER TABLE project DROP COLUMN inherited_world_state_version_id;
ALTER TABLE project DROP COLUMN episode_number;
ALTER TABLE project DROP COLUMN series_id;
DROP TABLE series_version;
DROP TABLE series;

COMMIT;
PRAGMA foreign_keys = ON;
