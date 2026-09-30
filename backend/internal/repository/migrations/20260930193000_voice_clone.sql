-- +goose Up
-- Cloned voices of the Audio Director (F-20, ElevenLabs instant voice cloning): the voice that
-- speaks the lines of one speaker of the script in a Project. One voice per speaker.
CREATE TABLE voice_clone (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    speaker    TEXT NOT NULL,
    name       TEXT NOT NULL,
    voice_id   TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (project_id, speaker)
);

-- +goose Down
DROP TABLE voice_clone;
