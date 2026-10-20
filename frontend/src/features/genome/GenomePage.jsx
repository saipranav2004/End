import { useCallback, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Download, SearchX, ShieldQuestion } from 'lucide-react';
import { fetchGenomeIdentity, fetchGenomeSummary, fetchMonitoredFleet } from '../../lib/api/genome';
import { genomeRoute } from '../../lib/identityLinks';
import { useDebouncedValue, useQuery } from '../../lib/hooks';
import { severityMeta } from '../../lib/domain';
import { exportRowsToCsv, timestampedName } from '../../lib/csv';
import { NEUTRAL_SERIES, SERIES_TOKENS } from '../../lib/domain';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { DataGrid } from '../../ui/DataGrid';
import { SearchInput, Select } from '../../ui/Field';
import { Pagination } from '../../ui/Pagination';
import { Panel, PanelHeader } from '../../ui/Panel';
import { ChartSkeleton, StatStripSkeleton } from '../../ui/Skeleton';
import { MetricTile } from '../../ui/Stat';
import { EmptyState, ErrorState } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { SegmentedControl } from '../../ui/Tabs';
import { RefreshButton, TableToolbar } from '../../ui/TableTools';
import { useToast } from '../../ui/Toast';
import { RecordBar, ResultCount } from '../../ui/WorkArea';
import { CompositionDonut } from '../../charts/CompositionDonut';
import { TrendChart } from '../../charts/TrendChart';
import { dimensionLabel, sourceTypeLabel } from './meta';

const PAGE_SIZES = [10, 25, 50, 100];
const WINDOWS = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
];
const WINDOW_WORDS = { '24h': '24 hours', '7d': '7 days', '30d': '30 days' };
const DAY_MS = 86400000;
const DONUT_SLICES = 6;

/**
 * NHI Genome - fleet view.
 *
 * Every workload identity (compute, functions, tasks, pipelines) first learns
 * its own normal behaviour from its calls. The top says how far the fleet is
 * (watched, established, still learning) and what was baselined lately; the
 * list below is every monitored identity in scope, each opening its genome,
 * activity and timeline.
 *
 * The list is the discovery inventory's workload identities: the genome
 * service has no list of its own, and the inventory is the one source of
 * which identities exist in the selected accounts and regions.
 */
