import { useCallback, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ArrowLeft, Boxes, Dna, Fingerprint as FingerprintIcon, History, Network, SearchX } from 'lucide-react';
import {
  fetchGenomeActivity,
  fetchGenomeIdentity,
  fetchGenomeProfile,
  fetchGenomeTimeline,
  resolveDiscoveryIdentity,
  WINDOW_MS,
} from '../../lib/api/genome';
import { accessGraphRoute, identityRoute, parseGenomeId } from '../../lib/identityLinks';
import { useQuery } from '../../lib/hooks';
import { severityMeta } from '../../lib/domain';
import { formatDate, formatDateTime, formatNumber, formatRelative, titleCaseEnum } from '../../lib/format';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { CopyableValue } from '../../ui/Copyable';
import { DataGrid } from '../../ui/DataGrid';
import { Panel, PanelHeader, SectionLabel } from '../../ui/Panel';
import { ChartSkeleton, ListSkeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState, InlineError } from '../../ui/States';
import { SegmentedControl, Tabs } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { TrendChart } from '../../charts/TrendChart';
import { ApiShareList, Fingerprint, formatShare, ScheduleGrid } from './visuals';
import {
  anomalyStatusMeta,
  CursorPager,
  dimensionLabel,
  dimensionValueLabel,
  Measured,
  parseAnomaly,
  sourceTypeLabel,
  useCursorPager,
} from './meta';

const WINDOWS = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '14d', label: '14d' },
  { value: '30d', label: '30d' },
];
const WINDOW_WORDS = { '24h': '24 hours', '7d': '7 days', '14d': '14 days', '30d': '30 days' };
const DEFAULT_WINDOW = '14d';

const TAB_KEYS = ['genome', 'activity', 'timeline'];
const TIMELINE_PAGE = 50;
const RECENT_PAGE = 25;

/** The `:id` segment is `SOURCE_TYPE:ARN`; tolerate a still-encoded value. */
function readId(raw) {
  let value = String(raw ?? '');
  if (/%[0-9A-Fa-f]{2}/.test(value)) {
    try {
      value = decodeURIComponent(value);
    } catch {
      /* keep the raw value */
    }
  }
  return parseGenomeId(value);
}

/**
 * The identity's history in pages, newest first. The first page is a query;
 * older pages are appended on request. The baseline state in the header, the
 * open-departure banner and the Timeline tab all read the same list.
 */
function useTimeline(sourceType, identityName) {
  const key = `${sourceType}:${identityName}`;
  const first = useQuery(
    (signal) => fetchGenomeTimeline(sourceType, identityName, { limit: TIMELINE_PAGE }, signal),
    [key],
  );
  const [more, setMore] = useState({ key, pages: [], loading: false, error: null });
  const own = more.key === key ? more : { key, pages: [], loading: false, error: null };
  const items = useMemo(
    () => [...(first.data?.items ?? []), ...own.pages.flatMap((page) => page.items)],
    [first.data, own.pages],
  );
  const nextCursor = own.pages.length ? own.pages[own.pages.length - 1].nextCursor : first.data?.nextCursor ?? null;

  const loadMore = useCallback(async () => {
    if (!nextCursor) return;
    setMore((value) => ({ ...(value.key === key ? value : { key, pages: [] }), loading: true, error: null }));
    try {
      const page = await fetchGenomeTimeline(sourceType, identityName, { limit: TIMELINE_PAGE, cursor: nextCursor });
      setMore((value) => ({ key, pages: [...(value.key === key ? value.pages : []), page], loading: false, error: null }));
    } catch (error) {
      setMore((value) => ({ ...(value.key === key ? value : { key, pages: [] }), loading: false, error }));
    }
  }, [key, nextCursor, sourceType, identityName]);

  const refetch = useCallback(() => {
    setMore({ key, pages: [], loading: false, error: null });
    first.refetch();
  }, [key, first]);

  return {
    items,
    nextCursor,
    loadMore,
    loadingMore: own.loading,
    moreError: own.error,
    isLoading: first.isLoading && !first.data,
    isError: first.isError && !first.data,
    error: first.error,
    refetch,
  };
}

/**
 * One identity's genome.
 *
 * Three read-only views of the same record, in the order an investigation
 * runs: what is normal for this identity (Genome), the calls behind it
 * (Activity) and how it got here (Timeline: learning, baseline versions,
 * anomalies). The genome service changes nothing, so there are no
 * disposition or containment controls here.
 */
