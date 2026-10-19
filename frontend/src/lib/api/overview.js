import client from './client';
import { shapeRecentChanges } from './changes';
import { ApiError, unwrapGenome, unwrapMain, unwrapPosture } from './http';

/*
 * The organisation dashboard's reads. Each service answers on its own, so the
 * screen asks each one separately and a service that is down blanks only the
 * panels it feeds.
 *
 *   MAIN     GET /api/v1/dashboard/overview     KPIs + charts, scoped by saved settings
 *            GET /api/v1/settings               the user's saved scope
 *            PUT /api/v1/settings               { selected_account_ids, selected_regions }
 *            GET /api/v1/filters                every account / region that has data
 *   POSTURE  GET /api/v1/ispm/posture/overview  fleet score, pillars, quick wins
 *   GENOME   GET /api/v1/genome/dashboard/summary  baselines, anomalies (nulls when not monitored)
 */

const list = (value) => (Array.isArray(value) ? value : []);
const num = (value) => (Number.isFinite(Number(value)) ? Number(value) : 0);
const numOrNull = (value) => (value === null || value === undefined || !Number.isFinite(Number(value)) ? null : Number(value));

function shapeOverview(d) {
  if (!d || typeof d !== 'object' || !d.kpis || !d.charts) {
    throw new ApiError({ message: 'The discovery service answered with an unexpected response.' });
  }
  const k = d.kpis;
  const c = d.charts;
  return {
    kpis: {
      totalIdentities: num(k.total_identities),
      totalCredentials: num(k.total_credentials),
      orphanedCredentials: num(k.orphaned_credentials),
      staleIdentities: num(k.stale_identities),
      staleCredentials: num(k.stale_credentials),
      publiclyAccessible: num(k.publicly_accessible),
      shadowIdentities: num(k.shadow_identities),
      accountsScanned: num(k.accounts_scanned),
      permissionUtilizationPct: numOrNull(k.permission_utilization_pct),
      /* Not served by every build of the API; shown only when present. */
      lastScanTime: k.last_scan_time ?? null,
    },
    charts: {
      byType: list(c.identities_by_type).map((r) => ({ key: r.type, value: num(r.count) })),
      byClass: list(c.identities_by_functional_class).map((r) => ({ key: r.class, value: num(r.count) })),
      byAccount: list(c.identities_by_account).map((r) => ({ key: r.account_id, value: num(r.count) })),
      byRegion: list(c.identities_by_region).map((r) => ({ key: r.region, value: num(r.count) })),
      byEnvironment: list(c.identities_by_environment).map((r) => ({ key: r.environment, value: num(r.count) })),
      byStatus: list(c.identities_by_status).map((r) => ({ key: r.status, value: num(r.count) })),
      identityGrowth: list(c.identity_growth).map((r) => ({ date: r.date, count: num(r.count) })),
      credentialGrowth: list(c.credential_growth).map((r) => ({ date: r.date, count: num(r.count) })),
      staleVsActive: { active: num(c.stale_vs_active?.active), stale: num(c.stale_vs_active?.stale) },
      credentialTypes: list(c.credential_types).map((r) => ({ key: r.type, value: num(r.count) })),
      topOwners: list(c.top_owners).map((r) => ({ key: r.owner, value: num(r.count) })),
      expiringSoon: list(c.credentials_expiring_soon).map((r) => ({
        name: r.credential_name,
        expiresAt: r.expires_at,
        daysRemaining: num(r.days_remaining),
      })),
    },
    activityDataAvailable: Boolean(d.activity_data_available),
    recentChanges: shapeRecentChanges(d.recent_changes),
  };
}

export async function fetchDashboardOverview(signal) {
  return shapeOverview(unwrapMain(await client.get('/api/v1/dashboard/overview', { signal })));
}

/** { accounts: string[], regions: string[], accountsDefault, regionsDefault, source } */
export async function fetchScopeSettings(signal) {
  const d = unwrapMain(await client.get('/api/v1/settings', { signal }));
  const s = d?.settings ?? {};
  return {
    accounts: list(s.accounts?.selected),
    regions: list(s.regions?.selected),
    accountsDefault: Boolean(s.accounts?.is_default),
    regionsDefault: Boolean(s.regions?.is_default),
    source: d?.source ?? '',
  };
}

