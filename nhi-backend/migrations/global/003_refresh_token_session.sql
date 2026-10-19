-- Apply once to the shared nhi_db database (after 002).
-- A "session" is the chain of refresh tokens created from one login. The access
-- token carries the session id (jti); logout stamps session_ended_at on every
-- row of the session so the access token stops working immediately.
-- Existing rows get a random session id each. Access tokens issued before this
-- migration have no jti and are rejected, so users must log in again once.
BEGIN;

ALTER TABLE nhi_global.refresh_tokens
    ADD COLUMN IF NOT EXISTS session_id uuid,
    ADD COLUMN IF NOT EXISTS session_ended_at timestamptz;

UPDATE nhi_global.refresh_tokens SET session_id = gen_random_uuid() WHERE session_id IS NULL;

ALTER TABLE nhi_global.refresh_tokens ALTER COLUMN session_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_session
    ON nhi_global.refresh_tokens (session_id);

COMMIT;
