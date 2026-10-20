#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════
# End-to-end verification of the tenant guarantees, against a running server.
#
#   ./scripts/seed_two_tenants.sh     # once
#   NHI_CONFIG_FILE=configs/config.yaml ./nhi-api &
#   ./scripts/e2e_tenant_scope.sh
#
# These assertions are the ones that matter for a multi-tenant product. Unit
# tests cover the pure logic; this script proves the behaviour holds when a
# real PostgreSQL is behind it.
# ═══════════════════════════════════════════════════════════════════════════
set -uo pipefail

BASE="${BASE:-http://127.0.0.1:8099}"
PASS=0; FAIL=0

chk() { # chk <label> <actual> <expected>
  if [ "$2" = "$3" ]; then
    printf '  \033[32mPASS\033[0m  %s\n' "$1"; PASS=$((PASS+1))
  else
    printf '  \033[31mFAIL\033[0m  %s  (got %q, want %q)\n' "$1" "$2" "$3"; FAIL=$((FAIL+1))
  fi
}

login() {
  curl -s -X POST "$BASE/api/v1/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$1\",\"password\":\"StrongPass123!\"}" |
    python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["token"])'
}

ALICE="$(login alice@acme.test)"
BOB="$(login bob@globex.test)"

echo "── Authentication ───────────────────────────────────────────────────────"
chk "no token -> 401" \
  "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/identities")" "401"
chk "wrong password -> 401" \
  "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/login" -H 'Content-Type: application/json' \
      -d '{"email":"alice@acme.test","password":"wrong"}')" "401"

# Re-point a valid token at the other tenant's schema. The signature covers
# db_name, so this must be rejected — otherwise tenant isolation is decorative.
TAMPERED="$(python3 - "$ALICE" <<'PY'
import base64, json, sys
h, p, s = sys.argv[1].strip().split('.')
d = json.loads(base64.urlsafe_b64decode(p + '=' * (-len(p) % 4)))
d['db_name'] = 'tenant_b'
nb = base64.urlsafe_b64encode(json.dumps(d).encode()).decode().rstrip('=')
print(f"{h}.{nb}.{s}")
PY
)"
chk "token with tampered db_name -> 401" \
  "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TAMPERED" "$BASE/api/v1/identities")" "401"

echo "── Tenant isolation ─────────────────────────────────────────────────────"
accounts() { # accounts <token>
  curl -s -H "Authorization: Bearer $1" "$BASE/api/v1/identities" |
    python3 -c 'import sys,json;print(",".join(sorted({i["account_id"] for i in json.load(sys.stdin)["data"]["data"]})))'
}
chk "tenant A only sees tenant A accounts" "$(accounts "$ALICE")" "111111111111"
chk "tenant B only sees tenant B accounts" "$(accounts "$BOB")" "999999999999"

echo "── Filter resolution (user_settings -> accounts) ─────────"
source_of() { curl -s -H "Authorization: Bearer $1" "$BASE/api/v1/settings" |
  python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["source"])'; }
save() { curl -s -o /dev/null -X PUT -H "Authorization: Bearer $1" -H 'Content-Type: application/json' -d "$2" "$BASE/api/v1/settings"; }

# Start from a clean slate so the fallback is genuinely exercised.
PGHOST="${PGHOST:-127.0.0.1}" PGPORT="${PGPORT:-5432}" PGUSER="${PGUSER:-postgres}" \
  psql -q -d "${SEED_DB_NAME:-nhi_seed_test}" -c "DELETE FROM tenant_a.user_settings WHERE user_id='alice@acme.test';" >/dev/null

chk "no saved settings -> source=accounts" "$(source_of "$ALICE")" "accounts"
save "$ALICE" '{"selected_account_ids":["222222222222"],"selected_regions":["us-east-1"]}'
chk "after save -> source=user_settings" "$(source_of "$ALICE")" "user_settings"
chk "saved filter narrows the result set" "$(accounts "$ALICE")" "222222222222"
save "$ALICE" '{"selected_account_ids":["111111111111"],"selected_regions":["us-east-1","eu-west-1"]}'
chk "restoring the default restores results" "$(accounts "$ALICE")" "111111111111"

echo "── Request limits ───────────────────────────────────────────────────────"
WEB_ID="$(curl -s -H "Authorization: Bearer $ALICE" "$BASE/api/v1/identities" |
  python3 -c 'import sys,json;print(next(i["id"] for i in json.load(sys.stdin)["data"]["data"] if i["name"]=="web-1"))')"
chk "page_size above 100 clamps to 100" \
  "$(curl -s -H "Authorization: Bearer $ALICE" "$BASE/api/v1/identities?page_size=9999" |
      python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["pagination"]["page_size"])')" "100"
chk "depth=4 rejected (documented max 3)" \
  "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $ALICE" \
      "$BASE/api/v1/identities/$WEB_ID?tab=relationships&depth=4")" "400"
chk "invalid time_range rejected" \
  "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $ALICE" \
      "$BASE/api/v1/identities/$WEB_ID?tab=activity&time_range=banana")" "400"
chk "unknown tab rejected" \
  "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $ALICE" \
      "$BASE/api/v1/identities/$WEB_ID?tab=nope")" "400"

echo "── Scoped graph traversal (the relationships tab) ───────────────────────"
# secret-1 lives in account 333…, which is NOT in the user's selection, yet it
# is two hops from web-1 through trust_edges. It must never appear.
for depth in 1 2 3; do
  leaked="$(curl -s -H "Authorization: Bearer $ALICE" \
    "$BASE/api/v1/identities/$WEB_ID?tab=relationships&depth=$depth" |
    python3 -c 'import sys,json;g=json.load(sys.stdin)["data"]["graph"]
print(",".join(sorted({n["arn"] for n in g["nodes"] if "333333333333" in n["arn"]})) or "none")')"
  chk "depth=$depth leaks no out-of-scope node" "$leaked" "none"
done

echo
printf 'RESULT: \033[32m%d passed\033[0m, ' "$PASS"
if [ "$FAIL" -gt 0 ]; then printf '\033[31m%d FAILED\033[0m\n' "$FAIL"; exit 1; fi
printf '0 failed\n'
