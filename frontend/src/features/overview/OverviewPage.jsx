import { useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  AlertTriangle,
  ArrowRight,
  Bot,
  CalendarClock,
  FileWarning,
  Fingerprint,
  Gauge,
  Globe2,
  KeyRound,
  LineChart,
  RotateCw,
  ScanEye,
  Users,
} from 'lucide-react';
import { useAccess } from '../../app/useAccess';
import { useWorkspace } from '../../app/WorkspaceContext';
import { fetchFindings } from '../../lib/api/exposure';
import {
  fetchDashboardOverview,
  fetchGenomeSummary,
  fetchPostureSummary,
  fetchScopeSettings,
} from '../../lib/api/overview';
import { useQuery } from '../../lib/hooks';
import { NEUTRAL_SERIES, SERIES_TOKENS, severityMeta } from '../../lib/domain';
import { formatDate, formatNumber, formatRelative, humanizeToken, parseDate, percentValue } from '../../lib/format';
import { genomeRoute } from '../../lib/identityLinks';
import { PageHeader } from '../../shell/PageHeader';
import { BarList } from '../../charts/BarList';
import { CompositionDonut } from '../../charts/CompositionDonut';
import { TrendChart } from '../../charts/TrendChart';
import { Button } from '../../ui/Button';
import { Meter } from '../../ui/Meter';
import { Panel, PanelHeader } from '../../ui/Panel';
import { MetricTile } from '../../ui/Stat';
import { Select } from '../../ui/Field';
import { Tag } from '../../ui/Tag';
import { cn } from '../../ui/cn';
import { ChartSkeleton, DonutSkeleton, ListSkeleton, LoadingAnnouncement, Skeleton, StatStripSkeleton } from '../../ui/Skeleton';
import { ClearState, EmptyState, ErrorState, InlineError } from '../../ui/States';
import { postureRoute, windowPhrase } from '../../lib/api/posture';
import { RecentChangesCard } from '../changes/RecentChangesCard';
import { BandTag, Delta, ScoreRing } from '../posture/parts';
import { credentialTypeMeta, environmentLabel, functionalClassMeta, identityTypeLabel, lifecycleMeta } from '../identities/meta';
import { edgeKindMeta } from '../access/graphModel';
import { describeExposureError, summariseFindings } from '../exposure/scannerState';
import { sourceTypeLabel } from '../genome/meta';
import { ScopeStrip } from './ScopeStrip';
import { SignalList } from './SignalList';

/**
 * The organisation dashboard.
 *
 * Three services feed it - discovery (inventory counts, scoped by the user's
 * saved accounts and regions), posture (the fleet score) and behaviour
 * (baselines and anomalies) - plus credential exposure when the workspace has
 * it switched on and the role may see it. Each is asked separately and each
 * panel fails on its own, so one service being down never blanks the page.
 */
export default function OverviewPage() {
  const { can } = useAccess();
  const { exposureEnabled, status: workspaceStatus } = useWorkspace();
  /* Exposure figures appear only once the feature is switched on in
     Integrations AND the role may open them; otherwise the API is not asked. */
  const exposure = can('exposure.view') && exposureEnabled;
  const offerExposureSetup = !exposureEnabled && workspaceStatus === 'ready' && can('integrations.manage');
  return <OrganisationDashboard exposure={exposure} offerExposureSetup={offerExposureSetup} />;
}

