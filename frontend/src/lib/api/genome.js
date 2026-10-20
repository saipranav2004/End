import client from './client';
import { ApiError, params, unwrapGenome, unwrapMain } from './http';
import { canonicalGenomeName, genomeSourceType } from '../identityLinks';

/*
 * NHI Genome (behaviour monitoring), served by the NHI backend under
 * `/api/v1/genome/*` from the pipeline's ClickHouse log_events (plus the
 * baselines / baseline_versions / alerts tables for the timeline):
 *
 *   GET /dashboard/summary                                   ?from &to
 *   GET /identities/{source_type}/{identity_name}            ?from &to
 *   GET .../genome                                           ?from &to &limit
 *   GET .../activity                                         ?from &to &bucket &recent_limit &recent_cursor
 *   GET .../timeline                                         ?limit &cursor
 *
 * The genome has no list of its own: the monitored fleet is the discovery
 * inventory's workload identities (see fetchMonitoredFleet), addressed by
 * the (source_type, identity_name) key in lib/identityLinks.js.
 *
 * Envelope { data, meta: { request_id, generated_at, next_cursor } }. Unknown
 * params are rejected, so only set values are sent. Missing monitoring data is
 * a 200 with nulls / empty lists, never invented values - the screens show
 * those as "not measured yet" rather than as zero.
 */
const BASE = '/api/v1/genome';
const FLEET_PAGE = 100; // the inventory list's page-size cap
const FLEET_MAX_PAGES = 10;

const HOUR = 3600 * 1000;
const DAY = 24 * HOUR;

/** Detection windows the console offers, as durations. */
export const WINDOW_MS = { '24h': DAY, '7d': 7 * DAY, '14d': 14 * DAY, '30d': 30 * DAY };

/**
 * `from`/`to` for a window ending now. `to` is rounded up to the next minute
 * so that re-renders inside the same minute ask the same question.
 */
export function windowRange(window) {
  const span = WINDOW_MS[window];
  if (!span) return {};
  const to = new Date(Math.ceil(Date.now() / 60000) * 60000);
  const from = new Date(to.getTime() - span);
  return { from: from.toISOString(), to: to.toISOString() };
}

function identityPath(sourceType, identityName, suffix = '') {
  return `${BASE}/identities/${encodeURIComponent(sourceType)}/${encodeURIComponent(identityName)}${suffix}`;
}

function unexpected() {
  return new ApiError({ message: 'The behaviour service answered with an unexpected response.' });
}

function list(value) {
  return Array.isArray(value) ? value : [];
}

/**
 * Fleet dashboard: identities watched, baselines established, still learning
 * and the recently baselined feed. With no window the service answers cards
 * for the last 24 hours; with a window it uses that.
 */
export async function fetchGenomeSummary({ window, sourceType, accountId, region } = {}, signal) {
  const range = window && window !== '24h' ? windowRange(window) : {};
  const { data, meta } = unwrapGenome(
    await client.get(`${BASE}/dashboard/summary`, {
      params: params({ ...range, source_type: sourceType, account_id: accountId, region }),
      signal,
    }),
  );
  if (!data || typeof data !== 'object' || !data.cards) throw unexpected();
  return {
    cards: data.cards,
    recentlyBaselined: list(data.recently_baselined),
    generatedAt: meta.generated_at ?? null,
  };
}

/**
 * The monitored fleet: every discovery identity of a type the genome watches,
 * with its genome key. Read in pages of 100 up to 1,000 identities;
 * `complete` is false beyond that. Rows keep the inventory fields (id, name,
 * arn, identity_type, account_id, region, status, last_activity_at);
 * `activityAvailable` is false when CloudTrail could not be read, so a
 * missing last_activity_at is unknown rather than "never".
 */
