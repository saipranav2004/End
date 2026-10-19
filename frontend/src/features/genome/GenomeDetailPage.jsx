import { useCallback, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ArrowLeft, Boxes, Dna, Fingerprint as FingerprintIcon, History, Network, SearchX } from 'lucide-react';
import {
  fetchGenomeActivity,
  fetchGenomeBaseline,
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
import { Meter } from '../../ui/Meter';
import { Panel, PanelHeader, SectionLabel } from '../../ui/Panel';
import { ChartSkeleton, DetailSkeleton, ListSkeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState, InlineError } from '../../ui/States';
import { SegmentedControl, Tabs } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { TrendChart } from '../../charts/TrendChart';
import { ApiShareList, Fingerprint, formatShare, ScheduleGrid } from './visuals';
import { AnomalyList, NoAnomalies, useAnomalyList } from './AnomalyReview';
import {
  anomalyStatusMeta,
  baselineMeta,
  CursorPager,
  dimensionLabel,
  dimensionValueLabel,
  learningPercent,
  Measured,
  parseAnomaly,
  Row,
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

const TAB_KEYS = ['genome', 'anomalies', 'activity', 'baseline', 'timeline'];
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
 * older pages are appended on request. The Anomalies tab, the open-departure
 * banner and the Timeline tab all read the same list.
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
 * Five read-only views of the same record, in the order an investigation runs:
 * what is normal for this identity (Genome), what departed (Anomalies), the
 * calls behind it (Activity), what the baseline is built from (Baseline) and
 * how it got here (Timeline).
 * The genome service changes nothing, so there are no disposition or
 * containment controls here.
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

  const summary = useQuery(
    (signal) => fetchGenomeIdentity(sourceType, identityName, { window }, signal),
    [sourceType, identityName, window],
  );
  const baseline = useQuery((signal) => fetchGenomeBaseline(sourceType, identityName, { limit: 25 }, signal), [
    sourceType,
    identityName,
  ]);
  const timeline = useTimeline(sourceType, identityName);
  /* The inventory record behind this identity, for the cross-links. A failure
     only costs the direct links; the search link still works. */
  const discovery = useQuery((signal) => resolveDiscoveryIdentity(identityName, signal).catch(() => null), [identityName]);

  const record = summary.data;
  const openCount = record?.headline_metrics?.open_anomalies;
  const openAnomalies = useMemo(
    () => timeline.items.filter((item) => item.type === 'anomaly' && String(item.status).toLowerCase() === 'open'),
    [timeline.items],
  );

  if (summary.isError && !record) {
    const notFound = summary.error?.status === 404;
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Genome"
          actions={
            <Button variant="secondary" as={Link} to="/genome" icon={ArrowLeft}>
              All identities
            </Button>
          }
        />
        <Panel prominence="lead">
          {notFound ? (
            <EmptyState
              icon={SearchX}
              title="This identity is not monitored in your scope"
              description="It is not in the genome's fleet for your selected accounts and regions, or discovery no longer sees it."
              action={
                <Button variant="secondary" size="sm" as={Link} to={`/identities?search=${encodeURIComponent(identityName)}`} icon={Boxes}>
                  Look it up in the inventory
                </Button>
              }
            />
          ) : (
            <ErrorState error={summary.error} onRetry={summary.refetch} />
          )}
        </Panel>
      </div>
    );
  }

  if (!record) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Genome" />
        <Panel prominence="lead">
          <DetailSkeleton rows={8} />
        </Panel>
      </div>
    );
  }

  const { identity, risk, baseline_summary: baselineSummary, headline_metrics: metrics } = record;
  const state = baselineMeta(baselineSummary?.status);
  const severity = severityMeta(risk?.severity);
  const progress = learningPercent(baselineSummary?.built_days, baselineSummary?.required_days);
  const discoveryId = discovery.data?.id ?? null;

  const tabs = [
    { value: 'genome', label: 'Genome' },
    { value: 'anomalies', label: 'Anomalies', count: openCount || undefined },
    { value: 'activity', label: 'Activity' },
    { value: 'baseline', label: 'Baseline' },
    { value: 'timeline', label: 'Timeline' },
  ];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={identity.display_name || identity.identity_name}
        lede={[sourceTypeLabel(identity.source_type), identity.account_id, identity.region].filter(Boolean).join(' · ')}
        actions={
          <>
            <SegmentedControl
              label="Behaviour window"
              options={WINDOWS}
              value={window}
              onChange={(value) => setParam('window', value === DEFAULT_WINDOW ? '' : value)}
            />
            <Button variant="secondary" as={Link} to="/genome" icon={ArrowLeft}>
              All identities
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
              <Button variant="ghost" as={Link} to={`/identities?search=${encodeURIComponent(identity.identity_name)}`} icon={Boxes}>
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
            {state.key !== 'active' && progress !== null && (
              <span className="text-[11.5px] text-ink-3">
                {formatNumber(baselineSummary.built_days)} of {formatNumber(baselineSummary.required_days)} learning days
              </span>
            )}
            {risk?.score !== null && risk?.score !== undefined ? (
              <Tag tone={risk.severity ? severity.tone : 'neutral'} size="sm">
                Risk {risk.score}/100{risk.severity ? ` · ${severity.label}` : ''} · last {window}
              </Tag>
            ) : (
              <Tag tone="neutral" size="sm">
                Not scored yet
              </Tag>
            )}
            {identity.service_name && (
              <Tag tone="neutral" size="sm">
                {identity.service_name}
              </Tag>
            )}
            {identity.status && (
              <Tag tone="neutral" size="sm">
                {identity.status}
              </Tag>
            )}
            <CopyableValue value={identity.identity_name} className="max-w-full text-[11.5px] text-ink-3" />
            <span className="text-[11.5px] text-ink-3" title={metrics?.last_seen ? formatDateTime(metrics.last_seen) : undefined}>
              {metrics?.last_seen ? `Last seen ${formatRelative(metrics.last_seen)}` : 'No calls seen yet'}
            </span>
          </div>
        }
        tabs={<Tabs size="sm" tabs={tabs} value={tab} onChange={setTab} />}
      />

      {/* An open departure is the reason anyone opened this record, so it is
          stated above the tabs rather than hidden inside one of them. */}
      {openAnomalies.length > 0 && tab !== 'anomalies' && (
        <Panel prominence="lead" className="animate-rise border-critical/30">
          <PanelHeader
            prominence="lead"
            title={`${formatNumber(openCount ?? openAnomalies.length)} open ${(openCount ?? openAnomalies.length) === 1 ? 'anomaly' : 'anomalies'} to review`}
            actions={
              <Button variant="secondary" size="sm" onClick={() => setTab('anomalies')}>
                Review them
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
              And {formatNumber(openAnomalies.length - 4)} more in the Anomalies tab.
            </p>
          )}
        </Panel>
      )}

      {tab === 'genome' && (
        <GenomeTab sourceType={sourceType} identityName={identityName} window={window} baseline={baseline} />
      )}
      {tab === 'anomalies' && <AnomaliesTab sourceType={sourceType} identityName={identityName} baselineState={state} />}
      {tab === 'activity' && <ActivityTab sourceType={sourceType} identityName={identityName} window={window} />}
      {tab === 'baseline' && <BaselineTab baseline={baseline} />}
      {tab === 'timeline' && <TimelineTab timeline={timeline} />}
    </div>
  );
}