export default function GenomeDetailPage() {
  const { id } = useParams();
  const parsed = readId(id);

  if (!parsed) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Genome" />
        <Panel prominence="lead">
          <EmptyState
            icon={SearchX}
            title="This is not a genome identity link"
            description="Genome links name a monitored identity by its source type and ARN. Open the identity from the fleet instead."
            action={
              <Button variant="secondary" size="sm" as={Link} to="/genome" icon={ArrowLeft}>
                Back to the fleet
              </Button>
            }
          />
        </Panel>
      </div>
    );
  }

  return <GenomeIdentity key={`${parsed.sourceType}:${parsed.identityName}`} {...parsed} />;
}

/**
 * Where the baseline stands: the summary's baselines row says whether it is
 * established, the timeline's milestones say since when.
 */
function baselineState(baseline, items) {
  const established = items.find((item) => item.type === 'baseline_established');
  const learning = items.find((item) => item.type === 'learning_started');
  const active = baseline ? String(baseline.status).toLowerCase() === 'active' : Boolean(established);
  if (active) return { key: 'active', label: 'Baseline established', tone: 'low', at: established?.occurred_at ?? null };
  if (baseline || learning) return { key: 'learning', label: 'Learning', tone: 'medium', at: learning?.occurred_at ?? null };
  return { key: 'none', label: 'No baseline yet', tone: 'neutral', at: null };
}

/** A readable name from an ARN when the inventory has no row for it. */
function arnName(arn) {
  const tail = String(arn ?? '').split(':').slice(5).join(':');
  return tail.split('/').pop() || arn;
}

