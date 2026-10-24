# NHI Backend API

One Go service (`cmd/api-server`, port 8080) behind the NHI console. It serves
every API the console uses - inventory, posture, NHI Genome, integrations,
change tracking and credential exposure - from one process, behind one sign-in.

- **Tenant** comes only from the signed JWT (`tenant_id`, `db_name` = the
  tenant's PostgreSQL schema). Nothing a client sends can choose another tenant.
- **Scope**: inventory, genome and change tracking answer only for the accounts
  and regions the user selected (Settings; a saved `"all"` means every
  account or region of the tenant); posture covers the whole tenant. Genome
  event reads are filtered on `account_id` as well as the identity.
- **Stores**: PostgreSQL (`nhi_db`, one schema per tenant), ClickHouse for
  CloudTrail events, and a second ClickHouse for the genome's `log_events`.
  Either ClickHouse may be missing; the fields that need it then say
  "unavailable" and everything else keeps working. A configured ClickHouse
  that is down (at start-up or later) is pinged every 15 s and used again as
  soon as it answers; while it is down it is skipped, not waited on.
- **Timeouts**: Postgres statements stop after 25 s (`statement_timeout`), the
  HTTP write timeout is 30 s.

```sh
go build ./... && go vet ./... && go test ./...
NHI_CONFIG_FILE=configs/config.yaml go run ./cmd/api-server
```

Contents: [Quick start](#quick-start) · [APIs by feature](#apis-by-feature) ·
[Features and their files](#features-and-their-files) · [Reference](#reference)

---

## Quick start

1. **Database** - apply the migrations (each file is safe to run again):

   `scripts/migrate.sh` runs all of the below in order (every tenant schema
   in `nhi_global.tenants`, or the schemas you name); each file is safe to
   run again:

   ```sh
   DSN='postgres://user:pass@host:5432/nhi_db' scripts/migrate.sh   # or: scripts/migrate.sh bimasugam
   ```

   By hand:

   ```sh
   # nhi_db, once
   psql "$DSN" -f migrations/global/001_auth_tenant_registry.sql
   psql "$DSN" -f migrations/global/002_refresh_tokens.sql
   psql "$DSN" -f migrations/global/003_refresh_token_session.sql   # sessions
   psql "$DSN" -f migrations/global/004_tenant_collector.sql        # AWS integration values on tenants

   # every tenant schema (e.g. bimasugam)
   psql "$DSN" -v schema=bimasugam -f migrations/tenant/001_user_settings.sql
   psql "$DSN" -v schema=bimasugam -f migrations/tenant/002_accounts_regions_array.sql  # converts an old accounts table; no-op otherwise
   psql "$DSN" -v schema=bimasugam -f migrations/tenant/003_register_global_region.sql  # after inventory data exists
   psql "$DSN" -v schema=bimasugam -f migrations/tenant/004_accounts_connection.sql  # connection columns on accounts
   psql "$DSN" -v schema=bimasugam -f migrations/tenant/005_register_discovered_regions.sql  # every scanned region selectable
   ```

   The inventory tables (`identities`, `credentials`, ...), posture (`ispm_*`)
   and genome (`baselines`, `baseline_versions`, `alerts`, `identity_risk_scores`) tables, and the
   scanner's `nhi_scans_aws` / `scan_changes`, are written by their workers,
   not created here.

2. **Configuration** - `configs/config.yaml`, overridden by `NHI_*` environment
   variables (see `.env.example`): database, JWT secret, CloudTrail ClickHouse
   (`NHI_CLICKHOUSE_*`), genome ClickHouse (`NHI_MONITORING_CLICKHOUSE_*`), the
   and the `aws_integration` block. No AWS keys go in config
   (see [AWS identity](#aws-identity-of-this-service)).

3. **Run** - `go run ./cmd/api-server`. At start-up the log says
   `clickhouse connected (...)` and `monitoring clickhouse connected (...)`, or
   why not.

4. **Try it** - `postman/nhi-backend-api.postman_collection.json` has a
   request for every endpoint below; "Login" stores the token for the rest.

---

## APIs by feature

Every path is under `/api/v1` (most are also served under `/api`). Every
endpoint except login, refresh and `/api/v1/health` needs `Authorization: Bearer
<access token>`. Unknown query parameters are a 400.

Response shapes: inventory, integrations and change tracking answer
`{success, message, data}` (errors `{success: false, message, error: CODE}`);
posture and genome answer `{data, meta}` (errors `{error: {code, message}}`,
genome adds `request_id`); credential exposure answers bare JSON (errors
`{error: CODE, message}`). Sign-in failures (401) and crashes (500) use the
same shape as the API family the path belongs to.

When the tenant's schema is missing a table an endpoint reads (the workspace
is not fully set up), every API answers **503 `TENANT_NOT_PROVISIONED`**.

### Health

| Method | Path | What it does |
|---|---|---|
| GET | `/api/v1/health` | Liveness probe (no token). |
| GET | `/ready` | Readiness probe (no token): 200 when Postgres answers, 503 when not; also reports each ClickHouse as `ok`, `down` or `not_configured`. |

### Sign-in and session

| Method | Path | What it does |
|---|---|---|
| POST | `/login` | Email + password → access token (1 h) and refresh token (7 d); starts a session. 429 `RATE_LIMITED` after 60 tries a minute per address or 5 per account; 403 `ACCOUNT_DISABLED` for a disabled account with the right password. |
| POST | `/refresh` | Swaps a refresh token for a new pair (single use; reusing an old one ends every session of the user). Refused once the session is 30 days old. |
| POST | `/logout` | Ends the session; its access token stops working everywhere at once. |
| GET | `/me` | The signed-in user: email, role, tenant. |

### Workspace scope

| Method | Path | What it does |
|---|---|---|
| GET | `/settings` | The accounts and regions this user has selected (or the tenant default). |
| PUT | `/settings` | Saves the selection (`selected_account_ids`, `selected_regions`). |
| GET | `/filters` | Every value the inventory filters offer (accounts, regions, types, owners ...), with counts. |

### Dashboard

Scan changes, the Changes tabs and `recent_changes` also carry the reference contract's field names (`scan_time`, `summary.created`/`deleted`/`modified`/`by_entity`, `changes[].id`/`entity_id`, evidence `created_by`/`created_at`, `identity_arn`/`credential_arn`, `last_scan_time`) next to ours, so clients written against either work.

| Method | Path | What it does |
|---|---|---|
| GET | `/dashboard/overview` | KPIs and charts for the selected scope, plus `recent_changes`: what the latest discovery scan changed (null before the first scan). Includes `kpis.active_credentials` (total minus stale), `kpis.trust_edges` / `cross_account_trust_edges` (active `trust_edges` in the selected accounts), `charts.trust_edges_by_type`, `charts.event_volume` (CloudTrail events per UTC day, last 30 days, selected accounts, zero-filled; empty when CloudTrail cannot be read; regions do not apply because IAM calls are recorded in us-east-1) and `charts.credential_utilization` (credentials by last use - CloudTrail or AWS's own last-used date, whichever is later: `used_7d`, `used_30d`, `used_90d`, `idle_90d`, `never_used`). |

### Dashboard trends

| Method | Path | What it does |
|---|---|---|
| GET | `/dashboard/trends` | Identity and credential totals across completed scans (`nhi_scans_aws.total_identities/total_credentials`), oldest first, for the trend chart. `account_ids` and `regions` (comma-separated) override the saved selection (a region keeps accounts that read it); `limit` 1-200 (default 20); `aggregate=true` (default) gives one point per scan hour with each account's latest totals summed (`accounts` says how many), `aggregate=false` one point per scan with `scan_id` and `account_id`. |

### Scan change tracking

(`GET /scans/{scan_id}/status` answers `{scan_id, status, scan_time}`, 404 `SCAN_NOT_FOUND` outside your scope; `scan_id` may be `latest`.)

What each discovery scan found had changed since the scan before it. The
discovery scanner writes `nhi_scans_aws` (one row per run) and `scan_changes`
(one row per difference); these endpoints only read them.

| Method | Path | What it does |
|---|---|---|
| GET | `/scans` | Newest completed scans with their change counts (`limit`, default 20, max 50). |
| GET | `/scans/{scan_id}/changes` | One scan (`scan_id` or `latest`): its summary (totals by change type and entity type) and a page of its changes. Filters `change_type` (CREATED / DELETED / MODIFIED), `entity_type` (identity / credential / permission / binding / trust_edge), `page`, `limit` (max 100). 404 `SCAN_NOT_FOUND`. COMPLETED and PARTIAL scans are listed (`status`). |
| GET | `/identities/{id}?tab=changes` | Every change recorded for one identity, newest first (max 50), with the CloudTrail call behind each creation or deletion (`cloudtrail_evidence`). |
| GET | `/credentials/{id}?tab=changes` | The same for one credential. |

The identity and credential lists also mark each row with `recent_change`
(`CREATED` / `MODIFIED` / null) from the latest scan.

### Identities (inventory)

| Method | Path | What it does |
|---|---|---|
| GET | `/identities` | Paged, searchable, filterable, sortable list of non-human identities (`page_size` max 100). |
| GET | `/identities/{id}?tab=overview` | The identity record: type, owner, network exposure, risk signals, bound credentials. |
| GET | `/identities/{id}?tab=permissions` | What its credentials' policies grant and which actions CloudTrail saw it use (90 days). |
| GET | `/identities/{id}?tab=activity` | Its CloudTrail events and activity analytics for a `time_range`. |
| GET | `/identities/{id}?tab=relationships` | Its graph: the credentials it runs as (bindings), roles it can assume, who trusts it, resources it called (`depth` 1-3, `edge_type`), plus a plain-language relationship table. |
| GET | `/identities/{id}?tab=credentials` | Credentials bound to it, with usage. |
| GET | `/identities/{id}?tab=raw` | The stored discovery record. |
| GET | `/identities/{id}?tab=changes` | See change tracking. |
| GET | `/identities/{id}/events` | Forensic event views, one `section` at a time: timeline, detail, credentials, resources, heatmap, sessions, errors, cross_account, ips. |

### Credentials (inventory)

| Method | Path | What it does |
|---|---|---|
| GET | `/credentials` | Paged list of roles, keys, secrets and certificates, with rotation and usage. |
| GET | `/credentials/{id}?tab=overview` | The credential record and who holds it. |
| GET | `/credentials/{id}?tab=trust` | Trust policy, permissions and who can assume it. |
| GET | `/credentials/{id}?tab=activity` | Its CloudTrail events. |
| GET | `/credentials/{id}?tab=rotation` | Age, rotation and expiry. |
| GET | `/credentials/{id}?tab=relationships` | Its graph (as for identities). |
| GET | `/credentials/{id}?tab=raw` | The stored discovery record. |
| GET | `/credentials/{id}?tab=changes` | See change tracking. |
| GET | `/permission-source` | The policy statement that allowed one CloudTrail call (`credential_id`, `event_source`, `event_name`). |

The console's **Access graph** is built from the identities, credentials,
relationships tabs and settings above.

### Integrations (connect AWS)

Reads for every signed-in user; changes for admins.

| Method | Path | What it does |
|---|---|---|
| GET | `/integrations/aws` | Setup values for this tenant (admins only), every connected account, `has_connected_account`, `exposure_enabled`, `can_manage`. The console shows only the setup until `has_connected_account` is true. |
| POST | `/integrations/aws/verify` | Runs the three connection checks for `{role_arn, regions}` and saves the account; the first full pass connects it and queues its first sync. |
| PUT | `/integrations/aws/accounts/{account_id}/regions` | Changes the regions a sync may read. |
| PUT | `/integrations/aws/accounts/{account_id}/exposure` | Switches exposed-credential scanning of GitHub / CodeCommit on or off (each is checked first). |
| POST | `/integrations/aws/accounts/{account_id}/sync` | Queues a discovery sync (the worker runs it). |
| DELETE | `/integrations/aws/accounts/{account_id}` | Disconnects the account and removes it from every scope. |

### Credential exposure (secrets in code; admin only)

| Method | Path | What it does |
|---|---|---|
| GET | `/credential-exposure/findings` | The **main list**: secrets the scanner found in GitHub / CodeCommit that need action (not marked safe, not set aside by the rules in "Finding lists"), newest first. Same 41 fields as the scanner's own API (the 37 earlier ones plus `ml_veto_score`, `ml_veto_model_version`, `value_fingerprint`, `noise_flags`); timestamps always carry an offset (rows stored without one are UTC). `?ml_flagged=true` reads the ML-flagged list instead. |
| GET | `/credential-exposure/findings/review` | **Needs review**: the models are unsure either way, so a person should decide. |
| GET | `/credential-exposure/findings/repeated` | **Same value repeated**: one value (`value_fingerprint`) at 3+ different places in one commit. The console groups it by repeat count. |
| GET | `/credential-exposure/findings/noise` | **Known harmless**: values tagged in `noise_flags` (published example keys, hashes, ARNs, plain URLs...). |
| GET | `/credential-exposure/findings/flagged` (alias `/ml-flagged`) | **Likely false alarms**: the models think they are not real secrets. |
| GET | `/credential-exposure/findings/summary` | Counts by risk and repositories affected for one list (main unless `?ml_flagged=true`), plus `lists: {main, review, repeated, noise, ml_flagged}` - the five counts add up to every finding that is not marked safe. |
| GET | `/credential-exposure/allowlist` | Findings marked safe (the Accepted list). |
| POST | `/credential-exposure/allowlist` | Marks a finding as safe, with a reason. |
| DELETE | `/credential-exposure/allowlist` | Restores a finding to the live list. |
| POST | `/credential-exposure/decisions` | Records an analyst verdict on a finding (real secret / false positive). The signed-in user is recorded as `decided_by` (a value in the body is ignored); the allowlist records `dismissed_by` the same way. |
| POST | `/credential-exposure/deep-scan` | Requests a full-history walk of one repository (`{client_id, repository, platform?}`, 202). Written to the tenant's `secscan_deepscan_<platform>` row for the scanner worker - no external call. Answers `{accepted: true, status: "running", ...}`. 400 `INVALID_REPOSITORY` when a GitHub repository is not `owner/repo` or a CodeCommit repository is not one of the client's onboarded repositories; 404 `CLIENT_NOT_FOUND` when this workspace's scanner has never seen the client on that platform. A walk already waiting or running is left alone (`already_running: true`). |
| GET | `/credential-exposure/deep-scan/status` | Progress of that rescan; the same object with `status: "not_started"` when none was requested. |

### Posture scans (ISPM rule engine)

The posture rules run in the separately deployed **ispm-worker**; this backend only asks it to scan
and relays the job status (`internal/ispmworker`). Set `WORKER_URL` (its `ENGINE_HEALTH_PORT`, never this
server's own port) and `WORKER_SCAN_TOKEN` (its `SCAN_API_TOKEN`); `NHI_WORKER_TIMEOUT_SECONDS` (default 10).

| Method | Path | What it does |
|---|---|---|
| POST | `/ispm-scans` | **No login.** `{tenant_id, requested_by?}` + the worker's key in `X-API-Key` (or `Authorization: Bearer`, or body `api_key`; never in the URL). 202 with `{data:{job_id, already_running:false}}`, 200 when that tenant is already being scanned. The key is forwarded and checked by the worker. 30 requests/minute per client address. |
| GET | `/ispm-scans/{job_id}` | **No login**, same key. The worker's job (`QUEUED` / `RUNNING` / `COMPLETED` / `PARTIAL` / `FAILED` / `SKIPPED`, per-tenant results). Poll every 3-5 s. |
| POST | `/ispm/scans` | Console "Run checks again": no body, the tenant is the token's; analyst / manager / admin. Uses `WORKER_SCAN_TOKEN`. |
| GET | `/ispm/scans/{job_id}` | Status of that job. |

Errors: `{error:{code,message,field}}`: 400 `invalid_parameter`, 401 `unauthorized`, 404 `not_found`, 429
`rate_limited`, 502/503 `scan_unavailable` (the real reason is in the server log).

### Posture (ISPM) - `/ispm/...`

Scores every identity from 100 down by the checks it fails. Precomputed by the
posture worker for the whole tenant.

| Method | Path | What it does |
|---|---|---|
| GET | `/ispm/posture/overview` | The Posture page: fleet score and trend, score bands, security areas, quick wins, weakest identities (`window`). |
| GET | `/ispm/posture/quick-wins/{rule_id}/identities` | The identities failing one quick-win check. |
| GET | `/ispm/posture/export` | CSV of the identity list with the same filters. |
| GET | `/ispm/identities` | Scored identities, filtered, sorted and paged (cursor). |
| GET | `/ispm/identities/facets` | Counts for every filter of that list. |
| GET | `/ispm/identities/{id}` | One identity's posture header: score, band, grade, badges. |
| GET | `/ispm/identities/{id}/posture` | Why the score is what it is: security areas, failed checks, what each fix is worth, peer group. |
| GET | `/ispm/identities/{id}/checks` | Every check that applies, failed and passed (`status`, `pillar`). |
| GET | `/ispm/identities/{id}/history` | Score over time and what moved it. |
| GET | `/ispm/identities/{id}/credentials` | Its credentials and their posture. Details come from the scanner's `credentials` and/or `dummy_credentials` (posture sample data), whichever the schema has; every field that applies to the kind (role: privilege; key: usage) is always present, null when unknown. |
| GET | `/ispm/identities/{id}/role-use` | Roles it assumed and who assumed it. |
| GET | `/ispm/identities/{id}/activity` | Its recent calls: from the posture worker's `ispm_identity_events` table when the tenant schema has it, else CloudTrail in ClickHouse. |
| GET | `/ispm/identities/{id}/alerts` | Its findings, open or closed. |
| GET | `/ispm/identities/{id}/graph` | Its access graph as posture sees it. |
| GET | `/ispm/checks/{rule_id}` | One check: what it means, how to fix it, compliance mappings. |
| PATCH | `/ispm/findings/{id}` | Accept, suppress, reopen, assign or note a finding (analyst or admin). Only the id and the change are needed; an optional `If-Match: "<updated_at>"` makes it fail with 409 when someone changed the finding meanwhile. The response `ETag` is the new `updated_at`. |

### NHI Genome - `/genome/...`

Each workload identity learns its normal behaviour (a baseline); departures
from it are raised as anomalies. The first five endpoints are the console's core views; the rest of
the contract (backend_api_contract.md) follows below. Everything answers `{data, meta}` / `{error}` with a request id.

| Method | Path | What it does |
|---|---|---|
| GET | `/genome/dashboard/summary` | Cards (identities watched, baselines established, `still_learning` = watched without an established baseline, including those with no baseline row yet; `open_anomalies` raised in the card window, 24 hours by default), open departures by dimension and anomalies per day (14 days by default), most anomalous identities (empty until `identity_risk_scores` carries scores) and recently baselined. |
| GET | `/genome/identities/{source_type}/{identity_name}` | The identity page header, trimmed to what the stores fill: `identity` (discovery's latest snapshot), `baseline_summary` (status, built/required days; absent until a baselines row exists) and `headline_metrics` (calls in `from`/`to`, default 14 days; open anomalies; last seen). Absent values are omitted, not null. |
| GET | `/genome/identities/{source_type}/{identity_name}/genome` | Its behaviour fingerprint (`dimensions_json`), typical actions, typical resources and hour-of-week schedule. |
| GET | `/genome/identities/{source_type}/{identity_name}/activity` | Calls per day / hour, recent calls (cursor) and the total. |
| GET | `/genome/identities/{source_type}/{identity_name}/timeline` | Learning started, baseline established, versions published, and anomalies grouped per detection window ("1 active anomaly"). |

| GET | `/genome/workload-types` | The workload families shown (`NHI_GENOME_WORKLOAD_TYPES`): `{families, all}`. The console offers Genome links only for these. |
| GET | `/genome/dashboard/activity` | Fleet call volume per day (ClickHouse) and anomalies per day. |
| GET | `/genome/dashboard/drift` | Fleet baseline-health / drift / model-confidence snapshots. |
| GET | `/genome/identities` | The watched fleet with baseline, risk and open-anomaly figures (`source_type`, `status`, `search`, `limit`, `cursor`). |
| GET | `/genome/identities/{source_type}/{identity_name}/baseline` | Learning progress, versions and the profile of each version. |
| GET | `/genome/identities/{source_type}/{identity_name}/drift` | Drift status, confidence, retraining and per-dimension change. |
| GET | `/genome/identities/{source_type}/{identity_name}/peers` | Similar identities (same family, account, region) with calls, risk and the dimension where this one stands out. |
| GET | `/genome/identities/{source_type}/{identity_name}/containment` | Containment state (not available until the pipeline persists actions). |
| POST | `/genome/identities/{source_type}/{identity_name}/containment/freeze` | Credential freeze request: 501 until an action worker exists. |
| GET | `/genome/anomalies` | Departures, newest first (`state`, `dimension`, `source_type`, `search`, `limit`, `cursor`). |
| GET | `/genome/anomalies/{alert_id}` | One departure: what changed, why it was graded, evidence ids. |
| GET | `/genome/anomalies/{alert_id}/evidence` | The call behind it (ClickHouse), baseline-vs-observed, call risk and the reviewer's feedback. |
| POST | `/genome/anomalies/{alert_id}/feedback` | `{decision: legitimate\|malicious\|unknown, approved_for_baseline, reason}`; the reviewer is the signed-in user. Needs `alert_feedback` keyed by `(account_id, source_type, event_id)` as in the pipeline schema. |
| POST | `/genome/anomalies/{alert_id}/disposition` | 501: no disposition table yet (contract section 6.5). |
| GET | `/genome/containment` | Containment requests (empty, `available:false`). |

`identity_name` is URL-encoded (an ARN's `/` as `%2F`).

**Which workloads are shown.** `NHI_GENOME_WORKLOAD_TYPES` is a comma list of source families,
for example `ECS,EC2,GLUE,LAMBDA,STEPFUNCTION` (`STEP_FUNCTIONS` also works). Empty shows every
family. A kind that is switched off is not counted, listed, drifted or reviewable: its identities
answer `IDENTITY_NOT_FOUND`, its anomalies `ANOMALY_NOT_FOUND`. An unknown name stops the server
at start with the list of valid ones, so a typo cannot hide everything. Families: `APP_RUNNER, AWS_SLR,
BATCH, BEDROCK, CLOUDFORMATION, CODEBUILD, CODEDEPLOY, CODEPIPELINE, DATASYNC, DMS, EC2, ECS,
ELASTIC_BEAN, EMR, EVENTBRIDGE, GLUE, IAM, IOT, KINESIS, LAMBDA, MWAA, SAGEMAKER, SSM, STEP_FUNCTIONS,
SYNTHETICS, TRANSFER` (code: `internal/workloadtypes`).

---

## Features and their files

Each API follows the same layers: `handlers` (HTTP, validation) → `services`
(scope, composition) → `repository` (SQL / ClickHouse) → `models` (JSON shapes).
Shared by everything: `cmd/api-server/main.go` (wiring), `internal/routes/routes.go`
(every route), `internal/middleware` (JWT, CORS, logging, panic recovery),
`internal/scope` (tenant + account/region scope), `internal/config`,
`internal/database`, `pkg/jwt`.

| Feature | Files |
|---|---|
| Sign-in and session | `handlers/auth.go`, `services/auth_service.go`, `repository/user_repo.go`, `repository/refresh_token_repo.go`, `models/user.go`, `pkg/jwt`, `middleware/auth.go`, `migrations/global/001-003` |
| Workspace scope | `handlers/v1/settings_handler.go`, `filters_handler.go` · `services/v1/settings_service.go`, `filters_service.go` · `repository/v1/settings_repo.go`, `filters_repo.go` · `models/v1/settings.go`, `filters.go` · `scope/` · `migrations/tenant/001, 002, 004` |
| Dashboard | `handlers/v1/dashboard_overview_handler.go` · `services/v1/dashboard_overview_service.go` · `repository/v1/dashboard_overview_repo.go` · `models/v1/dashboard_overview.go` |
| Scan change tracking | `handlers/v1/scan_changes_handler.go` · `services/v1/scan_changes_service.go` (+ `AttachChangeEvidence` in `activity_service.go`) · `repository/v1/scan_changes_repo.go` · `repository/clickhouse/lookups.go` (`ChangeEvidenceByARNs`) · `models/v1/scan_changes.go` |
| Identities | `handlers/v1/identities_list_handler.go`, `identity_detail_handler.go`, `identity_events_handler.go` · `services/v1/identities_list_service.go`, `identity_detail_service.go`, `identity_events_service.go`, `identity_resolver.go` · `repository/v1/identities_list_repo.go`, `identity_detail_repo.go`, `identity_events_repo.go`, `identity_lookup_repo.go` · `models/v1/identities_list.go`, `identity_detail.go`, `identity_events.go` |
| Credentials | `handlers/v1/credentials_list_handler.go`, `credential_detail_handler.go` · `services/v1/credentials_service.go`, `credential_detail_service.go` · `repository/v1/credentials_list_repo.go`, `credential_detail_repo.go`, `trust_policy_parser.go` · `models/v1/credentials_list.go`, `credential_detail.go` |
| Relationship graph (access graph) | `repository/v1/graph_scope.go` (scope rules), `relationship_activity.go` (CloudTrail edges), `credential_detail_repo.go` / `identity_detail_repo.go` (`GetRelationships`) |
| CloudTrail activity | `internal/clickhouse/client.go` · `repository/clickhouse/*.go` · `repository/v1/activity_source.go` · `services/v1/activity_service.go`, `activity_tabs.go`, `activity_analytics.go`, `time_range.go`, `events_time_range.go` |
| Permission source | `handlers/v1/permission_source_handler.go` · `services/v1/permission_source_service.go` · `repository/v1/permission_source_repo.go` · `models/v1/permission_source.go` |
| Integrations (AWS) | `handlers/v1/aws_integration_handler.go` · `services/v1/aws_integration_service.go` · `repository/aws_integration_repo.go` · `models/v1/aws_integration.go` · `internal/awsconnect/` (STS, IAM, EventBridge, CodeCommit, Secrets Manager) · `internal/githubcheck/` · `config/aws_integration.go` · `repository/clickhouse/integration.go` · `migrations/global/004`, `migrations/tenant/003` |
| Credential exposure | `handlers/credential_exposure/` · `services/credential_exposure/` · `repository/credential_exposure/` · `models/credential_exposure/models.go` |
| Posture (ISPM) | `handlers/postures/` (one file per endpoint + `common.go`) · `services/postures/` · `repository/postures/` · `models/postures/` · `repository/clickhouse/posture_activity.go` |
| NHI Genome | `handlers/genome/` (one file per endpoint + `common.go`, `identity_route.go`) · `services/genome/` (+ `identity_lookup.go`, `identity_normalizer.go`) · `repository/genome/` (+ `monitoring_events.go`, `common.go`) · `models/genome/` · `config/monitoring_clickhouse.go` |
| Local tooling | `scripts/migrate.sh` (all migrations, in order), `scripts/seed_two_tenants.sh`, `scripts/e2e_tenant_scope.sh`, `scripts/test_schema.sql` (a local stand-in schema), `scripts/check_rds.sh` |

(Paths are under `internal/` unless they start with `cmd/`, `pkg/`, `migrations/` or `scripts/`.)

---

## Reference

### Sessions

Login starts a session: the access token carries its id (`jti`) and every
request checks that the session still exists and has not ended. Refresh tokens
are single-use and stay in the same session; reusing an old one ends every
session of that user (a token from a session that was logged out is just
refused). A session lives at most `jwt.session_max_hours` (default 720, 30
days) however often it is refreshed. Logout ends the session, so its access
token stops working at once - on every API above, since they share the
middleware. The role is re-read from `nhi_users` on every request, so a role
change applies at once.

Login always runs one bcrypt comparison (against a placeholder hash for
unknown emails), so response time does not reveal which emails exist.
`/login` and `/refresh` are rate limited in process per client address and
`/login` also per account; the address is `X-Real-IP` when the request comes
from a private or loopback peer (the console's nginx), else the peer itself.
Access tokens issued before `003_refresh_token_session.sql` have no session id
and are rejected, so users sign in again once after the upgrade.

### Tenant architecture

One PostgreSQL database (`nhi_db`); tenants are separated by schema:

```
nhi_db (config: database)
  ├── nhi_global (config: database.schema)
  │     ├── tenants     tenant_id (uuid) → db_name  (db_name = the tenant's schema name)
  │     └── nhi_users   email, bcrypt hash, role, tenant_id, status, last_login_at
  └── <tenant schema>   e.g. tenant_a — name comes from the JWT, never from config
        └── identities, credentials, trust_edges, user_settings, …
```

Login validates the user in `nhi_global`, looks up the tenant's schema name, and issues a
JWT carrying `tenant_id` and `db_name` (the schema name). Every tenant query is
schema-qualified from `DataScope` (`resolved.QSchema()` / `resolved.Table("x")`);
`search_path` is never changed. Schema names are validated (`config.ValidateSchemaName`)
and quoted before use.

**One choke point.** Every data request resolves through `scope.Resolver.Resolve()`,
which returns a `DataScope` carrying the tenant's pool *and* the effective
`AccountIDs / Regions`:

```go
resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
// every repository entry point takes the scope, never a bare *sql.DB
rows, err := s.repo.List(ctx, resolved, filter)
// DataScope.Predicates("i") → i.account_id = ANY($1) AND i.region = ANY($2)
```

Because the repositories cannot be called without a `DataScope`, steps 3–7 of the
document's "Every Data API" flow are structurally impossible to skip.
`user_settings` appears in exactly one non-test file in the repository.

**No fallback.** `Manager.Tenant("")` returns `ErrTenantDBNotConfigured`. A tenant with no
`db_name` is an error, never a silent switch to another schema.

### Running and testing locally

```bash
# 1. Postgres, then seed a throwaway database (nhi_seed_test; the script
#    drops and recreates it and refuses nhi_db) with two tenant schemas
export PGHOST=127.0.0.1 PGPORT=5432 PGUSER=postgres
./scripts/seed_two_tenants.sh

# 2. Point configs/config.yaml at that Postgres and run
NHI_DB_NAME=nhi_seed_test NHI_CONFIG_FILE=configs/config.yaml go run ./cmd/api-server

# 3. Verify the tenant guarantees end to end
./scripts/e2e_tenant_scope.sh          # 16 assertions
```

`scripts/test_schema.sql` is a stand-in for the scanner's schema, which this repository
does not own. It is only for local verification — column names were derived from the
queries in `internal/repository/v1`, so treat it as a harness, not as the real schema.

#### Integration tests against real databases

These skip unless pointed at a tenant (a SKIP means nothing was tested):

```bash
export NHI_TEST_TENANT_DB=<tenant schema name> NHI_TEST_TENANT_ID=<tenant uuid> NHI_TEST_USER_ID=<user email>
# ClickHouse comes from .env (NHI_CLICKHOUSE_*); use NHI_CLICKHOUSE_PROTOCOL=http|native
# CloudTrail fixtures: an account with events, an IAM user in it whose AssumeRole
# calls were denied, and that user's access key ID
export NHI_TEST_CT_ACCOUNT=<account id> NHI_TEST_CT_USER_ARN=<user arn> NHI_TEST_CT_ACCESS_KEY=<key id>
go test ./internal/repository/clickhouse ./internal/services/v1 ./internal/handlers/v1 -count=1 -v
```

They are read-only: every service and handler is exercised with ClickHouse,
without it, and with ClickHouse failing on every query.

### Identity Event Graph — `GET /api/v1/identities/{id}/events`

A forensic view of one identity's CloudTrail events, one endpoint with nine
sections chosen by `?section=` (`timeline` (default), `detail`, `credentials`,
`resources`, `heatmap`, `sessions`, `errors`, `cross_account`, `ips`).

| Param | Notes |
|---|---|
| `time_range` | `1h`, `6h`, `24h` (default), `7d`, `30d`, `90d`, `custom` (+ `start_date`/`end_date`). Hour ranges exist only here; other endpoints are day-based. |
| `service`, `action`, `result`, `resource`, `source_ip`, `credential`, `read_write` | Filters, applied to every section. `errors` always shows failures, so `result`/`read_write` are ignored there. `credential` must be one of the identity's own credential ARNs. |
| `page`, `page_size` | `page_size` default 100, max 500 (above that is a 400); `page * page_size` ≤ 100000. |
| `event_id` | Required for `section=detail`. |

Behaviour worth knowing:

- Events come from ClickHouse `cloudtrail_events`, matched by the identity's ARN,
  its bound credentials' ARNs and access-key ids, and always limited to the tenant's
  accounts. Postgres (`identities`, `credentials`, `identity_credentials`) is read
  once per request to enrich events in memory; `effective_permissions` is read only
  for `detail` (the policy statement behind the call).
- Fields the table does not store (`service`, `resource_name`, `status`,
  `credential_*`, `binding_type`, `is_cross_account`, `session_name`) are derived.
  `is_cross_account` compares the account in the resource ARN with the identity's
  account; S3 ARNs have no account and are never cross-account. `geo_location` is
  always `null`.
- `credentials` also lists credentials Postgres does not know (for example a role
  assumed from another account), with blank name/type/binding.
- `sessions` are built in Go: a caller's events more than 30 minutes apart start a
  new session (at most 50,000 events are read; `truncated` says if more existed).
- If ClickHouse is disabled or a query fails the section is returned empty with
  `data_available: false` (HTTP 200), like the Activity tab.
- Every ClickHouse query has a 10 s ceiling. The driver's read timeout is
  `NHI_CLICKHOUSE_TIMEOUT_SECONDS` (default 5); set it to 10 to allow the full 10 s.
- 404 for an identity outside the user's account/region selection, and for a
  `detail` `event_id` that does not belong to this identity and tenant.

### CloudTrail activity (ClickHouse)

CloudTrail events live in ClickHouse (`cloudtrail_events`); Postgres no longer
stores `identity_usage`, `permission_usage` or `resources`, and the columns
`identities.last_activity_at`, `identities.cloudtrail_creator_arn` and
`credentials.last_used_at` are no longer written by the collector. The two
databases are joined **by ARN only** (never by UUID):

| ClickHouse | Matches |
|---|---|
| `session_issuer_arn` | `credentials.arn` (a role) |
| `caller_arn` | `identities.arn` (IAM users, root) or an assumed-role session |
| `access_key_id` | `credentials.name` where `credential_type = 'access_key'` |

- **Layout:** `internal/clickhouse` (connection), `internal/repository/clickhouse`
  (SQL, raw rows), `internal/repository/v1/activity_source.go` (the interface the
  repositories use), `internal/services/v1/activity_*.go` (cache, analytics, tab
  builders), `internal/services/v1/identity_resolver.go` (caller → identity).
- **Isolation:** every ClickHouse query is bound to all accounts of the tenant
  (`accounts` table), whatever the user ticked in Settings.
- **Degradation:** unset (`NHI_CLICKHOUSE_HOST` empty), unreachable or failing
  ClickHouse never turns a response into an error. Activity fields are `null` or
  empty and `activity_data_available` is `false`.
- **Lists and dashboard:** idle days, `min_idle_days`, sorting by
  `last_activity_at` / `idle_days` / `last_used_at` / `permission_utilization_pct`
  (and the default sorts) are applied in Go over one bulk ClickHouse lookup,
  cached for 5 minutes. Stale detection falls back to the creation date for
  items never seen in CloudTrail.
- **Windows:** "what a credential actually used" (permission utilisation, the
  permissions tab, the trust summary) looks back 90 days; the relationships tab's
  ACCESSES edges use the requested `time_range`; last-seen is unbounded.
- **Events:** the activity tabs take `page` and `page_size` (max 100, at most 1000
  events per request). Every event carries `caller_arn`, `caller_type` and, when it
  resolves to the inventory, `credential_id/name`, `identity_id/name` and
  `terminated`; events that match nothing keep the raw ARN. ACCESSES edges carry
  `call_count`.
- **Relationship graph:** both ends of an edge must be in the caller's accounts
  and regions (IAM ARNs count as the `global` region, as in the lists), with two
  exceptions: an ACCESSES edge to an account-less ARN (an S3 bucket or object)
  is kept, since it is the identity's own recorded call, and a service principal
  (`lambda.amazonaws.com`) trusted by an in-scope role is drawn as a `service`
  node but never walked through. Edges come from `trust_edges` (assume_role,
  service_trust, cross_account, federated_saml / federated_oidc, pass_role)
  and from `identity_credentials`, whose rows are `bound_to` edges (identity ->
  the credential it runs as, live rows only): that is how depth 2 reaches what
  an instance's or function's own role trusts. Configured edges carry no
  `last_observed_at`; only CloudTrail edges do. `relationship_table[].relationship`
  is a plain label ("Uses", "Used by", "Can assume", "Trusted service", "Trusts
  another account", "Can hand a role to a service", "Signed in through SAML
  (company single sign-on)", ...). `edge_type` takes a comma-separated,
  case-insensitive list of the edge types above plus accesses, created_by,
  trusts, contains, manages, attached_to, uses. `activity_data_available: false`
  means CloudTrail could not be read, so ACCESSES / CREATED_BY edges may be
  missing.
- **Permission utilisation** is the same figure everywhere (list, permissions
  tab summary, dashboard): distinct actions used in 90 days out of the actions
  the policies grant.
- **Permission source (on demand):** `GET /api/v1/permission-source?credential_id=&event_source=&event_name=`
  returns the policy that granted one event (`policy_source`, `policy_type`,
  `effect`, `resource_arn`, `matched_action`; wildcards such as `ec2:Describe*`
  match, an exact Allow beats a wildcard or a Deny). Nothing matched gives
  `matched: false` and "Policy may have changed since this event".
- **"Receive your events" check (SQS):** with `NHI_AWS_EVENT_QUEUE_URL` set (an SQS
  queue in the platform account), the check *peeks* at up to 3 x 10 messages of that
  queue (`VisibilityTimeout=0`, nothing deleted, so the pipeline loses nothing) and
  passes when one has `account` = the customer's account. If the queue has nothing
  for the account or errors, it falls back to ClickHouse, then the CloudWatch log
  group, so no ClickHouse data is needed for the account to connect. One-time setup
  in the platform account: an SQS queue; a rule on the central event bus (pattern
  `{"account":[{"prefix":""}]}` or the same sources as the customer rule) with that queue
  as target; a queue policy allowing `events.amazonaws.com` `sqs:SendMessage` from
  the rule's ARN; and `sqs:ReceiveMessage` on the queue for the API's role (the
  assumer role / task role). Limits: a peek sees a sample, so on a busy shared queue
  a brand-new account may need a second click; peeks raise each message's receive
  count (set the DLQ's `maxReceiveCount` high).
- **Regions on the Integrations page:** the choices are `nhi_global.tenants.collector_regions`
  (empty = every region); the selected ones are the account's own `accounts.regions`.
  The "Values to copy" (account id, event bus ARN, region, External ID) are read from
  the tenant row, not hard-coded; the event sources and rule/role names come from code/config.
- **ClickHouse database per tenant:** leave `NHI_CLICKHOUSE_DATABASE` (and
  `NHI_MONITORING_CLICKHOUSE_DATABASE`) empty and every tenant reads its own database,
  `<nhi_global.tenants.db_name><suffix>` (suffix `NHI_CLICKHOUSE_DATABASE_SUFFIX`, default `_clickhouse`; for
  tenant `bimasugam`: `bimasugam_clickhouse`). The tenant comes from the signed token, never from the request;
  the ClickHouse user needs read access to each. Set the variable to pin one fixed database instead.
- **Account status:** `accounts.account_status` is `pending` / `active` / `action_needed`; a connected account is
  stored as `active` (migration `006_account_status_active.sql` converts old `connected` rows). The API still reports it as `connected`.
- **CodeCommit check:** the policy shown on the setup page is an inline policy with the four reads the scanner uses
  (`codecommit:GetBranch`, `GetCommit`, `GetDifferences`, `GetBlob`) for the account's regions; repositories are listed
  with the role's ViewOnlyAccess. The check lists repositories per selected region, then reads up to 3 per region
  (9 in all) the way the scanner does and names, per repository, which permission is missing.
- **Docker:** `docker compose up -d --build` builds the image (multi-stage, non-root,
  read-only, `/api/v1/health` check) and serves the API on `:8080`; databases are external
  (`.env`). `docker compose --profile tools run --rm migrate` applies the migrations;
  `--profile web` also builds and runs the console from `../frontend` on `:3000`.
- **Logging:** structured logs on stdout (`internal/logging`, Go `log/slog`).
  `NHI_LOG_LEVEL` (debug|info|warn|error, default info), `NHI_LOG_FORMAT`
  (json default, or text for local reading), `NHI_SLOW_QUERY_MS` (default 1000;
  slower ClickHouse queries log a `slow clickhouse query` warning). Every
  request gets an id (the caller's well-formed `X-Request-ID` or a new one),
  returned in the `X-Request-ID` header, in the genome `request_id` and on every
  log line of that request together with `tenant_id` and `user_id` once
  authenticated. One `http request` line per request (route template, status,
  duration; never the query string or headers), level by status (5xx error, 4xx
  warn). Sign-in, refresh and sign-out write `audit` lines; handler failures,
  degraded ClickHouse reads and panics (with stack) are logged with the request id.
- **Wrong table:** `NHI_CLICKHOUSE_TABLE` must be the CloudTrail events table
  (`cloudtrail_events`, with `caller_arn`, `session_issuer_arn`, `access_key_id`,
  `resource_arn`...), not the genome `log_events` table (that one is
  `NHI_MONITORING_CLICKHOUSE_TABLE`). At startup the table's columns are checked;
  a mismatch logs one `ERROR: clickhouse CloudTrail table unusable ...` naming the
  missing columns and turns activity data off, instead of failing every request
  with `Unknown expression ... caller_arn`.
- **Config:** `NHI_CLICKHOUSE_*` (see `.env.example`); `NHI_CLICKHOUSE_PROTOCOL`
  is `native` (TCP 9000/9440) or `http` (8123/8443).
- **Checking a deployment:** at start-up the server logs `clickhouse connected
  (host:port/db)` for the CloudTrail store and `monitoring clickhouse connected`
  (or why it is not) for the genome store. The server is the only program in
  this module (`cmd/api-server`).

### Integrations in detail

The six endpoints live in `internal/handlers/v1/aws_integration_handler.go`, its service and repository,
`internal/awsconnect` and `internal/githubcheck`. Reads are open to every signed-in user; changes need
the `admin` (or `super_admin`) role.

#### Database

Apply once to `nhi_db` (both safe to run again; see "Migrations, in order"
above for the full list):

```sh
psql "$DSN" -f migrations/global/004_tenant_collector.sql            # nhi_global.tenants columns
psql "$DSN" -v schema=bimasugam -f migrations/tenant/004_accounts_connection.sql   # per tenant schema
```

`nhi_global.tenants` gains `domain_name`, `collector_aws_account_id` (our
account the customer's role trusts), `collector_external_id` (generated, unique,
never change it once used), `collector_event_bridge_arn` (our central bus) and
`collector_regions` (regions customers may choose; empty means all).

`<tenant schema>.accounts` is one table for both the scope and the
connection (one row per AWS account, keyed by the unique `account_id`). The
scanner's columns are used as they are: `tenant_id`, `account_id`,
`regions text[]`, `is_default`, `client_aws_account_id`, `client_role_arn`,
`external_id`, `is_connected`, `account_status` (`pending` / `active` (the API shows it as `connected`) /
`action_needed`), `github_enabled`, `codecommit_enabled`. Migration 003 adds
only what the page needs on top: check results (`checks`, `last_verified_at`,
`last_verified_by`, `connected_at`), the GitHub secret ARN and per-platform
checks, and the sync queue columns. An AWS account can belong to one tenant
only (checked across schemas under an advisory lock).

Verify writes the customer's role, External ID and regions onto the
account's row (adding it when missing, in the default scope). `regions` always
includes the synthetic `global` region (IAM roles and users are recorded under
it, so they show as soon as the account connects); the API strips it from what
it returns. Editing regions rewrites the array (keeping `global`); removing
the account deletes the row. Rows the scanner wrote without a role are scope
only and are not listed on the Integrations page. Dashboards, filters,
settings and ClickHouse isolation read the same table.

Without the migrations the page answers 503 `AWS_PLATFORM_NOT_CONFIGURED`.

#### Connection checks (verify)

| Check | How | Fix shown |
|---|---|---|
| Assume your role | `sts:AssumeRole` with the tenant's External ID | Check the trust policy / External ID |
| Read your account | `iam:ListRoles` (1 item) | Attach SecurityAudit + ViewOnlyAccess |
| Receive your events | allows the account on our bus, then looks for one of its events in the last `event_lookback_hours` (ClickHouse, else `event_log_group`) | Check the EventBridge rule target ARN |

One check per tenant at a time (429 `AWS_VERIFY_IN_PROGRESS`), 12 s budget.
Nothing is saved unless AssumeRole succeeds. The first full pass sets
`connected_at`, registers the scope rows and queues the first sync.

#### Exposed credentials (GitHub / CodeCommit)

Only for an account that has connected. Each platform needs one extra grant on
the customer's existing role; the page shows the steps with their values filled in.

- **CodeCommit:** attach the AWS-managed `AWSCodeCommitReadOnly`, or an inline
  policy with `codecommit:ListRepositories, BatchGetRepositories, GetRepository,
  ListBranches, GetBranch, GetCommit, GetDifferences, GetBlob, GetFile,
  GetFolder, GitPull`. Checked by listing repositories in the account's regions
  and reading one repository's root folder.
- **GitHub:** a fine-grained token with *Contents: Read-only* (and
  *Metadata: Read-only*) stored in the customer's own Secrets Manager under a name
  starting with `github_secret_prefix` (`nhi/github`), and an inline policy on
  the role allowing `secretsmanager:GetSecretValue` and `DescribeSecret` on
  `arn:aws:secretsmanager:*:<account>:secret:nhi/github*` (plus `kms:Decrypt` if
  the secret uses a customer managed key). Checked by reading the secret through
  the role and calling GitHub (`/user`, `/user/repos`, one repository's
  contents). The token is never stored or logged; only the secret ARN is.

A platform is switched on only when its checks pass. `exposure_enabled` is
true when any account has one on; the console shows the exposed-credential
screens only then.

Secret scanner contract: scan CodeCommit where `codecommit_enabled`, and the
GitHub token at `github_secret_arn` where `github_enabled`, by assuming
`client_role_arn` with `external_id`.

#### Finding lists (read-time rules)

Nothing is deleted or stored per list: the API sorts every finding that is not
in `secscan_allowlist_dashboard` into exactly one list when it reads
`secscan_findings_codecommit` + `secscan_findings_github`, checking in this
order (the scanner's own rules, DATABASE_HANDOVER section 8b):

1. **Repeated** - the same `value_fingerprint` at 3 or more different
   `(file_path, line_number)` places in one `(platform, client_id, repository,
   commit_id)`. Detectors containing `private_key` are exempt.
2. **Known harmless** - `noise_flags` holds any of `known_example_value`,
   `expired_signed_url`, `vector_image_file`, `integrity_hash`,
   `arn_identifier`, `password_hash`, `plain_url`.
3. **Needs review** - `ml_decision = FALSE_POSITIVE` with `ml_score >= 0.25`, or
   `ml_decision = REAL_SECRET` with `ml_score < 0.40` (or, for a generic
   detector, `ml_veto_score < 0.50`).
4. **Likely false alarms** - `ml_decision = FALSE_POSITIVE`; or a generic
   detector with `ml_veto_score < 0.13305`; or a `*_broad` detector unless
   `ml_score >= 0.95` and `ml_veto_score >= 0.30`.
5. **Main list** - everything else, and always any row whose
   `ml_model_version` is `rule:private-key-with-body`.

A NULL score or tag means "not scored", never zero, so an old row is never
hidden by a rule that did not exist when it was written. Generic detectors are
`generic_*`, `high_entropy_string*`, `jwt*`, `basic_auth_header*`,
`bearer_token*`, `authorization_header*`, `url_embedded*`, `curl_*`,
`docker_login*`, `webhook_url_generic*`, `azure_sas_token*` and
`*_connection_string` (not `azure_storage_*`). The private-key exemption from
the repeated rule is matched on the detector name (`private_key`); confirm it
against your scanner's detector names. `ml_features` is never returned.

#### Deep-scan contract (scanner worker)

The API never calls the scanner. `POST /credential-exposure/deep-scan` upserts
`<schema>.secscan_deepscan_<platform>` (`client_id`, `repo_key`, `platform`,
`requested_at` in the scanner's `YYYY-MM-DDTHH:MM:SS+00:00` form) and clears
the previous run's `completed_at`, `commits_scanned`, `findings`, `failed_at`
and `error`; a row that is still waiting (no `completed_at`, no `failed_at`) is
left as it is and reported `already_running` (the scanner's own API restarts a running scan on re-POST; here a running one is left alone so an in-flight walk is not reset - a completed or failed one restarts, same as the guide). The platform is the request's,
else the one the client has ledger rows or findings on, else `github` for an
`owner/repo` name and `codecommit` otherwise. The worker claims waiting rows
and writes `completed_at` + `commits_scanned` + `findings`, or `failed_at` +
`error`; `GET /deep-scan/status` reads the same row.

#### Sync contract (discovery worker)

The API only queues (`sync_status = 'queued'`). The worker claims one account
at a time per tenant schema:

```sql
UPDATE <schema>.accounts SET sync_status = 'running', sync_started_at = now()
 WHERE account_id = (
       SELECT account_id FROM <schema>.accounts
        WHERE sync_status = 'queued' ORDER BY sync_requested_at
        FOR UPDATE SKIP LOCKED LIMIT 1)
RETURNING account_id, client_role_arn, external_id, regions;
```

It reads only `regions`, then sets `sync_status = 'succeeded', last_synced_at =
now(), last_sync_error = ''`, or `'failed'` with a short `last_sync_error`
(shown to the customer).

#### AWS identity of this service

No AWS keys in config. The SDK default chain is used:

- **Production:** run the API as `NHIPlatformAssumer` (ECS task role / EC2
  instance profile) with `assume_assumer_role: false`.
- **Locally:** `AWS_PROFILE=<profile>` and `NHI_AWS_ASSUME_ASSUMER_ROLE=true`;
  the profile must be allowed to assume
  `arn:aws:iam::<collector_aws_account_id>:role/NHIPlatformAssumer`.

`NHIPlatformAssumer` needs:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow", "Action": "sts:AssumeRole",
      "Resource": "arn:aws:iam::*:role/NHIPlatformCollector" },
    { "Effect": "Allow",
      "Action": ["events:DescribeEventBus", "events:PutPermission", "events:RemovePermission"],
      "Resource": "arn:aws:events:<region>:<our account>:event-bus/<bus name>" },
    { "Effect": "Allow", "Action": "logs:FilterLogEvents",
      "Resource": "arn:aws:logs:<region>:<our account>:log-group:<event_log_group>:*" }
  ]
}
```

The last statement is only needed with `event_log_group`. Settings are under
`aws_integration` in `configs/config.yaml`, with `NHI_AWS_*` /
`NHI_GITHUB_API_URL` overrides (see `.env.example`). Each connected account adds
one statement to the bus policy (about 10 KB, roughly 100 accounts per bus).

#### Trying the APIs locally: which ones need AWS

| Endpoint | Calls AWS? | Locally |
|---|---|---|
| `GET /integrations/aws` | no | works with only Postgres |
| `PUT …/accounts/{id}/regions` | no | works with only Postgres |
| `POST …/accounts/{id}/sync` | no (queues a row) | works; stays `queued` until a discovery worker claims it |
| `DELETE …/accounts/{id}` | best effort (removes the bus permission; a failure is only logged) | works without AWS |
| `POST /integrations/aws/verify` | yes: STS AssumeRole, IAM ListRoles, EventBridge PutPermission | needs AWS (below) |
| `PUT …/accounts/{id}/exposure` | yes: AssumeRole, CodeCommit, Secrets Manager, and GitHub | needs AWS (below) |

When the service has no AWS configuration at all, verify and exposure answer
503 `AWS_UNAVAILABLE`; when AWS is configured but cannot be reached (no or
expired credentials, network), they answer 200 with the affected checks marked
as not run, and nothing is switched on. Nothing else is affected. To run them for real you need: (1) AWS credentials
for a profile allowed to assume `NHIPlatformAssumer` in the collector account
(`AWS_PROFILE=… NHI_AWS_ASSUME_ASSUMER_ROLE=true`), (2) the tenant's collector
values in `nhi_global.tenants`, and (3) in a test "customer" account, the
`NHIPlatformCollector` role and EventBridge rule exactly as the Integrations
page shows them. The "receive your events" check also needs one event from
that account in ClickHouse (or `event_log_group`) within
`event_lookback_hours`.

#### Errors

| HTTP | `error` | When |
|---|---|---|
| 400 | `INVALID_ROLE_ARN`, `INVALID_REGIONS`, `INVALID_SECRET_ARN`, `INVALID_ACCOUNT_ID`, `INVALID_REQUEST` | bad input |
| 403 | `FORBIDDEN` | not an admin |
| 404 | `AWS_ACCOUNT_NOT_FOUND` | not this tenant's account |
| 409 | `AWS_ACCOUNT_NOT_CONNECTED` | sync or exposure before the account has connected |
| 409 | `AWS_ACCOUNT_IN_USE` | another tenant already has a row for this account (connected by them or registered by the scanner) |
| 429 | `AWS_VERIFY_IN_PROGRESS` | a check is already running for this tenant |
| 503 | `AWS_PLATFORM_NOT_CONFIGURED` | migrations missing or the tenant row has no collector values |
| 503 | `AWS_UNAVAILABLE` | AWS unreachable, or this service has no AWS identity |

### NHI Genome in detail

Every response is `{ "data": ..., "meta": { "request_id", "generated_at", "next_cursor" } }`; errors are
`{ "error": { "code", "message", "request_id" } }`. Data comes from the tenant schema (`baselines`,
`baseline_versions`, `alerts`, `identity_risk_scores`) and the
monitoring ClickHouse `log_events`. Empty tables give `0` / empty lists / `null` (a legitimate
"still learning" state), never invented values.

Columns read from `log_events` (ReplacingMergeTree on `ingested_at`, so counts
use `uniqExact(event_id)` and row reads use `FINAL`): `event_id`, `source_type`,
`identity_name`, `event_time`, `region`, `source_ip`, `event_name`,
`event_source`, `resource_arns` (a comma-separated string), `dimensions_json`,
`risk_status`, `event_risk_score`.

* An identity is addressed as `(source_type, identity_name)`: the discovery
  identity's family (`ECS`, `EC2`, `LAMBDA`, ...) and its canonical ARN, the same
  key the pipeline writes to `log_events` (an ECS task by its task ARN). The
  console lists identities from the inventory (`/api/v1/identities`) and links
  each monitored one to these endpoints. An identity outside the caller's
  accounts/regions answers `404 IDENTITY_NOT_FOUND` - run
  `005_register_discovered_regions.sql` so every scanned region can be selected.
* `identity_name` must be URL-encoded (Postman: paste the ARN and let Postman encode it, or encode `/` as `%2F`).
* `limit` default 25, max 100; `cursor` is opaque – copy `meta.next_cursor` into the next request unchanged.
* `from` inclusive / `to` exclusive, ISO-8601 UTC; default window = last 14 days.
* Unknown query parameters → `400 VALIDATION_ERROR`; tenant scope/discovery
  failure → `503 STORE_UNAVAILABLE` (`503 TENANT_NOT_PROVISIONED` when the tenant schema lacks a table); missing monitoring tables or ClickHouse → **200 with nulls / empty lists**.

### Scan change tracking in detail

- **Tables** (written by the discovery scanner): `nhi_scans_aws` (one row per
  run: `scan_id` text such as `scan-20261016-201642`, `account_id`, `status`
  `COMPLETED`, `scan_end`) and `scan_changes` (one row per difference: `id`,
  `scan_id`, `previous_scan_id`, `change_type`, `entity_type`, `entity_arn`,
  `entity_id`, `summary`, `details` jsonb, `detected_at`). A scan's previous
  scan is the account's preceding completed run. `scan_changes` has no
  account/region columns: they are read from the entity ARN (no region =
  `global`), and the entity name from the inventory row.
- **`details`** is an object the console renders as text, never as raw JSON:
  CREATED / DELETED carry facts (`type`, `name`, `account_id`, `region`, ...);
  MODIFIED carries `fields_changed: [{field, old, new}]` and, for permissions,
  `actions_added` / `actions_removed`; a trust edge carries `source`, `target`,
  `edge_type`, `external`.
- **Ownership**: a change belongs to the inventory row in `entity_id` (an
  identity, or a credential when `entity_type = 'credential'`) or with the same
  ARN; that drives the Changes tabs and the New / Modified list markers.
- **Scanner noise is not a change**: a MODIFIED row whose only difference is
  `scan_errors` inside `type_attributes` is left out of every count, list,
  badge and tab (`notScannerNoise` in `scan_changes_repo.go`). The scanner's
  AWS error text carries a fresh RequestID each run, so without this every
  identity would read as changed after every scan. Needs PostgreSQL 16+
  (`pg_input_is_valid`).
- **Scope**: a scan is visible when it ran for an account the user
  selected; a change when its account and region are selected (IAM is `global`).
- **CloudTrail evidence** is not stored: for CREATED / DELETED identity and
  credential changes the API looks up the creating / deleting call
  (`CreateRole`, `CreateAccessKey`, `DeleteUser`, ... - successful calls only) on
  `resource_arn` = `entity_arn` in the 30 days up to the scan, within the
  tenant's accounts.
- **Summary**: when the worker writes no `summary`, the API writes one
  ("Identity deploy-bot was added").
- **Before the migration** every endpoint answers as if no scan has run
  (`recent_changes: null`, empty tabs with `change_tracking_available: false`,
  `/scans/{id}/changes` 404).