export default function GenomePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const { notify } = useToast();

  const sourceType = searchParams.get('source') || '';
  const pageSize = PAGE_SIZES.includes(Number(searchParams.get('size'))) ? Number(searchParams.get('size')) : 25;
  const page = Math.max(1, Number(searchParams.get('page')) || 1);

  const setParams = useCallback(
    (changes) => {
      const next = new URLSearchParams(searchParams);
      for (const [key, value] of Object.entries(changes)) {
        if (value) next.set(key, String(value));
        else next.delete(key);
      }
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const [searchDraft, setSearchDraft] = useState(() => searchParams.get('q') || '');
  const search = useDebouncedValue(searchDraft, 250).trim().toLowerCase();

  const window = WINDOWS.some((entry) => entry.value === searchParams.get('window')) ? searchParams.get('window') : '24h';
  /* With 24h the service reads the cards over 24 hours and the charts over 14
     days; a wider window applies to both. */
  const chartDays = window === '30d' ? 30 : window === '7d' ? 7 : 14;
  const summary = useQuery((signal) => fetchGenomeSummary({ window }, signal), [window]);
  const fleet = useQuery((signal) => fetchMonitoredFleet(signal), []);
  const all = useMemo(() => fleet.data?.items ?? [], [fleet.data]);

  const filtered = useMemo(
    () =>
      all.filter(
        (row) =>
          (!sourceType || row.sourceType === sourceType) &&
          (!search ||
            [row.name, row.arn, row.account_id, row.region].some((value) => String(value ?? '').toLowerCase().includes(search))),
      ),
    [all, sourceType, search],
  );
  const lastPage = Math.max(1, Math.ceil(filtered.length / pageSize));
  const current = Math.min(page, lastPage);
  const rows = filtered.slice((current - 1) * pageSize, current * pageSize);
  const rowSummaries = useRowSummaries(rows);

  /* Workload mix of the watched fleet, largest first; the tail folds into
     "Other" so the ring stays readable. */
  const mix = useMemo(() => {
    const counts = new Map();
    for (const row of all) counts.set(row.sourceType, (counts.get(row.sourceType) ?? 0) + 1);
    const sorted = [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
    const head = sorted.slice(0, DONUT_SLICES).map(([key, value], index) => ({
      key,
      label: sourceTypeLabel(key),
      value,
      color: SERIES_TOKENS[index % SERIES_TOKENS.length],
    }));
    const rest = sorted.slice(DONUT_SLICES).reduce((sum, [, value]) => sum + value, 0);
    return rest ? [...head, { key: 'other', label: 'Other', value: rest, color: NEUTRAL_SERIES }] : head;
  }, [all]);

  const typeOptions = useMemo(
    () => [{ value: '', label: 'All workload types' }, ...mix.filter((m) => m.key !== 'other').map((m) => ({ value: m.key, label: m.label }))],
    [mix],
  );

  const onExport = useCallback(() => {
    exportRowsToCsv({
      filename: timestampedName('nhi-genome-fleet'),
      columns: [
        { header: 'Identity', value: (row) => row.name },
        { header: 'ARN', value: (row) => row.identityName },
        { header: 'Workload type', value: (row) => sourceTypeLabel(row.sourceType) },
        { header: 'Account', value: (row) => row.account_id },
        { header: 'Region', value: (row) => row.region ?? '' },
        { header: 'Status', value: (row) => row.status ?? '' },
        { header: 'Last seen in CloudTrail (UTC)', value: (row) => row.last_activity_at ?? '' },
      ],
      rows: filtered,
    });
    notify({ title: 'Exported', description: `${formatNumber(filtered.length)} identities written.`, variant: 'success' });
  }, [filtered, notify]);

  const columns = [
    {
      key: 'identity',
      header: 'Identity',
      primary: true,
      width: '30%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate text-[13px] font-semibold text-ink" title={row.identityName}>
            {row.name || row.identityName}
          </span>
          <span className="block truncate text-[11.5px] text-ink-3">
            {sourceTypeLabel(row.sourceType)}
            {row.status && String(row.status).toLowerCase() !== 'active' ? ` · ${row.status}` : ''}
          </span>
        </span>
      ),
    },
    {
      key: 'scope',
      header: 'Account / region',
      width: '17%',
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
      key: 'baseline',
      header: 'Baseline',
      width: '17%',
      cell: (row) => <RowCell summary={rowSummaries.data} id={row.id} render={baselineCell} />,
    },
    {
      key: 'open',
      header: 'Open departures',
      width: '12%',
      cell: (row) => <RowCell summary={rowSummaries.data} id={row.id} render={openCell} />,
    },
    {
      key: 'calls',
      header: 'Calls · 14 days',
      width: '11%',
      priority: 'wide',
      cell: (row) => <RowCell summary={rowSummaries.data} id={row.id} render={callsCell} />,
    },
    {
      key: 'last',
      header: 'Last call',
      width: '13%',
      cell: (row) => <RowCell summary={rowSummaries.data} id={row.id} render={lastCallCell} />,
    },
  ];

  const filteredView = Boolean(sourceType || search);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="NHI Genome"
        lede="Each workload identity learns its own normal behaviour from its calls. Open one to see its behavioural fingerprint, its calls and how its baseline got where it is."
        actions={
          <>
            <SegmentedControl
              label="Detection window"
              options={WINDOWS}
              value={window}
              onChange={(value) => setParams({ window: value === '24h' ? '' : value })}
            />
            <Button variant="secondary" icon={Download} onClick={onExport} disabled={filtered.length === 0}>
              Export
            </Button>
          </>
        }
      />

      {summary.isError && !summary.data ? (
        <Panel prominence="quiet">
          <ErrorState error={summary.error} onRetry={summary.refetch} title="The behaviour summary failed to load" />
        </Panel>
      ) : summary.isLoading && !summary.data ? (
        <StatStripSkeleton count={4} />
      ) : (
        <>
          <SummaryCards cards={summary.data.cards} window={window} />
          <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
            <AnomaliesPerDay days={summary.data.anomaliesByDay} span={chartDays} generatedAt={summary.data.generatedAt} />
            <DeparturesByType departures={summary.data.departures} span={chartDays} />
          </div>
          {summary.data.mostAnomalous.length > 0 && <MostAnomalous entries={summary.data.mostAnomalous} />}
        </>
      )}

      <div className="grid gap-4 @min-[52rem]:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
        <Panel prominence="quiet" className="animate-rise">
          <PanelHeader
            prominence="quiet"
            title="What is watched"
            subtitle="Monitored identities in your scope, by workload type. Select a type to list it."
          />
          {fleet.isLoading && !fleet.data ? (
            <ChartSkeleton height={184} />
          ) : mix.length === 0 ? (
            <p className="mt-3 text-[12.5px] text-ink-3">No workload identity is in your scope yet.</p>
          ) : (
            <CompositionDonut
              className="mt-4"
              data={mix}
              centerLabel="Watched"
              onSelect={(item) => item.key !== 'other' && setParams({ source: item.key === sourceType ? '' : item.key, page: '' })}
            />
          )}
        </Panel>
        <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
          <PanelHeader prominence="quiet" title="Recently baselined" subtitle="Baselines established or republished, newest first." />
          <RecentlyBaselined entries={summary.data?.recentlyBaselined} loading={summary.isLoading && !summary.data} />
        </Panel>
      </div>

      <Panel prominence="lead" flush className="animate-rise overflow-hidden">
        <RecordBar
          trailing={
            <TableToolbar className="w-full flex-wrap sm:w-auto">
              <SearchInput
                value={searchDraft}
                onChange={(value) => {
                  setSearchDraft(value);
                  if (page !== 1) setParams({ page: '' });
                }}
                placeholder="Search name, ARN, account, region..."
                size="sm"
                className="w-full sm:w-64"
                aria-label="Search monitored identities"
              />
              <Select
                size="sm"
                value={sourceType}
                onChange={(event) => setParams({ source: event.target.value, page: '' })}
                options={typeOptions}
                aria-label="Workload type"
                className="min-w-0 flex-1 sm:w-48 sm:flex-none"
              />
              <RefreshButton onRefresh={fleet.refetch} refreshing={fleet.isRefreshing} label="Refresh fleet" />
            </TableToolbar>
          }
        >
          <ResultCount
            shown={formatNumber(filtered.length)}
            total={formatNumber(all.length)}
            unit={fleet.data && !fleet.data.complete ? 'monitored identities (first 1,000)' : 'monitored identities'}
            filtered={filteredView}
            loading={fleet.isLoading && !fleet.data}
          />
        </RecordBar>

        {fleet.isError && !fleet.data ? (
          <ErrorState error={fleet.error} onRetry={fleet.refetch} />
        ) : (
          <DataGrid
            caption="Monitored identities"
            columns={columns}
            rows={rows}
            rowKey={(row) => row.id}
            loading={fleet.isLoading && !fleet.data}
            refreshing={fleet.isRefreshing}
            onRowClick={(row) => navigate(genomeRoute(row.sourceType, row.identityName))}
            density="comfortable"
            skeletonRows={8}
            emptyState={
              filteredView ? (
                <EmptyState
                  icon={SearchX}
                  title="No identity matches"
                  description="No monitored identity matches the search and workload type."
                  action={
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        setSearchDraft('');
                        setParams({ source: '', page: '' });
                      }}
                    >
                      Clear filters
                    </Button>
                  }
                />
              ) : (
                <EmptyState
                  icon={ShieldQuestion}
                  title="No workload identity in your scope"
                  description="The genome watches compute, functions, tasks and pipelines. None was discovered in the accounts and regions you selected; widen the scope or run a discovery scan."
                />
              )
            }
          />
        )}

        {filtered.length > PAGE_SIZES[0] && (
          <Pagination
            page={current}
            pageSize={pageSize}
            total={filtered.length}
            onPageChange={(next) => setParams({ page: next === 1 ? '' : next })}
            onPageSizeChange={(next) => setParams({ size: next === 25 ? '' : next, page: '' })}
            unit="identities"
          />
        )}
      </Panel>
    </div>
  );
}