export async function fetchMonitoredFleet(signal) {
  const items = [];
  let activityAvailable = true;
  for (let page = 1; page <= FLEET_MAX_PAGES; page += 1) {
    const data = unwrapMain(
      await client.get('/api/v1/identities', { params: { page, page_size: FLEET_PAGE, sort_by: 'name', sort_order: 'asc' }, signal }),
    );
    if (data?.activity_data_available === false) activityAvailable = false;
    for (const row of list(data?.data)) {
      const sourceType = genomeSourceType(row.identity_type);
      if (sourceType && row.arn) items.push({ ...row, sourceType, identityName: canonicalGenomeName(row.identity_type, row.arn) });
    }
    if (page >= (Number(data?.pagination?.total_pages) || 0)) return { items, complete: true, activityAvailable };
  }
  return { items, complete: false, activityAvailable };
}

/**
 * The identity page header: the identity as discovery sees it, its baseline
 * (absent until the pipeline has a baseline row) and the headline metrics -
 * calls in the window, open anomalies and the last call (absent until the
 * identity has been seen).
 */
export async function fetchGenomeIdentity(sourceType, identityName, { window } = {}, signal) {
  const { data } = unwrapGenome(
    await client.get(identityPath(sourceType, identityName), { params: params(windowRange(window)), signal }),
  );
  if (!data || typeof data !== 'object' || !data.identity) throw unexpected();
  const metrics = data.headline_metrics ?? {};
  return {
    identity: data.identity,
    baseline: data.baseline_summary ?? null,
    callsInWindow: metrics.calls_in_window ?? null,
    openAnomalies: metrics.open_anomalies ?? null,
    lastSeen: metrics.last_seen ?? null,
  };
}

/** Call volume per bucket plus one page of recent calls. */
export async function fetchGenomeActivity(
  sourceType,
  identityName,
  { window, bucket, recentLimit, recentCursor } = {},
  signal,
) {
  const { data, meta } = unwrapGenome(
    await client.get(identityPath(sourceType, identityName, '/activity'), {
      params: params({ ...windowRange(window), bucket, recent_limit: recentLimit, recent_cursor: recentCursor }),
      signal,
    }),
  );
  if (!data || typeof data !== 'object') throw unexpected();
  return {
    volume: list(data.volume),
    recentCalls: list(data.recent_calls),
    totalCalls: data.total_calls ?? null,
    nextCursor: meta.next_cursor || null,
    generatedAt: meta.generated_at ?? null,
  };
}

/** The behavioural profile: fingerprint, typical actions/resources, schedule. */
export async function fetchGenomeProfile(sourceType, identityName, { window, limit } = {}, signal) {
  const { data } = unwrapGenome(
    await client.get(identityPath(sourceType, identityName, '/genome'), {
      params: params({ ...windowRange(window), limit }),
      signal,
    }),
  );
  if (!data || typeof data !== 'object') throw unexpected();
  return {
    fingerprint: list(data.fingerprint),
    typicalActions: list(data.typical_actions),
    typicalResources: list(data.typical_resources),
    activitySchedule: list(data.activity_schedule),
  };
}

/** One page of the identity's history, newest first. */
export async function fetchGenomeTimeline(sourceType, identityName, { limit, cursor } = {}, signal) {
  const { data, meta } = unwrapGenome(
    await client.get(identityPath(sourceType, identityName, '/timeline'), {
      params: params({ limit, cursor }),
      signal,
    }),
  );
  if (!data || !Array.isArray(data.items)) throw unexpected();
  return { items: data.items, nextCursor: meta.next_cursor || null };
}

/**
 * The discovery (inventory) record of a genome identity, matched by ARN, or
 * null when it cannot be resolved. Genome keys Lambda and Step Functions by
 * their canonical ARN, so a discovery ARN is canonicalised before comparing.
 */
export async function resolveDiscoveryIdentity(identityName, signal) {
  const arn = String(identityName ?? '').trim();
  if (!arn.startsWith('arn:')) return null;
  const data = unwrapMain(
    await client.get('/api/v1/identities', { params: { search: arn, page_size: 25 }, signal }),
  );
  const rows = list(data?.data);
  return (
    rows.find((row) => row.arn === arn) ??
    rows.find((row) => canonicalGenomeName(row.identity_type, row.arn) === arn) ??
    null
  );
}
