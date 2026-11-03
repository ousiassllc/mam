-- +goose Up
-- The recipes of the generation test (Issue #977, spec/architecture/gentest/er-gentest.md): "今の設定" kept under a name. params is JSON (the model, the
-- sampling values, the seed, the negative prompt, the LoRA, the reference strength, the size, the quality and the values of the exposed inputs); it holds
-- no prompt text and no input image. easy = 1 puts the recipe among the choices of the easy tier. No foreign key: the model is named by its identifier
-- and may be gone (the recipe then says so and is kept).
CREATE TABLE gentest_recipe (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL COLLATE NOCASE UNIQUE CHECK (length(name) > 0),
    params     TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params)),
    easy       INTEGER NOT NULL DEFAULT 0 CHECK (easy IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- +goose Down
DROP TABLE gentest_recipe;