function OrganisationDashboard({ exposure, offerExposureSetup }) {
  const overviewQuery = useQuery((signal) => fetchDashboardOverview(signal), []);
  const scopeQuery = useQuery((signal) => fetchScopeSettings(signal), []);
  const postureQuery = useQuery((signal) => fetchPostureSummary({ window: '30d' }, signal), []);
  const genomeQuery = useQuery((signal) => fetchGenomeSummary(signal), []);
  const findingsQuery = useQuery((signal) => fetchFindings(signal), [], { enabled: exposure });

  const overview = overviewQuery.data;
  const kpis = overview?.kpis;
  const charts = overview?.charts;
  const loadingOverview = overviewQuery.isLoading && !overview;
  const overviewFailed = overviewQuery.isError && !overview;

  const findingSummary = useMemo(() => summariseFindings(findingsQuery.data?.findings), [findingsQuery.data]);

  const refreshAll = () => {
    overviewQuery.refetch();
    scopeQuery.refetch();
    postureQuery.refetch();
    genomeQuery.refetch();
    if (exposure) findingsQuery.refetch();
  };

  const refreshing =
    overviewQuery.isRefreshing || postureQuery.isRefreshing || genomeQuery.isRefreshing || scopeQuery.isRefreshing;

  /* No tenant tables yet: one calm message instead of a page of failed cards. */
  if (overviewFailed && overviewQuery.error?.code === 'TENANT_NOT_PROVISIONED') {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Dashboard" />
        <Panel>
          <ErrorState error={overviewQuery.error} onRetry={refreshAll} />
        </Panel>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Dashboard"
        actions={
          <Button variant="secondary" icon={RotateCw} onClick={refreshAll} loading={refreshing}>
            Refresh
          </Button>
        }
        meta={
          <ScopeStrip
            scope={scopeQuery}
            kpis={kpis}
            lastScanAt={overview?.lastScanAt}
            onScopeSaved={() => {
              scopeQuery.refetch();
              overviewQuery.refetch();
            }}
          />
        }
      />

      <PostureBanner query={postureQuery} />

      {/* ── Headline counters ───────────────────────────────────────────── */}
      {loadingOverview ? (
        <>
          <LoadingAnnouncement label="Loading inventory figures" />
          <StatStripSkeleton count={4} />
        </>
      ) : overviewFailed ? (
        <InlineError error={overviewQuery.error} onRetry={overviewQuery.refetch} label="Inventory figures unavailable" />
      ) : (
        <HeadlineTiles kpis={kpis} charts={charts} activity={overview.activityDataAvailable} exposure={exposure} findingsQuery={findingsQuery} findingSummary={findingSummary} />
      )}

      {/* ── What the latest scan changed ───────────────────────────────── */}
      {!loadingOverview && !overviewFailed && <RecentChangesCard recent={overview?.recentChanges} />}

      {/* ── Signals + population ────────────────────────────────────────── */}
      <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        <Panel prominence="lead" className="animate-rise" data-stagger="" style={{ '--stagger': 3 }}>
          <PanelHeader prominence="lead" title="Risk signals" />
          <div className="mt-3">
            {overviewFailed ? (
              <Unavailable source="discovery" />
            ) : (
              <SignalsBody overview={overview} loading={loadingOverview} />
            )}
          </div>
        </Panel>

        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 4 }}>
          <IdentityMix overview={overview} loading={loadingOverview} failed={overviewFailed} />
        </Panel>
      </div>

      {/* ── Credential surface + growth ─────────────────────────────────── */}
      <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)]">
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 5 }}>
          <CredentialSurface overview={overview} loading={loadingOverview} failed={overviewFailed} />
        </Panel>
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 6 }}>
          <InventoryGrowth overview={overview} loading={loadingOverview} failed={overviewFailed} />
        </Panel>
      </div>

      {/* ── Trust, credential use, activity ─────────────────────────────── */}
      <ActivityRow overview={overview} loading={loadingOverview} failed={overviewFailed} />

      {/* ── Behaviour + credential exposure ─────────────────────────────── */}
      <div className={cn('grid gap-4', exposure && '@min-[52rem]:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]')}>
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 7 }}>
          <BehaviourPanel query={genomeQuery} wide={!exposure} />
        </Panel>

        {exposure && (
          <Panel prominence="default" className="animate-rise" data-stagger="" style={{ '--stagger': 8 }}>
            <PanelHeader
              title="Credential exposure"
              actions={
                <Button as={Link} to="/exposure" variant="secondary" size="sm" iconRight={ArrowRight}>
                  Review findings
                </Button>
              }
            />
            <div className="mt-4">
              <FindingsMiniPanel query={findingsQuery} summary={findingSummary} />
            </div>
          </Panel>
        )}
      </div>

      {/* ── Where to look first ─────────────────────────────────────────── */}
      <div className="grid gap-4 @min-[44rem]:grid-cols-2 @min-[60rem]:grid-cols-3">
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 9 }}>
          <WeakestIdentities query={postureQuery} />
        </Panel>
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 10 }}>
          <ExpiringCredentials overview={overview} loading={loadingOverview} failed={overviewFailed} />
        </Panel>
        <Panel prominence="quiet" className="animate-rise @min-[44rem]:col-span-2 @min-[60rem]:col-span-1" data-stagger="" style={{ '--stagger': 11 }}>
          <TopOwners overview={overview} loading={loadingOverview} failed={overviewFailed} />
        </Panel>
      </div>

      {offerExposureSetup && (
        <p className="flex flex-wrap items-center gap-1.5 text-[12px] text-ink-3">
          <FileWarning aria-hidden="true" className="size-3.5 shrink-0" />
          Scanning repositories for committed credentials is off for this workspace.
          <Link to="/integrations" className="font-medium text-brand hover:underline">
            Set it up in Integrations
          </Link>
        </p>
      )}
    </div>
  );
}

/**
 * A secondary panel whose service failed. The failure and its Retry are shown
 * once, at the top of the page (inventory figures, posture score), so the
 * panels below say only that their part is missing.
 */
function Unavailable({ source }) {
  return (
    <p className="flex items-center gap-2 rounded-[var(--radius-control)] border border-dashed border-line px-3 py-3 text-[12.5px] text-ink-3">
      <AlertTriangle aria-hidden="true" className="size-4 shrink-0" />
      {source === 'posture'
        ? 'Not available - the posture service did not answer.'
        : 'Not available - inventory figures could not be loaded.'}
    </p>
  );
}

/* ── Headline tiles ───────────────────────────────────────────────────────── */

function HeadlineTiles({ kpis, charts, activity, exposure, findingsQuery, findingSummary }) {
  const identitySpark = charts.identityGrowth.length > 1 ? charts.identityGrowth.map((point) => point.count) : undefined;
  const credentialSpark = charts.credentialGrowth.length > 1 ? charts.credentialGrowth.map((point) => point.count) : undefined;
  return (
    <div className="grid gap-3 @min-[30rem]:grid-cols-2 @min-[54rem]:grid-cols-4">
      <MetricTile
        as={Link}
        to="/identities"
        data-stagger=""
        style={{ '--stagger': 0 }}
        className="animate-rise"
        label="Identities discovered"
        value={kpis.totalIdentities}
        sparkline={identitySpark}
        icon={Fingerprint}
        tone="brand"
        caption={`Non-human identities across ${formatNumber(kpis.accountsScanned)} account${kpis.accountsScanned === 1 ? '' : 's'}`}
      />
      <MetricTile
        data-stagger=""
        style={{ '--stagger': 1 }}
        className="animate-rise"
        label="Without an owner"
        value={kpis.shadowIdentities}
        icon={Bot}
        tone="high"
        caption={
          activity
            ? 'No owner tag, and no creator recorded in CloudTrail'
            : 'No owner tag (CloudTrail unavailable to name a creator)'
        }
        meter={percentValue(kpis.shadowIdentities, kpis.totalIdentities)}
        meterLabel="Share of all identities"
      />
      <MetricTile
        as={Link}
        to="/credentials"
        data-stagger=""
        style={{ '--stagger': 2 }}
        className="animate-rise"
        label="Credentials"
        value={kpis.totalCredentials}
        sparkline={credentialSpark}
        icon={KeyRound}
        tone="medium"
        caption={
          kpis.activeCredentials === null
            ? 'Roles, keys, secrets and certificates held by identities'
            : `${formatNumber(kpis.activeCredentials)} active, not stale · roles, keys, secrets and certificates`
        }
      />
      {exposure ? (
        <CodeExposureTile query={findingsQuery} summary={findingSummary} />
      ) : (
        <MetricTile
          as={Link}
          to="/identities?is_publicly_accessible=true"
          data-stagger=""
          style={{ '--stagger': 3 }}
          className="animate-rise"
          label="Publicly accessible"
          value={kpis.publiclyAccessible}
          icon={Globe2}
          tone="critical"
          caption="Identities reachable from the internet"
          meter={percentValue(kpis.publiclyAccessible, kpis.totalIdentities)}
          meterLabel="Share of all identities"
        />
      )}
    </div>
  );
}

