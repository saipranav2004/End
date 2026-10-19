-- Apply this migration once per tenant schema in nhi_db:
--   psql "$DSN" -v schema=tenant_a -f migrations/tenant/001_user_settings.sql
-- user_id is the canonical email from the JWT's user_id claim. The tenant
-- schema is the tenant boundary; do not put this table in nhi_global.
BEGIN;

-- The scanner schema is created by the discovery pipeline, but this migration
-- is often the first thing applied to a fresh tenant database, so create it
-- here too. Idempotent, and it makes the migration runnable standalone.
CREATE SCHEMA IF NOT EXISTS :"schema";

CREATE TABLE IF NOT EXISTS :"schema".user_settings (
    user_id                text PRIMARY KEY,
    selected_account_ids   jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(selected_account_ids) = 'array'),
    selected_regions       jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(selected_regions) = 'array'),
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

COMMIT;
