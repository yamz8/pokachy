CREATE INDEX IF NOT EXISTS account_user ON account(userId, providerId);
CREATE INDEX IF NOT EXISTS blocks_blocked ON blocks(blocked);
CREATE INDEX IF NOT EXISTS friendships_requester ON friendships(requester);
CREATE INDEX IF NOT EXISTS reports_reported ON reports(reported);
CREATE INDEX IF NOT EXISTS reports_reporter ON reports(reporter);
CREATE INDEX IF NOT EXISTS reports_queue ON reports(resolved, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS account_handoffs_user ON account_handoffs(user_id);