/** Tile for credential exposure, which has its own availability story. */
function CodeExposureTile({ query, summary }) {
  if (query.isLoading && !query.data) {
    return (
      <div className="rounded-[var(--radius-panel)] border border-line bg-surface p-4" aria-busy="true">
        <Skeleton className="h-3 w-28 rounded" />
        <Skeleton className="mt-3 h-8 w-16 rounded" />
        <Skeleton className="mt-3 h-1.5 w-full rounded-full" />
      </div>
    );
  }

  if (query.isError && !query.data) {
    const detail = describeExposureError(query.error);
    return (
      <div className="flex flex-col justify-between rounded-[var(--radius-panel)] border border-line bg-surface p-4">
        <p className="text-[11px] font-semibold tracking-[0.09em] text-ink-3 uppercase">Secrets in code</p>
        <p className="mt-2 text-[12.5px] leading-snug text-ink-2">{detail.title}</p>
        <Button variant="ghost" size="sm" icon={RotateCw} onClick={query.refetch} className="mt-2 self-start">
          Retry
        </Button>
      </div>
    );
  }

  const highish = (summary.byTier.CRITICAL || 0) + (summary.byTier.HIGH || 0);

  return (
    <MetricTile
      as={Link}
      to="/exposure"
      data-stagger=""
      style={{ '--stagger': 3 }}
      className="animate-rise"
      label="Secrets in code"
      value={summary.total}
      icon={FileWarning}
      tone={highish > 0 ? 'critical' : 'low'}
      caption={`${formatNumber(highish)} high or critical · ${formatNumber(summary.repositoryCount)} repositories`}
      meter={percentValue(highish, summary.total)}
      meterLabel="Share at high or critical risk"
    />
  );
}

/* ── Risk signals ─────────────────────────────────────────────────────────── */

function SignalsBody({ overview, loading }) {
  if (loading || !overview) return <SignalList rows={[]} loading />;
  const { kpis, activityDataAvailable: activity } = overview;
  const rows = [
    {
      key: 'public',
      label: 'Publicly accessible',
      rationale: 'Reachable from the internet - a leaked credential here is usable from anywhere.',
      count: kpis.publiclyAccessible,
      denominator: kpis.totalIdentities,
      denominatorLabel: 'of identities',
      tone: 'critical',
      to: '/identities?is_publicly_accessible=true',
    },
    {
      key: 'owner',
      label: 'No owner',
      rationale: activity
        ? 'No owner tag and no creator in CloudTrail - nobody to ask before changing it.'
        : 'No owner tag. CloudTrail could not be read, so creators were not checked.',
      count: kpis.shadowIdentities,
      denominator: kpis.totalIdentities,
      denominatorLabel: 'of identities',
      tone: 'high',
    },
    {
      key: 'orphaned',
      label: 'Unbound credentials',
      rationale: 'Not bound to any identity - nobody is accountable for rotating it.',
      count: kpis.orphanedCredentials,
      denominator: kpis.totalCredentials,
      denominatorLabel: 'of credentials',
      tone: 'high',
      to: '/credentials?is_orphaned=true',
    },
    {
      key: 'stale-identities',
      label: 'Stale identities',
      rationale: activity
        ? 'No activity in over 90 days, or never used and created over 90 days ago - candidates for removal.'
        : 'Created over 90 days ago. Activity could not be read, so use is not counted.',
      count: kpis.staleIdentities,
      denominator: kpis.totalIdentities,
      denominatorLabel: 'of identities',
      tone: 'medium',
      to: '/identities?min_idle_days=90',
    },
    {
      key: 'stale-credentials',
      label: 'Stale credentials',
      rationale: 'Not used in over 90 days, or never used and over 90 days old - rotate or remove.',
      count: kpis.staleCredentials,
      denominator: kpis.totalCredentials,
      denominatorLabel: 'of credentials',
      tone: 'medium',
      to: '/credentials?min_idle_days=90',
    },
  ];

  const utilisation = kpis.permissionUtilizationPct;
  return (
    <>
      <SignalList rows={rows} />
      <div className="mt-3 border-t border-line pt-3">
        {activity && utilisation !== null ? (
          <div className="flex items-center gap-4">
            <span className="min-w-0 flex-1">
              <span className="block text-[12.5px] font-semibold text-ink">Permissions in use</span>
              <span className="mt-0.5 block text-[11.5px] text-ink-3">
                Share of granted actions that CloudTrail saw credentials use. The rest is access nobody exercises.
              </span>
              <Meter value={Math.min(100, utilisation)} tone="info" height={4} className="mt-2" label="Permissions in use" />
            </span>
            <span data-numeric="" className="w-24 shrink-0 text-right font-display text-[19px] leading-none font-extrabold text-ink">
              {utilisation.toFixed(1)}%
            </span>
          </div>
        ) : (
          <p className="text-[11.5px] leading-relaxed text-ink-3">
            CloudTrail activity could not be read for this scope, so permission use is not measured and stale
            counts fall back to creation dates.
          </p>
        )}
      </div>
    </>
  );
}

/* ── Identity mix ─────────────────────────────────────────────────────────── */

const MIX_VIEWS = [
  { value: 'class', label: 'By class' },
  { value: 'type', label: 'By type' },
  { value: 'environment', label: 'By environment' },
  { value: 'account', label: 'By account' },
  { value: 'region', label: 'By region' },
  { value: 'status', label: 'By status' },
  { value: 'activity', label: 'Active vs stale' },
];

const asIs = (value) => value;

/* Type, environment, account, region and status have no canonical order, so
   hues follow rank and the tail folds into one neutral slice rather than
   inventing an eighth hue. A slice links to the list only where the list API
   has a filter for it (`param`); it has none for account or region. */
function rankedSlices(items, labelOf, param) {
  const sorted = [...items].filter((item) => item.value > 0).sort((a, b) => b.value - a.value);
  const head = sorted.slice(0, 6).map((item, index) => ({
    key: item.key,
    label: labelOf(item.key),
    value: item.value,
    color: SERIES_TOKENS[index],
    to: param ? `/identities?${param}=${encodeURIComponent(item.key)}` : undefined,
  }));
  const rest = sorted.slice(6).reduce((sum, item) => sum + item.value, 0);
  if (rest > 0) head.push({ key: '__other', label: 'Other', value: rest, color: NEUTRAL_SERIES });
  return head;
}

