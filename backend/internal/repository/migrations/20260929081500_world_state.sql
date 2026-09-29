-- +goose Up
-- World State (spec/spec.md §9.2): the parts of a Character that change from Shot to Shot.
-- Camera and Story live in world_state_version.state only; Character/Product/Location rows stay
-- the source of truth of their own fields (F-09).
ALTER TABLE character ADD COLUMN emotion TEXT;
ALTER TABLE character ADD COLUMN position TEXT;
ALTER TABLE character ADD COLUMN current_action TEXT;

-- +goose Down
ALTER TABLE character DROP COLUMN current_action;
ALTER TABLE character DROP COLUMN position;
ALTER TABLE character DROP COLUMN emotion;
