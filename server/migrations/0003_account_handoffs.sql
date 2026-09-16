CREATE TABLE account_handoffs (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES user(id) ON DELETE CASCADE, action TEXT NOT NULL CHECK(action IN ('account', 'github', 'email')), expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL);
CREATE INDEX account_handoffs_expires ON account_handoffs(expires_at);