function GenomeIdentity({ sourceType, identityName }) {
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = TAB_KEYS.includes(searchParams.get('tab')) ? searchParams.get('tab') : 'genome';
  const window = WINDOWS.some((entry) => entry.value === searchParams.get('window'))
    ? searchParams.get('window')
    : DEFAULT_WINDOW;

  const setParam = useCallback(
    (key, value) => {
      const next = new URLSearchParams(searchParams);
      if (value) next.set(key, value);
      else next.delete(key);
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );
  const setTab = (value) => setParam('tab', value === 'genome' ? '' : value);

  const timeline = useTimeline(sourceType, identityName);
  /* The header: identity, baseline and headline metrics. A failure only costs
     the header; the tabs still load. */
  const summary = useQuery(
    (signal) => fetchGenomeIdentity(sourceType, identityName, { window }, signal),
    [sourceType, identityName, window],
  );
  /* The inventory record behind this identity, for the cross-links to its
     identity record and access graph. */
  const discovery = useQuery((signal) => resolveDiscoveryIdentity(identityName, signal).catch(() => null), [identityName]);

  const openAnomalies = useMemo(
    () => timeline.items.filter((item) => item.type === 'anomaly' && String(item.status).toLowerCase() === 'open'),
    [timeline.items],
  );
  const timelineOpen = useMemo(
    () => openAnomalies.reduce((sum, item) => sum + (Number(/^(\d+)/.exec(item.title ?? '')?.[1]) || 1), 0),
    [openAnomalies],
  );
  // The summary counts open alerts directly; the timeline count stands in while it loads.
  const openCount = summary.data?.openAnomalies ?? timelineOpen;

  /* Every genome endpoint resolves the identity in the caller's scope first,
     so a 404 means the identity is not monitored here. */
  if ((timeline.isError && timeline.error?.status === 404) || (summary.isError && summary.error?.status === 404)) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Genome"
          actions={
            <Button variant="secondary" as={Link} to="/genome" icon={ArrowLeft}>
              Fleet
            </Button>
          }
        />
        <Panel prominence="lead">
          <EmptyState
            icon={SearchX}
            title="This identity is not monitored in your scope"
            description="It is outside the accounts and regions you selected, or discovery no longer sees it. Change the scope from the dashboard to include its region."
            action={
              <Button variant="secondary" size="sm" as={Link} to={`/identities?search=${encodeURIComponent(identityName)}`} icon={Boxes}>
                Look it up in the inventory
              </Button>
            }
          />
        </Panel>
      </div>
    );
  }

  const record = discovery.data;
  const identity = summary.data?.identity;
  const state = baselineState(summary.data?.baseline ?? null, timeline.items);
  const discoveryId = record?.id ?? null;
  const title = identity?.display_name || record?.name || arnName(identityName);

  const tabs = [
    { value: 'genome', label: 'Genome' },
    { value: 'activity', label: 'Activity' },
    { value: 'timeline', label: 'Timeline', count: openCount || undefined },
  ];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={title}
        lede={[sourceTypeLabel(sourceType), identity?.account_id ?? record?.account_id, identity?.region ?? record?.region]
          .filter(Boolean)
          .join(' · ')}
        actions={
          <>
            <SegmentedControl
              label="Behaviour window"
              options={WINDOWS}
              value={window}
              onChange={(value) => setParam('window', value === DEFAULT_WINDOW ? '' : value)}
            />
            <Button variant="secondary" as={Link} to="/genome" icon={ArrowLeft}>
              Fleet
            </Button>
            {discoveryId ? (
              <>
                <Button variant="ghost" as={Link} to={identityRoute(discoveryId)} icon={FingerprintIcon}>
                  Identity record
                </Button>
                <Button variant="ghost" as={Link} to={accessGraphRoute(discoveryId)} icon={Network}>
                  Access graph
                </Button>
              </>
            ) : (
              <Button variant="ghost" as={Link} to={`/identities?search=${encodeURIComponent(identityName)}`} icon={Boxes}>
                Open in inventory
              </Button>
            )}
          </>
        }
        meta={
          <div className="flex flex-wrap items-center gap-2">
            <Tag tone={state.tone} size="sm" dot>
              {state.label}
            </Tag>
            {state.at && (
              <span className="text-[11.5px] text-ink-3" title={formatDateTime(state.at)}>
                {state.key === 'active' ? `since ${formatDate(state.at)}` : `started ${formatRelative(state.at)}`}
              </span>
            )}
            {summary.data?.baseline && state.key !== 'active' && (
              <Tag tone="neutral" size="sm">
                Learned {formatNumber(summary.data.baseline.built_days)} of {formatNumber(summary.data.baseline.required_days)} days
              </Tag>
            )}
            {identity?.status && (
              <Tag tone="neutral" size="sm">
                {titleCaseEnum(identity.status)}
              </Tag>
            )}
            {summary.data?.lastSeen && (
              <span className="text-[11.5px] text-ink-3" title={formatDateTime(summary.data.lastSeen)}>
                Last seen {formatRelative(summary.data.lastSeen)}
              </span>
            )}
            <CopyableValue value={identityName} className="max-w-full text-[11.5px] text-ink-3" />
          </div>
        }
        tabs={<Tabs size="sm" tabs={tabs} value={tab} onChange={setTab} />}
      />

      {/* An open departure is the reason anyone opened this record, so it is
          stated above the tabs rather than hidden inside one of them. */}
      {openAnomalies.length > 0 && tab !== 'timeline' && (
        <Panel prominence="lead" className="animate-rise border-critical/30">
          <PanelHeader
            prominence="lead"
            title={`${formatNumber(openCount)} open ${openCount === 1 ? 'departure' : 'departures'} from baseline`}
            subtitle="This identity behaved outside its own baseline."
            actions={
              <Button variant="secondary" size="sm" onClick={() => setTab('timeline')}>
                Read the evidence
              </Button>
            }
          />
          <ul className="mt-3 flex flex-col gap-2">
            {openAnomalies.slice(0, 4).map((item) => {
              const { dimension, reason } = parseAnomaly(item);
              return (
                <li
                  key={`${item.occurred_at}-${item.description}`}
                  className="flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2"
                >
                  {dimension && (
                    <Tag tone="high" size="sm" dot>
                      {dimensionLabel(dimension)}
                    </Tag>
                  )}
                  <span className="min-w-0 flex-1 text-[12.5px] text-ink">{reason || item.title}</span>
                  <span className="shrink-0 text-[11.5px] text-ink-3" title={formatDateTime(item.occurred_at)}>
                    {formatRelative(item.occurred_at)}
                  </span>
                </li>
              );
            })}
          </ul>
          {openAnomalies.length > 4 && (
            <p className="mt-2 text-[11.5px] text-ink-3">
              And {formatNumber(openAnomalies.length - 4)} more in the timeline.
            </p>
          )}
        </Panel>
      )}

      {tab === 'genome' && (
        <GenomeTab sourceType={sourceType} identityName={identityName} window={window} summary={summary.data} timeline={timeline.items} />
      )}
      {tab === 'activity' && <ActivityTab sourceType={sourceType} identityName={identityName} window={window} />}
      {tab === 'timeline' && <TimelineTab timeline={timeline} />}
    </div>
  );
}

/* ── Genome ───────────────────────────────────────────────────────────────── */

const RADAR_AXES = 8;
const RESOURCES_SHOWN = 10;

