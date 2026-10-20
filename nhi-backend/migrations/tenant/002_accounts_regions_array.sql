-- Apply once per tenant schema. Safe to run again; a no-op on a schema that
-- already uses the array layout:
--   psql "$DSN" -v schema=bimasugam -f migrations/tenant/002_accounts_regions_array.sql
--
-- The scope table changed from one row per region
--   accounts (tenant_id, account_id, region text, is_default)
-- to one row per account
--   accounts (tenant_id, account_id, regions text[], is_default)
-- which is what the settings, filters and scope resolver read. This converts
-- an old-layout table in place, keeping every region: an account's regions
-- become one array, and it stays in the default scope if any of its rows was.
BEGIN;

SELECT set_config('nhi.migrate_schema', :'schema', true);

DO $$
DECLARE
    s text := current_setting('nhi.migrate_schema');
    has_region boolean;
    has_regions boolean;
BEGIN
    SELECT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = s AND table_name = 'accounts' AND column_name = 'region'),
           EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = s AND table_name = 'accounts' AND column_name = 'regions')
      INTO has_region, has_regions;

    IF NOT has_region THEN
        RAISE NOTICE '%.accounts already uses the regions array', s;
        RETURN;
    END IF;

    -- The conversion rewrites the rows with only the scope columns. If
    -- 004_accounts_connection.sql already ran and an account is connected,
    -- that would erase the connection, so stop and let a person decide.
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = s AND table_name = 'accounts' AND column_name = 'client_role_arn') THEN
        EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I.accounts WHERE COALESCE(client_role_arn, '''') <> '''')', s) INTO has_regions;
        IF has_regions THEN
            RAISE EXCEPTION '%.accounts has connected accounts on the old one-row-per-region layout; converting would erase them. Convert by hand.', s;
        END IF;
    END IF;

    IF NOT has_regions THEN
        EXECUTE format('ALTER TABLE %I.accounts ADD COLUMN regions text[] NOT NULL DEFAULT ''{}''', s);
    END IF;

    EXECUTE format($q$
        CREATE TEMP TABLE nhi_accounts_merged ON COMMIT DROP AS
        SELECT tenant_id,
               account_id,
               COALESCE(ARRAY(
                   SELECT DISTINCT BTRIM(r) FROM unnest(
                       array_agg(region) || COALESCE(array_agg(x) FILTER (WHERE x IS NOT NULL), '{}'::text[])
                   ) AS r
                   WHERE r IS NOT NULL AND BTRIM(r) <> ''
                   ORDER BY 1), '{}'::text[]) AS regions,
               BOOL_OR(COALESCE(is_default, false)) AS is_default
        FROM %1$I.accounts
        LEFT JOIN LATERAL unnest(regions) AS x ON true
        GROUP BY tenant_id, account_id$q$, s);

    EXECUTE format('DELETE FROM %I.accounts', s);
    EXECUTE format('ALTER TABLE %I.accounts DROP COLUMN region', s);
    EXECUTE format('INSERT INTO %I.accounts (tenant_id, account_id, regions, is_default)
                    SELECT tenant_id, account_id, regions, is_default FROM nhi_accounts_merged', s);
    RAISE NOTICE 'converted %.accounts to one row per account', s;
END $$;

COMMIT;
