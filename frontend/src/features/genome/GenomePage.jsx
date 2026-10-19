import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Download, SearchX, ShieldQuestion } from 'lucide-react';
import {
  fetchGenomeFleetIndex,
  fetchGenomeIdentities,
  fetchGenomeSummary,
} from '../../lib/api/genome';
import { genomeRoute } from '../../lib/identityLinks';
import { useDebouncedValue, useQuery } from '../../lib/hooks';
import { exportRowsToCsv, timestampedName } from '../../lib/csv';
import { severityMeta } from '../../lib/domain';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { DataGrid } from '../../ui/DataGrid';
import { SearchInput, Select } from '../../ui/Field';
import { AppliedFilters, FacetRail, useFacetRail } from '../../ui/FacetRail';
import { Panel, PanelHeader } from '../../ui/Panel';
import { StatStripSkeleton } from '../../ui/Skeleton';
import { MetricTile } from '../../ui/Stat';
import { EmptyState, ErrorState } from '../../ui/States';
import { SegmentedControl } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { RefreshButton, TableToolbar } from '../../ui/TableTools';
import { useToast } from '../../ui/Toast';
import { RecordBar, ResultCount, ShowFiltersButton, WorkArea } from '../../ui/WorkArea';
import { TrendChart } from '../../charts/TrendChart';
import { IdentityQuickView } from './IdentityQuickView';
import { AnomalyList, NoAnomalies, useAnomalyList } from './AnomalyReview';
import { CursorPager, dimensionLabel, RiskCell, sourceTypeLabel, useCursorPager } from './meta';

const WINDOWS = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
];

const SORTS = [
  { value: 'risk_desc', label: 'Highest risk first' },
  { value: 'last_seen_desc', label: 'Most recently seen' },
  { value: 'name_asc', label: 'Name (A-Z)' },
];

const PAGE_SIZES = [10, 25, 50, 100];

/* The fleet facets map one-to-one onto the list API's filters. Each takes a
   single value, so a group behaves as a radio set that can be cleared. */
const FACETS = [
  { key: 'source', param: 'sourceType', field: 'source_type', label: 'Source type', describe: sourceTypeLabel },
  { key: 'account', param: 'accountId', field: 'account_id', label: 'Account', describe: (value) => value },
  { key: 'region', param: 'region', field: 'region', label: 'Region', describe: (value) => value },
  { key: 'status', param: 'status', field: 'status', label: 'Lifecycle state', describe: (value) => value },
];

function windowLabel(hours) {
  if (!Number.isFinite(hours) || hours <= 0) return '-';
  if (hours < 48) return `${hours} hours`;
  return `${Math.round(hours / 24)} days`;
}

const utcDay = (date) => date.toISOString().slice(0, 10);

/** Every UTC day of the chart window, with the days that had no anomaly at 0. */
function dailySeries(rows, from) {
  const counts = new Map(rows.map((row) => [row.day, Number(row.count) || 0]));
  const end = new Date();
  const start = from ? new Date(from) : new Date(end.getTime() - 13 * 86400000);
  const out = [];
  const cursor = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth(), start.getUTCDate()));
  while (cursor <= end) {
    const day = utcDay(cursor);
    out.push({
      day,
      label: cursor.toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' }),
      anomalies: counts.get(day) ?? 0,
    });
    cursor.setUTCDate(cursor.getUTCDate() + 1);
  }
  return out;
}

const REVIEW_VIEWS = ['pending', 'reviewed', 'all'];

/**
 * NHI Genome - fleet view.
 *
 * Top: the anomalies waiting for a reviewer's verdict (real anomaly or false
 * positive). Then what departed from baselines in the window and who departed
 * most. Bottom: the identities whose baseline is established, filterable by
 * the fields the genome service filters on, each row opening a quick view.
 *
 * Identities still learning their baseline cannot raise an anomaly, so they
 * are not listed here; each one's own page shows its learning progress.
 */
