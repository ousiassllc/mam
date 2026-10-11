-- +goose Up
-- ChatRepository.ListByRef (job state changes, Judge saves) filters chat_message by
-- (ref_type, ref_id) without a project_id, which scanned every message.
CREATE INDEX idx_chat_message_ref ON chat_message (ref_type, ref_id);
-- ledgerBudget sums per shot_id, and deleting a shot/take makes SQLite look up
-- cost_ledger_entry by shot_id/take_id for the ON DELETE SET NULL foreign keys.
CREATE INDEX idx_cost_ledger_shot ON cost_ledger_entry (shot_id);
CREATE INDEX idx_cost_ledger_take ON cost_ledger_entry (take_id);

-- +goose Down
DROP INDEX idx_cost_ledger_take;
DROP INDEX idx_cost_ledger_shot;
DROP INDEX idx_chat_message_ref;
