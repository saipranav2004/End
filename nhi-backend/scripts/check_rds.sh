#!/usr/bin/env bash
# Diagnose RDS connectivity without starting the API.
# Usage:
#   ./scripts/check_rds.sh configs/config.yaml
#   PGPASSWORD=... ./scripts/check_rds.sh configs/config.rds.yaml
#   NHI_CONFIG_FILE=configs/config.rds.yaml ./scripts/check_rds.sh
set -uo pipefail

CFG="${1:-${NHI_CONFIG_FILE:-configs/config.yaml}}"
echo "Config: $CFG"
echo

python3 - "$CFG" <<'PY'
import sys, yaml, pathlib, os
path = pathlib.Path(sys.argv[1])
data = yaml.safe_load(path.read_text())
for key in ("database",):
    db = data.get(key, {})
    host = db.get("host","")
    port = db.get("port", 5432)
    name = db.get("database_name") or db.get("name") or "(missing database_name)"
    user = db.get("user","")
    ssl = db.get("sslmode","")
    print(f"{key}:")
    print(f"  host={host} port={port} db={name} user={user} sslmode={ssl}")

# Also show env overrides if set
for k in ("NHI_DB_HOST","NHI_DB_NAME","NHI_DB_USER","NHI_DB_PASSWORD"):
    if os.getenv(k):
        print(f"  env override {k} is set")
PY
echo

# Extract host/port for a quick TCP check using python (no nc dependency)
python3 - "$CFG" <<'PY'
import socket, yaml, pathlib, sys
path = pathlib.Path(sys.argv[1])
data = yaml.safe_load(path.read_text())
hosts = []
for key in ("database",):
    db = data.get(key, {})
    h = db.get("host")
    p = int(db.get("port") or 5432)
    if h and (h,p) not in hosts:
        hosts.append((h,p))

for host, port in hosts:
    print(f"Checking TCP {host}:{port} ...", end=" ", flush=True)
    try:
        # getaddrinfo to surface DNS issues
        infos = socket.getaddrinfo(host, port, type=socket.SOCK_STREAM)
        ip = infos[0][4][0]
        print(f"DNS -> {ip}", end=" ", flush=True)
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.settimeout(5)
        s.connect((host, port))
        s.close()
        print("TCP OK")
    except socket.gaierror as e:
        print(f"DNS FAILED: {e}")
        print("  -> Check the RDS endpoint string, VPC DNS, or that you are inside the VPC / VPN.")
    except socket.timeout:
        print("TIMEOUT")
        print("  -> This is what you see when the security group drops packets.")
        print("     1) RDS is on port 5432, NOT 5433. Fix configs/config.yaml port.")
        print("     2) In AWS Console -> RDS -> Connectivity -> Security Group -> Inbound rules")
        print("        must allow your IP (or 0.0.0.0/0 for testing) on 5432.")
        print("     3) If RDS is private, you must run this from inside the VPC (EC2/bastion).")
    except Exception as e:
        print(f"FAILED: {e}")

PY

echo
echo "If TCP OK but API still fails, check:"
echo "  - PGPASSWORD / NHI_DB_PASSWORD set correctly"
echo "  - database nhi_db exists (psql -h HOST -U postgres -d nhi_db -c '\\dn')"
echo "  - sslmode=require for RDS (rds.force_ssl=1 by default)"
