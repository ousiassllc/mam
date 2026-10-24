-- +goose Up
-- Bearer tokens of the built-in MCP server (spec/architecture/mcp.md). Only the SHA-256 of the value is
-- stored: the value is shown once, when the token is issued, and cannot be read back. token_prefix is the
-- first characters of the value, kept for display so that a person can tell the tokens apart.
CREATE TABLE mcp_token (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL CHECK (length(name) > 0),
    token_hash   TEXT NOT NULL UNIQUE,
    token_prefix TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_used_at TEXT,
    revoked_at   TEXT
);

-- +goose Down
DROP TABLE mcp_token;