export default function GenomePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { notify } = useToast();
  const { railOpen, toggleRail } = useFacetRail();
  const [selected, setSelected] = useState(null);

  const window = WINDOWS.some((entry) => entry.value === searchParams.get('window')) ? searchParams.get('window') : '24h';
  const sort = SORTS.some((entry) => entry.value === searchParams.get('sort')) ? searchParams.get('sort') : 'risk_desc';
  const pageSize = PAGE_SIZES.includes(Number(searchParams.get('size'))) ? Number(searchParams.get('size')) : 25;
  const filters = useMemo(
    () => Object.fromEntries(FACETS.map((facet) => [facet.param, searchParams.get(facet.key) || ''])),
    [searchParams],
  );

  const setParam = useCallback(
    (key, value) => {
      const next = new URLSearchParams(searchParams);
      if (value) next.set(key, value);
      else next.delete(key);
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  /* Search is typed, so it is debounced before it becomes a request; the URL
     follows it so a filtered view can be shared. */
  const [searchDraft, setSearchDraft] = useState(() => searchParams.get('q') || '');
  const search = useDebouncedValue(searchDraft, 320).trim();
  const lastSearch = useRef(search);
  useEffect(() => {
    if (lastSearch.current === search) return;
    lastSearch.current = search;
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        if (search) next.set('q', search);
        else next.delete('q');
        return next;
      },
      { replace: true },
    );
  }, [search, setSearchParams]);

  const clearAll = useCallback(() => {
    const next = new URLSearchParams();
    for (const key of ['window', 'sort', 'size']) {
      if (searchParams.get(key)) next.set(key, searchParams.get(key));
    }
    setSearchDraft('');
    setSearchParams(next, { replace: true });
  }, [searchParams, setSearchParams, setSearchDraft]);

  const summary = useQuery((signal) => fetchGenomeSummary({ window }, signal), [window]);
  const index = useQuery((signal) => fetchGenomeFleetIndex({ baseline: 'established' }, signal), []);

  const reviewView = REVIEW_VIEWS.includes(searchParams.get('review')) ? searchParams.get('review') : 'pending';
  const anomalies = useAnomalyList({ review: reviewView === 'all' ? '' : reviewView });

  const listKey = JSON.stringify([filters, search, sort, pageSize]);
  const pager = useCursorPager(listKey);
  const fleet = useQuery(
    (signal) =>
      fetchGenomeIdentities(
        { ...filters, search, sort, baseline: 'established', limit: pageSize, cursor: pager.cursor ?? undefined },
        signal,
      ),
    [listKey, pager.cursor],
  );

  const rows = useMemo(() => fleet.data?.items ?? [], [fleet.data]);
  const loading = fleet.isLoading && !fleet.data;

  const chips = useMemo(
    () =>
      [
        ...FACETS.filter((facet) => filters[facet.param]).map((facet) => ({
          key: facet.key,
          label: facet.label,
          value: facet.describe(filters[facet.param]),
        })),
        search && { key: 'q', label: 'Search', value: search },
      ].filter(Boolean),
    [filters, search],
  );

  /* Facet options and counts come from the whole fleet, read once. The counts
     describe the fleet rather than the current result, so they do not move
     while you type. Beyond 1,000 identities they would be partial, so the rail
     lists options without counts instead of undercounting. */
  const indexItems = useMemo(() => index.data?.items ?? [], [index.data]);
  const countsKnown = Boolean(index.data?.complete);
  const facetGroups = useMemo(
    () =>
      FACETS.map((facet) => {
        const active = filters[facet.param];
        const counts = new Map();
        for (const item of indexItems) {
          const value = item[facet.field];
          if (value) counts.set(value, (counts.get(value) ?? 0) + 1);
        }
        if (active && !counts.has(active)) counts.set(active, undefined);
        const options = [...counts.entries()]
          .sort((a, b) => (b[1] ?? 0) - (a[1] ?? 0) || String(a[0]).localeCompare(String(b[0])))
          .map(([value, count]) => ({
            value,
            label: facet.describe(value),
            active: active === value,
            count: countsKnown ? count : undefined,
          }));
        return {
          key: facet.key,
          label: facet.label,
          options,
          onToggle: (value) => setParam(facet.key, active === value ? '' : value),
          note:
            facet.key === 'status'
              ? 'The lifecycle state discovery recorded for the workload, as the cloud reports it.'
              : undefined,
        };
      }),
    [filters, indexItems, countsKnown, setParam],
  );

  /* An exact total is only known when the whole fleet is indexed and the
     filters are ones the index can apply the same way the service does. */
  const knownTotal = useMemo(() => {
    if (!countsKnown || search) return null;
    return indexItems.filter((item) =>
      FACETS.every((facet) => {
        const want = filters[facet.param];
        return !want || String(item[facet.field] ?? '').toLowerCase() === want.toLowerCase();
      }),
    ).length;
  }, [countsKnown, search, indexItems, filters]);

  const onExport = useCallback(() => {
    exportRowsToCsv({
      filename: timestampedName('nhi-genome-fleet'),
      columns: [
        { header: 'Identity', value: (row) => row.display_name },
        { header: 'ARN', value: (row) => row.identity_name },
        { header: 'Source type', value: (row) => row.source_type },
        { header: 'Account', value: (row) => row.account_id },
        { header: 'Region', value: (row) => row.region ?? '' },
        { header: 'Lifecycle state', value: (row) => row.status ?? '' },
        { header: 'Risk score', value: (row) => row.risk?.score ?? '' },
        { header: 'Severity', value: (row) => row.risk?.severity ?? '' },
        { header: 'Open anomalies', value: (row) => row.open_anomaly_count ?? '' },
        { header: 'Last seen (UTC)', value: (row) => row.last_seen ?? '' },
      ],
      rows,
    });
    notify({
      title: 'Exported this page',
      description: `${formatNumber(rows.length)} identities written, in the order shown.`,
      variant: 'success',
    });
  }, [rows, notify]);

  const columns = [
    {
      key: 'identity',
      header: 'Identity',
      primary: true,
      width: '34%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate text-[13px] font-semibold text-ink" title={row.identity_name}>
            {row.display_name || row.identity_name}
          </span>
          <span className="block truncate text-[11.5px] text-ink-3">
            {sourceTypeLabel(row.source_type)}
            {row.status ? ` · ${row.status}` : ''}
          </span>
        </span>
      ),
    },
    {
      key: 'scope',
      header: 'Account / region',
      width: '18%',
      priority: 'wide',
      cell: (row) => (
        <span className="block min-w-0">
          <span data-numeric="" className="block truncate font-mono text-[12px] text-ink-2">
            {row.account_id || '-'}
          </span>
          <span className="block truncate text-[11.5px] text-ink-3">{row.region || 'Global'}</span>
        </span>
      ),
    },
    {
      key: 'risk',
      header: 'Latest risk',
      width: '16%',
      cell: (row) => <RiskCell score={row.risk?.score} severity={row.risk?.severity} />,
    },
    {
      key: 'anomalies',
      header: 'Open anomalies',
      width: '14%',
      cell: (row) =>
        row.open_anomaly_count === null || row.open_anomaly_count === undefined ? (
          <span className="text-[12px] text-ink-3" title="Not measured yet">
            -
          </span>
        ) : (
          <span
            data-numeric=""
            className={row.open_anomaly_count > 0 ? 'text-[13px] font-semibold text-high' : 'text-[12.5px] text-ink-3'}
          >
            {formatNumber(row.open_anomaly_count)}
          </span>
        ),
    },
    {
      key: 'seen',
      header: 'Last seen',
      width: '13%',
      cell: (row) =>
        row.last_seen ? (
          <span className="block whitespace-nowrap text-[12.5px] text-ink-2" title={formatDateTime(row.last_seen)}>
            {formatRelative(row.last_seen)}
          </span>
        ) : (
          <span className="text-[12px] text-ink-3">No calls seen</span>
        ),
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="NHI Genome"
        lede="Each workload identity first learns its own normal behaviour. Once that baseline is established, anything it does outside it is raised here as an anomaly for you to review."
        actions={
          <>
            <SegmentedControl
              label="Detection window"
              options={WINDOWS}
              value={window}
              onChange={(value) => setParam('window', value === '24h' ? '' : value)}
            />
            <Button variant="secondary" icon={Download} onClick={onExport} disabled={rows.length === 0}>
              Export page
            </Button>
          </>
        }
      />

      <ReviewQueue
        list={anomalies}
        view={reviewView}
        onView={(value) => setParam('review', value === 'pending' ? '' : value)}
      />

      {summary.isError && !summary.data ? (
        <Panel prominence="quiet">
          <ErrorState error={summary.error} onRetry={summary.refetch} title="The behaviour summary failed to load" />
        </Panel>
      ) : summary.isLoading && !summary.data ? (
        <StatStripSkeleton count={4} />
      ) : (
        <SummarySection summary={summary.data} window={window} />
      )}

      <WorkArea
        railOpen={railOpen}
        rail={
          <FacetRail
            groups={facetGroups}
            appliedCount={chips.length}
            onClearAll={clearAll}
            onClose={toggleRail}
            mobileTitle="Filter identities"
          />
        }
      >
        <Panel prominence="lead" flush className="animate-rise overflow-hidden">
          <RecordBar
            trailing={
              <TableToolbar>
                <SearchInput
                  value={searchDraft}
                  onChange={setSearchDraft}
                  placeholder="Search name, ARN, service, role..."
                  size="sm"
                  className="w-full sm:w-64"
                  aria-label="Search monitored identities"
                />
                <Select
                  size="sm"
                  value={sort}
                  onChange={(event) => setParam('sort', event.target.value === 'risk_desc' ? '' : event.target.value)}
                  options={SORTS}
                  aria-label="Sort identities"
                  className="w-44"
                />
                {!railOpen && <ShowFiltersButton onClick={toggleRail} appliedCount={chips.length} />}
                <RefreshButton onRefresh={fleet.refetch} refreshing={fleet.isRefreshing} label="Refresh fleet" />
              </TableToolbar>
            }
          >
            {knownTotal !== null ? (
              <ResultCount
                shown={formatNumber(knownTotal)}
                total={formatNumber(indexItems.length)}
                unit="identities with a baseline"
                filtered={chips.length > 0}
                loading={loading}
              />
            ) : (
              <ResultCount
                shown={`${formatNumber(rows.length)}${fleet.data?.nextCursor ? '+' : ''}`}
                unit={search ? 'matching identities' : 'identities with a baseline'}
                loading={loading}
              />
            )}
            <span className="hidden text-[11.5px] text-ink-3 xl:inline">Open a row for a quick view</span>
          </RecordBar>

          <AppliedFilters
            filters={chips}
            onRemove={(key) => {
              if (key === 'q') setSearchDraft('');
              else setParam(key, '');
            }}
            onClearAll={clearAll}
          />

          {fleet.isError && !fleet.data ? (
            <ErrorState error={fleet.error} onRetry={fleet.refetch} />
          ) : (
            <DataGrid
              caption="Identities with an established baseline"
              columns={columns}
              rows={rows}
              rowKey={(row) => row.id}
              loading={loading}
              refreshing={fleet.isRefreshing}
              onRowClick={setSelected}
              density="comfortable"
              skeletonRows={8}
              emptyState={
                chips.length > 0 ? (
                  <EmptyState
                    icon={SearchX}
                    title="No identity matches"
                    description="No identity with an established baseline matches the current search and filters."
                    action={
                      <Button variant="secondary" size="sm" onClick={clearAll}>
                        Clear filters
                      </Button>
                    }
                  />
                ) : (
                  <EmptyState
                    icon={ShieldQuestion}
                    title="No baseline is established yet"
                    description="Workload identities (compute, functions, pipelines) learn their normal behaviour first. They appear here once their baseline is established; until then each identity's own page shows its learning progress."
                  />
                )
              }
            />
          )}

          {!loading && !fleet.isError && (pager.page > 1 || fleet.data?.nextCursor) && (
            <CursorPager
              page={pager.page}
              shown={rows.length}
              pageSize={pageSize}
              hasNext={Boolean(fleet.data?.nextCursor)}
              onPrev={pager.prev}
              onNext={() => pager.next(fleet.data?.nextCursor)}
              onPageSizeChange={(next) => setParam('size', next === 25 ? '' : String(next))}
              unit="identities"
            />
          )}
        </Panel>
      </WorkArea>

      <IdentityQuickView identity={selected} onClose={() => setSelected(null)} />
    </div>
  );
}