/** Every account and region that holds data, with counts. */
export async function fetchScopeOptions(signal) {
  const d = unwrapMain(await client.get('/api/v1/filters', { signal }));
  return {
    accounts: list(d?.accounts).map((a) => ({
      id: a.account_id,
      identities: num(a.identity_count),
      credentials: num(a.credential_count),
    })),
    regions: list(d?.regions).map((r) => ({ id: r.region, identities: num(r.count) })),
  };
}

export async function updateScopeSettings({ accounts, regions }) {
  return unwrapMain(
    await client.put('/api/v1/settings', { selected_account_ids: accounts, selected_regions: regions }),
  );
}

/** Posture overview: { data, meta } where meta.data_available says whether a score exists. */
export async function fetchPostureSummary({ window = '30d' } = {}, signal) {
  const { data, meta } = unwrapPosture(await client.get('/api/v1/ispm/posture/overview', { params: { window }, signal }));
  const fleet = data?.fleet ?? {};
  const available = meta?.data_available !== false && numOrNull(fleet.score) !== null;
  return {
    available,
    computedAt: meta?.computed_at ?? null,
    window: meta?.window || window,
    fleet: {
      score: numOrNull(fleet.score),
      grade: fleet.grade || '',
      band: fleet.band || '',
      scored: num(fleet.identities_scored),
      delta: numOrNull(fleet.change?.delta),
      changeWindow: fleet.change?.window || window,
    },
    trend: list(data?.trend).map((p) => ({ at: p.at, score: num(p.score) })),
    distribution: {
      healthy: num(data?.distribution?.healthy),
      fair: num(data?.distribution?.fair),
      poor: num(data?.distribution?.poor),
      critical: num(data?.distribution?.critical),
    },
    pillars: list(data?.pillars).map((p) => ({
      key: p.pillar,
      name: p.name || p.pillar,
      score: numOrNull(p.score),
      failing: num(p.failing),
      evaluated: num(p.evaluated),
      status: String(p.status || '').toUpperCase(),
    })),
    quickWins: list(data?.quick_wins).map((w) => ({
      ruleId: w.rule_id,
      title: w.title,
      pillar: w.pillar,
      identities: num(w.identities),
      fleetGain: num(w.fleet_gain),
    })),
    weakest: list(data?.weakest_identities).map((w) => ({
      id: w.identity_id,
      name: w.name,
      functionalClass: w.functional_class ?? null,
      account: w.account,
      score: numOrNull(w.score),
      band: w.band,
      checksFailed: num(w.checks_failed),
    })),
  };
}

/** Behaviour summary. Every card can be null: the service says "not measured" rather than zero. */
export async function fetchGenomeSummary(signal) {
  const { data, meta } = unwrapGenome(await client.get('/api/v1/genome/dashboard/summary', { signal }));
  const cards = data?.cards ?? {};
  return {
    generatedAt: meta?.generated_at ?? null,
    cards: {
      watched: numOrNull(cards.identities_watched),
      established: numOrNull(cards.baselines_established),
      learning: numOrNull(cards.still_learning),
      openAnomalies: numOrNull(cards.open_anomalies),
      coveragePercent: numOrNull(cards.baseline_coverage_percent),
      windowHours: numOrNull(cards.window_hours),
    },
    anomaliesByDay: list(data?.anomalies_by_day).map((r) => ({ day: r.day, count: num(r.count) })),
    mostAnomalous: list(data?.most_anomalous_identities).map((r) => ({
      sourceType: r.source_type,
      identityName: r.identity_name,
      displayName: r.display_name || r.identity_name,
      accountId: r.account_id,
      region: r.region,
      riskScore: numOrNull(r.risk_score),
      severity: r.severity || '',
      anomalousEvents: num(r.anomalous_event_count),
      events: num(r.event_count),
    })),
    departures: list(data?.departures_by_dimension).map((r) => ({ key: r.dimension, value: num(r.count) })),
  };
}