function SummaryCards({ cards, window }) {
  const watched = cards.identities_watched;
  const established = cards.baselines_established;
  const open = cards.open_anomalies;
  const coverage = Number.isFinite(watched) && watched > 0 && Number.isFinite(established) ? (established / watched) * 100 : null;
  // Critical anomalies is null until the pipeline scores severities; the tile is shown only once it is measured.
  const critical = cards.critical_anomalies;
  const tiles = [
    <MetricTile
      key="established"
      label="Baselines established"
      value={established}
      tone="low"
      meter={coverage}
      meterLabel="Share of watched identities with an established baseline"
      caption={Number.isFinite(watched) ? `of ${formatNumber(watched)} identities watched` : 'Normal behaviour learned and watched'}
    />,
    <MetricTile
      key="learning"
      label="No baseline yet"
      value={cards.still_learning}
      tone="medium"
      caption="Learning, or not started; nothing can be flagged for them yet"
    />,
    <MetricTile
      key="open"
      label={`New anomalies · ${window}`}
      value={open}
      tone={open > 0 ? 'critical' : 'neutral'}
      caption={`Raised in the last ${WINDOW_WORDS[window]} and still open; older open ones are in the list below`}
    />,
    critical !== null && critical !== undefined && (
      <MetricTile key="critical" label="Critical anomalies" value={critical} tone="critical" caption="Highest-confidence departures" />
    ),
  ].filter(Boolean);
  return (
    <div className={`grid gap-3 @min-[30rem]:grid-cols-2 ${tiles.length === 4 ? '@min-[54rem]:grid-cols-4' : '@min-[54rem]:grid-cols-3'}`}>
      {tiles.map((tile, index) => (
        <div key={tile.key} className="animate-rise" data-stagger="" style={{ '--stagger': index }}>
          {tile}
        </div>
      ))}
    </div>
  );
}

