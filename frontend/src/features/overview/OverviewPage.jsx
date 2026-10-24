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
  fetchScanTrends,
  fetchGenomeSummary,
  fetchPostureSummary,
  fetchScopeSettings,
} from '../../lib/api/overview';
import { useQuery } from '../../lib/hooks';
import { NEUTRAL_SERIES, SERIES_TOKENS } from '../../lib/domain';
import { formatDate, formatDateTime, formatNumber, formatRelative, humanizeToken, parseDate, percentValue } from '../../lib/format';
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
import { SegmentedControl } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { cn } from '../../ui/cn';
import { ChartSkeleton, DonutSkeleton, ListSkeleton, LoadingAnnouncement, Skeleton, StatStripSkeleton } from '../../ui/Skeleton';
import { ClearState, EmptyState, ErrorState, InlineError } from '../../ui/States';
import { postureRoute, windowPhrase } from '../../lib/api/posture';
import { RecentChangesCard } from '../changes/RecentChangesCard';
import { BandTag, Delta, ScoreRing } from '../posture/parts';
import { credentialTypeMeta, environmentLabel, functionalClassMeta, identityTypeLabel, lifecycleMeta } from '../identities/meta';
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
  const trendsQuery = useQuery((signal) => fetchScanTrends(signal), []);

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
    trendsQuery.refetch();
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
    <div className="flex flex-col gap-5">
      <PageHeader
        title="Dashboard"
        lede="What is at risk across your AWS accounts, what changed in the last sync, and where to look first."
        actions={
          <Button variant="secondary" icon={RotateCw} onClick={refreshAll} loading={refreshing}>
            Refresh figures
          </Button>
        }
        meta={
          <ScopeStrip
            scope={scopeQuery}
            lastScanAt={overview?.lastScanAt}
            onScopeSaved={() => {
              scopeQuery.refetch();
              overviewQuery.refetch();
            }}
          />
        }
      />

      {/* ── The answer first: one score, then the four numbers behind it ── */}
      <PostureBanner query={postureQuery} />

      {loadingOverview ? (
        <>
          <LoadingAnnouncement label="Loading inventory figures" />
          <StatStripSkeleton count={4} />
        </>
      ) : overviewFailed ? (
        <InlineError error={overviewQuery.error} onRetry={overviewQuery.refetch} label="Inventory figures could not be loaded" />
      ) : (
        <HeadlineTiles kpis={kpis} charts={charts} activity={overview.activityDataAvailable} exposure={exposure} findingsQuery={findingsQuery} findingSummary={findingSummary} />
      )}

      {/* ── Why: what needs attention, next to what the estate is made of ── */}
      <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
        <Panel prominence="lead" className="animate-rise" data-stagger="" style={{ '--stagger': 3 }}>
          <PanelHeader
            prominence="lead"
            title="What needs attention"
            subtitle="Each line is a share of all your identities or credentials, largest first. Select one to open the list."
          />
          <div className="mt-3">
            {overviewFailed ? <Unavailable source="discovery" /> : <SignalsBody overview={overview} loading={loadingOverview} />}
          </div>
        </Panel>

        <Panel prominence="default" className="animate-rise" data-stagger="" style={{ '--stagger': 4 }}>
          <EstateMix overview={overview} loading={loadingOverview} failed={overviewFailed} />
        </Panel>
      </div>

      {/* ── What the last sync changed ─────────────────────────────────────── */}
      {!loadingOverview && !overviewFailed && overview?.recentChanges && <RecentChangesCard recent={overview.recentChanges} />}

      {/* ── Over time, and how the workloads behave ────────────────────────── */}
      <div className="grid gap-4 @min-[60rem]:grid-cols-[minmax(0,1.45fr)_minmax(0,1fr)]">
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 5 }}>
          <OverTime overview={overview} trends={trendsQuery.data} loading={loadingOverview} failed={overviewFailed} />
        </Panel>
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 6 }}>
          <BehaviourPanel query={genomeQuery} />
        </Panel>
      </div>

      {/* ── Where to start ─────────────────────────────────────────────────── */}
      <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 7 }}>
        <StartHere postureQuery={postureQuery} overview={overview} loading={loadingOverview} failed={overviewFailed} />
      </Panel>

      {offerExposureSetup && (
        <p className="flex flex-wrap items-center gap-1.5 text-[12px] text-ink-3">
          <FileWarning aria-hidden="true" className="size-3.5 shrink-0" />
          Looking for secrets committed to your code repositories is off for this workspace.
          <Link to="/integrations" className="font-medium text-brand hover:underline">
            Turn it on in Integrations
          </Link>
        </p>
      )}
    </div>
  );
}