/* ── Genome ───────────────────────────────────────────────────────────────── */

const RADAR_AXES = 8;
const RESOURCES_SHOWN = 10;

function GenomeTab({ sourceType, identityName, window, baseline }) {
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

      <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        {!empty && (
          <Panel prominence="quiet" className="animate-rise">
            <PanelHeader
              prominence="quiet"
              title="Activity schedule"
              subtitle="Seven days by hour, UTC. Darker means busier; activity outside these usual hours is flagged."
            />
            {activitySchedule.length === 0 ? (
              <p className="mt-3 text-[12.5px] text-ink-3">No calls in this window.</p>
            ) : (
              <ScheduleGrid className="mt-3" cells={activitySchedule} />
            )}
          </Panel>
        )}

        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
          <PanelHeader prominence="quiet" title="How the baseline was built" subtitle="The data the anomaly checks for this identity compare against." />
          {baseline.isError && !baseline.data ? (
            <div className="mt-3">
              <InlineError error={baseline.error} onRetry={baseline.refetch} label="Baseline failed to load" />
            </div>
          ) : !baseline.data ? (
            <div className="mt-3">
              <ListSkeleton rows={4} />
            </div>
          ) : (
            <dl className="mt-3 flex flex-col">
              <Row label="State">{baselineMeta(baseline.data.status).label}</Row>
              <Row label="Learning period">
                <Measured value={baseline.data.required_days}>{formatNumber(baseline.data.required_days)} days</Measured>
              </Row>
              <Row label="Data points">
                <Measured value={baseline.data.data_points}>{formatNumber(baseline.data.data_points)} events</Measured>
              </Row>
              <Row label="Baseline established">
                {baseline.data.active_since ? formatDateTime(baseline.data.active_since) : 'Still learning'}
              </Row>
              <Row label="Last model update">
                {baseline.data.updated_at ? formatRelative(baseline.data.updated_at) : <span className="text-ink-3">Not yet</span>}
              </Row>
              <Row label="Model confidence">
                <span className="text-ink-3">Not measured yet</span>
              </Row>
            </dl>
          )}
        </Panel>
      </div>

      {!empty && (
        <div className="grid gap-4 @min-[52rem]:grid-cols-2">
          <Panel prominence="quiet" className="animate-rise">
            <PanelHeader prominence="quiet" title="Values by dimension" subtitle="Each dimension's values and their share of calls." />
            {dimensions.length === 0 ? (
              <p className="mt-3 text-[12.5px] text-ink-3">No behavioural dimensions were recorded in this window.</p>
            ) : (
              <div className="mt-3 flex flex-col gap-4">
                {dimensions.map((entry) => (
                  <div key={entry.dimension}>
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

          <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
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
        </div>
      )}
    </div>
  );
}

/* ── Anomalies ────────────────────────────────────────────────────────────── */

function AnomaliesTab({ sourceType, identityName, baselineState }) {
  const [view, setView] = useState('all');
  const list = useAnomalyList({ identity: { sourceType, identityName }, review: view === 'all' ? '' : view });
  const counts = list.counts;
  const learning = baselineState.key !== 'active' && (counts?.total ?? 0) === 0;
  return (
    <Panel prominence="lead" flush className="animate-rise overflow-hidden">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-line px-4 py-3.5">
        <PanelHeader
          title="Anomalies"
          subtitle="Each time this identity behaved outside its own baseline. Say whether it is a real anomaly or a false positive."
        />
        {!learning && (
          <SegmentedControl
            label="Which anomalies"
            value={view}
            onChange={setView}
            options={[
              { value: 'all', label: counts ? `All (${formatNumber(counts.total)})` : 'All' },
              { value: 'pending', label: counts ? `Needs review (${formatNumber(counts.needs_review)})` : 'Needs review' },
              { value: 'reviewed', label: counts ? `Reviewed (${formatNumber(counts.reviewed)})` : 'Reviewed' },
            ]}
          />
        )}
      </div>
      <AnomalyList
        list={list}
        showIdentity={false}
        empty={<NoAnomalies identityPage learning={learning} review={view === 'all' ? '' : view} total={list.counts?.total} />}
      />
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
          <p className="mt-3 text-[12.5px] text-ink-3">No calls in this window.</p>
        ) : (
          <TrendChart className="mt-3" data={volume} dataKey="calls" label="API calls" height={150} />
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
            <EmptyState
              compact
              icon={Dna}
              title="No calls in this window"
              description="This identity made no recorded API calls in the selected window."
            />
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

/* ── Baseline ─────────────────────────────────────────────────────────────── */

function BaselineTab({ baseline }) {
  if (baseline.isError && !baseline.data) {
    return (
      <Panel prominence="lead">
        <ErrorState error={baseline.error} onRetry={baseline.refetch} />
      </Panel>
    );
  }
  if (!baseline.data) {
    return (
      <Panel prominence="lead">
        <DetailSkeleton rows={6} />
      </Panel>
    );
  }

  const data = baseline.data;
  const state = baselineMeta(data.status);
  const pct = learningPercent(data.built_days, data.required_days);
  const ready = data.dimensions.filter((dimension) => dimension.ready === true).length;
  const readyStated = data.dimensions.some((dimension) => dimension.ready !== null && dimension.ready !== undefined);

  if (!data.status && data.dimensions.length === 0 && data.versions.length === 0) {
    return (
      <Panel prominence="lead">
        <EmptyState
          icon={Dna}
          title="No baseline yet"
          description="Learning has not started for this identity. A baseline is built from its own calls over the learning period, and no anomaly can be raised before it exists."
        />
      </Panel>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <Panel prominence="lead" className="animate-rise">
        <PanelHeader
          prominence="lead"
          title={state.key === 'active' ? 'Baseline established' : 'Baseline still learning'}
          subtitle="A baseline is this identity's own normal, learned from its calls over the learning period."
          actions={
            <Tag tone={state.tone} size="sm" dot>
              {state.label}
            </Tag>
          }
        />
        {pct !== null && (
          <div className="mt-4 flex items-center gap-3">
            <Meter value={pct} tone={state.key === 'active' ? 'low' : 'medium'} height={6} className="min-w-0 flex-1" label="Learning progress" />
            <span data-numeric="" className="shrink-0 text-[13px] font-semibold text-ink">
              {formatNumber(data.built_days)} of {formatNumber(data.required_days)} days
            </span>
          </div>
        )}
        <dl className="mt-4 grid gap-x-6 @min-[46rem]:grid-cols-2">
          <div className="flex flex-col">
            <Row label="Days remaining">
              <Measured value={data.days_remaining}>
                {data.days_remaining === 0 ? 'None' : `${formatNumber(data.days_remaining)} days`}
              </Measured>
            </Row>
            <Row label="Learning started">{data.first_seen_date ? formatDate(`${data.first_seen_date}T00:00:00Z`) : '-'}</Row>
            <Row label="Established">{data.active_since ? formatDateTime(data.active_since) : 'Not yet'}</Row>
          </div>
          <div className="flex flex-col">
            <Row label="Data points">
              <Measured value={data.data_points}>{formatNumber(data.data_points)} events</Measured>
            </Row>
            <Row label="Last updated">{data.updated_at ? formatDateTime(data.updated_at) : '-'}</Row>
            <Row label="Model confidence">
              <span className="text-ink-3">Not measured yet</span>
            </Row>
          </div>
        </dl>
      </Panel>

      <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <Panel prominence="quiet" flush className="animate-rise overflow-hidden">
          <div className="border-b border-line px-4 py-3">
            <PanelHeader
              prominence="quiet"
              title="Dimensions"
              subtitle={
                data.dimensions.length && readyStated
                  ? `${formatNumber(ready)} of ${formatNumber(data.dimensions.length)} ready to judge departures.`
                  : `${formatNumber(data.dimensions.length)} modelled. Readiness is not stated by this profile.`
              }
            />
          </div>
          <DataGrid
            caption="Baseline dimensions"
            columns={[
              {
                key: 'dimension',
                header: 'Dimension',
                primary: true,
                width: '50%',
                cell: (row) => (
                  <span className="block truncate text-[12.5px] text-ink-2" title={row.dimension}>
                    {dimensionLabel(row.dimension)}
                  </span>
                ),
              },
              {
                key: 'ready',
                header: 'Ready',
                width: '25%',
                cell: (row) =>
                  row.ready === true ? (
                    <Tag tone="low" size="sm" dot>
                      Ready
                    </Tag>
                  ) : row.ready === false ? (
                    <Tag tone="medium" size="sm" dot>
                      Learning
                    </Tag>
                  ) : (
                    <span className="text-[11.5px] text-ink-3">Not stated</span>
                  ),
              },
              {
                key: 'values',
                header: 'Distinct values',
                width: '25%',
                cell: (row) => (
                  <span data-numeric="" className="text-[12.5px] text-ink-2">
                    {row.value_count === null || row.value_count === undefined ? '-' : formatNumber(row.value_count)}
                  </span>
                ),
              },
            ]}
            rows={data.dimensions}
            rowKey={(row) => row.dimension}
            density="compact"
            emptyState={<EmptyState compact icon={Dna} title="No dimensions yet" description="The baseline has not modelled any dimension yet." />}
          />
        </Panel>

        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
          <PanelHeader prominence="quiet" title="Published versions" subtitle="Newest first. Each retrain publishes a new version." />
          {data.versions.length === 0 ? (
            <p className="mt-3 text-[12.5px] text-ink-3">No version has been published yet.</p>
          ) : (
            <ul className="mt-3 flex flex-col">
              {data.versions.map((version) => {
                const meta = Object.entries(version.profile?.meta ?? {}).filter(
                  ([key, value]) =>
                    key !== 'version' && value !== null && ['string', 'number', 'boolean'].includes(typeof value),
                );
                const dims = version.profile?.dimensions ?? [];
                return (
                  <li key={version.version} className="border-b border-line py-2.5 last:border-0">
                    <div className="flex items-baseline justify-between gap-3">
                      <span className="text-[13px] font-semibold text-ink">Version {version.version}</span>
                      <span className="shrink-0 text-[11.5px] text-ink-3" title={formatDateTime(version.published_at)}>
                        {formatRelative(version.published_at)}
                      </span>
                    </div>
                    <p className="mt-0.5 text-[11.5px] text-ink-3">
                      {formatNumber(dims.length)} {dims.length === 1 ? 'dimension' : 'dimensions'}
                      {dims.some((d) => d.ready !== null && d.ready !== undefined)
                        ? `, ${formatNumber(dims.filter((d) => d.ready === true).length)} ready`
                        : ''}
                    </p>
                    {meta.length > 0 && (
                      <dl className="mt-1.5 flex flex-wrap gap-1.5">
                        {meta.slice(0, 8).map(([key, value]) => (
                          <div key={key} className="inline-flex max-w-full items-center gap-1 rounded-full border border-line bg-surface-2 px-2 py-0.5 text-[11px]">
                            <dt className="text-ink-3">{titleCaseEnum(key)}</dt>
                            <dd className="truncate font-medium text-ink-2">{String(value)}</dd>
                          </div>
                        ))}
                      </dl>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </Panel>
      </div>
    </div>
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

