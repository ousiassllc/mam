-- +goose Up
-- Draft mark of a Character (Issue #1022, spec/architecture/er.md §4): 1 = made from an image attached to the Brief when the planning could not run for
-- want of an LLM (internal/characterdraft), not by the planning and not by the user. It is cleared when the user saves an edit of the Character.
-- A plain column with a default: the rows that exist are not drafts, and nothing else reads it.
ALTER TABLE character ADD COLUMN draft INTEGER NOT NULL DEFAULT 0 CHECK (draft IN (0, 1));

-- +goose Down
ALTER TABLE character DROP COLUMN draft;