function GenomeTab({ sourceType, identityName, window, summary, timeline }) {
  const [showAllResources, setShowAllResources] = useState(false);
  const profile = useQuery(
    (signal) => fetchGenomeProfile(sourceType, identityName, { window, limit: 25 }, signal),
    [sourceType, identityName, window],
  );

  /* The fingerprint arrives as (dimension, value, share) rows. Grouped by
     dimension, the share of the most common value says how predictable this
     identity is on that axis - which is the shape the radar draws. */
  const dimensions = useMemo(() => {
    const groups = new Map();
    for (const point of profile.data?.fingerprint ?? []) {
      if (!groups.has(point.dimension)) groups.set(point.dimension, []);
      groups.get(point.dimension).push(point);
    }
    return [...groups.entries()].map(([dimension, values]) => ({
      dimension,
      values: [...values].sort((a, b) => b.share_percent - a.share_percent),
      top: Math.max(0, ...values.map((value) => Number(value.share_percent) || 0)),
    }));
  }, [profile.data]);

  /* The schedule's (day, hour) cells folded onto one day: when in the day
     this identity works, whatever the weekday. */
  const hourly = useMemo(() => {
    const calls = Array(24).fill(0);
    for (const cell of profile.data?.activitySchedule ?? []) {
      const hour = Number(cell.hour);
      if (hour >= 0 && hour < 24) calls[hour] += Number(cell.calls) || 0;
    }
    return calls.map((value, hour) => ({ label: `${String(hour).padStart(2, '0')}:00`, calls: value }));
  }, [profile.data]);

  if (profile.isError && !profile.data) {
    return (
      <Panel prominence="lead">
        <ErrorState error={profile.error} onRetry={profile.refetch} />
      </Panel>
    );
  }
  if (!profile.data) {
    return (
      <div className="grid gap-4 @min-[52rem]:grid-cols-2">
        <Panel prominence="lead">
          <ChartSkeleton height={240} />
        </Panel>
        <Panel prominence="quiet">
          <ListSkeleton rows={6} />
        </Panel>
      </div>
    );
  }

  const { typicalActions, typicalResources, activitySchedule } = profile.data;
  const span = WINDOW_WORDS[window];
  const empty = dimensions.length === 0 && typicalActions.length === 0 && activitySchedule.length === 0;
  const axes = dimensions.slice(0, RADAR_AXES).map((entry) => ({ key: entry.dimension, label: dimensionLabel(entry.dimension) }));

  return (
    <div className="flex flex-col gap-4">
      {empty && (
        <Panel prominence="quiet" className="animate-rise">
          <EmptyState
            compact
            icon={Dna}
            title={`No calls recorded in the last ${span}`}
            description="The genome is drawn from this identity's own calls. Widen the window, or check back once it has been active."
          />
        </Panel>
      )}
      {empty && <BaselineProvenance summary={summary} timeline={timeline} span={span} />}

      {!empty && (
        <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)]">
          <Panel prominence="lead" className="animate-rise">
            <PanelHeader
              prominence="lead"
              title="Behavioural fingerprint"
              subtitle={`How concentrated each dimension is over the last ${span}: the share of calls carried by its most common value. A full axis means one value carries every call.`}
            />
            {axes.length >= 3 ? (
              <Fingerprint
                className="mt-4"
                axes={axes}
                valueSuffix="%"
                series={[
                  {
                    key: 'top',
                    label: 'Most common value',
                    values: Object.fromEntries(dimensions.map((entry) => [entry.dimension, entry.top])),
                    tone: 'var(--t-series-1)',
                  },
                ]}
              />
            ) : (
              <p className="mt-4 text-[12.5px] text-ink-3">
                {dimensions.length === 0
                  ? 'No behavioural dimensions were recorded in this window.'
                  : 'Too few dimensions to draw a shape. Their values are listed below.'}
              </p>
            )}
          </Panel>

          <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
            <PanelHeader prominence="quiet" title="Typical actions" subtitle={`Share of calls over the last ${span}.`} />
            {typicalActions.length === 0 ? (
              <p className="mt-3 text-[12.5px] text-ink-3">No API calls in this window.</p>
            ) : (
              <ApiShareList
                className="mt-3"
                actions={typicalActions.map((action) => ({
                  api: action.api_operation,
                  share: Number(action.share_percent) || 0,
                  calls: Number(action.calls) || 0,
                }))}
              />
            )}
          </Panel>
        </div>
      )}

      {!empty && (
        <div className="grid gap-4 @min-[60rem]:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
          <Panel prominence="quiet" className="animate-rise">
            <PanelHeader
              prominence="quiet"
              title="Activity schedule"
              subtitle="Seven days by hour, UTC. Darker means busier; activity outside these usual hours is what a departure looks like."
            />
            {activitySchedule.length === 0 ? (
              <p className="mt-3 text-[12.5px] text-ink-3">No calls in this window.</p>
            ) : (
              <ScheduleGrid className="mt-3" cells={activitySchedule} />
            )}
          </Panel>
          <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
            <PanelHeader prominence="quiet" title="Calls by hour of day" subtitle={`All days of the last ${span} folded onto one day, UTC.`} />
            {hourly.every((entry) => entry.calls === 0) ? (
              <p className="mt-3 text-[12.5px] text-ink-3">No calls in this window.</p>
            ) : (
              <TrendChart className="mt-3" variant="bar" data={hourly} dataKey="calls" label="API calls" height={150} />
            )}
          </Panel>
        </div>
      )}

      {!empty && (
        <div className="grid gap-4 @min-[52rem]:grid-cols-2">
          <Panel prominence="quiet" className="animate-rise" >
            <PanelHeader prominence="quiet" title="Typical resources" subtitle={`Resources touched over the last ${span}, by calls.`} />
            {typicalResources.length === 0 ? (
              <p className="mt-3 text-[12.5px] text-ink-3">No resource ARNs were recorded on this identity's calls.</p>
            ) : (
              <ul className="mt-3 flex flex-col">
                {(showAllResources ? typicalResources : typicalResources.slice(0, RESOURCES_SHOWN)).map((resource) => (
                  <li key={resource.resource} className="border-b border-line py-2 last:border-0">
                    <div className="flex items-baseline justify-between gap-3">
                      <span className="min-w-0 truncate font-mono text-[12px] text-ink-2" title={resource.resource}>
                        {resource.resource}
                      </span>
                      <span data-numeric="" className="shrink-0 text-[11.5px] text-ink-3">
                        {formatNumber(resource.calls)}
                      </span>
                    </div>
                    {resource.resource_type && (
                      <span className="mt-0.5 block text-[11px] text-ink-3">{resource.resource_type}</span>
                    )}
                  </li>
                ))}
              </ul>
            )}
            {typicalResources.length > RESOURCES_SHOWN && (
              <Button
                variant="link"
                size="sm"
                className="mt-2"
                aria-expanded={showAllResources}
                onClick={() => setShowAllResources((value) => !value)}
              >
                {showAllResources ? 'Show fewer' : `Show all ${formatNumber(typicalResources.length)} resources`}
              </Button>
            )}
          </Panel>
          <BaselineProvenance summary={summary} timeline={timeline} span={span} />
        </div>
      )}

      {!empty && (
          <Panel prominence="quiet" className="animate-rise">
            <PanelHeader prominence="quiet" title="Values by dimension" subtitle="Each dimension's values and their share of calls." />
            {dimensions.length === 0 ? (
              <p className="mt-3 text-[12.5px] text-ink-3">No behavioural dimensions were recorded in this window.</p>
            ) : (
              <div className="mt-3 gap-x-8 @min-[52rem]:columns-2">
                {dimensions.map((entry) => (
                  <div key={entry.dimension} className="mb-4 break-inside-avoid">
                    <SectionLabel>{dimensionLabel(entry.dimension)}</SectionLabel>
                    <ul className="mt-1.5 flex flex-col gap-1.5">
                      {entry.values.slice(0, 6).map((point) => (
                        <li key={point.value} className="flex items-center gap-3">
                          <span className="w-[42%] min-w-0 truncate font-mono text-[11.5px] text-ink-2" title={point.value}>
                            {dimensionValueLabel(entry.dimension, point.value)}
                          </span>
                          <span className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-track">
                            <span
                              className="block h-full rounded-full"
                              style={{ width: `${Math.min(100, point.share_percent)}%`, background: 'var(--t-series-1)' }}
                            />
                          </span>
                          <span data-numeric="" className="w-11 shrink-0 text-right text-[11.5px] font-semibold text-ink">
                            {formatShare(point.share_percent)}
                          </span>
                        </li>
                      ))}
                    </ul>
                    {entry.values.length > 6 && (
                      <p className="mt-1 text-[11px] text-ink-3">and {formatNumber(entry.values.length - 6)} rarer values</p>
                    )}
                  </div>
                ))}
              </div>
            )}
          </Panel>
      )}
    </div>
  );
}

