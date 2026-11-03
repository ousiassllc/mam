-- +goose Up
-- The failure of a result as the common error (Issue #1010, spec/architecture/errors.md): the JSON of apperr.Record (code, arguments, detail, error id).
-- "error" keeps the sentence for the places that show one line. '' for a result that did not fail, and for the results that failed before this column existed
-- (they have the sentence only).
ALTER TABLE gentest_result ADD COLUMN error_info TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE gentest_result DROP COLUMN error_info;
