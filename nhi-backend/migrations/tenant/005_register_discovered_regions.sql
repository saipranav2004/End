-- Run once per tenant schema after inventory data is present; safe to run again:
--   psql "$DSN" -v schema=bimasugam -f migrations/tenant/005_register_discovered_regions.sql
--
-- Adds to each account's accounts.regions every region the scanner found
-- live identities or credentials in (002 does this for 'global' only).
--
-- Why: every screen scopes by accounts.regions, and Settings only offers the
-- regions listed there. An account scanned in a region that is missing from
-- the array has inventory, CloudTrail and NHI Genome data nobody can select
-- (in nhi_db: the ECS task in ap-south-2 whose log_events drive the genome).
BEGIN;

WITH discovered AS (
    SELECT DISTINCT r.account_id, LOWER(BTRIM(r.region)) AS region
    FROM (
        SELECT i.account_id, i.region FROM :"schema".identities AS i WHERE i.deleted_at IS NULL
        UNION
        SELECT c.account_id, c.region FROM :"schema".credentials AS c WHERE c.deleted_at IS NULL
    ) AS r
    WHERE r.account_id IS NOT NULL AND BTRIM(r.account_id) <> ''
      AND r.region IS NOT NULL AND BTRIM(r.region) <> ''
), missing AS (
    SELECT a.account_id, array_agg(d.region ORDER BY d.region) AS regions
    FROM :"schema".accounts AS a
    JOIN discovered AS d ON d.account_id = a.account_id
    WHERE NOT EXISTS (SELECT 1 FROM unnest(COALESCE(a.regions, '{}'::text[])) AS x WHERE LOWER(BTRIM(x)) = d.region)
    GROUP BY a.account_id
)
UPDATE :"schema".accounts AS a
SET regions = ARRAY(SELECT DISTINCT r FROM unnest(COALESCE(a.regions, '{}'::text[]) || m.regions) AS r ORDER BY r),
    updated_at = now()
FROM missing AS m
WHERE m.account_id = a.account_id;

COMMIT;