/* Each of these panels shows only when the API serves its figures; older builds do not. */
const showsUse = (overview) => Boolean(overview) && overview.charts.credentialUtilization !== null;
const showsEvents = (overview) => Boolean(overview) && overview.charts.eventVolume !== null;

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
        ? 'Not available: security scores could not be loaded. Use Retry at the top of the page.'
        : 'Not available: inventory figures could not be loaded. Use Retry at the top of the page.'}
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
        caption={`Workloads, services and automations in ${formatNumber(kpis.accountsScanned)} AWS account${kpis.accountsScanned === 1 ? '' : 's'}`}
      />
      <MetricTile
        data-stagger=""
        style={{ '--stagger': 1 }}
        className="animate-rise"
        label="Created outside your normal process"
        value={kpis.shadowIdentities}
        icon={Bot}
        tone="high"
        caption={
          activity
            ? 'Nobody is recorded as its owner or creator'
            : 'No owner tag. The AWS activity log could not be read to name a creator'
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
            ? 'What identities sign in with: roles, access keys, secrets and certificates'
            : `${formatNumber(kpis.activeCredentials)} used or created in the last 90 days · roles, access keys, secrets and certificates`
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
          label="Reachable from the internet"
          value={kpis.publiclyAccessible}
          icon={Globe2}
          tone="critical"
          caption="Identities anyone on the internet could reach"
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
        <p className="text-[11px] font-semibold tracking-[0.09em] text-ink-3 uppercase">Exposed secrets</p>
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
      label="Exposed secrets"
      value={summary.total}
      icon={FileWarning}
      tone={highish > 0 ? 'critical' : 'low'}
      caption={`Secrets found in code · ${formatNumber(highish)} high or critical · in ${formatNumber(summary.repositoryCount)} repositor${summary.repositoryCount === 1 ? 'y' : 'ies'}`}
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
      label: 'Reachable from the internet',
      rationale: 'Anyone on the internet can reach it, so a leaked credential here can be used from anywhere.',
      count: kpis.publiclyAccessible,
      denominator: kpis.totalIdentities,
      denominatorLabel: 'of identities',
      tone: 'critical',
      to: '/identities?is_publicly_accessible=true',
    },
    {
      key: 'owner',
      label: 'Created outside your normal process',
      rationale: activity
        ? 'Nobody is recorded as its owner or creator, so there is nobody to ask before changing it.'
        : 'No owner tag. The AWS activity log (CloudTrail) could not be read, so creators were not checked.',
      count: kpis.shadowIdentities,
      denominator: kpis.totalIdentities,
      denominatorLabel: 'of identities',
      tone: 'high',
    },
    {
      key: 'orphaned',
      label: 'Credentials not used by any identity',
      rationale: 'No identity is set up to use them, so nobody is responsible for changing (rotating) or removing them.',
      count: kpis.orphanedCredentials,
      denominator: kpis.totalCredentials,
      denominatorLabel: 'of credentials',
      tone: 'high',
      to: '/credentials?is_orphaned=true',
    },
    {
      key: 'stale-identities',
      label: 'Identities unused for 90+ days',
      rationale: activity
        ? 'No recorded activity in the last 90 days, or never used and created over 90 days ago. Candidates for removal.'
        : 'Created over 90 days ago. The AWS activity log could not be read, so use is not counted.',
      count: kpis.staleIdentities,
      denominator: kpis.totalIdentities,
      denominatorLabel: 'of identities',
      tone: 'medium',
      to: '/identities?min_idle_days=90',
    },
    {
      key: 'stale-credentials',
      label: 'Credentials unused for 90+ days',
      rationale: 'No recorded use in the last 90 days, or never used and over 90 days old. Change (rotate) or remove them.',
      count: kpis.staleCredentials,
      denominator: kpis.totalCredentials,
      denominatorLabel: 'of credentials',
      tone: 'medium',
      to: '/credentials?min_idle_days=90',
    },
  ];

  return <SignalList rows={rows} />;
}

