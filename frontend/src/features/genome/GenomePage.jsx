import { useCallback, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Download, SearchX, ShieldQuestion } from 'lucide-react';
import { fetchGenomeSummary, fetchMonitoredFleet } from '../../lib/api/genome';
import { genomeRoute } from '../../lib/identityLinks';
import { useDebouncedValue, useQuery } from '../../lib/hooks';
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
import { RefreshButton, TableToolbar } from '../../ui/TableTools';
import { useToast } from '../../ui/Toast';
import { RecordBar, ResultCount } from '../../ui/WorkArea';
import { CompositionDonut } from '../../charts/CompositionDonut';
import { sourceTypeLabel } from './meta';

const PAGE_SIZES = [10, 25, 50, 100];
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

  const summary = useQuery((signal) => fetchGenomeSummary({}, signal), []);
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
        { header: 'Last activity (UTC)', value: (row) => row.last_activity_at ?? '' },
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
      width: '40%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate text-[13px] font-semibold text-ink" title={row.identityName}>
            {row.name || row.identityName}
          </span>
          <span className="block truncate text-[11.5px] text-ink-3">{sourceTypeLabel(row.sourceType)}</span>
        </span>
      ),
    },
    {
      key: 'scope',
      header: 'Account / region',
      width: '22%',
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
      key: 'status',
      header: 'Status',
      width: '14%',
      cell: (row) =>
        row.status ? (
          <Tag tone={String(row.status).toLowerCase() === 'active' ? 'low' : 'neutral'} size="sm" dot>
            {row.status}
          </Tag>
        ) : (
          <span className="text-[12px] text-ink-3">-</span>
        ),
    },
    {
      key: 'seen',
      header: 'Last activity',
      width: '18%',
      cell: (row) =>
        row.last_activity_at ? (
          <span className="block whitespace-nowrap text-[12.5px] text-ink-2" title={formatDateTime(row.last_activity_at)}>
            {formatRelative(row.last_activity_at)}
          </span>
        ) : fleet.data?.activityAvailable === false ? (
          <span className="text-[12px] text-ink-3" title="CloudTrail activity could not be read">
            Not available
          </span>
        ) : (
          <span className="text-[12px] text-ink-3">No calls seen</span>
        ),
    },
  ];

  const filteredView = Boolean(sourceType || search);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="NHI Genome"
        lede="Each workload identity learns its own normal behaviour from its calls. Open one to see its behavioural fingerprint, its calls and how its baseline got where it is."
        actions={
          <Button variant="secondary" icon={Download} onClick={onExport} disabled={filtered.length === 0}>
            Export
          </Button>
        }
      />

      {summary.isError && !summary.data ? (
        <Panel prominence="quiet">
          <ErrorState error={summary.error} onRetry={summary.refetch} title="The behaviour summary failed to load" />
        </Panel>
      ) : summary.isLoading && !summary.data ? (
        <StatStripSkeleton count={3} />
      ) : (
        <SummaryCards cards={summary.data.cards} />
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
            <TableToolbar>
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
                className="w-48"
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

function SummaryCards({ cards }) {
  const watched = cards.identities_watched;
  const established = cards.baselines_established;
  const coverage = Number.isFinite(watched) && watched > 0 && Number.isFinite(established) ? (established / watched) * 100 : null;
  return (
    <div className="grid gap-3 @min-[30rem]:grid-cols-3">
      <MetricTile
        label="Identities watched"
        value={watched}
        tone="info"
        caption="Workload identities in your scope whose calls the genome reads"
        className="animate-rise"
      />
      <MetricTile
        label="Baselines established"
        value={established}
        tone="low"
        meter={coverage}
        meterLabel="Share of watched identities with an established baseline"
        caption={coverage === null ? 'Normal behaviour learned and watched' : `${Math.round(coverage)}% of the watched fleet`}
        className="animate-rise"
        data-stagger=""
        style={{ '--stagger': 1 }}
      />
      <MetricTile
        label="Still learning"
        value={cards.still_learning}
        tone="medium"
        caption="Learning their normal behaviour; nothing is flagged for them yet"
        className="animate-rise"
        data-stagger=""
        style={{ '--stagger': 2 }}
      />
    </div>
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
