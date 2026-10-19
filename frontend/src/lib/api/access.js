import client from './client';
import { ApiError, params, unwrapMain, unwrapPosture } from './http';

/**
 * Access graph data, read from the discovery service (MAIN) and, where it can
 * be matched, the posture service.
 *
 *   Nodes and edges  GET /api/v1/identities/{id}?tab=relationships
 *                    GET /api/v1/credentials/{id}?tab=relationships
 *                    Trust edges come from Postgres (trust_edges); `accesses`
 *                    and `created_by` edges from CloudTrail in ClickHouse. The
 *                    service prunes both to the caller's saved account/region
 *                    scope, so nothing out of scope can be drawn.
 *   Node records     the identity and credential lists and detail tabs.
 *   Posture score    GET /api/v1/ispm/identities?q=<arn> -> exact-ARN match.
 *
 * Relationship graphs name nodes by ARN. Opening a node needs its discovery
 * id, so ARNs are resolved through the two lists and the answer is cached for
 * the session (`resolveArn`).
 */

const PAGE = 100;
/* How many list pages are read up front to build the ARN index. Small tenants
   are indexed whole; anything past this is resolved one search at a time. */
const INDEX_PAGES = 5;

/* ── Lists ───────────────────────────────────────────────────────────────── */

export async function listIdentities(query = {}, signal) {
  const res = await client.get('/api/v1/identities', { params: params(query), signal });
  return unwrapMain(res);
}

export async function listCredentials(query = {}, signal) {
  const res = await client.get('/api/v1/credentials', { params: params(query), signal });
  return unwrapMain(res);
}

async function readAllPages(fetchPage, signal) {
  const rows = [];
  let total = 0;
  let activity = true;
  for (let page = 1; page <= INDEX_PAGES; page += 1) {
    const body = await fetchPage({ page, page_size: PAGE }, signal);
    rows.push(...(body?.data ?? []));
    total = body?.pagination?.total_count ?? rows.length;
    activity = body?.activity_data_available !== false;
    if (page >= (body?.pagination?.total_pages ?? 1)) break;
  }
  return { rows, total, complete: rows.length >= total, activityAvailable: activity };
}

/* ── Session cache: ARN -> discovery record ──────────────────────────────── */

const byArn = new Map(); // arn -> { identity?: row, credential?: row }
const byId = new Map(); // discovery id -> { kind, row }
const postureByArn = new Map(); // arn -> Promise<posture header | null>
let indexPromise = null;

function remember(kind, row) {
  if (!row?.arn || !row?.id) return;
  const entry = byArn.get(row.arn) ?? {};
  entry[kind] = row;
  byArn.set(row.arn, entry);
  byId.set(row.id, { kind, row });
}

/**
 * The inventory index: every identity and credential in scope (up to
 * INDEX_PAGES pages of each). Shared by the focus picker and the graph, read
 * once per session. A failure is not cached, so a retry reads again.
 */
export function loadInventoryIndex() {
  /* Not tied to any one caller's AbortSignal: the index is shared, and one
     screen unmounting must not cancel it for the next. */
  if (!indexPromise) {
    indexPromise = Promise.all([readAllPages(listIdentities), readAllPages(listCredentials)])
      .then(([identities, credentials]) => {
        identities.rows.forEach((row) => remember('identity', row));
        credentials.rows.forEach((row) => remember('credential', row));
        return { identities, credentials };
      })
      .catch((error) => {
        indexPromise = null;
        throw error;
      });
  }
  return indexPromise;
}

let cacheOwner = null;

/** Drops every cached answer, e.g. after the data scope changed. */
export function resetInventoryIndex() {
  indexPromise = null;
  byArn.clear();
  byId.clear();
  postureByArn.clear();
}

/**
 * Scopes the session cache to one signed-in user and tenant: a different
 * sign-in in the same tab must never see the previous tenant's ARNs.
 */
export function scopeCacheTo(owner) {
  if (owner === cacheOwner) return;
  cacheOwner = owner;
  resetInventoryIndex();
}

const found = (entry) => (entry && (entry.identity || entry.credential) ? entry : null);

export function recordForArn(arn) {
  return found(byArn.get(arn));
}

export function recordForId(id) {
  return byId.get(id) ?? null;
}

const pendingResolutions = new Map();

/**
 * Resolves a graph ARN to its discovery records ({ identity?, credential? }),
 * or null when neither list has it (a resource, or a principal outside the
 * inventory). Searches by the ARN itself - both lists match `search` against
 * the ARN - and keeps only an exact match.
 */
