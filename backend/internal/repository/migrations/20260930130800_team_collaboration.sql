-- +goose Up
-- Team Collaboration (spec/spec.md §27.1, M3): review and approval by handing files over. No accounts,
-- no server, no shared state: the owner (who has the Project) makes a Review Package from one Export and
-- imports the Review Responses that come back; a reviewer imports the package into an instance of their own.
--
-- Owner side (belongs to a Project, travels in the Project bundle):
--   review_request   one Review Package: the Export it carries and the owner's note.
--   review_response  one Review Response imported for it. id is the reviewer's response_id, so the same
--                    file is imported once. reviewer_name is what the reviewer typed (not verified).
--   review_comment   a comment of a response, optionally at a position of the video (timecode_ms).
CREATE TABLE review_request (
    id         TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    export_id  TEXT NOT NULL REFERENCES export (id) ON DELETE CASCADE,
    note       TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_review_request_project ON review_request (project_id, created_at);

CREATE TABLE review_response (
    id                TEXT PRIMARY KEY,
    review_request_id TEXT NOT NULL REFERENCES review_request (id) ON DELETE CASCADE,
    reviewer_name     TEXT NOT NULL,
    decision          TEXT NOT NULL CHECK (decision IN ('approved', 'changes_requested')),
    comment           TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    imported_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_review_response_request ON review_response (review_request_id);

CREATE TABLE review_comment (
    id                 TEXT PRIMARY KEY,
    review_response_id TEXT NOT NULL REFERENCES review_response (id) ON DELETE CASCADE,
    timecode_ms        INTEGER CHECK (timecode_ms IS NULL OR timecode_ms >= 0),
    body               TEXT NOT NULL,
    resolved           INTEGER NOT NULL DEFAULT 0 CHECK (resolved IN (0, 1))
);
CREATE INDEX idx_review_comment_response ON review_comment (review_response_id);

-- Reviewer side (belongs to no Project; this instance's own state, not in the Project bundle):
--   review_inbox          one imported Review Package with the reviewer's decision. video_path is
--                         relative to the assets directory. response_id is new after every change of the
--                         decision or the comments, so a changed answer is a different response.
--   review_inbox_comment  a comment of the reviewer.
CREATE TABLE review_inbox (
    id            TEXT PRIMARY KEY,
    review_id     TEXT NOT NULL UNIQUE,
    project_name  TEXT NOT NULL,
    note          TEXT NOT NULL DEFAULT '',
    video_path    TEXT NOT NULL,
    file_name     TEXT NOT NULL,
    resolution    TEXT NOT NULL,
    aspect_ratio  TEXT NOT NULL,
    codec         TEXT NOT NULL,
    reviewer_name TEXT NOT NULL DEFAULT '',
    decision      TEXT CHECK (decision IS NULL OR decision IN ('approved', 'changes_requested')),
    comment       TEXT NOT NULL DEFAULT '',
    response_id   TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE review_inbox_comment (
    id          TEXT PRIMARY KEY,
    inbox_id    TEXT NOT NULL REFERENCES review_inbox (id) ON DELETE CASCADE,
    timecode_ms INTEGER CHECK (timecode_ms IS NULL OR timecode_ms >= 0),
    body        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_review_inbox_comment_inbox ON review_inbox_comment (inbox_id);

-- +goose Down
DROP TABLE review_inbox_comment;
DROP TABLE review_inbox;
DROP TABLE review_comment;
DROP TABLE review_response;
DROP TABLE review_request;