function IdentityMix({ overview, loading, failed }) {
  const navigate = useNavigate();
  const [view, setView] = useState('class');

  const slices = useMemo(() => {
    if (!overview) return [];
    const { charts } = overview;
    if (view === 'type') return rankedSlices(charts.byType, identityTypeLabel, 'identity_type');
    if (view === 'environment') return rankedSlices(charts.byEnvironment, environmentLabel, 'environment');
    if (view === 'account') return rankedSlices(charts.byAccount, asIs, null);
    if (view === 'region') return rankedSlices(charts.byRegion, asIs, null);
    if (view === 'status') return rankedSlices(charts.byStatus, (value) => lifecycleMeta(value).label, 'status');
    if (view === 'activity') {
      /* Stale is the same 90-day measure as the risk signal, so it opens the same list. */
      return [
        { key: 'active', label: 'Active', value: charts.staleVsActive.active, color: SERIES_TOKENS[0] },
        { key: 'stale', label: 'Stale 90+ days', value: charts.staleVsActive.stale, color: SERIES_TOKENS[1], to: '/identities?min_idle_days=90' },
      ].filter((item) => item.value > 0);
    }
    return charts.byClass
      .filter((item) => item.value > 0)
      .map((item) => {
        const meta = functionalClassMeta(item.key);
        return {
          key: item.key,
          label: meta.label,
          value: item.value,
          color: meta.color,
          to: `/identities?functional_class=${encodeURIComponent(item.key)}`,
        };
      });
  }, [overview, view]);

  const typeCount = overview?.charts.byType.filter((item) => item.value > 0).length ?? 0;

  return (
    <>
      <PanelHeader
        prominence="quiet"
        title="Identity mix"
        subtitle={typeCount > 0 ? `${formatNumber(typeCount)} identity type${typeCount === 1 ? '' : 's'}` : undefined}
        actions={
          <Select
            size="sm"
            aria-label="Break down by"
            options={MIX_VIEWS}
            value={view}
            onChange={(event) => setView(event.target.value)}
          />
        }
      />
      <div className="mt-4">
        {loading ? (
          <DonutSkeleton />
        ) : failed ? (
          <Unavailable source="discovery" />
        ) : slices.length === 0 ? (
          <EmptyState
            compact
            title="Nothing to break down yet"
            description="No identities have been discovered in the accounts and regions in your scope."
          />
        ) : (
          <CompositionDonut
            data={slices}
            total={overview.kpis.totalIdentities}
            centerLabel="Identities"
            onSelect={(slice) => slice?.to && navigate(slice.to)}
          />
        )}
      </div>
    </>
  );
}

/* ── Credential surface ───────────────────────────────────────────────────── */

function CredentialSurface({ overview, loading, failed }) {
  const navigate = useNavigate();
  const items = useMemo(
    () =>
      (overview?.charts.credentialTypes ?? [])
        .filter((item) => item.value > 0)
        .map((item) => ({ key: item.key, label: credentialTypeMeta(item.key).label, value: item.value }))
        .sort((a, b) => b.value - a.value),
    [overview],
  );

  return (
    <>
      <PanelHeader
        prominence="quiet"
        title="Credential surface"
        actions={
          <Button as={Link} to="/credentials" variant="secondary" size="sm" iconRight={ArrowRight}>
            Inspect
          </Button>
        }
      />
      <div className="mt-3">
        {loading ? (
          <ChartSkeleton height={220} bars={6} />
        ) : failed ? (
          <Unavailable source="discovery" />
        ) : items.length === 0 ? (
          <ClearState
            compact
            title="No credentials recorded"
            description="No identity in your scope holds a role, key, secret or certificate."
          />
        ) : (
          <>
            <BarList
              items={items}
              onSelect={(item) => navigate(`/credentials?credential_type=${encodeURIComponent(item.key)}`)}
            />
            <dl className="mt-4 grid grid-cols-2 gap-4 border-t border-line pt-3">
              <Figure label="Total credentials" value={formatNumber(overview.kpis.totalCredentials)} />
              <Figure label="Distinct types" value={formatNumber(items.length)} />
            </dl>
          </>
        )}
      </div>
    </>
  );
}

function Figure({ label, value }) {
  return (
    <div>
      <dt className="text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">{label}</dt>
      <dd data-numeric="" className="mt-0.5 font-display text-[19px] leading-none font-bold text-ink">
        {value}
      </dd>
    </div>
  );
}

/* ── Inventory growth ─────────────────────────────────────────────────────── */

const monthLabel = (value) => {
  const date = parseDate(`${value}T00:00:00Z`) ?? parseDate(value);
  return date ? date.toLocaleDateString(undefined, { month: 'short', year: 'numeric', timeZone: 'UTC' }) : String(value);
};

/* The API sends each curve cumulative by first-seen month; the two are put on
   one month axis, carrying a curve's last value forward (and zero before its
   first month) so the shared cursor reads both at the same month. */
function mergeGrowth(identities, credentials) {
  const months = [...new Set([...identities, ...credentials].map((point) => point.date))].sort();
  const read = (series) => {
    const byDate = new Map(series.map((point) => [point.date, point.count]));
    let last = 0;
    return months.map((month) => {
      if (byDate.has(month)) last = byDate.get(month);
      return last;
    });
  };
  const ids = read(identities);
  const creds = read(credentials);
  return months.map((month, index) => ({
    label: monthLabel(month),
    subtitle: 'First seen by this month',
    identities: ids[index],
    credentials: creds[index],
  }));
}

