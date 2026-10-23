import client from './client';
import { ApiError, params, unwrapMain, unwrapPosture } from './http';

/*
 * Identity security posture (ISPM) - the posture service, same origin under
 * /api/v1/ispm. Every answer is { data, page?, meta }:
 *
 *   meta = { scan_id, computed_at, window, data_available }
 *   page = { next_cursor, limit }        keyset paging; cursors are opaque
 *
 * Parameters are strict (an unknown or blank one is a 400), so only what is
 * set is sent. The service allows 20 requests a second per tenant; a 429
 * carries Retry-After, and reads retry once after it when the wait is short.
 *
 *   GET   /posture/overview                    fleet score, trend, bands, pillars, quick wins
 *   GET   /posture/quick-wins/{rule}/identities identities a quick win applies to
 *   GET   /posture/export?format=csv           the identity list as CSV (same filters)
 *   GET   /identities, /identities/facets      list + facet counts (same filters)
 *   GET   /identities/{id}[/posture|checks|history|credentials|role-use|activity|alerts|graph]
 *   GET   /checks/{rule}                       one check: fix, remediation, compliance
 *   PATCH /findings/{id}                       accept / suppress / reopen / assign / note
 *
 * Posture identities are not addressed by the discovery id. They are matched
 * by ARN (`resolvePostureIdentity`), and a posture row is opened on the
 * identity page through `resolveDiscoveryId`.
 */
const BASE = '/api/v1/ispm';

/** Windows the service accepts, in the order the control shows them. */
export const POSTURE_WINDOWS = ['24h', '3d', '7d', '30d', '60d', '90d'];
export const DEFAULT_WINDOW = '30d';

export function normaliseWindow(value) {
  return POSTURE_WINDOWS.includes(value) ? value : DEFAULT_WINDOW;
}

/** What a window is called in a sentence: "24 hours", "30 days". */
export function windowPhrase(window) {
  if (window === '24h') return '24 hours';
  const days = Number.parseInt(window, 10);
  return Number.isFinite(days) ? `${days} days` : window;
}

const wait = (ms, signal) =>
  new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener?.('abort', () => {
      clearTimeout(timer);
      reject(new ApiError({ message: 'Request cancelled', code: 'CANCELLED' }));
    });
  });

/**
 * Start a posture scan of the signed-in tenant on the ISPM rule engine
 * (POST /ispm/scans). 202 = started, 200 = one is already running.
 * Returns { jobId, alreadyRunning }.
 */
export async function startPostureScan() {
  const res = await client.post(`${BASE}/scans`, {});
  const d = res?.data?.data ?? {};
  return { jobId: d.job_id, alreadyRunning: Boolean(d.already_running) };
}

/** The scan job's state: QUEUED | RUNNING | COMPLETED | PARTIAL | FAILED | SKIPPED. */
export async function fetchPostureScanJob(jobId, signal) {
  const res = await client.get(`${BASE}/scans/${encodeURIComponent(jobId)}`, { signal });
  const d = res?.data?.data ?? {};
  return { status: String(d.status || '').toUpperCase(), tenants: Array.isArray(d.tenants) ? d.tenants : [] };
}

/** GET with one retry on a short 429, unwrapped to { data, page, meta }. */
async function get(path, query, signal) {
  const request = () => client.get(`${BASE}${path}`, { params: params(query), signal });
  try {
    return unwrapPosture(await request());
  } catch (error) {
    if (error?.status === 429 && (error.retryAfter ?? 1) <= 5) {
      await wait((error.retryAfter ?? 1) * 1000, signal);
      return unwrapPosture(await request());
    }
    throw error;
  }
}

const list = (value) => (Array.isArray(value) ? value : []);

/* ── Fleet ──────────────────────────────────────────────────────────────── */

export async function fetchPostureOverview({ window } = {}, signal) {
  const { data, meta } = await get('/posture/overview', { window }, signal);
  return { overview: data ?? {}, meta };
}

export async function fetchQuickWinIdentities(ruleId, { limit = 25, cursor } = {}, signal) {
  const { data, page, meta } = await get(`/posture/quick-wins/${encodeURIComponent(ruleId)}/identities`, { limit, cursor }, signal);
  return {
    rule: data?.rule ?? null,
    identities: list(data?.identities),
    nextCursor: page?.next_cursor ?? null,
    meta,
  };
}

export async function fetchCheckDetail(ruleId, signal) {
  const { data } = await get(`/checks/${encodeURIComponent(ruleId)}`, {}, signal);
  return data;
}

/**
 * The shared identity filters of /identities, /identities/facets and
 * /posture/export: { window, band[], severity[], category[], account[],
 * type[], pillar[], failing_check[], changed, q }.
 */
