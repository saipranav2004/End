-- ═══════════════════════════════════════════════════════════════════════════
-- Minimal stand-in for the scanner's data schema.
--
-- The production schema is owned by the NHI discovery pipeline and is NOT part
-- of this repository. These tables exist only so the API can be exercised
-- locally (see scripts/e2e_tenant_scope.sh). Apply per tenant schema:
--   psql -v schema=tenant_a -f scripts/test_schema.sql
-- locally (see scripts/e2e_tenant_scope.sh). Columns were derived from the
-- queries in internal/repository/v1.
-- ═══════════════════════════════════════════════════════════════════════════
CREATE SCHEMA IF NOT EXISTS :"schema";

CREATE TABLE IF NOT EXISTS :"schema".accounts (
  tenant_id uuid, account_id text, regions text[] NOT NULL DEFAULT '{}', is_default boolean DEFAULT false);

CREATE TABLE IF NOT EXISTS :"schema".identities (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id text, region text, arn text, name text,
  identity_type text, functional_class text, category text,
  environment text, owner text, status text, vpc_id text, subnet_id text,
  security_group_ids jsonb, tags jsonb, is_publicly_accessible boolean DEFAULT false,
  created_at_aws timestamptz, first_seen_at timestamptz, last_seen_at timestamptz,
  last_activity_at timestamptz, deleted_at timestamptz, parent_arn text,
  cloudtrail_creator_arn text, discovery_source text, type_attributes jsonb,
  raw_api_response jsonb, created_at timestamptz DEFAULT now(),
  updated_at timestamptz DEFAULT now());

CREATE TABLE IF NOT EXISTS :"schema".credentials (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id text, region text, arn text, name text,
  credential_type text, status text, expires_at timestamptz,
  last_used_at timestamptz, last_rotated_at timestamptz,
  created_at_aws timestamptz, first_seen_at timestamptz, last_seen_at timestamptz,
  last_seen timestamptz, deleted_at timestamptz,
  rotation_enabled boolean DEFAULT false, rotation_interval_days int,
  max_session_duration int, privilege_level text,
  tags jsonb, owner_principal_arn text, permission_boundary_arn text,
  trust_policy jsonb, attached_policies jsonb, inline_policies jsonb,
  assumable_role_arns jsonb, discovery_source text, type_attributes jsonb,
  raw_api_response jsonb, created_at timestamptz DEFAULT now(),
  updated_at timestamptz DEFAULT now());

CREATE TABLE IF NOT EXISTS :"schema".identity_credentials (
  identity_id text, credential_id text, binding_type text,
  binding_source text, trust_principal text, is_primary boolean DEFAULT false);

CREATE TABLE IF NOT EXISTS :"schema".effective_permissions (
  credential_id text, granted_count int DEFAULT 0,
  permissions jsonb DEFAULT '[]'::jsonb);

CREATE TABLE IF NOT EXISTS :"schema".permission_usage (
  credential_id text, usage_count_90d int DEFAULT 0,
  action text, service text, resource_arn text, event_count int DEFAULT 0,
  used boolean DEFAULT false, last_used_at timestamptz, source_ips jsonb);

CREATE TABLE IF NOT EXISTS :"schema".trust_edges (
  source_arn text, target_arn text, edge_type text,
  created_at timestamptz DEFAULT now());

CREATE TABLE IF NOT EXISTS :"schema".resources (
  arn text, name text, resource_type text,
  account_id text, region text);

CREATE TABLE IF NOT EXISTS :"schema".identity_usage (
  identity_id text, credential_id text,
  window_start timestamptz, first_seen timestamptz, last_seen timestamptz,
  last_used_at timestamptz, event_count int DEFAULT 0, event_name text,
  service text, action text, resource_arn text,
  used boolean DEFAULT false, source_ips jsonb);

-- ── Credential Exposure — secret scanner findings (tenant-isolated) ────────
-- These tables are owned by the secret scanner pipeline. Minimal columns only,
-- enough for the API to be exercised locally.

CREATE TABLE IF NOT EXISTS :"schema".secscan_findings_codecommit (
  finding_id text PRIMARY KEY, client_id text, repository text, branch text,
  commit_id text, author text, created_at text, detector text, severity text,
  file_path text, line_number int, redacted text, verification_status text,
  risk_tier text, recommended_action text, codecommit_uri text, platform text,
  github_uri text, line_preview text, confidence text, category text,
  committer text, commit_message text, commit_authored_at text,
  files_changed int, additions int, deletions int, repo_visibility text,
  repo_stars int, repo_forks int, repo_pushed_at text, ml_score double precision,
  ml_decision text, ml_model_version text, analyst_decision text,
  analyst_decided_at text, analyst_decided_by text,
  ml_veto_score double precision, ml_veto_model_version text,
  value_fingerprint text, noise_flags text);

CREATE TABLE IF NOT EXISTS :"schema".secscan_findings_github (
  finding_id text PRIMARY KEY, client_id text, repository text, branch text,
  commit_id text, author text, created_at text, detector text, severity text,
  file_path text, line_number int, redacted text, verification_status text,
  risk_tier text, recommended_action text, codecommit_uri text, platform text,
  github_uri text, line_preview text, confidence text, category text,
  committer text, commit_message text, commit_authored_at text,
  files_changed int, additions int, deletions int, repo_visibility text,
  repo_stars int, repo_forks int, repo_pushed_at text, ml_score double precision,
  ml_decision text, ml_model_version text, analyst_decision text,
  analyst_decided_at text, analyst_decided_by text,
  ml_veto_score double precision, ml_veto_model_version text,
  value_fingerprint text, noise_flags text);

CREATE TABLE IF NOT EXISTS :"schema".secscan_allowlist_dashboard (
  client_id text, file_path text, detector text, redacted text,
  reason text, dismissed_by text, dismissed_at text,
  PRIMARY KEY (client_id, file_path, detector, redacted));

CREATE TABLE IF NOT EXISTS :"schema".secscan_deepscan_codecommit (
  client_id text, repo_key text, platform text, requested_at text,
  completed_at text, commits_scanned int, findings int, failed_at text,
  error text, PRIMARY KEY (client_id, repo_key, platform));

CREATE TABLE IF NOT EXISTS :"schema".secscan_deepscan_github (
  client_id text, repo_key text, platform text, requested_at text,
  completed_at text, commits_scanned int, findings int, failed_at text,
  error text, PRIMARY KEY (client_id, repo_key, platform));

CREATE TABLE IF NOT EXISTS :"schema".secscan_ledger_codecommit (
  client_id text, repo_key text, platform text, last_commit_id text,
  updated_at text, pending_commit_id text, pending_since text,
  PRIMARY KEY (client_id, repo_key, platform));

CREATE TABLE IF NOT EXISTS :"schema".secscan_ledger_github (
  client_id text, repo_key text, platform text, last_commit_id text,
  updated_at text, pending_commit_id text, pending_since text,
  PRIMARY KEY (client_id, repo_key, platform));
