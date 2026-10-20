#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# Seed a local Postgres with TWO tenants in TWO schemas of ONE database, so the
# tenant isolation guarantees can actually be verified rather than assumed.
#
#   Tenant A  schema tenant_a   accounts 111…, 222…, 333…   (defaults: 111…)
#   Tenant B  schema tenant_b   account  999…
#
# The trust graph in tenant A deliberately crosses out of the default account
# selection:
#
#   web-1 (111…) ──assume_role──▶ api-1 (111…) ──assume_role──▶ secret-1 (333…)
#
# secret-1 lives in an account the user never selected, so it must never appear
# in the relationships tab at any depth. That is the assertion the e2e script
# makes.
#
# Usage: PGHOST=127.0.0.1 PGPORT=5432 PGUSER=postgres [SEED_DB_NAME=nhi_seed_test] ./scripts/seed_two_tenants.sh
# Point the API at it with NHI_DB_NAME=nhi_seed_test.
# ═══════════════════════════════════════════════════════════════════════════
set -euo pipefail

PGHOST="${PGHOST:-127.0.0.1}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-postgres}"
export PGHOST PGPORT PGUSER

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

# bcrypt hash of "StrongPass123!" at default cost
HASH='$2a$10$f7Q2R6aA0ZRcXPIaLBAjmObMWS9NWPkszwXYpoTVrkoV35R8d46HC'

# This DROPs and recreates the database, so it never defaults to (or accepts)
# the real one.
DB="${SEED_DB_NAME:-nhi_seed_test}"
case "$DB" in
  nhi_db|postgres|template0|template1) echo "refusing to drop database '$DB'; set SEED_DB_NAME to a throwaway name" >&2; exit 1 ;;
esac
[[ "$DB" =~ ^[a-z_][a-z0-9_]*$ ]] || { echo "SEED_DB_NAME must be a plain lower-case name" >&2; exit 1; }
psql -q -c "DROP DATABASE IF EXISTS $DB;"
psql -q -c "CREATE DATABASE $DB;"

# ── nhi_global: the only schema that knows which tenant lives where ──
psql -q -d "$DB" -v ON_ERROR_STOP=1 -f "$ROOT/migrations/global/001_auth_tenant_registry.sql"
for m in 002_refresh_tokens 003_refresh_token_session 004_tenant_collector; do
  psql -q -d "$DB" -v ON_ERROR_STOP=1 -f "$ROOT/migrations/global/$m.sql"
done
psql -q -d "$DB" <<SQL
INSERT INTO nhi_global.tenants (tenant_id, db_name) VALUES
  ('11111111-1111-1111-1111-111111111111','tenant_a'),
  ('22222222-2222-2222-2222-222222222222','tenant_b');
INSERT INTO nhi_global.nhi_users (email,password,full_name,role,tenant_id,status) VALUES
  ('alice@acme.test','$HASH','Alice (Tenant A)','admin','11111111-1111-1111-1111-111111111111','active'),
  ('bob@globex.test','$HASH','Bob (Tenant B)','admin','22222222-2222-2222-2222-222222222222','active');
SQL

# ── Tenant data schema + user_settings in every tenant schema ──
for schema in tenant_a tenant_b; do
  psql -q -d "$DB" -v ON_ERROR_STOP=1 -v schema="$schema" -f "$ROOT/scripts/test_schema.sql"
  psql -q -d "$DB" -v ON_ERROR_STOP=1 -v schema="$schema" -f "$ROOT/migrations/tenant/001_user_settings.sql"
  psql -q -d "$DB" -v ON_ERROR_STOP=1 -v schema="$schema" -f "$ROOT/migrations/tenant/004_accounts_connection.sql"
done

psql -q -d "$DB" -v ON_ERROR_STOP=1 <<SQL
-- Default selection is account 111… across two regions.
INSERT INTO tenant_a.accounts (tenant_id,account_id,regions,is_default) VALUES
 ('11111111-1111-1111-1111-111111111111','111111111111','{us-east-1,eu-west-1}',true),
 ('11111111-1111-1111-1111-111111111111','222222222222','{us-east-1}',false),
 ('11111111-1111-1111-1111-111111111111','333333333333','{us-east-1}',false);

-- In default scope.
INSERT INTO tenant_a.identities (account_id,region,arn,name,identity_type,functional_class,environment,owner,status,last_activity_at) VALUES
 ('111111111111','us-east-1','arn:aws:iam::111111111111:role/web-1','web-1','ec2_instance','application','production','platform-team','active',now()),
 ('111111111111','eu-west-1','arn:aws:iam::111111111111:role/api-1','api-1','lambda_function','service','production','data-team','active',now()),
 ('111111111111','us-east-1','arn:aws:iam::111111111111:role/batch-1','batch-1','batch_job','service','staging','data-team','active',now()-interval '200 days');
-- In the tenant's database but OUTSIDE the default account selection.
INSERT INTO tenant_a.identities (account_id,region,arn,name,identity_type,functional_class,environment,owner,status,last_activity_at) VALUES
 ('222222222222','us-east-1','arn:aws:iam::222222222222:role/other-1','other-1','ec2_instance','application','production','platform-team','active',now()),
 ('333333333333','us-east-1','arn:aws:iam::333333333333:role/secret-1','secret-1','ec2_instance','application','production','platform-team','active',now());

INSERT INTO tenant_a.credentials (account_id,region,arn,name,credential_type,status,last_used_at) VALUES
 ('111111111111','us-east-1','arn:aws:iam::111111111111:role/web-role','web-role','iam_role','active',now()),
 ('333333333333','us-east-1','arn:aws:iam::333333333333:role/secret-role','secret-role','iam_role','active',now());

INSERT INTO tenant_a.identity_credentials (identity_id,credential_id,binding_type,is_primary)
 SELECT i.id::text, c.id::text, 'instance_profile', true
 FROM tenant_a.identities i CROSS JOIN tenant_a.credentials c
 WHERE i.name='web-1' AND c.name='web-role';

-- The graph walks from an in-scope account into an out-of-scope one.
INSERT INTO tenant_a.trust_edges (source_arn,target_arn,edge_type) VALUES
 ('arn:aws:iam::111111111111:role/web-1','arn:aws:iam::111111111111:role/api-1','assume_role'),
 ('arn:aws:iam::111111111111:role/api-1','arn:aws:iam::333333333333:role/secret-1','assume_role'),
 ('arn:aws:iam::111111111111:role/web-role','arn:aws:iam::333333333333:role/secret-role','assume_role');
SQL

psql -q -d "$DB" -v ON_ERROR_STOP=1 <<SQL
INSERT INTO tenant_b.accounts (tenant_id,account_id,regions,is_default) VALUES
 ('22222222-2222-2222-2222-222222222222','999999999999','{ap-south-1}',true);
INSERT INTO tenant_b.identities (account_id,region,arn,name,identity_type,functional_class,environment,owner,status,last_activity_at) VALUES
 ('999999999999','ap-south-1','arn:aws:iam::999999999999:role/b-1','b-1','ec2_instance','application','production','team-b','active',now()),
 ('999999999999','ap-south-1','arn:aws:iam::999999999999:role/b-2','b-2','ec2_instance','application','production','team-b','active',now());
INSERT INTO tenant_b.credentials (account_id,region,arn,name,credential_type,status,last_used_at) VALUES
 ('999999999999','ap-south-1','arn:aws:iam::999999999999:role/b-role','b-role','iam_role','active',now());
SQL

echo "Seeded: $DB with schemas nhi_global (2 tenants), tenant_a, tenant_b"
echo "Login:  alice@acme.test / bob@globex.test  password: StrongPass123!"