function filterQuery(filters = {}) {
  const q = String(filters.q ?? '').trim();
  const runes = [...q].length;
  return {
    window: filters.window,
    band: filters.band,
    severity: filters.severity,
    category: filters.category,
    account: filters.account,
    type: filters.type,
    pillar: filters.pillar,
    failing_check: filters.failing_check,
    changed: filters.changed,
    q: runes >= 2 && runes <= 100 ? q : undefined,
  };
}

/** One keyset page of the identity list. sort: score | -score | failed | change | name. */
export async function fetchPostureIdentities(filters, { sort, limit = 25, cursor } = {}, signal) {
  const { data, page, meta } = await get('/identities', { ...filterQuery(filters), sort, limit, cursor }, signal);
  return { items: list(data), nextCursor: page?.next_cursor ?? null, meta };
}

/**
 * Every scored identity's score, band and grade by ARN, for screens that list
 * discovery identities and show their posture beside them. Read in pages of
 * 100, up to 1,000 identities.
 */
export async function fetchPostureScoreIndex(signal) {
  const index = new Map();
  let cursor;
  for (let page = 0; page < 10; page += 1) {
    const result = await fetchPostureIdentities({}, { sort: 'name', limit: 100, cursor }, signal);
    for (const item of result.items) {
      if (item.arn) index.set(item.arn, { score: item.score, band: item.band, grade: item.grade, id: item.identity_id });
    }
    if (!result.nextCursor) break;
    cursor = result.nextCursor;
  }
  return index;
}

export async function fetchPostureFacets(filters, signal) {
  const { data, meta } = await get('/identities/facets', filterQuery(filters), signal);
  return { facets: data ?? {}, meta };
}

function filenameFrom(disposition) {
  const match = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(disposition ?? '');
  if (!match) return null;
  try {
    return decodeURIComponent(match[1]).replace(/[/\\]/g, '_');
  } catch {
    return match[1];
  }
}

/**
 * The identity list as CSV, with the same filters as the screen. Downloaded
 * through the authenticated client (a plain link would not carry the token)
 * and saved under the name the service gives it.
 */
export async function exportPostureCsv(filters) {
  let response;
  try {
    response = await client.get(`${BASE}/posture/export`, {
      params: params({ format: 'csv', ...filterQuery(filters) }),
      responseType: 'blob',
      headers: { Accept: 'text/csv' },
      timeout: 120000,
    });
  } catch (error) {
    if (error?.status === 400) {
      throw new ApiError({ ...error, message: 'The export was refused: one of the filters is not valid.' });
    }
    throw error;
  }
  const filename =
    filenameFrom(response.headers?.['content-disposition']) ||
    `identity-posture-${new Date().toISOString().slice(0, 10)}.csv`;
  const url = URL.createObjectURL(response.data);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
  return { filename };
}

/* ── One identity ───────────────────────────────────────────────────────── */

const idPath = (id) => `/identities/${encodeURIComponent(id)}`;

/** Header: name, arn, type, account, region, score, band, grade, badges, counts. */
export async function fetchPostureHeader(id, signal) {
  const { data, meta } = await get(idPath(id), {}, signal);
  return { header: data, meta };
}

export async function fetchIdentityPosture(id, { window } = {}, signal) {
  const { data, meta } = await get(`${idPath(id)}/posture`, { window }, signal);
  return { posture: data, meta };
}

/** status: failed | passed | all; pillar: a pillar code. */
export async function fetchIdentityChecks(id, { status, pillar } = {}, signal) {
  const { data, meta } = await get(`${idPath(id)}/checks`, { status, pillar }, signal);
  return { checks: list(data), meta };
}

export async function fetchIdentityHistory(id, { window } = {}, signal) {
  const { data, meta } = await get(`${idPath(id)}/history`, { window }, signal);
  return { points: list(data), meta };
}

export async function fetchIdentityCredentials(id, signal) {
  const { data, meta } = await get(`${idPath(id)}/credentials`, {}, signal);
  return { credentials: list(data), meta };
}

export async function fetchIdentityRoleUse(id, signal) {
  const { data, meta } = await get(`${idPath(id)}/role-use`, {}, signal);
  return {
    roleUse: { assumable: Boolean(data?.assumable), assumedBy: list(data?.assumed_by), assumes: list(data?.assumes) },
    meta,
  };
}

/** mutating: true | false | undefined (both); service: e.g. "s3". */
export async function fetchIdentityActivity(id, { window, mutating, service, limit = 50, cursor } = {}, signal) {
  const { data, page, meta } = await get(
    `${idPath(id)}/activity`,
    { window, mutating: mutating === undefined ? undefined : String(mutating), service, limit, cursor },
    signal,
  );
  return { events: list(data), nextCursor: page?.next_cursor ?? null, meta };
}

