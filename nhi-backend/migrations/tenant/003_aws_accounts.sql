-- Apply once per tenant schema (after 001). Safe to run again:
--   psql "$DSN" -v schema=bimasugam -f migrations/tenant/003_aws_accounts.sql
--
-- One row per AWS account the tenant connects on the Integrations page.
--
--   client_aws_account_id  the customer's 12-digit account
--   client_role_arn        read-only role they created for us
--   regions                regions they allow us to read (a sync covers these only)
--   external_id            the tenant's collector_external_id the role was verified with
--   is_connected           true while the latest check passed every step
--   account_status         pending        saved, has never passed every check
--                          connected      latest check passed
--                          action_needed  passed before, latest check failed
--   github_connected       exposed-credential scanning of GitHub is enabled and verified
--   codecommit_connected   exposed-credential scanning of CodeCommit is enabled and verified
--
-- The scope table (<schema>.accounts: one row per account, regions text[]) is
-- kept in step by the API; this table is the source of truth for the connection.
BEGIN;

CREATE SCHEMA IF NOT EXISTS :"schema";

CREATE TABLE IF NOT EXISTS :"schema".aws_accounts (
    client_aws_account_id  text PRIMARY KEY CHECK (client_aws_account_id ~ '^[0-9]{12}$'),
    client_role_arn        text NOT NULL,
    regions                text[] NOT NULL DEFAULT '{}',
    external_id            text NOT NULL,
    is_connected           boolean NOT NULL DEFAULT false,
    account_status         text NOT NULL DEFAULT 'pending'
        CHECK (account_status IN ('pending', 'connected', 'action_needed')),
    github_connected       boolean NOT NULL DEFAULT false,
    codecommit_connected   boolean NOT NULL DEFAULT false,

    -- Last connection check (shown on the page).
    checks                 jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(checks) = 'array'),
    last_verified_at       timestamptz NOT NULL DEFAULT now(),
    last_verified_by       text NOT NULL DEFAULT '',
    connected_at           timestamptz,

    -- Exposed credentials: what was last checked for each platform.
    github_secret_arn      text,
    github_checks          jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(github_checks) = 'array'),
    github_verified_at     timestamptz,
    codecommit_checks      jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(codecommit_checks) = 'array'),
    codecommit_verified_at timestamptz,
    exposure_updated_by    text NOT NULL DEFAULT '',

    -- Sync queue. The API sets 'queued'; the discovery worker claims it.
    sync_status            text NOT NULL DEFAULT 'idle'
        CHECK (sync_status IN ('idle', 'queued', 'running', 'succeeded', 'failed')),
    sync_requested_at      timestamptz,
    sync_requested_by      text NOT NULL DEFAULT '',
    sync_started_at        timestamptz,
    last_synced_at         timestamptz,
    last_sync_error        text NOT NULL DEFAULT '',

    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    -- Region codes are checked against the API's catalogue on every write.
    CHECK (cardinality(regions) <= 64 AND array_position(regions, NULL) IS NULL)
);

-- The worker's queue.
CREATE INDEX IF NOT EXISTS aws_accounts_sync_queue
    ON :"schema".aws_accounts (sync_requested_at) WHERE sync_status = 'queued';

COMMIT;

-- Discovery worker contract: claim one queued account at a time
--   UPDATE <schema>.aws_accounts SET sync_status = 'running', sync_started_at = now()
--    WHERE client_aws_account_id = (
--          SELECT client_aws_account_id FROM <schema>.aws_accounts
--           WHERE sync_status = 'queued' ORDER BY sync_requested_at
--           FOR UPDATE SKIP LOCKED LIMIT 1)
--   RETURNING client_aws_account_id, client_role_arn, external_id, regions;
-- read only `regions`, then set sync_status = 'succeeded', last_synced_at = now(),
-- last_sync_error = '' (or 'failed' with a short last_sync_error).
--
-- Secret scanner contract: scan CodeCommit where codecommit_connected, and the
-- GitHub token stored at github_secret_arn where github_connected, by assuming
-- client_role_arn with external_id.
