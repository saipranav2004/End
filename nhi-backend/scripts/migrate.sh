#!/usr/bin/env bash
# Applies every migration, in order, to nhi_db: the global ones once, then the
# tenant ones to each tenant schema in nhi_global.tenants (or only the schemas
# given as arguments). Every migration is safe to run again, so this is too.
#
#   DSN='postgres://user:pass@host:5432/nhi_db' scripts/migrate.sh [schema ...]
set -euo pipefail
: "${DSN:?set DSN to the nhi_db connection string}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
run() { PGOPTIONS="-c client_min_messages=warning" psql "$DSN" -q -v ON_ERROR_STOP=1 "$@"; }

for f in "$ROOT"/migrations/global/*.sql; do
  echo "global: $(basename "$f")"
  run -f "$f" >/dev/null
done

if [ $# -gt 0 ]; then
  schemas=("$@")
else
  # Only schemas that exist; a registered tenant without a schema is skipped.
  mapfile -t schemas < <(run -At -c "SELECT t.db_name FROM nhi_global.tenants t
    JOIN pg_namespace n ON n.nspname = t.db_name ORDER BY 1")
fi

for s in "${schemas[@]}"; do
  [[ "$s" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]] || { echo "skip invalid schema name: $s" >&2; continue; }
  if [ "$(run -At -c "SELECT to_regclass('\"$s\".accounts') IS NOT NULL AND to_regclass('\"$s\".identities') IS NOT NULL")" != t ]; then
    echo "$s: no accounts table yet (the scanner creates it); skipped" >&2
    continue
  fi
  for f in "$ROOT"/migrations/tenant/*.sql; do
    echo "$s: $(basename "$f")"
    run -v schema="$s" -f "$f" >/dev/null
  done
done