/** Of all the actions identities are allowed to take, the share the activity log saw them use. */
function PermissionShare({ overview }) {
  if (!overview) return null;
  const { kpis, activityDataAvailable: activity } = overview;
  const utilisation = kpis.permissionUtilizationPct;
  return (
    <div className="mt-4 border-t border-line pt-3">
      {activity && utilisation !== null ? (
        <div className="flex items-center gap-4">
          <span className="min-w-0 flex-1">
            <span className="block text-[12.5px] font-semibold text-ink">Share of permissions actually used</span>
            <span className="mt-0.5 block text-[11.5px] text-ink-3">
              Of all the actions your identities are allowed to take, the share the AWS activity log (CloudTrail) saw them use.
              The rest is access nobody uses, which could be removed.
            </span>
            <Meter value={Math.min(100, utilisation)} tone="info" height={4} className="mt-2" label="Share of permissions actually used" />
          </span>
          <span data-numeric="" className="w-20 shrink-0 text-right font-display text-[19px] leading-none font-extrabold text-ink">
            {utilisation.toFixed(1)}%
          </span>
        </div>
      ) : (
        <p className="text-[11.5px] leading-relaxed text-ink-3">
          The AWS activity log (CloudTrail) could not be read for these accounts, so the share of permissions used is not
          measured and the 90-day counts go by creation date only.
        </p>
      )}
    </div>
  );
}

/* ── Identity mix ─────────────────────────────────────────────────────────── */

