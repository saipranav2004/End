-- Run once per tenant schema after inventory data is present:
--   psql "$DSN" -v schema=tenant_a -f migrations/tenant/002_register_global_region.sql
-- Register the synthetic AWS "global" region for accounts that have
-- non-deleted global resources, by adding 'global' to that account's
-- accounts.regions array (one row per account: tenant_id, account_id,
-- regions text[], is_default). No tenant ID, account ID or database name is
-- hard-coded.
--
-- Re-runnable: 'global' is added only where it is not already present.
-- A schema still on the old one-row-per-region layout must run
-- 004_accounts_regions_array.sql first.
BEGIN;

WITH global_account_ids AS (
    SELECT DISTINCT resources.account_id
    FROM (
        SELECT i.account_id
        FROM :"schema".identities AS i
        WHERE i.deleted_at IS NULL
          AND LOWER(BTRIM(i.region)) = 'global'
          AND i.account_id IS NOT NULL
          AND BTRIM(i.account_id) <> ''

        UNION

        SELECT c.account_id
        FROM :"schema".credentials AS c
        WHERE c.deleted_at IS NULL
          AND LOWER(BTRIM(c.region)) = 'global'
          AND c.account_id IS NOT NULL
          AND BTRIM(c.account_id) <> ''
    ) AS resources
)
UPDATE :"schema".accounts AS a
SET regions = array_append(COALESCE(a.regions, '{}'::text[]), 'global')
FROM global_account_ids AS g
WHERE g.account_id = a.account_id
  AND NOT EXISTS (
      SELECT 1 FROM unnest(COALESCE(a.regions, '{}'::text[])) AS r
      WHERE LOWER(BTRIM(r)) = 'global'
  );

COMMIT;
