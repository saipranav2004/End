-- Apply once per tenant schema. Safe to run again:
--   psql "$DSN" -v schema=bimasugam -f migrations/tenant/005_scan_changes.sql
--
-- Scan change tracking. The discovery worker writes both tables; the API only
-- reads them (Overview "Recent changes", GET /api/v1/scans/{id}/changes and
-- the Changes tab of an identity or credential).
--
-- discovery_scans: one row per discovery run.
--   previous_scan_id  the run this one was compared with; NULL for the first
--                     run (the console then says "first scan", not "no changes")
--   account_id        the account the run covered; NULL when it covered every
--                     connected account of the tenant
--   status            running | completed | failed. Only completed runs are
--                     shown; write the changes before setting 'completed'.
--
-- scan_changes: one row per difference between a run and its previous run.
--   change_type   CREATED | DELETED | MODIFIED
--   entity_type   identity | credential | permission | binding | trust_edge
--   entity_arn    the ARN of what changed (for a permission, binding or trust
--                 edge: the identity or credential it belongs to)
--   identity_id   the inventory id of the identity the change belongs to, when
--   credential_id there is one (permission / binding / trust_edge changes set
--                 the identity, a binding also its credential). These drive
--                 the Changes tabs and the NEW / CHANGED list badges.
--   account_id,   scope: a user only sees changes in the accounts and regions
--   region        they have selected (IAM is region 'global', as in identities)
--   summary       one readable sentence, e.g. "Role deploy-bot was created"
--   details       jsonb object, rendered by the console:
--                   CREATED / DELETED: {"type","name","account_id","region", ...}
--                   MODIFIED: {"fields_changed": [{"field","old","new"}],
--                              "actions_added": [...], "actions_removed": [...]}
--
-- CloudTrail evidence ("created by ... from IP ...") is not stored: the API
-- looks it up in the CloudTrail events table by entity_arn when it is read.
BEGIN;

CREATE SCHEMA IF NOT EXISTS :"schema";

CREATE TABLE IF NOT EXISTS :"schema".discovery_scans (
    scan_id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    previous_scan_id  uuid REFERENCES :"schema".discovery_scans (scan_id) ON DELETE SET NULL,
    account_id        text CHECK (account_id IS NULL OR account_id ~ '^[0-9]{12}$'),
    status            text NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'completed', 'failed')),
    started_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    CHECK (status <> 'completed' OR completed_at IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS discovery_scans_completed_idx
    ON :"schema".discovery_scans (completed_at DESC) WHERE status = 'completed';

CREATE TABLE IF NOT EXISTS :"schema".scan_changes (
    change_id      bigserial PRIMARY KEY,
    scan_id        uuid NOT NULL REFERENCES :"schema".discovery_scans (scan_id) ON DELETE CASCADE,
    change_type    text NOT NULL CHECK (change_type IN ('CREATED', 'DELETED', 'MODIFIED')),
    entity_type    text NOT NULL
        CHECK (entity_type IN ('identity', 'credential', 'permission', 'binding', 'trust_edge')),
    entity_arn     text NOT NULL,
    entity_name    text NOT NULL DEFAULT '',
    identity_id    uuid,
    credential_id  uuid,
    account_id     text NOT NULL,
    region         text NOT NULL DEFAULT 'global',
    summary        text NOT NULL DEFAULT '',
    details        jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details) = 'object'),
    detected_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS scan_changes_scan_idx
    ON :"schema".scan_changes (scan_id, change_type, entity_type);
CREATE INDEX IF NOT EXISTS scan_changes_identity_idx
    ON :"schema".scan_changes (identity_id, detected_at DESC) WHERE identity_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS scan_changes_credential_idx
    ON :"schema".scan_changes (credential_id, detected_at DESC) WHERE credential_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS scan_changes_arn_idx
    ON :"schema".scan_changes (entity_arn, detected_at DESC);

COMMIT;
