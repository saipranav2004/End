-- Apply once per tenant schema. Safe to run again:
--   psql "$DSN" -v schema=bimasugam -f migrations/tenant/006_account_status_active.sql
--
-- accounts.account_status now reads  pending | active | action_needed:  an
-- account whose connection check passed is 'active' (it used to be 'connected').
BEGIN;
UPDATE :"schema".accounts SET account_status = 'active' WHERE account_status = 'connected';
COMMIT;