/** Anomalies raised per UTC day, open or resolved. Days without one are a measured zero. */
function AnomaliesPerDay({ days, span, generatedAt }) {
  const data = useMemo(() => {
    const counts = new Map(days.map((entry) => [String(entry.day).slice(0, 10), Number(entry.count) || 0]));
    // The server's clock, not the browser's: the days are UTC days it counted.
    const end = Date.parse(generatedAt ?? '') || Date.parse(days.at(-1)?.day ?? '') || 0;
    const today = Math.floor(end / DAY_MS) * DAY_MS;
    return Array.from({ length: span }, (_, index) => {
      const iso = new Date(today - (span - 1 - index) * DAY_MS).toISOString().slice(0, 10);
      const label = new Date(`${iso}T00:00:00Z`).toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' });
      return { label, anomalies: counts.get(iso) ?? 0 };
    });
  }, [days, span, generatedAt]);
  const total = data.reduce((sum, entry) => sum + entry.anomalies, 0);
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
      <PanelHeader prominence="quiet" title="Anomalies per day" />
      {/* Bars, not a smoothed area: these are whole counts per day, and a curve would draw anomalies between days. */}
      <TrendChart className="mt-3" variant="bar" data={data} dataKey="anomalies" label="Anomalies raised" height={132} />
      <p className="mt-1 text-[11.5px] text-ink-3">
        Last {span} days, open or resolved, UTC · {formatNumber(total)} in all.
      </p>
    </Panel>
  );
}