/**
 * What this identity's verdicts are built from: the baselines row (learning
 * period, days learned), the timeline's milestones and versions, and the
 * calls the genome read. Rows the stores have not filled are left out.
 */
function BaselineProvenance({ summary, timeline, span }) {
  const baseline = summary?.baseline;
  const at = (type) => timeline.find((item) => item.type === type)?.occurred_at;
  const versions = timeline.filter((item) => item.type === 'baseline_version_published');
  const latest = versions.length ? /(\d+)/.exec(versions[0].description ?? '')?.[1] : null;
  const date = (iso) => <span title={formatDateTime(iso)}>{formatDate(iso)}</span>;
  const rows = [
    baseline && ['Learning period', `${formatNumber(baseline.required_days)} ${baseline.required_days === 1 ? 'day' : 'days'}`],
    baseline && ['Days learned', `${formatNumber(baseline.built_days)} (${titleCaseEnum(baseline.status).toLowerCase()})`],
    at('learning_started') && ['Learning started', date(at('learning_started'))],
    at('baseline_established') && ['Baseline established', date(at('baseline_established'))],
    latest && ['Current version', `Version ${latest}, published ${formatRelative(versions[0].occurred_at)}`],
    summary?.callsInWindow !== null && summary?.callsInWindow !== undefined && [`Calls, last ${span}`, formatNumber(summary.callsInWindow)],
    summary?.identity?.region && ['Region', summary.identity.region],
    summary?.identity?.role_arn && [
      'Runs as',
      <CopyableValue key="role" value={summary.identity.role_arn} className="text-[12.5px]">
        {arnName(summary.identity.role_arn)}
      </CopyableValue>,
    ],
  ].filter(Boolean);
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
      <PanelHeader prominence="quiet" title="Baseline provenance" subtitle="What the verdicts on this identity are built from." />
      {rows.length === 0 ? (
        <p className="mt-3 text-[12.5px] text-ink-3">No baseline has been recorded for this identity yet.</p>
      ) : (
        <dl className="mt-2 flex flex-col">
          {rows.map(([label, value]) => (
            <div key={label} className="grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)] gap-3 border-b border-line py-2 last:border-0">
              <dt className="text-[12px] text-ink-3">{label}</dt>
              <dd className="min-w-0 text-[12.5px] text-ink">{value}</dd>
            </div>
          ))}
        </dl>
      )}
    </Panel>
  );
}

