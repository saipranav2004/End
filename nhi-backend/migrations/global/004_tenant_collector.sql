-- Apply once to nhi_db, after 001-003. Safe to run again.
--
-- The values each tenant's "Connect AWS Account" page shows, filled in by the
-- super-admin onboarding page:
--
--   domain_name                 acme.com  (lowercase, no "@")
--   collector_aws_account_id    our AWS account the customer's role trusts
--   collector_external_id       unique per tenant; generated when not given
--   collector_event_bridge_arn  our central event bus the customer forwards to
--   collector_regions           regions customers may choose; empty = all
--
-- Columns that already exist are left as they are.
BEGIN;

ALTER TABLE nhi_global.tenants ADD COLUMN IF NOT EXISTS domain_name                text;
ALTER TABLE nhi_global.tenants ADD COLUMN IF NOT EXISTS collector_aws_account_id   text;
ALTER TABLE nhi_global.tenants ADD COLUMN IF NOT EXISTS collector_event_bridge_arn text;
ALTER TABLE nhi_global.tenants ADD COLUMN IF NOT EXISTS collector_regions          text[] NOT NULL DEFAULT '{}';
-- Never change an External ID once a customer's role trusts it.
ALTER TABLE nhi_global.tenants ADD COLUMN IF NOT EXISTS collector_external_id text
    DEFAULT ('ext-' || replace(gen_random_uuid()::text, '-', ''));
-- ADD COLUMN IF NOT EXISTS skips the DEFAULT when the column already exists
-- (as in nhi_db), so set it explicitly.
ALTER TABLE nhi_global.tenants ALTER COLUMN collector_external_id
    SET DEFAULT ('ext-' || replace(gen_random_uuid()::text, '-', ''));

UPDATE nhi_global.tenants
   SET collector_external_id = 'ext-' || replace(gen_random_uuid()::text, '-', '')
 WHERE collector_external_id IS NULL OR btrim(collector_external_id) = '';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_collector_external_id_format') THEN
        -- AWS accepts 2-1224 characters from this alphabet; 16+ keeps it unguessable.
        ALTER TABLE nhi_global.tenants ADD CONSTRAINT tenants_collector_external_id_format
            CHECK (length(collector_external_id) BETWEEN 16 AND 1224
                   AND collector_external_id ~ '^[A-Za-z0-9+=,.@:/_-]+$') NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_collector_account_format') THEN
        ALTER TABLE nhi_global.tenants ADD CONSTRAINT tenants_collector_account_format
            CHECK (collector_aws_account_id IS NULL OR collector_aws_account_id ~ '^[0-9]{12}$') NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_collector_bus_format') THEN
        ALTER TABLE nhi_global.tenants ADD CONSTRAINT tenants_collector_bus_format
            CHECK (collector_event_bridge_arn IS NULL OR (length(collector_event_bridge_arn) <= 320
                   AND collector_event_bridge_arn ~ '^arn:aws[a-z-]*:events:[a-z0-9-]+:[0-9]{12}:event-bus/[.\-_A-Za-z0-9]+$')) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_domain_name_format') THEN
        ALTER TABLE nhi_global.tenants ADD CONSTRAINT tenants_domain_name_format
            CHECK (domain_name IS NULL OR domain_name ~ '^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$') NOT VALID;
    END IF;
END $$;

-- Constraints are NOT VALID so existing rows never block the migration; new
-- and updated rows are checked. Validate once existing rows are clean:
--   ALTER TABLE nhi_global.tenants VALIDATE CONSTRAINT tenants_collector_external_id_format;

CREATE UNIQUE INDEX IF NOT EXISTS uq_tenants_collector_external_id
    ON nhi_global.tenants (collector_external_id) WHERE collector_external_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_tenants_domain_name
    ON nhi_global.tenants (lower(domain_name)) WHERE domain_name IS NOT NULL;

COMMIT;