/** Open departures grouped by the dimension that departed. */
function DeparturesByType({ departures, span }) {
  const total = departures.reduce((sum, entry) => sum + (Number(entry.count) || 0), 0);
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 2 }}>
      <PanelHeader prominence="quiet" title="Departures by type" subtitle={`Open anomalies raised in the last ${span} days, by what departed from the baseline.`} />
      {departures.length === 0 ? (
        <p className="mt-3 rounded-[var(--radius-control)] border border-dashed border-line px-3 py-6 text-center text-[12.5px] text-ink-3">
          No open departure in this window.
        </p>
      ) : (
        <ul className="mt-3 flex flex-col">
          {departures.map((entry) => (
            <li key={entry.dimension} className="border-b border-line py-2 last:border-0">
              <div className="flex items-center gap-3">
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[12.5px] text-ink">{dimensionLabel(entry.dimension)}</span>
                  <span className="mt-1 block h-1.5 overflow-hidden rounded-full bg-track">
                    <span
                      className="block h-full rounded-full"
                      style={{ width: `${total ? (entry.count / total) * 100 : 0}%`, background: 'var(--t-series-1)' }}
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
  );
}

/** The identities the pipeline scores riskiest; shown only when it has scored some. */
function MostAnomalous({ entries }) {
  return (
    <Panel prominence="quiet" className="animate-rise">
      <PanelHeader prominence="quiet" title="Most anomalous identities" subtitle="Highest risk score from the latest scoring window first." />
      <ul className="mt-2 flex flex-col divide-y divide-line">
        {entries.map((entry) => {
          const severity = severityMeta(entry.severity);
          return (
            <li key={entry.id} className="flex items-center gap-3 py-2">
              <span className="min-w-0 flex-1">
                <Link
                  to={genomeRoute(entry.source_type, entry.identity_name)}
                  className="block truncate text-[12.5px] font-medium text-ink hover:text-brand"
                  title={entry.identity_name}
                >
                  {entry.display_name || entry.identity_name}
                </Link>
                <span className="block truncate text-[11px] text-ink-3">
                  {sourceTypeLabel(entry.source_type)}
                  {entry.region ? ` · ${entry.region}` : ''}
                </span>
              </span>
              {Number(entry.anomalous_event_count) > 0 && (
                <span className="shrink-0 text-[11.5px] text-ink-3">{formatNumber(entry.anomalous_event_count)} anomalous calls</span>
              )}
              <span data-numeric="" className="w-8 shrink-0 text-right text-[13px] font-semibold text-ink">
                {entry.risk_score}
              </span>
              <Tag tone={severity.tone} size="sm" dot>
                {severity.label}
              </Tag>
            </li>
          );
        })}
      </ul>
    </Panel>
  );
}

function RecentlyBaselined({ entries, loading }) {
  if (loading) return <ChartSkeleton height={120} />;
  if (!entries?.length) {
    return (
      <p className="mt-3 rounded-[var(--radius-control)] border border-dashed border-line px-3 py-6 text-center text-[12.5px] text-ink-3">
        No baseline has been established yet. Identities appear here as their learning period completes.
      </p>
    );
  }
  return (
    <ul className="mt-2 flex flex-col divide-y divide-line">
      {entries.map((entry) => (
        <li key={`${entry.id}-${entry.occurred_at}`} className="flex items-center gap-2 py-2">
          <span className="min-w-0 flex-1">
            <Link
              to={genomeRoute(entry.source_type, entry.identity_name)}
              className="block truncate text-[12.5px] font-medium text-ink hover:text-brand"
              title={entry.identity_name}
            >
              {entry.display_name || entry.identity_name}
            </Link>
            <span className="block truncate text-[11px] text-ink-3">
              {sourceTypeLabel(entry.source_type)}
              {entry.region ? ` · ${entry.region}` : ''}
            </span>
          </span>
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
  );
}

/**
 * The genome's own state for the rows on screen: one identity summary each,
 * six at a time. A row whose summary fails shows "-" rather than failing the
 * table. ponytail: one request per visible row; a batch endpoint is the
 * upgrade if pages of 100 become common.
 */
function useRowSummaries(rows) {
  const key = rows.map((row) => row.id).join(',');
  return useQuery(
    async (signal) => {
      const out = new Map();
      let next = 0;
      const work = async () => {
        while (next < rows.length) {
          const row = rows[next++];
          try {
            out.set(row.id, await fetchGenomeIdentity(row.sourceType, row.identityName, {}, signal));
          } catch (error) {
            if (signal?.aborted) throw error;
            out.set(row.id, null);
          }
        }
      };
      await Promise.all(Array.from({ length: Math.min(6, rows.length) }, work));
      return out;
    },
    [key],
  );
}

function RowCell({ summary, id, render }) {
  if (!summary) return <span className="text-[12px] text-ink-3">…</span>;
  const entry = summary.get(id);
  return entry ? render(entry) : <span className="text-[12px] text-ink-3">-</span>;
}

function baselineCell({ baseline }) {
  if (!baseline) return <span className="text-[12px] text-ink-3">Not started</span>;
  if (String(baseline.status).toLowerCase() === 'active') {
    return (
      <Tag tone="low" size="sm" dot>
        Established
      </Tag>
    );
  }
  return (
    <span className="flex flex-col items-start gap-0.5">
      <Tag tone="medium" size="sm" dot>
        Learning
      </Tag>
      <span className="text-[11px] text-ink-3">
        {formatNumber(baseline.built_days)} of {formatNumber(baseline.required_days)} days
      </span>
    </span>
  );
}

function openCell({ openAnomalies }) {
  if (openAnomalies === null || openAnomalies === undefined) return <span className="text-[12px] text-ink-3">-</span>;
  return openAnomalies > 0 ? (
    <span data-numeric="" className="text-[13px] font-semibold text-critical">
      {formatNumber(openAnomalies)}
    </span>
  ) : (
    <span className="text-[12px] text-ink-3">None</span>
  );
}

function callsCell({ callsInWindow }) {
  if (callsInWindow === null || callsInWindow === undefined) return <span className="text-[12px] text-ink-3" title="The call store could not be read">-</span>;
  return (
    <span data-numeric="" className="text-[12.5px] text-ink-2">
      {formatNumber(callsInWindow)}
    </span>
  );
}

function lastCallCell({ lastSeen, callsInWindow }) {
  if (lastSeen) {
    return (
      <span className="whitespace-nowrap text-[12.5px] text-ink-2" title={formatDateTime(lastSeen)}>
        {formatRelative(lastSeen)}
      </span>
    );
  }
  // "Never" only when the store was read and counted nothing; otherwise unknown.
  return <span className="text-[12px] text-ink-3">{callsInWindow === 0 ? 'Never' : '-'}</span>;
}
