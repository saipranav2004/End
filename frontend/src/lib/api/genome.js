import client from './client';
import { ApiError, params, unwrapGenome, unwrapMain } from './http';
import { canonicalGenomeName } from '../identityLinks';

/*
 * NHI Genome (behaviour monitoring) API, served by the NHI backend under
 * `/api/v1/genome/*` (its /identities is a different list from the discovery
 * API's /api/v1/identities). The one write is a reviewer's verdict on an
 * anomaly; there is no disposition, suppression or containment in this API.
 *
 *   GET /dashboard/summary                       ?from &to &source_type &account_id &region
 *   GET /identities                              ?source_type &search &account_id &region &status &baseline &sort &limit &cursor
 *   GET /anomalies                               ?source_type &identity_name &search &dimension &state &review &from &to &limit &cursor
 *   POST /anomalies/{id}/feedback                { decision, approved_for_baseline, reason }
 *   GET /identities/{source_type}/{identity_name}            ?from &to
 *   GET .../activity                                         ?from &to &bucket &recent_limit &recent_cursor
 *   GET .../genome                                           ?from &to &limit
 *   GET .../baseline                                         ?limit
 *   GET .../timeline                                         ?limit &cursor
 *
 * Envelope { data, meta: { request_id, generated_at, next_cursor } }. Unknown
 * params are rejected, so only set values are sent. Missing monitoring data is
 * a 200 with nulls / empty lists, never invented values - the screens show
 * those as "not measured yet" rather than as zero.
 */
const BASE = '/api/v1/genome';
const FLEET_PAGE = 100;
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
 * Fleet dashboard. With no window the service answers cards for the last 24
 * hours and charts for the last 14 days; with a window both use it.
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
    departuresByDimension: list(data.departures_by_dimension),
    anomaliesByDay: list(data.anomalies_by_day),
    mostAnomalous: list(data.most_anomalous_identities),
    recentlyBaselined: list(data.recently_baselined),
    chartsFrom: range.from ?? null,
    generatedAt: meta.generated_at ?? null,
  };
}

/** One page of the monitored fleet. */
export async function fetchGenomeIdentities(
  { sourceType, search, accountId, region, status, baseline, sort, limit, cursor } = {},
  signal,
) {
  const { data, meta } = unwrapGenome(
    await client.get(`${BASE}/identities`, {
      params: params({
        source_type: sourceType,
        search,
        account_id: accountId,
        region,
        status,
        baseline,
        sort,
        limit,
        cursor,
      }),
      signal,
    }),
  );
  if (!data || !Array.isArray(data.items)) throw unexpected();
  return { items: data.items, nextCursor: meta.next_cursor || null };
}

/**
 * The whole fleet, unfiltered, for the facet rail's options and counts. Read
 * in pages of 100 up to 1,000 identities; `complete` is false beyond that, and
 * the rail then stops claiming counts it does not have.
 */
export async function fetchGenomeFleetIndex({ baseline } = {}, signal) {
  const items = [];
  let cursor;
  for (let page = 0; page < FLEET_MAX_PAGES; page += 1) {
    const result = await fetchGenomeIdentities({ baseline, sort: 'name_asc', limit: FLEET_PAGE, cursor }, signal);
    items.push(...result.items);
    if (!result.nextCursor) return { items, complete: true };
    cursor = result.nextCursor;
  }
  return { items, complete: false };
}

/** Header record: identity, risk, baseline summary, headline metrics. */
export async function fetchGenomeIdentity(sourceType, identityName, { window } = {}, signal) {
  const { data } = unwrapGenome(
    await client.get(identityPath(sourceType, identityName), { params: params(windowRange(window)), signal }),
  );
  if (!data || typeof data !== 'object' || !data.identity) throw unexpected();
  return data;
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

/** Baseline status, progress, dimensions and published versions. */
export async function fetchGenomeBaseline(sourceType, identityName, { limit } = {}, signal) {
  const { data } = unwrapGenome(
    await client.get(identityPath(sourceType, identityName, '/baseline'), { params: params({ limit }), signal }),
  );
  if (!data || typeof data !== 'object') throw unexpected();
  return { ...data, dimensions: list(data.dimensions), versions: list(data.versions) };
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

/**
 * One page of anomalies, newest first, with the review counts of every
 * anomaly matching the filters (review aside). `review` is 'pending',
 * 'reviewed' or empty for both; `identity` narrows to one identity.
 */
export async function fetchAnomalies({ review, identity, search, limit = 25, cursor } = {}, signal) {
  const { data, meta } = unwrapGenome(
    await client.get(`${BASE}/anomalies`, {
      params: params({
        review,
        source_type: identity?.sourceType,
        identity_name: identity?.identityName,
        search,
        limit,
        cursor,
      }),
      signal,
    }),
  );
  if (!data || !Array.isArray(data.items) || !data.counts) throw unexpected();
  return {
    items: data.items,
    counts: data.counts,
    feedbackAvailable: data.feedback_available !== false,
    nextCursor: meta.next_cursor || null,
  };
}

/** Reviewer verdicts, as stored and as the console names them. */
export const ANOMALY_VERDICTS = {
  malicious: { label: 'Real anomaly', tone: 'critical' },
  legitimate: { label: 'False positive', tone: 'low' },
  unknown: { label: 'Not sure', tone: 'neutral' },
};

/**
 * Records a reviewer's verdict on one anomaly. It does not close the anomaly
 * or change the baseline; `approvedForBaseline` asks the pipeline to treat
 * the behaviour as normal at its next baseline update.
 */
export async function submitAnomalyFeedback(alertId, { decision, approvedForBaseline = false, reason } = {}) {
  const { data } = unwrapGenome(
    await client.post(`${BASE}/anomalies/${encodeURIComponent(alertId)}/feedback`, {
      decision,
      approved_for_baseline: approvedForBaseline,
      ...(reason?.trim() ? { reason: reason.trim() } : {}),
    }),
  );
  if (!data?.feedback) throw unexpected();
  return data.feedback;
}