function InventoryGrowth({ overview, loading, failed }) {
  const data = useMemo(
    () => (overview ? mergeGrowth(overview.charts.identityGrowth, overview.charts.credentialGrowth) : []),
    [overview],
  );

  return (
    <>
      <PanelHeader
        prominence="quiet"
        title="Inventory growth"
        subtitle="Cumulative count by the month each item was first seen. Only what exists today is counted - removed items leave the curve."
      />
      <div className="mt-4">
        {loading ? (
          <ChartSkeleton height={220} bars={8} />
        ) : failed ? (
          <Unavailable source="discovery" />
        ) : data.length === 0 ? (
          <EmptyState
            compact
            icon={LineChart}
            title="No first-seen dates yet"
            description="Nothing in your scope has been discovered, so there is no growth to plot."
          />
        ) : data.length < 3 ? (
          /* One or two months is not a trend: a two-point line starting from an
             empty month draws growth that never happened. */
          <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-3.5 py-3 text-[12.5px] leading-relaxed text-ink-2">
            Everything in scope was first seen in{' '}
            <strong className="font-semibold text-ink">{data.map((point) => point.label).join(' and ')}</strong>, so there is
            no trend to draw yet. Today:{' '}
            <strong className="font-semibold text-ink" data-numeric="">
              {formatNumber(data[data.length - 1].identities)}
            </strong>{' '}
            identities and{' '}
            <strong className="font-semibold text-ink" data-numeric="">
              {formatNumber(data[data.length - 1].credentials)}
            </strong>{' '}
            credentials. The chart appears once three months of discoveries are recorded.
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            {[
              { key: 'identities', label: 'Identities', color: 'var(--t-series-1)' },
              { key: 'credentials', label: 'Credentials', color: 'var(--t-series-2)' },
            ].map((series, index, list) => (
              <div key={series.key}>
                <div className="flex items-baseline justify-between gap-3">
                  <p className="text-[12px] font-semibold text-ink-2">{series.label}</p>
                  <p className="text-[11.5px] text-ink-3" data-numeric="">
                    now {formatNumber(data[data.length - 1][series.key])}
                  </p>
                </div>
                <TrendChart
                  data={data}
                  dataKey={series.key}
                  label={series.label}
                  color={series.color}
                  height={index === list.length - 1 ? 124 : 104}
                  showXAxis={index === list.length - 1}
                  syncId="inventory-growth"
                />
              </div>
            ))}
          </div>
        )}
      </div>
    </>
  );
}

/* ── Trust, credential use, activity ──────────────────────────────────────── */

/* Each panel shows only when the API serves its figures; older builds do not. */
function ActivityRow({ overview, loading, failed }) {
  if (loading || failed || !overview) return null;
  const { kpis, charts } = overview;
  const trust = charts.trustEdgesByType !== null || kpis.trustEdges !== null;
  const use = charts.credentialUtilization !== null;
  const events = charts.eventVolume !== null;
  if (!trust && !use && !events) return null;
  return (
    <div className="grid gap-4 @min-[44rem]:grid-cols-2 @min-[60rem]:grid-cols-3">
      {trust && (
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 7 }}>
          <TrustEdges overview={overview} />
        </Panel>
      )}
      {use && (
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 8 }}>
          <CredentialUse overview={overview} />
        </Panel>
      )}
      {events && (
        <Panel
          prominence="quiet"
          className={cn('animate-rise', trust && use && '@min-[44rem]:col-span-2 @min-[60rem]:col-span-1')}
          data-stagger=""
          style={{ '--stagger': 9 }}
        >
          <EventVolume overview={overview} />
        </Panel>
      )}
    </div>
  );
}

/* Canonical order, so an edge type keeps its hue whatever else is present.
   Types outside it fold into the neutral "Other" slice. */
const TRUST_EDGE_ORDER = ['service_trust', 'assume_role', 'cross_account', 'federated_saml', 'federated_oidc', 'pass_role', 'trusts'];

function TrustEdges({ overview }) {
  const { kpis, charts } = overview;
  const slices = useMemo(() => {
    const items = (charts.trustEdgesByType ?? []).filter((item) => item.value > 0);
    const known = TRUST_EDGE_ORDER.flatMap((type, index) => {
      const item = items.find((entry) => entry.key === type);
      return item ? [{ key: type, label: edgeKindMeta(type).label, value: item.value, color: SERIES_TOKENS[index] }] : [];
    });
    const rest = items.filter((item) => !TRUST_EDGE_ORDER.includes(item.key)).reduce((sum, item) => sum + item.value, 0);
    if (rest > 0) known.push({ key: '__other', label: 'Other', value: rest, color: NEUTRAL_SERIES });
    return known;
  }, [charts.trustEdgesByType]);

  return (
    <>
      <PanelHeader
        prominence="quiet"
        title="Trust relationships"
        actions={
          <Button as={Link} to="/access-graph" variant="secondary" size="sm" iconRight={ArrowRight}>
            Access graph
          </Button>
        }
      />
      <div className="mt-4">
        {charts.trustEdgesByType !== null &&
          (slices.length === 0 ? (
            <ClearState compact title="No trust edges" description="No role in your scope trusts another principal or service." />
          ) : (
            <CompositionDonut data={slices} total={kpis.trustEdges ?? undefined} centerLabel="Trust edges" size={164} />
          ))}
        {(kpis.trustEdges !== null || kpis.crossAccountTrustEdges !== null) && (
          <dl className={cn('grid grid-cols-2 gap-4', charts.trustEdgesByType !== null && 'mt-4 border-t border-line pt-3')}>
            {kpis.trustEdges !== null && <Figure label="Trust edges" value={formatNumber(kpis.trustEdges)} />}
            {kpis.crossAccountTrustEdges !== null && (
              <Figure label="Cross-account" value={formatNumber(kpis.crossAccountTrustEdges)} />
            )}
          </dl>
        )}
      </div>
    </>
  );
}

const USE_BUCKETS = {
  used_7d: 'Used in the last 7 days',
  used_30d: 'Last used 8-30 days ago',
  used_90d: 'Last used 31-90 days ago',
  idle_90d: 'Last used over 90 days ago',
  never_used: 'Never used',
};

function CredentialUse({ overview }) {
  const { charts, activityDataAvailable: activity } = overview;
  /* Ordinal buckets keep the API's order (recent to never), not value order. */
  const items = (charts.credentialUtilization ?? []).map((item) => ({
    key: item.key,
    label: USE_BUCKETS[item.key] ?? humanizeToken(item.key),
    value: item.value,
  }));
  const total = items.reduce((sum, item) => sum + item.value, 0);

  return (
    <>
      <PanelHeader prominence="quiet" title="Credential use" subtitle="Each credential by when CloudTrail last saw it used." />
      <div className="mt-3">
        {!activity ? (
          <p className="text-[11.5px] leading-relaxed text-ink-3">
            CloudTrail activity could not be read for this scope, so when credentials were last used is unknown.
          </p>
        ) : total === 0 ? (
          <ClearState compact title="No credentials recorded" description="No identity in your scope holds a credential." />
        ) : (
          <BarList items={items} total={total} />
        )}
      </div>
    </>
  );
}