function LoadOlder({ timeline }) {
  if (!timeline.nextCursor && !timeline.moreError) return null;
  return (
    <div className="flex flex-col items-center gap-2">
      {timeline.moreError && (
        <InlineError error={timeline.moreError} onRetry={timeline.loadMore} label="Older history failed to load" />
      )}
      {timeline.nextCursor && (
        <Button variant="secondary" size="sm" icon={History} onClick={timeline.loadMore} loading={timeline.loadingMore}>
          Load older history
        </Button>
      )}
    </div>
  );
}

/* ── Activity ─────────────────────────────────────────────────────────────── */

function bucketLabel(bucket, hourly) {
  const date = new Date(hourly ? bucket : `${bucket}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) return bucket;
  return hourly
    ? date.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZone: 'UTC' })
    : date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' });
}

function ActivityTab({ sourceType, identityName, window }) {
  const hourly = window === '24h' || window === '7d';
  const pager = useCursorPager(`${sourceType}:${identityName}:${window}`);
  const activity = useQuery(
    (signal) =>
      fetchGenomeActivity(
        sourceType,
        identityName,
        { window, bucket: hourly ? 'hour' : 'day', recentLimit: RECENT_PAGE, recentCursor: pager.cursor ?? undefined },
        signal,
      ),
    [sourceType, identityName, window, pager.cursor],
  );

  /* Buckets with no calls are absent from the response. When the store was
     read (total_calls is a number) an absent bucket is a measured zero, so the
     series is filled; otherwise only what was returned is drawn. */
  const volume = useMemo(() => {
    const points = activity.data?.volume ?? [];
    const keyOf = (bucket) => {
      const date = new Date(hourly ? bucket : `${bucket}T00:00:00Z`);
      return Number.isNaN(date.getTime()) ? String(bucket) : date.toISOString().slice(0, hourly ? 13 : 10);
    };
    const counts = new Map(points.map((point) => [keyOf(point.bucket), Number(point.calls) || 0]));
    const step = hourly ? 3600000 : 86400000;
    const span = WINDOW_MS[window];
    if (activity.data?.totalCalls === null || activity.data?.totalCalls === undefined || !span) {
      return points.map((point) => ({ label: `${bucketLabel(point.bucket, hourly)}${hourly ? ' UTC' : ''}`, calls: Number(point.calls) || 0 }));
    }
    const end = new Date(activity.data.generatedAt ?? '').getTime();
    if (!Number.isFinite(end)) return points.map((point) => ({ label: bucketLabel(point.bucket, hourly), calls: Number(point.calls) || 0 }));
    const start = Math.floor((end - span) / step) * step;
    const out = [];
    for (let at = start; at <= end; at += step) {
      const iso = new Date(at).toISOString();
      const key = iso.slice(0, hourly ? 13 : 10);
      out.push({ label: `${bucketLabel(hourly ? iso : key, hourly)}${hourly ? ' UTC' : ''}`, calls: counts.get(key) ?? 0 });
    }
    return out;
  }, [activity.data, hourly, window]);

  if (activity.isError && !activity.data) {
    return (
      <Panel prominence="lead">
        <ErrorState error={activity.error} onRetry={activity.refetch} />
      </Panel>
    );
  }

  const total = activity.data?.totalCalls;
  const span = WINDOW_WORDS[window];

  return (
    <div className="flex flex-col gap-4">
      <Panel prominence="quiet" className="animate-rise">
        <PanelHeader
          prominence="quiet"
          title={`Call volume, last ${span}`}
          subtitle={hourly ? 'Calls per hour, UTC.' : 'Calls per day, UTC.'}
          actions={
            activity.data && (
              <span className="text-[12px] text-ink-3">
                <Measured value={total} empty="Total not available">
                  <span data-numeric="" className="font-semibold text-ink">
                    {formatNumber(total)}
                  </span>{' '}
                  calls in the window
                </Measured>
              </span>
            )
          }
        />
        {!activity.data ? (
          <ChartSkeleton height={150} />
        ) : volume.length === 0 ? (
          // A null total means the call store could not be read, which is not "no calls".
          <p className="mt-3 text-[12.5px] text-ink-3">{total === null ? 'Call history is not available right now.' : 'No calls in this window.'}</p>
        ) : (
          <TrendChart className="mt-3" variant="bar" data={volume} dataKey="calls" label="API calls" height={170} />
        )}
      </Panel>

      <Panel prominence="lead" flush className="animate-rise overflow-hidden">
        <div className="border-b border-line px-4 py-3">
          <PanelHeader prominence="lead" title="Recent calls" subtitle="Newest first, with the risk the detector gave each call." />
        </div>
        <DataGrid
          caption="Recent API calls"
          loading={!activity.data}
          refreshing={activity.isRefreshing}
          columns={[
            {
              key: 'at',
              header: 'When',
              width: '12%',
              cell: (row) => (
                <span className="whitespace-nowrap text-[12.5px] text-ink-2" title={formatDateTime(row.event_time)}>
                  {formatRelative(row.event_time)}
                </span>
              ),
            },
            {
              key: 'api',
              header: 'API call',
              primary: true,
              width: '24%',
              cell: (row) => (
                <span className="block min-w-0 truncate font-mono text-[12px] text-ink" title={row.api_call}>
                  {row.api_call}
                </span>
              ),
            },
            {
              key: 'resource',
              header: 'Resource',
              width: '27%',
              cell: (row) =>
                row.resource ? (
                  <span className="block min-w-0 truncate font-mono text-[11.5px] text-ink-3" title={row.resource}>
                    {row.resource}
                  </span>
                ) : (
                  <span className="text-[11.5px] text-ink-3">-</span>
                ),
            },
            {
              key: 'source',
              header: 'Source',
              width: '12%',
              priority: 'wide',
              cell: (row) => <span className="block truncate font-mono text-[11.5px] text-ink-3">{row.source_ip || '-'}</span>,
            },
            {
              key: 'region',
              header: 'Region',
              width: '9%',
              priority: 'wide',
              cell: (row) => <span className="text-[12px] text-ink-2">{row.region || '-'}</span>,
            },
            {
              key: 'risk',
              header: 'Risk',
              width: '16%',
              cell: (row) => <CallRisk status={row.risk_status} score={row.risk_score} />,
            },
          ]}
          rows={activity.data?.recentCalls ?? []}
          rowKey={(row) => row.event_id}
          density="compact"
          skeletonRows={10}
          emptyState={
            total === null ? (
              <EmptyState
                compact
                icon={Dna}
                title="Call history is not available"
                description="The genome's call store could not be read. Try again shortly."
              />
            ) : (
              <EmptyState
                compact
                icon={Dna}
                title="No calls in this window"
                description="This identity made no recorded API calls in the selected window."
              />
            )
          }
        />
        {activity.data && (pager.page > 1 || activity.data.nextCursor) && (
          <CursorPager
            page={pager.page}
            shown={activity.data.recentCalls.length}
            pageSize={RECENT_PAGE}
            hasNext={Boolean(activity.data.nextCursor)}
            onPrev={pager.prev}
            onNext={() => pager.next(activity.data.nextCursor)}
            unit="calls"
          />
        )}
      </Panel>
    </div>
  );
}

const CALL_RISK = {
  anomalous: { label: 'Anomalous', tone: 'critical' },
  elevated: { label: 'Elevated', tone: 'medium' },
  normal: { label: 'Normal', tone: 'neutral' },
};

function CallRisk({ status, score }) {
  if (!status && (score === null || score === undefined)) return <span className="text-[11.5px] text-ink-3">-</span>;
  const key = String(status ?? '').toLowerCase();
  const meta = CALL_RISK[key] ?? (severityMeta(status).tone !== 'neutral' ? severityMeta(status) : { label: titleCaseEnum(status), tone: 'neutral' });
  return (
    <span className="flex min-w-0 items-center gap-1.5" title={score !== null && score !== undefined ? `Call risk score ${score}` : undefined}>
      {score !== null && score !== undefined && (
        <span data-numeric="" className="w-6 shrink-0 text-[12px] font-semibold text-ink">
          {score}
        </span>
      )}
      {status && (
        <Tag tone={meta.tone} size="sm">
          {meta.label}
        </Tag>
      )}
    </span>
  );
}

/* ── Timeline ─────────────────────────────────────────────────────────────── */

const TIMELINE_KINDS = {
  anomaly: { tone: 'border-critical/40 bg-critical-soft text-critical' },
  baseline_version_published: { tone: 'border-brand/30 bg-info-soft text-brand' },
  baseline_established: { tone: 'border-low/40 bg-low-soft text-low' },
  learning_started: { tone: 'border-line-strong bg-surface-3 text-ink-3' },
};

function TimelineTab({ timeline }) {
  if (timeline.isError) {
    return (
      <Panel prominence="lead">
        <ErrorState error={timeline.error} onRetry={timeline.refetch} />
      </Panel>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      <Panel prominence="lead" className="animate-rise">
        <PanelHeader
          prominence="lead"
          title="How this identity got here"
          subtitle="Newest first: anomalies and baseline updates for this identity."
        />
        {timeline.isLoading ? (
          <div className="mt-4">
            <ListSkeleton rows={4} />
          </div>
        ) : timeline.items.length === 0 ? (
          <EmptyState
            compact
            icon={History}
            title="Nothing on the record yet"
            description="Learning, baseline and anomaly events appear here once the genome starts watching this identity."
          />
        ) : (
          <ol className="mt-4 flex flex-col">
            {timeline.items.map((entry, index) => {
              const anomaly = entry.type === 'anomaly' ? parseAnomaly(entry) : null;
              const status = entry.status ? anomalyStatusMeta(entry.status) : null;
              return (
                <li key={`${entry.type}-${entry.occurred_at}-${index}`} className="flex gap-3">
                  <div className="flex shrink-0 flex-col items-center">
                    <span
                      aria-hidden="true"
                      className={`grid size-7 place-items-center rounded-full border text-[11px] font-bold ${(TIMELINE_KINDS[entry.type] ?? TIMELINE_KINDS.learning_started).tone}`}
                    >
                      {index + 1}
                    </span>
                    {index < timeline.items.length - 1 && <span aria-hidden="true" className="min-h-8 w-px flex-1 bg-line" />}
                  </div>
                  <div className="min-w-0 flex-1 pb-5">
                    <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                      <p className="flex flex-wrap items-center gap-2 text-[13px] font-semibold text-ink">
                        {entry.title}
                        {status && (
                          <Tag tone={status.tone} size="sm">
                            {status.label}
                          </Tag>
                        )}
                      </p>
                      <p className="shrink-0 text-[11.5px] text-ink-3" title={formatDateTime(entry.occurred_at)}>
                        {formatRelative(entry.occurred_at)}
                      </p>
                    </div>
                    {anomaly ? (
                      <p className="mt-0.5 text-[12.5px] leading-relaxed text-ink-2">
                        {anomaly.dimension && <span className="text-ink-3">{dimensionLabel(anomaly.dimension)}: </span>}
                        {anomaly.reason}
                      </p>
                    ) : (
                      entry.description && <p className="mt-0.5 text-[12.5px] leading-relaxed text-ink-2">{entry.description}</p>
                    )}
                  </div>
                </li>
              );
            })}
          </ol>
        )}
      </Panel>
      <LoadOlder timeline={timeline} />
    </div>
  );
}