const MIX_VIEWS = [
  { value: 'class', label: 'By purpose' },
  { value: 'type', label: 'By AWS type' },
  { value: 'environment', label: 'By environment' },
  { value: 'account', label: 'By AWS account' },
  { value: 'region', label: 'By region' },
  { value: 'status', label: 'By lifecycle status' },
  { value: 'activity', label: 'Active vs unused 90+ days' },
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

const WHAT_OPTIONS = [
  { value: 'identities', label: 'Identities' },
  { value: 'credentials', label: 'Credentials' },
];

/** What the estate is made of: identities by purpose, type, account...; or credentials by kind. */
function EstateMix({ overview, loading, failed }) {
  const navigate = useNavigate();
  const [what, setWhat] = useState('identities');
  const [view, setView] = useState('class');

  const slices = useMemo(() => {
    if (!overview) return [];
    const { charts } = overview;
    if (what === 'credentials') {
      const kinds = charts.credentialTypes.map((item) => ({ key: item.key, value: item.value }));
      return rankedSlices(kinds, (key) => credentialTypeMeta(key).label, 'credential_type').map((slice) => ({
        ...slice,
        to: slice.to ? slice.to.replace('/identities?', '/credentials?') : undefined,
      }));
    }
    if (view === 'type') return rankedSlices(charts.byType, identityTypeLabel, 'identity_type');
    if (view === 'environment') return rankedSlices(charts.byEnvironment, environmentLabel, 'environment');
    if (view === 'account') return rankedSlices(charts.byAccount, asIs, null);
    if (view === 'region') return rankedSlices(charts.byRegion, asIs, null);
    if (view === 'status') return rankedSlices(charts.byStatus, (value) => lifecycleMeta(value).label, 'status');
    if (view === 'activity') {
      /* Stale is the same 90-day measure as the risk signal, so it opens the same list. */
      return [
        { key: 'active', label: 'Active', value: charts.staleVsActive.active, color: SERIES_TOKENS[0] },
        { key: 'stale', label: 'Unused for 90+ days', value: charts.staleVsActive.stale, color: SERIES_TOKENS[1], to: '/identities?min_idle_days=90' },
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
  }, [overview, view, what]);

  const credentials = what === 'credentials';
  const typeCount = overview?.charts.byType.filter((item) => item.value > 0).length ?? 0;

  return (
    <>
      <PanelHeader
        title={credentials ? 'Credential mix' : 'Identity mix'}
        subtitle={
          credentials
            ? 'What your identities sign in with. Select a slice to list those credentials.'
            : typeCount > 0
              ? `${formatNumber(typeCount)} AWS type${typeCount === 1 ? '' : 's'} in all. Select a slice to list those identities.`
              : undefined
        }
        actions={<SegmentedControl label="Show" options={WHAT_OPTIONS} value={what} onChange={setWhat} />}
      />
      {!credentials && (
        <div className="mt-3">
          <Select size="sm" aria-label="Break down by" options={MIX_VIEWS} value={view} onChange={(event) => setView(event.target.value)} />
        </div>
      )}
      <div className="mt-4">
        {loading ? (
          <DonutSkeleton />
        ) : failed ? (
          <Unavailable source="discovery" />
        ) : slices.length === 0 ? (
          <EmptyState
            compact
            title={credentials ? 'No credentials yet' : 'No identities yet'}
            description="Discovery has not found any in the accounts and regions these figures cover."
          />
        ) : (
          <CompositionDonut
            data={slices}
            total={credentials ? overview.kpis.totalCredentials : overview.kpis.totalIdentities}
            centerLabel={credentials ? 'Credentials' : 'Identities'}
            onSelect={(slice) => slice?.to && navigate(slice.to)}
          />
        )}
      </div>
      {!loading && !failed && <PermissionShare overview={overview} />}
    </>
  );
}

/* ── Credential surface ───────────────────────────────────────────────────── */



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

/* Each scan hour as a point: what the scans found, not when things were first seen. */
function scanPoints(trends) {
  return trends.map((point) => ({
    label: formatDate(point.date),
    subtitle: `Scan finished ${formatDateTime(point.date)}`,
    identities: point.identities,
    credentials: point.credentials,
  }));
}

function InventoryGrowth({ overview, trends, loading, failed, embedded }) {
  /* Real scan totals once there are enough scans to draw a line; the
     discovery-date history otherwise. */
  const fromScans = (trends?.length ?? 0) >= 3;
  const data = useMemo(() => {
    if (fromScans) return scanPoints(trends);
    return overview ? mergeGrowth(overview.charts.identityGrowth, overview.charts.credentialGrowth) : [];
  }, [overview, trends, fromScans]);

  return (
    <>
      <Head embedded={embedded} title="Inventory over time"
        subtitle={
          fromScans
            ? 'How many identities and credentials each discovery scan found, across the accounts you selected.'
            : "How many of today's identities and credentials had been discovered by each month. Removed items are not counted."
        }
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
            title="Nothing to plot yet"
            description="Nothing has been discovered in these accounts and regions, so there is no history to draw."
          />
        ) : data.length < 3 ? (
          /* One or two months is not a trend: a two-point line starting from an
             empty month draws growth that never happened. */
          <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-3.5 py-3 text-[12.5px] leading-relaxed text-ink-2">
            Everything here was first discovered in{' '}
            <strong className="font-semibold text-ink">{data.map((point) => point.label).join(' and ')}</strong>, so there is
            no trend to draw yet. Today:{' '}
            <strong className="font-semibold text-ink" data-numeric="">
              {formatNumber(data[data.length - 1].identities)}
            </strong>{' '}
            identities and{' '}
            <strong className="font-semibold text-ink" data-numeric="">
              {formatNumber(data[data.length - 1].credentials)}
            </strong>{' '}
            credentials. The chart appears once discoveries span three months.
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
                    {formatNumber(data[data.length - 1][series.key])} today

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

/** A panel's title block; inside a tabbed panel the tab is the title, so only the note and action remain. */
function Head({ embedded, title, subtitle, actions }) {
  if (!embedded) return <PanelHeader prominence="quiet" title={title} subtitle={subtitle} actions={actions} />;
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
      {subtitle ? <p className="min-w-0 flex-1 text-[12px] leading-relaxed text-ink-3">{subtitle}</p> : <span />}
      {actions}
    </div>
  );
}

const OVER_TIME_TABS = [
  { value: 'inventory', label: 'Inventory' },
  { value: 'activity', label: 'AWS activity' },
  { value: 'use', label: 'Credential use' },
];

/** Growth, daily activity and credential use share one panel: one question ("how is it moving?"), three views. */
function OverTime({ overview, trends, loading, failed }) {
  const available = OVER_TIME_TABS.filter((tab) => tab.value === 'inventory' || !overview || (tab.value === 'activity' ? showsEvents(overview) : showsUse(overview)));
  const [tab, setTab] = useState('inventory');
  const current = available.some((item) => item.value === tab) ? tab : 'inventory';
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-[14.5px] font-semibold text-ink">Over time</h2>
        {available.length > 1 && <SegmentedControl label="Over time view" options={available} value={current} onChange={setTab} />}
      </div>
      <div className="mt-3">
        {current === 'inventory' && <InventoryGrowth embedded overview={overview} trends={trends} loading={loading} failed={failed} />}
        {current === 'activity' && overview && <EventVolume embedded overview={overview} />}
        {current === 'use' && overview && <CredentialUse embedded overview={overview} />}
      </div>
    </>
  );
}

const START_TABS = [
  { value: 'weakest', label: 'Lowest security scores' },
  { value: 'expiring', label: 'Expiring soon' },
  { value: 'owners', label: 'Who owns them' },
];

/** The three short lists that answer "where do I begin?", one at a time. */
function StartHere({ postureQuery, overview, loading, failed }) {
  const [tab, setTab] = useState('weakest');
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-[14.5px] font-semibold text-ink">Where to start</h2>
        <SegmentedControl label="Where to start view" options={START_TABS} value={tab} onChange={setTab} />
      </div>
      <div className="mt-3">
        {tab === 'weakest' && <WeakestIdentities embedded query={postureQuery} />}
        {tab === 'expiring' && <ExpiringCredentials embedded overview={overview} loading={loading} failed={failed} />}
        {tab === 'owners' && <TopOwners embedded overview={overview} loading={loading} failed={failed} />}
      </div>
    </>
  );
}

/* ── Credential use, activity ─────────────────────────────────────────────── */

/* Canonical order, so an edge type keeps its hue whatever else is present.
   Types outside it fold into the neutral "Other" slice. */

/* The console's plain words for each kind of trust; anything else keeps the access graph's label. */


const USE_BUCKETS = {
  used_7d: 'Used in the last 7 days',
  used_30d: 'Last used 8-30 days ago',
  used_90d: 'Last used 31-90 days ago',
  idle_90d: 'Last used over 90 days ago',
  never_used: 'Never used',
};

function CredentialUse({ overview, embedded }) {
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
      <Head embedded={embedded} title="When credentials were last used"
        subtitle="Each credential by when it was last used: the later of the AWS activity log (CloudTrail) and AWS's own last-used date."
      />
      <div className="mt-3">
        {!activity ? (
          <p className="text-[11.5px] leading-relaxed text-ink-3">
            The AWS activity log (CloudTrail) could not be read for these accounts, so when credentials were last used is not
            known.
          </p>
        ) : total === 0 ? (
          <ClearState compact title="No credentials recorded" description="No identity in these accounts and regions uses a credential." />
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

function EventVolume({ overview, embedded }) {
  const { charts, activityDataAvailable: activity } = overview;
  const data = useMemo(
    () =>
      (charts.eventVolume ?? []).map((point) => ({
        label: dayLabel(point.date),
        subtitle: 'Events in the AWS activity log that day',
        count: point.count,
      })),
    [charts.eventVolume],
  );
  const total = data.reduce((sum, point) => sum + point.count, 0);

  return (
    <>
      <Head embedded={embedded} title="AWS activity per day"
        subtitle={`Events recorded in the AWS activity log (CloudTrail) each day for the last 30 days${
          activity && total > 0 ? `, ${formatNumber(total)} in all` : ''
        }.`}
        actions={
          <Button as={Link} to="/activity" variant="secondary" size="sm" iconRight={ArrowRight}>
            Open activity
          </Button>
        }
      />
      <div className="mt-4">
        {!activity || data.length === 0 ? (
          <EmptyState
            compact
            icon={LineChart}
            title="No activity log data"
            description="The AWS activity log (CloudTrail) could not be read for these accounts, so daily activity is not measured."
          />
        ) : total === 0 ? (
          <EmptyState compact icon={LineChart} title="Nothing to show for this period" description="The AWS activity log recorded no events in these accounts over the last 30 days." />
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

function BehaviourPanel({ query }) {
  const data = query.data;
  const header = (
    <PanelHeader
      prominence="quiet"
      title="Normal behaviour learned"
      subtitle="Each workload first learns what it usually does, so unusual activity can be spotted."
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
          <InlineError error={query.error} onRetry={query.refetch} label="Normal-behaviour figures could not be loaded" />
        </div>
      </>
    );
  }

  const { cards, recentlyBaselined } = data;
  const stats = [
    { label: 'Watched', value: cards.watched },
    { label: 'Learned', value: cards.established, tone: 'text-low' },
    { label: 'Still learning', value: cards.learning },
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
          <Meter value={coverage} tone="low" height={6} className="min-w-0 flex-1" label="Share of watched workloads with normal behaviour learned" />
          <span data-numeric="" className="shrink-0 text-[12px] text-ink-3">
            {coverage}% of watched learned
          </span>
        </div>
      )}

      <div className="mt-4 min-w-0">
        <p className="text-[12px] font-semibold text-ink-2">Recently learned</p>
        {recentlyBaselined.length === 0 ? (
          <EmptyState
            compact
            icon={ScanEye}
            title="Nothing learned yet"
            description="Each workload learns its normal behaviour first. Workloads appear here as their learning completes."
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
                  <span className="shrink-0 text-[11px] text-ink-3" title={formatDateTime(row.occurredAt)}>
                    {formatRelative(row.occurredAt)}
                  </span>
                  <Tag tone={row.version ? 'info' : 'low'} size="sm">
                    {row.version ? `Updated, version ${row.version}` : 'Learned'}
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


/* ── Lists: weakest posture, expiring credentials, owners ─────────────────── */

function WeakestIdentities({ query, embedded }) {
  const data = query.data;
  return (
    <>
      <Head embedded={embedded} title="Lowest security scores"
        subtitle="Start fixing here."
        actions={
          <Button as={Link} to="/posture" variant="secondary" size="sm" iconRight={ArrowRight}>
            All scores
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
            title="No security scores yet"
            description="Identities appear here once they have been scored, after a discovery scan."
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
                    <span className="grid size-9 place-items-center text-center text-[10px] leading-tight text-ink-3">Not scored</span>
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
                    {formatNumber(row.checksFailed)} failed {row.checksFailed === 1 ? 'check' : 'checks'}
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

function ExpiringCredentials({ overview, loading, failed, embedded }) {
  const rows = overview?.charts.expiringSoon ?? [];
  return (
    <>
      <Head embedded={embedded} title="Expiring in the next 30 days"
        subtitle="Credentials to replace before they stop working."
        actions={
          <Button as={Link} to="/credentials?max_days_until_expiry=30" variant="secondary" size="sm" iconRight={ArrowRight}>
            All expiring
          </Button>
        }
      />
      <div className="mt-3">
        {loading ? (
          <ListSkeleton rows={4} />
        ) : failed ? (
          <Unavailable source="discovery" />
        ) : rows.length === 0 ? (
          <ClearState compact title="Nothing expires soon" description="No credential in these accounts and regions expires in the next 30 days." />
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
                {formatNumber(rows.length - 6)} more {rows.length >= 10 ? 'or later ' : ''}in All expiring.
              </p>
            )}
          </>
        )}
      </div>
    </>
  );
}

function TopOwners({ overview, loading, failed, embedded }) {
  const navigate = useNavigate();
  const items = (overview?.charts.topOwners ?? []).map((item) => ({ key: item.key, label: item.key, value: item.value }));
  const owned = items.reduce((sum, item) => sum + item.value, 0);
  return (
    <>
      <Head embedded={embedded} title="Who owns them" subtitle="Identities by their owner tag. Select an owner to list them." />
      <div className="mt-3">
        {loading ? (
          <ChartSkeleton height={180} bars={5} />
        ) : failed ? (
          <Unavailable source="discovery" />
        ) : items.length === 0 ? (
          <EmptyState compact icon={Users} title="No owner recorded" description="No identity in these accounts and regions has an owner tag yet." />
        ) : (
          <>
            <BarList
              items={items}
              total={overview.kpis.totalIdentities}
              onSelect={(item) => navigate(`/identities?owner=${encodeURIComponent(item.key)}`)}
            />
            <p className="mt-2 text-[11.5px] text-ink-3" data-numeric="">
              {formatNumber(Math.max(0, overview.kpis.totalIdentities - owned))} of {formatNumber(overview.kpis.totalIdentities)}{' '}
              identities have no owner listed here{items.length >= 10 ? ' (top 10 owners shown)' : ''}.
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
    return <InlineError error={query.error} onRetry={query.refetch} label="Security score could not be loaded" />;
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
            <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Security score (0-100)</p>
            <p className="mt-1 text-[13px] font-semibold text-ink">No score yet</p>
            <p className="mt-0.5 text-[12.5px] text-ink-3">
              This workspace's identities have not been scored yet. The score appears after the next discovery scan is
              scored.
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
            <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Security score (0-100)</p>
            <p className="mt-1.5 flex flex-wrap items-center gap-2 text-[12.5px] text-ink-2">
              <BandTag score={score} />
              {fleet.grade && <span className="font-medium">Grade {fleet.grade}</span>}
            </p>
            <p className="mt-1.5 flex flex-wrap items-center gap-1 text-[12px] text-ink-3" data-numeric="">
              {delta === null ? (
                <span>First score, nothing to compare with yet</span>
              ) : (
                <>
                  <Delta value={delta} />
                  <span>over {windowPhrase(fleet.changeWindow)}</span>
                </>
              )}
            </p>
            <p className="mt-0.5 text-[11.5px] text-ink-3" data-numeric="">
              Average of {formatNumber(fleet.scored)} {fleet.scored === 1 ? 'identity' : 'identities'}
              {data.computedAt ? (
                <span title={formatDateTime(data.computedAt)}> · scored {formatRelative(data.computedAt)}</span>
              ) : (
                ''
              )}
            </p>
          </div>
        </div>

        <PostureFact label="Weakest security area" className="@min-[60rem]:border-l @min-[60rem]:border-line @min-[60rem]:pl-8">
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

        <PostureFact label="Fix that helps most" className="@min-[60rem]:border-l @min-[60rem]:border-line @min-[60rem]:pl-8">
          {win ? (
            <>
              <span className="line-clamp-2 text-[13.5px] leading-snug font-semibold text-ink" title={win.title}>
                {win.title}
              </span>
              <span className="mt-0.5 block text-[12px] text-ink-3" data-numeric="">
                <span className="font-semibold text-low">+{gain}</span> to the overall score · fixes {formatNumber(win.identities)}{' '}
                {win.identities === 1 ? 'identity' : 'identities'}
              </span>
            </>
          ) : (
            <span className="text-[12.5px] text-ink-3">Nothing left to fix</span>
          )}
        </PostureFact>

        <div className="@min-[44rem]:justify-self-start @min-[60rem]:justify-self-end">
          <Button as={Link} to="/posture" variant="secondary" size="sm" iconRight={ArrowRight}>
            See what to fix
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