const dayLabel = (value) => {
  const date = parseDate(`${value}T00:00:00Z`) ?? parseDate(value);
  return date ? date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' }) : String(value);
};

function EventVolume({ overview }) {
  const { charts, activityDataAvailable: activity } = overview;
  const data = useMemo(
    () =>
      (charts.eventVolume ?? []).map((point) => ({
        label: dayLabel(point.date),
        subtitle: 'CloudTrail events that day',
        count: point.count,
      })),
    [charts.eventVolume],
  );
  const total = data.reduce((sum, point) => sum + point.count, 0);

  return (
    <>
      <PanelHeader
        prominence="quiet"
        title="Event volume"
        subtitle={`CloudTrail events per day over the last 30 days, in the accounts in your scope${
          activity && total > 0 ? ` - ${formatNumber(total)} in all` : ''
        }.`}
        actions={
          <Button as={Link} to="/activity" variant="secondary" size="sm" iconRight={ArrowRight}>
            Activity
          </Button>
        }
      />
      <div className="mt-4">
        {!activity || data.length === 0 ? (
          <EmptyState
            compact
            icon={LineChart}
            title="No CloudTrail data"
            description="CloudTrail could not be read for this scope, so daily event volume is not measured."
          />
        ) : total === 0 ? (
          <EmptyState compact icon={LineChart} title="No events in 30 days" description="CloudTrail recorded no events in your scope over the last 30 days." />
        ) : (
          /* Bars, not a line: each day is its own count, and a line would
             invent values between days. */
          <TrendChart data={data} dataKey="count" label="Events" variant="bar" height={180} />
        )}
      </div>
    </>
  );
}

/* ── Behaviour (genome) ───────────────────────────────────────────────────── */

