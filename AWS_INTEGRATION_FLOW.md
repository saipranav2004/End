# AWS Integration — flow, storage, and discovery link

A handoff note. Covers how "Connect AWS Account" works, which DB tables and
columns hold the data, how it feeds discovery, and what is done so far.

Backend code: `nhi-backend-merged.zip` (module `nhi-backend-api`, one service,
`cmd/api-server`, port 8085). Frontend: `frontend-nhi-console.zip`. Branch:
`ccr-73318dc0-otel9f`.

---

## 1. The flow (what happens when a tenant connects an account)

1. **Super-admin onboards the tenant** (one row in `nhi_global.tenants`): our
   AWS account, our event bus, the tenant's domain, and a generated External ID.
2. **Tenant user signs in.** The JWT carries `tenant_id`, `db_name` (the
   tenant's PostgreSQL **schema** name) and `role`. The tenant is only ever
   taken from this token.
3. **Integrations page** calls `GET /api/v1/integrations/aws`. It shows the
   tenant's AWS account number, External ID and event bus (admins only), and
   lists any accounts already connected. Until an account has connected, the
   console shows only this page (`has_connected_account = false`).
4. **Customer creates, in their own AWS account**, a read-only IAM role that
   trusts our account + External ID, and an EventBridge rule forwarding to our
   bus. They paste the role ARN, pick regions, and click **Verify**.
5. **`POST /api/v1/integrations/aws/verify`** runs three checks (admin only):
   - **Assume your role** — `sts:AssumeRole` with the External ID
   - **Read your account** — `iam:ListRoles`
   - **Receive your events** — allow the account on our bus, then look for one
     of its events in CloudTrail (last `event_lookback_hours`)
   Nothing is saved unless AssumeRole succeeds. When **all three pass**, the
   account row gets `account_status = 'connected'`, `connected_at` is set, the
   scope row is written, and the **first sync is queued**.
6. **Accounts table** then offers: **Sync now**, **Edit regions**,
   **Re-check**, **Remove**, and **Code scanning** (GitHub / CodeCommit).

Code: `internal/handlers/v1/aws_integration_handler.go` →
`internal/services/v1/aws_integration_service.go` →
`internal/repository/aws_integration_repo.go`; AWS calls in
`internal/awsconnect/`, GitHub in `internal/githubcheck/`.

---

## 2. Where the data is stored

### `nhi_global.tenants` (one row per tenant) — migration `global/004_tenant_collector.sql`
Per-tenant setup values shown on the page.

| Column | Meaning |
|---|---|
| `domain_name` | tenant email domain, lowercase |
| `collector_aws_account_id` | **our** AWS account the customer's role trusts (12 digits) |
| `collector_external_id` | unique per tenant, generated; **never change once used** |
| `collector_event_bridge_arn` | **our** central event bus |
| `collector_regions` | `text[]` — regions customers may choose (empty = all) |

### `<tenant schema>.aws_accounts` (one row per connected AWS account) — migration `tenant/003_aws_accounts.sql`
**Source of truth for the connection.** `<tenant schema>` = JWT `db_name`.

| Column | Meaning |
|---|---|
| `client_aws_account_id` | **PK** — customer's 12-digit account |
| `client_role_arn` | read-only role they created for us |
| `regions` | `text[]` — regions a sync may read |
| `external_id` | the External ID the role was verified with |
| `is_connected` | true while the latest check passed every step |
| `account_status` | `pending` / `connected` / `action_needed` |
| `checks` | `jsonb` — last connection-check results (shown on page) |
| `last_verified_at`, `last_verified_by`, `connected_at` | verify metadata |
| `github_connected`, `codecommit_connected` | exposed-cred scanning on + verified |
| `github_secret_arn` | where the customer stored the GitHub token (ARN only; token never stored) |
| `github_checks`, `github_verified_at`, `codecommit_checks`, `codecommit_verified_at`, `exposure_updated_by` | code-scanning check state |
| `sync_status` | `idle` / `queued` / `running` / `succeeded` / `failed` |
| `sync_requested_at`, `sync_requested_by`, `sync_started_at`, `last_synced_at`, `last_sync_error` | sync queue + result |
| `created_at`, `updated_at` | row timestamps |

Index `aws_accounts_sync_queue` on `sync_requested_at WHERE sync_status='queued'` = the worker's queue.

### `<tenant schema>.accounts` (scope table, one row per account)
Kept in step by the API. Columns: `tenant_id`, `account_id`, `regions text[]`,
`is_default`. On connect, a row is written with the allowed regions **plus
`global`** (IAM roles/users live under the synthetic `global` region).
Edit-regions rewrites this array (keeping `global`); remove deletes the row.

---

## 3. How it connects to discovery

The API **only queues** work — it never scans AWS itself.

- **Verify success / Sync now** → sets `aws_accounts.sync_status = 'queued'`,
  `sync_requested_at = now()`. (`POST /integrations/aws/accounts/{id}/sync`.)
- **The discovery worker** (separate process, not in this repo) claims one
  queued account per tenant:
  ```sql
  UPDATE <schema>.aws_accounts SET sync_status='running', sync_started_at=now()
   WHERE client_aws_account_id = (
     SELECT client_aws_account_id FROM <schema>.aws_accounts
      WHERE sync_status='queued' ORDER BY sync_requested_at
      FOR UPDATE SKIP LOCKED LIMIT 1)
  RETURNING client_aws_account_id, client_role_arn, external_id, regions;
  ```
  It assumes `client_role_arn` with `external_id`, reads the `regions`, writes
  the inventory (`identities`, `credentials`, `trust_edges`, …) in the tenant
  schema, then sets `sync_status='succeeded'`, `last_synced_at=now()` (or
  `'failed'` with `last_sync_error`).
- **The scope table (`accounts`) is the bridge to the rest of the product.**
  Because a connected account has a row there, it immediately appears in
  Settings, filters, the dashboard, and every scoped query — so discovered
  identities/credentials for that account show up across the console.
- **Code scanning:** the secret scanner scans CodeCommit where
  `codecommit_connected`, and the GitHub token at `github_secret_arn` where
  `github_connected`, by assuming `client_role_arn` with `external_id`.

**AWS identity of our service:** no keys in config. Production runs as
`NHIPlatformAssumer` (ECS/EC2 role). Locally, `AWS_PROFILE` + 
`NHI_AWS_ASSUME_ASSUMER_ROLE=true`. See backend `README.md` → "Integrations in
detail".

---

## 4. API endpoints

| Method | Path | Does |
|---|---|---|
| GET | `/api/v1/integrations/aws` | setup values, connected accounts, `has_connected_account`, `exposure_enabled`, `can_manage` |
| POST | `/api/v1/integrations/aws/verify` | run 3 checks, save account, queue first sync |
| PUT | `/api/v1/integrations/aws/accounts/{id}/regions` | change regions |
| PUT | `/api/v1/integrations/aws/accounts/{id}/exposure` | turn GitHub/CodeCommit scanning on/off |
| POST | `/api/v1/integrations/aws/accounts/{id}/sync` | queue a sync |
| DELETE | `/api/v1/integrations/aws/accounts/{id}` | disconnect + remove from scope |

Reads: any signed-in user. Changes: `admin` / `super_admin` only.
Error codes and local AWS-cred setup: backend `README.md`.

---

## 5. Done so far / where the next chat continues

**Done and tested (39/39 integration API checks against fake AWS + fake GitHub, plus live gate test):**
- All 6 integration endpoints on the merged backend (`nhi_db`, schema-per-tenant).
- Verify (3 checks), regions edit, sync queue, remove (also revokes bus
  permission best-effort), GitHub/CodeCommit enablement.
- Frontend Integrations page: overview tiles, accounts table, account drawer
  (checks / data sync / code scanning / activity), wizard. Gate confirmed:
  a workspace with no connected account is held on the Integrations page until
  all 3 checks pass, then the full console opens.
- Review build (`frontend-nhi-console-preview.zip`) runs this page on in-browser
  sample accounts so it can be demoed before the AWS side is deployed.

**Not in this repo / next steps:**
- **The discovery worker itself** (the process that claims `sync_status='queued'`
  rows and writes the inventory) — contract is in `tenant/003_aws_accounts.sql`
  and §3 above, but the worker is external.
- **Real-AWS run:** verify + code scanning were tested only against local
  stand-ins. One run against a real test account (role + EventBridge rule +
  `NHIPlatformAssumer`) is still needed. Needed AWS setup: backend `README.md`
  → "Trying the APIs locally: which ones need AWS".
- Known gap: access-key inherited policies not shown; posture sub-tab
  duplication; genome evidence endpoints (§6.2/6.3) not implemented.

**Local dev:** `$S/run4/start_all.sh [--restart-apis|--reseed]` (API :8085,
frontend :3000, preview :3001). Seeds in `$S/seeds/`. Reset integration state:
`$S/video/reset.sh`.