/** status: open | closed | all. */
export async function fetchIdentityAlerts(id, { window, status, limit = 50, cursor } = {}, signal) {
  const { data, page, meta } = await get(`${idPath(id)}/alerts`, { window, status, limit, cursor }, signal);
  return { alerts: list(data), nextCursor: page?.next_cursor ?? null, meta };
}

/* ── Findings ───────────────────────────────────────────────────────────── */

/**
 * patch: any of { status: 'ACCEPTED'|'SUPPRESSED'|'OPEN', accepted_reason,
 * suppressed_until (future ISO), assigned_to, note }. Returns the FindingState.
 * ifMatch: the finding's updated_at as last read; the API then answers 409
 * (field "If-Match") when someone changed it in the meantime.
 */
export async function updateFinding(findingId, patch, { ifMatch } = {}) {
  const body = Object.fromEntries(Object.entries(patch).filter(([, value]) => value !== undefined));
  const headers = ifMatch ? { 'If-Match': `"${ifMatch}"` } : undefined;
  const res = await client.patch(`${BASE}/findings/${encodeURIComponent(findingId)}`, body, { headers });
  const state = res?.data?.data;
  if (!state || typeof state !== 'object' || !state.finding_id) {
    throw new ApiError({ message: 'The posture service answered with an unexpected response.', status: res?.status ?? null });
  }
  return state;
}

/* ── Linking posture and discovery identities ───────────────────────────── */

/** The `q` the list accepts (2-100 runes) that finds an ARN. */
export function postureNeedleFor(arn) {
  const value = String(arn ?? '').trim();
  const runes = [...value];
  if (runes.length <= 100) return runes.length >= 2 ? value : null;
  const tail = value.split(/[/:]/).filter(Boolean).pop() ?? '';
  const tailRunes = [...tail];
  if (tailRunes.length >= 2 && tailRunes.length <= 100) return tail;
  return runes.slice(-100).join('');
}

const postureByArn = new Map();

/**
 * The posture identity of a discovery identity { id, arn, name }, matched on
 * the exact ARN. Returns the posture header, or null when the identity has
 * not been scored.
 */
export async function resolvePostureIdentity(identity, signal) {
  const arn = String(identity?.arn ?? '').trim();
  if (!arn) return null;
  const known = postureByArn.get(arn);
  if (known) {
    const { header } = await fetchPostureHeader(known, signal);
    if (header?.arn === arn) return header;
    postureByArn.delete(arn);
  }
  const needle = postureNeedleFor(arn);
  if (!needle) return null;
  const { items } = await fetchPostureIdentities({ q: needle }, { limit: 100 }, signal);
  if (items.length === 0) return null;
  /* List items carry the ARN: an exact match needs no further request. */
  const exact = items.find((item) => item.arn === arn);
  if (exact) {
    const { header } = await fetchPostureHeader(exact.identity_id, signal);
    if (header?.arn === arn) {
      postureByArn.set(arn, exact.identity_id);
      return header;
    }
  }
  if (items.some((item) => item.arn)) return null;
  /* An older service without `arn` on list items: confirm candidates against
     the header, the one named like the identity first. */
  const name = identity?.name ?? '';
  const candidates = [...items].sort((a, b) => Number(b.name === name) - Number(a.name === name)).slice(0, 10);
  for (const candidate of candidates) {
    const { header } = await fetchPostureHeader(candidate.identity_id, signal);
    if (header?.arn === arn) {
      postureByArn.set(arn, candidate.identity_id);
      return header;
    }
  }
  return null;
}

const discoveryByPosture = new Map();

/**
 * The discovery id for a posture identity, so a posture row opens the
 * identity page. Matched on the exact ARN through the discovery search;
 * null when discovery does not hold it (then link `identitySearchRoute`).
 */
export async function resolveDiscoveryId({ postureId, name, arn }, signal) {
  if (postureId && discoveryByPosture.has(postureId)) return discoveryByPosture.get(postureId);
  let targetArn = arn;
  let targetName = name;
  if (!targetArn && postureId) {
    const { header } = await fetchPostureHeader(postureId, signal);
    targetArn = header?.arn;
    targetName = targetName || header?.name;
  }
  if (!targetArn || !targetName) return null;
  const res = await client.get('/api/v1/identities', {
    params: params({ search: targetName, page_size: 100 }),
    signal,
  });
  const body = unwrapMain(res);
  const match = list(body?.data).find((item) => item.arn === targetArn);
  const id = match?.id ?? null;
  if (postureId) discoveryByPosture.set(postureId, id);
  return id;
}

export const postureRoute = (postureId) => `/posture/${encodeURIComponent(postureId)}`;
export const identitySearchRoute = (name) => `/identities?search=${encodeURIComponent(name ?? '')}`;
