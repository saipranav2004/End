-- Apply once per tenant schema (after the scanner has created <schema>.accounts).
-- Safe to run again:
--   psql "$DSN" -v schema=bimasugam -f migrations/tenant/004_accounts_connection.sql
--
-- <schema>.accounts is both the scope table (one row per AWS account, regions
-- text[], is_default) and the connection record of the Integrations page.
-- The scanner's columns are kept exactly as they are:
--
--   account_id             the customer's 12-digit account (UNIQUE) - the key
--   client_aws_account_id  same value, written on connect
--   client_role_arn        read-only role they created for us
--   external_id            the tenant's collector_external_id the role was verified with
--   regions                regions a sync may read, plus the synthetic 'global'
--   is_connected           true while the latest check passed every step
--   account_status         pending | connected | action_needed
--   github_enabled         exposed-credential scanning of GitHub is on and verified
--   codecommit_enabled     exposed-credential scanning of CodeCommit is on and verified
--
-- This migration only adds the columns the console needs to show check
-- results and drive the sync queue. Nothing existing is renamed or dropped.
BEGIN;

ALTER TABLE :"schema".accounts
    ADD COLUMN IF NOT EXISTS client_role_arn        varchar(512),
    ADD COLUMN IF NOT EXISTS client_aws_account_id  varchar(12),
    ADD COLUMN IF NOT EXISTS external_id            varchar(255),
    ADD COLUMN IF NOT EXISTS is_connected           boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS account_status         varchar(20) NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS codecommit_enabled     boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS github_enabled         boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS created_at             timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS updated_at             timestamptz NOT NULL DEFAULT now(),

    -- Last connection check (shown on the page).
    ADD COLUMN IF NOT EXISTS checks                 jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS last_verified_at       timestamptz,
    ADD COLUMN IF NOT EXISTS last_verified_by       text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS connected_at           timestamptz,

    -- Exposed credentials: what was last checked for each platform.
    ADD COLUMN IF NOT EXISTS github_secret_arn      text,
    ADD COLUMN IF NOT EXISTS github_checks          jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS github_verified_at     timestamptz,
    ADD COLUMN IF NOT EXISTS codecommit_checks      jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS codecommit_verified_at timestamptz,
    ADD COLUMN IF NOT EXISTS exposure_updated_by    text NOT NULL DEFAULT '',

    -- Sync queue. The API sets 'queued'; the discovery worker claims it.
    ADD COLUMN IF NOT EXISTS sync_status            text NOT NULL DEFAULT 'idle',
    ADD COLUMN IF NOT EXISTS sync_requested_at      timestamptz,
    ADD COLUMN IF NOT EXISTS sync_requested_by      text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sync_started_at        timestamptz,
    ADD COLUMN IF NOT EXISTS last_synced_at         timestamptz,
    ADD COLUMN IF NOT EXISTS last_sync_error        text NOT NULL DEFAULT '';

-- The API upserts on account_id. The scanner normally creates this as
-- account_account_id_unique; add one only when no unique index covers it.
SELECT set_config('nhi.migrate_schema', :'schema', true);
DO $$
DECLARE s text := current_setting('nhi.migrate_schema');
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index i
        JOIN pg_class t ON t.oid = i.indrelid
        JOIN pg_namespace n ON n.oid = t.relnamespace
        JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
        WHERE n.nspname = s AND t.relname = 'accounts' AND i.indisunique
          AND i.indnatts = 1 AND a.attname = 'account_id') THEN
        EXECUTE format('CREATE UNIQUE INDEX accounts_account_id_uq ON %I.accounts (account_id)', s);
    END IF;
END $$;

-- The worker's queue.
CREATE INDEX IF NOT EXISTS accounts_sync_queue
    ON :"schema".accounts (sync_requested_at) WHERE sync_status = 'queued';

COMMIT;

-- Discovery worker contract: claim one queued account at a time
--   UPDATE <schema>.accounts SET sync_status = 'running', sync_started_at = now()
--    WHERE account_id = (
--          SELECT account_id FROM <schema>.accounts
--           WHERE sync_status = 'queued' ORDER BY sync_requested_at
--           FOR UPDATE SKIP LOCKED LIMIT 1)
--   RETURNING account_id, client_role_arn, external_id, regions;
-- then set sync_status = 'succeeded', last_synced_at = now(), last_sync_error = ''
-- (or 'failed' with a short last_sync_error).
--
-- Secret scanner contract: scan CodeCommit where codecommit_enabled, and the
-- GitHub token stored at github_secret_arn where github_enabled, by assuming
-- client_role_arn with external_id.