function BehaviourPanel({ query, wide }) {
  const data = query.data;
  const header = (
    <PanelHeader
      prominence="quiet"
      title="Behaviour baselines"
      actions={
        <Button as={Link} to="/genome" variant="secondary" size="sm" iconRight={ArrowRight}>
          Open NHI Genome
        </Button>
      }
    />
  );

  if (query.isLoading && !data) {
    return (
      <>
        {header}
        <div className="mt-4">
          <ListSkeleton rows={4} />
        </div>
      </>
    );
  }
  if (query.isError && !data) {
    return (
      <>
        {header}
        <div className="mt-4">
          <InlineError error={query.error} onRetry={query.refetch} label="Behaviour data unavailable" />
        </div>
      </>
    );
  }

  const { cards, recentlyBaselined } = data;
  const stats = [
    { label: 'Watched', value: cards.watched },
    { label: 'Established', value: cards.established, tone: 'text-low' },
    { label: 'No baseline yet', value: cards.learning },
  ];
  const coverage =
    cards.watched > 0 && cards.established !== null ? Math.round((cards.established / cards.watched) * 100) : null;

  return (
    <>
      {header}
      <dl className="mt-4 grid grid-cols-3 gap-x-4 gap-y-3 rounded-[var(--radius-control)] border border-line bg-inset px-3.5 py-3">
        {stats.map((stat) => (
          <div key={stat.label} className="min-w-0">
            <dt className="truncate text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{stat.label}</dt>
            <dd className="mt-1">
              {stat.value === null ? (
                <span className="text-[12px] text-ink-3">Not measured</span>
              ) : (
                <span data-numeric="" className={cn('font-display text-[19px] leading-none font-bold text-ink', stat.tone)}>
                  {formatNumber(stat.value)}
                </span>
              )}
            </dd>
          </div>
        ))}
      </dl>
      {coverage !== null && (
        <div className="mt-3 flex items-center gap-3">
          <Meter value={coverage} tone="low" height={6} className="min-w-0 flex-1" label="Baseline coverage" />
          <span data-numeric="" className="shrink-0 text-[12px] text-ink-3">
            {coverage}% baselined
          </span>
        </div>
      )}

      <div className={cn('mt-4 min-w-0', wide && '@min-[52rem]:max-w-none')}>
        <p className="text-[12px] font-semibold text-ink-2">Recently baselined</p>
        {recentlyBaselined.length === 0 ? (
          <EmptyState
            compact
            icon={ScanEye}
            title="No baseline established yet"
            description="Each workload identity learns its normal behaviour first. Baselines appear here as learning completes."
          />
        ) : (
          <ul className="mt-1 flex flex-col divide-y divide-line">
            {recentlyBaselined.slice(0, 5).map((row) => (
              <li key={`${row.sourceType}:${row.identityName}:${row.occurredAt}`}>
                <Link to={genomeRoute(row.sourceType, row.identityName)} className="group flex items-center gap-3 py-2.5">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[12.5px] font-medium text-ink group-hover:text-brand" title={row.identityName}>
                      {row.displayName}
                    </span>
                    <span className="block truncate text-[11px] text-ink-3">
                      {sourceTypeLabel(row.sourceType)}
                      {row.region ? ` · ${row.region}` : ''}
                    </span>
                  </span>
                  <span className="shrink-0 text-[11px] text-ink-3">{formatRelative(row.occurredAt)}</span>
                  <Tag tone={row.version ? 'info' : 'low'} size="sm">
                    {row.version ? `Version ${row.version}` : 'Established'}
                  </Tag>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

/* ── Credential exposure ──────────────────────────────────────────────────── */

function FindingsMiniPanel({ query, summary }) {
  if (query.isLoading && !query.data) {
    return <ChartSkeleton height={180} bars={4} />;
  }

  if (query.isError && !query.data) {
    const detail = describeExposureError(query.error);
    return <InlineError error={{ message: detail.message }} onRetry={query.refetch} label={detail.title} />;
  }

  if (summary.total === 0) {
    return (
      <ClearState
        compact
        title="No live exposures"
        description="Nothing is currently flagged across the connected repositories, or everything found has been reviewed and accepted."
      />
    );
  }

  const tiers = ['CRITICAL', 'HIGH', 'MEDIUM', 'LOW']
    .map((tier) => ({ tier, count: summary.byTier[tier] || 0 }))
    .filter((row) => row.count > 0);

  return (
    <div className="flex flex-col gap-6">
      <ul className="flex flex-col divide-y divide-line">
        {tiers.map(({ tier, count }) => {
          const meta = severityMeta(tier);
          return (
            <li key={tier} className="flex items-center gap-3 py-2.5">
              <Tag tone={meta.tone} dot size="sm" className="w-24 justify-center">
                {meta.label}
              </Tag>
              <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-track">
                <span
                  className="block h-full rounded-full transition-[width] duration-[900ms] ease-[var(--ease-out-quint)]"
                  style={{ width: `${percentValue(count, summary.total)}%`, background: `var(--t-${meta.tone})` }}
                />
              </span>
              <span data-numeric="" className="w-10 text-right font-display text-[15px] font-bold text-ink">
                {formatNumber(count)}
              </span>
            </li>
          );
        })}
      </ul>

      <div className="grid grid-cols-2 gap-3 border-t border-line pt-3">
        <div>
          <p className="text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">GitHub</p>
          <p data-numeric="" className="mt-0.5 font-display text-[17px] font-bold text-ink">
            {formatNumber(summary.byPlatform.github)}
          </p>
        </div>
        <div>
          <p className="text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">CodeCommit</p>
          <p data-numeric="" className="mt-0.5 font-display text-[17px] font-bold text-ink">
            {formatNumber(summary.byPlatform.codecommit)}
          </p>
        </div>
      </div>

      {summary.detectors.length > 0 && (
        <div className="border-t border-line pt-3">
          <p className="text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">Most frequent detectors</p>
          <ul className="mt-2 flex flex-col gap-1.5">
            {summary.detectors.slice(0, 4).map((entry) => (
              <li key={entry.key} className="flex items-baseline justify-between gap-3">
                <span className="min-w-0 truncate text-[12.5px] text-ink-2">{humanizeToken(entry.key)}</span>
                <span data-numeric="" className="text-[12.5px] font-semibold text-ink">
                  {formatNumber(entry.value)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

/* ── Lists: weakest posture, expiring credentials, owners ─────────────────── */

function WeakestIdentities({ query }) {
  const data = query.data;
  return (
    <>
      <PanelHeader
        prominence="quiet"
        title="Lowest posture scores"
        actions={
          <Button as={Link} to="/posture" variant="secondary" size="sm" iconRight={ArrowRight}>
            Posture
          </Button>
        }
      />
      <div className="mt-3">
        {query.isLoading && !data ? (
          <ListSkeleton rows={5} />
        ) : query.isError && !data ? (
          <Unavailable source="posture" />
        ) : !data.available || data.weakest.length === 0 ? (
          <EmptyState
            compact
            icon={Gauge}
            title="No posture scores yet"
            description="Identities appear here once the posture service has scored them."
          />
        ) : (
          <ul className="flex flex-col divide-y divide-line">
            {data.weakest.slice(0, 5).map((row) => (
              <li key={row.id}>
                <Link
                  to={row.id ? postureRoute(row.id) : `/identities?search=${encodeURIComponent(row.name)}`}
                  className="group flex items-center gap-3 py-2.5"
                >
                  {row.score === null ? (
                    <span className="grid size-9 place-items-center text-[12px] text-ink-3">-</span>
                  ) : (
                    <ScoreRing score={Math.round(row.score)} size={36} />
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[12.5px] font-medium text-ink group-hover:text-brand" title={row.name}>
                      {row.name}
                    </span>
                    <span className="block truncate text-[11px] text-ink-3">
                      {[row.account, row.functionalClass ? functionalClassMeta(row.functionalClass).label : null]
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  </span>
                  <span className="shrink-0 text-right text-[11.5px] text-ink-3" data-numeric="">
                    {formatNumber(row.checksFailed)} failing
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

function ExpiringCredentials({ overview, loading, failed }) {
  const rows = overview?.charts.expiringSoon ?? [];
  return (
    <>
      <PanelHeader
        prominence="quiet"
        title="Expiring within 30 days"
        actions={
          <Button as={Link} to="/credentials?max_days_until_expiry=30" variant="secondary" size="sm" iconRight={ArrowRight}>
            All
          </Button>
        }
      />
      <div className="mt-3">
        {loading ? (
          <ListSkeleton rows={4} />
        ) : failed ? (
          <Unavailable source="discovery" />
        ) : rows.length === 0 ? (
          <ClearState compact title="Nothing expires soon" description="No credential in your scope expires in the next 30 days." />
        ) : (
          <>
            <ul className="flex flex-col divide-y divide-line">
              {rows.slice(0, 6).map((row) => {
                const tone = row.daysRemaining <= 7 ? 'critical' : row.daysRemaining <= 14 ? 'high' : 'medium';
                return (
                  <li key={`${row.name}-${row.expiresAt}`}>
                    <Link
                      to={`/credentials?search=${encodeURIComponent(row.name)}${row.credentialId ? `&credential=${encodeURIComponent(row.credentialId)}` : ''}`}
                      className="group flex items-center gap-3 py-2.5"
                    >
                      <CalendarClock aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-mono text-[12px] text-ink group-hover:text-brand" title={row.name}>
                          {row.name}
                        </span>
                        <span className="block truncate text-[11px] text-ink-3">Expires {formatDate(row.expiresAt)}</span>
                      </span>
                      <Tag tone={tone} size="sm">
                        {row.daysRemaining === 0 ? 'Today' : `${row.daysRemaining} day${row.daysRemaining === 1 ? '' : 's'}`}
                      </Tag>
                    </Link>
                  </li>
                );
              })}
            </ul>
            {rows.length > 6 && (
              <p className="mt-2 text-[11.5px] text-ink-3">
                {formatNumber(rows.length - 6)} more {rows.length >= 10 ? 'or later ' : ''}in the credential list.
              </p>
            )}
          </>
        )}
      </div>
    </>
  );
}

function TopOwners({ overview, loading, failed }) {
  const navigate = useNavigate();
  const items = (overview?.charts.topOwners ?? []).map((item) => ({ key: item.key, label: item.key, value: item.value }));
  const owned = items.reduce((sum, item) => sum + item.value, 0);
  return (
    <>
      <PanelHeader prominence="quiet" title="Identities by owner" />
      <div className="mt-3">
        {loading ? (
          <ChartSkeleton height={180} bars={5} />
        ) : failed ? (
          <Unavailable source="discovery" />
        ) : items.length === 0 ? (
          <EmptyState compact icon={Users} title="No owner tags" description="No identity in your scope carries an owner tag yet." />
        ) : (
          <>
            <BarList
              items={items}
              total={overview.kpis.totalIdentities}
              onSelect={(item) => navigate(`/identities?owner=${encodeURIComponent(item.key)}`)}
            />
            <p className="mt-2 text-[11.5px] text-ink-3" data-numeric="">
              {formatNumber(Math.max(0, overview.kpis.totalIdentities - owned))} of {formatNumber(overview.kpis.totalIdentities)}{' '}
              identities are not among the owners listed{items.length >= 10 ? ' (top 10 shown)' : ''}.
            </p>
          </>
        )}
      </div>
    </>
  );
}

/* ── Posture banner ───────────────────────────────────────────────────────── */

/**
 * The posture score leads the dashboard: it is the one number that says how
 * exposed the estate is. Its detail lives on Posture; this says where it
 * stands, which way it is moving, and the single fix that would move it most.
 */
function PostureBanner({ query }) {
  const data = query.data;
  if (query.isError && !data) {
    return <InlineError error={query.error} onRetry={query.refetch} label="Posture score unavailable" />;
  }
  if (!data) {
    return (
      <Panel prominence="lead" aria-busy="true">
        <div className="flex items-center gap-4">
          <Skeleton className="size-16 rounded-full" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-48 rounded" />
            <Skeleton className="h-3 w-72 rounded" />
          </div>
        </div>
      </Panel>
    );
  }

  if (!data.available) {
    return (
      <Panel prominence="lead" className="animate-rise">
        <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
          <span className="grid size-[72px] shrink-0 place-items-center rounded-full border-[6px] border-track text-ink-3">
            <Gauge aria-hidden="true" className="size-6" />
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Posture score</p>
            <p className="mt-1 text-[13px] font-semibold text-ink">No score yet</p>
            <p className="mt-0.5 text-[12.5px] text-ink-3">
              The posture service has not scored this workspace's identities. The score appears after its next
              evaluation.
            </p>
          </div>
          <Button as={Link} to="/posture" variant="secondary" size="sm" iconRight={ArrowRight}>
            Open Posture
          </Button>
        </div>
      </Panel>
    );
  }

  const { fleet } = data;
  const score = Math.round(fleet.score);
  const weakest = data.pillars.filter((pillar) => pillar.score !== null).sort((a, b) => a.score - b.score)[0];
  const win = [...data.quickWins].sort((a, b) => b.fleetGain - a.fleetGain)[0];
  const delta = fleet.delta === null ? null : Math.round(fleet.delta * 10) / 10;

  const gain = win ? Math.round(win.fleetGain * 10) / 10 : null;

  return (
    <Panel prominence="lead" className="animate-rise">
      <div className="grid items-center gap-x-8 gap-y-5 @min-[44rem]:grid-cols-2 @min-[60rem]:grid-cols-[auto_minmax(0,1fr)_minmax(0,1.3fr)_auto]">
        <div className="flex items-center gap-4">
          <ScoreRing score={score} size={76} grade={fleet.grade} />
          <div className="min-w-0">
            <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Posture score</p>
            <p className="mt-1.5 flex flex-wrap items-center gap-2 text-[12.5px] text-ink-2">
              <BandTag score={score} />
              {fleet.grade && <span className="font-medium">Grade {fleet.grade}</span>}
            </p>
            <p className="mt-1.5 flex flex-wrap items-center gap-1 text-[12px] text-ink-3" data-numeric="">
              {delta === null ? (
                <span>First score - no change to compare yet</span>
              ) : (
                <>
                  <Delta value={delta} />
                  <span>over {windowPhrase(fleet.changeWindow)}</span>
                </>
              )}
            </p>
            <p className="mt-0.5 text-[11.5px] text-ink-3" data-numeric="">
              {formatNumber(fleet.scored)} {fleet.scored === 1 ? 'identity' : 'identities'} scored
              {data.computedAt ? ` · ${formatRelative(data.computedAt)}` : ''}
            </p>
          </div>
        </div>

        <PostureFact label="Weakest area" className="@min-[60rem]:border-l @min-[60rem]:border-line @min-[60rem]:pl-8">
          {weakest ? (
            <>
              <span className="block truncate text-[14px] font-semibold text-ink">{weakest.name}</span>
              <span className="mt-0.5 block text-[12px] text-ink-3" data-numeric="">
                Averages {Math.round(weakest.score)} of 100
              </span>
            </>
          ) : (
            <span className="text-[12.5px] text-ink-3">No area scores yet</span>
          )}
        </PostureFact>

        <PostureFact label="Biggest quick win" className="@min-[60rem]:border-l @min-[60rem]:border-line @min-[60rem]:pl-8">
          {win ? (
            <>
              <span className="line-clamp-2 text-[13.5px] leading-snug font-semibold text-ink" title={win.title}>
                {win.title}
              </span>
              <span className="mt-0.5 block text-[12px] text-ink-3" data-numeric="">
                <span className="font-semibold text-low">+{gain}</span> to the score · {formatNumber(win.identities)}{' '}
                {win.identities === 1 ? 'identity' : 'identities'}
              </span>
            </>
          ) : (
            <span className="text-[12.5px] text-ink-3">Nothing left to fix</span>
          )}
        </PostureFact>

        <div className="@min-[44rem]:justify-self-start @min-[60rem]:justify-self-end">
          <Button as={Link} to="/posture" variant="secondary" size="sm" iconRight={ArrowRight}>
            Open Posture
          </Button>
        </div>
      </div>
    </Panel>
  );
}

function PostureFact({ label, className, children }) {
  return (
    <div className={cn('min-w-0', className)}>
      <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{label}</p>
      <div className="mt-1.5">{children}</div>
    </div>
  );
}
