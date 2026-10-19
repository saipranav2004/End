# NHI Console - frontend

React 19 + Vite + Tailwind v4. The console for the NHI backend, one Go service
that serves every API below on one origin, behind one sign-in. No demo data:
every screen reads a real API.

| Browser path | API | Used by |
|---|---|---|
| `/api/v1/*` | Discovery | sign-in, dashboard, scan changes, identities, credentials, activity, access graph, exposed credentials, integrations |
| `/api/v1/ispm/*` | ISPM posture | Posture page, identity Posture tab, posture scores elsewhere |
| `/api/v1/genome/*` | NHI Genome | NHI Genome pages, behaviour panel on the dashboard |

## Run locally

```sh
npm ci
cp .env.example .env      # backend URL for the dev proxy; nothing secret
npm run dev               # http://localhost:3000
npm run lint && npm run build
```

The dev server proxies `/api` to the backend (see `vite.config.js`). In
production nginx does the same (`nginx/nginx.conf.template`); the image takes
`API_UPSTREAM` and `DNS_RESOLVER` at run time (see `Dockerfile`).

## Integrations preview (before the AWS side is deployed)

Verifying a real AWS account needs the platform's AWS side deployed. To review
the console before that, build with `VITE_INTEGRATIONS_PREVIEW=true`:

```sh
npm run dev:preview        # or: npm run build:preview
docker build --build-arg VITE_INTEGRATIONS_PREVIEW=true -t nhi-console:preview .
```

In that build:

- the console opens fully at first sign-in - no "connect your first AWS
  account" step, nothing locked;
- the Integrations page runs on three sample AWS accounts held in the browser
  (healthy, syncing, needs attention), and connecting, syncing, editing
  regions, removing and code-scanning set-up all work on them. A banner says so
  and "Reset sample" restores them. Nothing is sent to the server or to AWS;
- every other page - dashboard, posture, identities, credentials, activity,
  access graph, NHI Genome, exposed credentials - reads the real backend.

Leave it off (the default) in production.

## Sessions

`POST /api/v1/login` starts a session and returns an access token and a refresh
token. A 401 refreshes once (single-flight, shared across tabs with the Web
Locks API) and retries; when that fails the console returns to sign-in.
Signing out ends the session on the server, so its access token stops working
everywhere at once, other tabs included. See `src/lib/api/{client,session,auth}.js`.

## What a workspace sees

Driven by `GET /api/v1/integrations/aws` (`src/app/WorkspaceContext.jsx`):

1. **No AWS account connected yet:** only the Integrations setup, in a focused
   layout. Any other URL redirects there (`src/app/ConnectionGate.jsx`).
2. **An account has connected:** the full console, except the exposed-credential
   screens.
3. **Exposed-credential scanning switched on** (GitHub or CodeCommit passed its
   checks for any account): *Credential exposure → Exposed credentials /
   Accepted* join the sidebar and the command palette. A typed URL before that
   shows how to switch it on (`src/app/RequireFeature.jsx`).

If that check cannot be made at all, the console opens as usual rather than
locking out a workspace that is already set up.

## Roles

Server roles map to console roles in `src/lib/api/auth.js`; what each may do is
in `src/lib/roles.js`. Changing integrations and the exposed-credential screens
are admin-only (the API enforces the same); finding updates on Posture are for
analysts and admins.

## Screens

| Route | Source |
|---|---|
| `/integrations` | Connect AWS (role, EventBridge rule, verify with regions), accounts (sync, regions, re-check, remove), exposed-credential scanning per account |
| `/overview` | discovery dashboard, recent changes (latest scan), posture overview, genome summary, exposure summary (only when on) |
| `/scans/:scanId/changes` | everything one discovery scan changed: summary, filters by change and entity type, details with before / after |
| `/posture`, `/posture/:id` | posture overview, facets, identities, checks, CSV export; one identity's posture with "Open full record" |
| `/identities`, `/identities/:id` | discovery identities (NEW / CHANGED from the latest scan); tabs overview, posture, permissions, credentials, role use, activity, changes |
| `/credentials` | discovery credentials (NEW / CHANGED markers) and their detail tabs, including changes |
| `/activity` | CloudTrail activity for a chosen identity |
| `/access-graph`, `/access-graph/:id` | relationships around an identity or credential; granted vs used |
| `/genome`, `/genome/:id` | anomalies to review (real anomaly / false positive), established baselines, activity, timeline |
| `/exposure`, `/exposure/dismissed` | exposed-credential findings, verdicts, accepted list, deep scans |

Pages in the design reference with no API behind them (Alerts, Reports, Users,
Scans) are not part of this build.
