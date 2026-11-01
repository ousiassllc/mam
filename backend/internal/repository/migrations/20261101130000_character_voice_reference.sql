-- +goose Up
-- Reference voice of a Character (F-20, F-29; spec/architecture/er.md §4 "キャラクターの参照音声"): the recording that
-- a local voice provider clones, with its transcript and language, as one JSON object on the character row:
-- {file_path, mime_type, size_bytes, duration_ms, text, language} (domain/voiceref.Reference). NULL = none.
-- The Series canon row and the Episode copies hold their own copy of the object, which shares the file (the same
-- rule as the canon images), so a plain column is enough: no table rebuild (asset.type has a CHECK) and the canon
-- snapshot (series_version.state) carries it as it carries the other canon fields.
ALTER TABLE character ADD COLUMN voice_reference TEXT CHECK (voice_reference IS NULL OR json_valid(voice_reference));

-- +goose Down
ALTER TABLE character DROP COLUMN voice_reference;