/* ── Summary ──────────────────────────────────────────────────────────────── */

function SummarySection({ summary, window }) {
  const { cards } = summary;
  const established = cards.baselines_established;
  const baselinesKnown = established !== null && established !== undefined;
  /* The alert-backed cards and panels share one store. When the open-anomaly
     count is null that store could not be read, and an empty chart would
     claim "no anomalies" rather than "not known". */
  const alertsKnown = cards.open_anomalies !== null && cards.open_anomalies !== undefined;
  const cardWindow = windowLabel(cards.window_hours);
  const chartWindow = window === '24h' ? '14 days' : window === '7d' ? '7 days' : '30 days';
  const trend = useMemo(() => dailySeries(summary.anomaliesByDay, summary.chartsFrom), [summary]);
  const totalDepartures = summary.departuresByDimension.reduce((sum, row) => sum + (Number(row.count) || 0), 0);
  const notReported = 'Not reported yet for this scope';
  const topScore = summary.mostAnomalous[0];

  return (
    <>
      <div className="grid gap-3 @min-[30rem]:grid-cols-3">
        <MetricTile
          label="Open anomalies"
          value={cards.open_anomalies}
          tone="high"
          caption={alertsKnown ? `Raised in the last ${cardWindow} and still open` : notReported}
          className="animate-rise"
        />
        <MetricTile
          label="Baselines established"
          value={established}
          tone="info"
          caption={baselinesKnown ? 'Identities whose normal behaviour is learned and watched' : notReported}
          className="animate-rise"
          data-stagger=""
          style={{ '--stagger': 1 }}
        />
        <MetricTile
          label="Highest risk score"
          value={topScore ? topScore.risk_score : null}
          unit={topScore ? '/ 100' : undefined}
          tone="medium"
          caption={topScore ? `${topScore.display_name || topScore.identity_name}, the identity departing most` : 'No identity has a risk score yet'}
          className="animate-rise"
          data-stagger=""
          style={{ '--stagger': 2 }}
        />
      </div>

      <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
        <Panel prominence="quiet" className="animate-rise">
          <PanelHeader prominence="quiet" title="Anomalies raised per day" subtitle={`Last ${chartWindow}, by the day they were raised (UTC), whatever their verdict.`} />
          {alertsKnown ? (
            <TrendChart className="mt-3" data={trend} dataKey="anomalies" label="Anomalies raised" height={150} />
          ) : (
            <NotReported>Anomaly history has not been reported yet.</NotReported>
          )}
        </Panel>

        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
          <PanelHeader
            prominence="quiet"
            title="What changed"
            subtitle={`Open anomalies raised in the last ${chartWindow}, by the kind of behaviour that departed from its baseline.`}
          />
          {!alertsKnown ? (
            <NotReported>Departures have not been reported yet.</NotReported>
          ) : summary.departuresByDimension.length === 0 ? (
            <p className="mt-3 text-[12.5px] text-ink-3">No open anomaly in this window.</p>
          ) : (
            <ul className="mt-3 flex flex-col">
              {summary.departuresByDimension.map((entry) => (
                <li key={entry.dimension} className="border-b border-line py-2 last:border-0">
                  <div className="flex items-center gap-3">
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[12.5px] text-ink-2" title={entry.dimension}>
                        {dimensionLabel(entry.dimension)}
                      </span>
                      <span className="mt-1 block h-1.5 overflow-hidden rounded-full bg-track">
                        <span
                          className="block h-full rounded-full"
                          style={{
                            width: `${totalDepartures ? (entry.count / totalDepartures) * 100 : 0}%`,
                            background: 'var(--t-series-1)',
                          }}
                        />
                      </span>
                    </span>
                    <span data-numeric="" className="shrink-0 text-[12.5px] font-semibold text-ink">
                      {formatNumber(entry.count)}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Panel>
      </div>

      <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        <Panel prominence="quiet" flush className="animate-rise overflow-hidden">
          <div className="border-b border-line px-4 py-3">
            <PanelHeader
              prominence="quiet"
              title="Most anomalous identities"
              subtitle="Highest latest risk score. Anomalous calls are counted in the scoring window."
            />
          </div>
          {summary.mostAnomalous.length === 0 ? (
            <p className="px-4 py-6 text-[12.5px] text-ink-3">No identity has a risk score yet.</p>
          ) : (
            <ul className="divide-y divide-line">
              {summary.mostAnomalous.map((identity) => {
                const severity = severityMeta(identity.severity);
                return (
                  <li key={identity.id}>
                    <Link
                      to={genomeRoute(identity.source_type, identity.identity_name)}
                      className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-surface-3"
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[13px] font-medium text-ink" title={identity.identity_name}>
                          {identity.display_name || identity.identity_name}
                        </span>
                        <span className="block truncate text-[11.5px] text-ink-3">
                          {sourceTypeLabel(identity.source_type)} · {identity.account_id}
                          {identity.region ? ` · ${identity.region}` : ''}
                        </span>
                      </span>
                      <span
                        data-numeric=""
                        className="hidden shrink-0 text-[12px] text-ink-3 sm:inline"
                        title={`${formatNumber(identity.anomalous_event_count)} anomalous of ${formatNumber(identity.event_count)} calls scored`}
                      >
                        {identity.event_count
                          ? `${formatNumber(identity.anomalous_event_count)} of ${formatNumber(identity.event_count)} calls anomalous`
                          : 'No calls scored'}
                      </span>
                      <span data-numeric="" className="w-10 shrink-0 text-right text-[13px] font-semibold text-ink">
                        {formatNumber(identity.risk_score)}
                      </span>
                      {identity.severity && (
                        <Tag tone={severity.tone} size="sm" dot>
                          {severity.label}
                        </Tag>
                      )}
                    </Link>
                  </li>
                );
              })}
            </ul>
          )}
        </Panel>

        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
          <PanelHeader prominence="quiet" title="Recently baselined" />
          {summary.recentlyBaselined.length === 0 ? (
            <p className="mt-2 text-[12.5px] text-ink-3">No baseline has been established or republished yet.</p>
          ) : (
            <ul className="mt-2 flex flex-col gap-1.5">
              {summary.recentlyBaselined.map((entry) => (
                <li key={entry.id} className="flex items-center gap-2">
                  <Link
                    to={genomeRoute(entry.source_type, entry.identity_name)}
                    className="min-w-0 flex-1 truncate text-[12px] text-ink-2 hover:text-ink hover:underline"
                    title={entry.identity_name}
                  >
                    {entry.display_name || entry.identity_name}
                  </Link>
                  <span className="shrink-0 text-[11px] text-ink-3" title={formatDateTime(entry.occurred_at)}>
                    {formatRelative(entry.occurred_at)}
                  </span>
                  {entry.event === 'baseline_version_published' ? (
                    <Tag tone="info" size="sm">
                      {entry.version ? `Version ${entry.version}` : 'New version'}
                    </Tag>
                  ) : (
                    <Tag tone="low" size="sm">
                      Established
                    </Tag>
                  )}
                </li>
              ))}
            </ul>
          )}
        </Panel>
      </div>
    </>
  );
}

function NotReported({ children }) {
  return <p className="mt-3 rounded-[var(--radius-control)] border border-dashed border-line px-3 py-6 text-center text-[12.5px] text-ink-3">{children}</p>;
}

/* ── Review queue ─────────────────────────────────────────────────────────── */

function ReviewQueue({ list, view, onView }) {
  const counts = list.counts;
  const options = [
    { value: 'pending', label: counts ? `Needs review (${formatNumber(counts.needs_review)})` : 'Needs review' },
    { value: 'reviewed', label: counts ? `Reviewed (${formatNumber(counts.reviewed)})` : 'Reviewed' },
    { value: 'all', label: counts ? `All (${formatNumber(counts.total)})` : 'All' },
  ];
  return (
    <Panel prominence="lead" flush className="animate-rise overflow-hidden">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-line px-4 py-3.5">
        <PanelHeader
          title="Anomalies to review"
          subtitle={
            counts && counts.reviewed > 0
              ? `Say whether each one is a real anomaly or a false positive. So far ${formatNumber(counts.real_anomaly)} confirmed real and ${formatNumber(counts.false_positive)} false positive.`
              : 'Say whether each one is a real anomaly or a false positive. Your verdict is recorded with your name and reason.'
          }
        />
        <SegmentedControl label="Which anomalies" options={options} value={view} onChange={onView} />
      </div>
      <AnomalyList list={list} empty={<NoAnomalies review={view === 'all' ? '' : view} total={list.counts?.total} />} />
    </Panel>
  );
}