export function resolveArn(arn, signal) {
  if (!arn) return Promise.resolve(null);
  if (byArn.has(arn)) return Promise.resolve(found(byArn.get(arn)));
  if (pendingResolutions.has(arn)) return pendingResolutions.get(arn);
  const search = arn.length > 200 ? arn.slice(-200) : arn;
  const promise = Promise.all([
    listIdentities({ search, page_size: 20 }, signal).catch(() => null),
    listCredentials({ search, page_size: 20 }, signal).catch(() => null),
  ])
    .then(([identities, credentials]) => {
      (identities?.data ?? []).filter((row) => row.arn === arn).forEach((row) => remember('identity', row));
      (credentials?.data ?? []).filter((row) => row.arn === arn).forEach((row) => remember('credential', row));
      const entry = found(byArn.get(arn));
      /* Only a definite answer is cached: two failed searches say nothing. */
      if (!entry && identities && credentials) byArn.set(arn, {});
      return entry;
    })
    .finally(() => pendingResolutions.delete(arn));
  pendingResolutions.set(arn, promise);
  return promise;
}

/**
 * Looks a discovery id up as an identity, then as a credential. Used when a
 * focus id arrives in the URL and is not in the index.
 */
export async function resolveId(id, signal) {
  if (byId.has(id)) return byId.get(id);
  try {
    const overview = await getIdentityTab(id, 'overview', {}, signal);
    const row = overview?.identity;
    if (row) {
      remember('identity', row);
      return { kind: 'identity', row };
    }
  } catch (error) {
    if (error?.status !== 404 && error?.status !== 400) throw error;
  }
  const overview = await getCredentialTab(id, 'overview', {}, signal);
  const row = overview?.credential;
  if (!row) throw new ApiError({ message: 'That identity is not in the current scope.', status: 404 });
  remember('credential', row);
  return { kind: 'credential', row };
}

/* ── Detail tabs ─────────────────────────────────────────────────────────── */

export async function getIdentityTab(id, tab, query = {}, signal) {
  const res = await client.get(`/api/v1/identities/${encodeURIComponent(id)}`, {
    params: params({ tab, ...query }),
    signal,
  });
  return unwrapMain(res);
}

export async function getCredentialTab(id, tab, query = {}, signal) {
  const res = await client.get(`/api/v1/credentials/${encodeURIComponent(id)}`, {
    params: params({ tab, ...query }),
    signal,
  });
  return unwrapMain(res);
}

export async function getIdentityEvents(id, section, query = {}, signal) {
  const res = await client.get(`/api/v1/identities/${encodeURIComponent(id)}/events`, {
    params: params({ section, ...query }),
    signal,
  });
  return unwrapMain(res);
}

/** One node's neighbourhood (depth 1), as the relationships tab returns it. */
export function getRelationships(kind, id, signal) {
  return kind === 'credential'
    ? getCredentialTab(id, 'relationships', { depth: 1 }, signal)
    : getIdentityTab(id, 'relationships', { depth: 1 }, signal);
}

export async function getSettings(signal) {
  const res = await client.get('/api/v1/settings', { signal });
  return unwrapMain(res);
}

/* ── Posture (optional) ──────────────────────────────────────────────────── */

/**
 * The posture record for an ARN: { identity_id, score, band, grade } or null.
 * Posture ids are not assumed to equal discovery ids, so the identity is found
 * by ARN (`q` is an ILIKE on name/arn, 2-100 characters) and confirmed against
 * the header's own `arn`. Anything that fails resolves to null: a missing score
 * is shown as "no score", never as an error on a screen about something else.
 */
export function findPosture(arn, name) {
  if (!arn) return Promise.resolve(null);
  if (postureByArn.has(arn)) return postureByArn.get(arn);
  const q = arn.length <= 100 ? arn : String(name ?? '').slice(0, 100);
  if (q.length < 2) return Promise.resolve(null);
  const promise = (async () => {
    /* No AbortSignal: the answer is shared by every caller, so one caller
       going away must not cancel it for the rest. */
    const res = await client.get('/api/v1/ispm/identities', { params: params({ q, limit: 5 }) });
    const { data } = unwrapPosture(res);
    for (const row of (data ?? []).slice(0, 3)) {
      const header = unwrapPosture(
        await client.get(`/api/v1/ispm/identities/${encodeURIComponent(row.identity_id)}`),
      ).data;
      if (header?.arn === arn) return header;
    }
    return null;
  })().catch(() => {
    postureByArn.delete(arn);
    return null;
  });
  postureByArn.set(arn, promise);
  return promise;
}
