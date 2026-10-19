import { useMemo } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { ArrowLeft, GitCompareArrows, History, SearchX } from 'lucide-react';
import { fetchScanChanges, fetchScans, scanChangesRoute } from '../../lib/api/changes';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { NEUTRAL_SERIES, SERIES_TOKENS } from '../../lib/domain';
import { Bar, BarChart, CartesianGrid, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { TooltipCard } from '../../charts/ChartTooltip';
import { useQuery } from '../../lib/hooks';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { Select } from '../../ui/Field';
import { Pagination } from '../../ui/Pagination';
import { Panel } from '../../ui/Panel';
import { ListSkeleton, Skeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState } from '../../ui/States';
import { SegmentedControl } from '../../ui/Tabs';
import { cn } from '../../ui/cn';
import { CHANGE_GRID, CHANGE_META, CHANGE_TYPES, ChangeRow, ENTITY_META, ENTITY_TYPES } from './parts';

const PAGE_SIZE = 25;

/* The chart palette: entity types are categories, so they must not borrow
   the green / red / amber that mean created / deleted / modified here. */
const ENTITY_COLOR = {
  identity: SERIES_TOKENS[0],
  credential: SERIES_TOKENS[1],
  permission: SERIES_TOKENS[2],
  binding: SERIES_TOKENS[3],
  trust_edge: SERIES_TOKENS[4],
};

const STAT_STYLE = {
  CREATED: { box: 'border-low/30 bg-low-soft/60', active: 'ring-2 ring-low/40', text: 'text-low' },
  DELETED: { box: 'border-critical/30 bg-critical-soft/60', active: 'ring-2 ring-critical/40', text: 'text-critical' },
  MODIFIED: { box: 'border-medium/35 bg-medium-soft/60', active: 'ring-2 ring-medium/40', text: 'text-medium' },
};

/**
 * `/scans/:scanId/changes` - everything one discovery scan found had changed
 * since the scan before it, within the selected accounts and regions. Opened
 * from "View all changes" on the dashboard; the scan picker moves between
 * recent scans. Filters and page live in the URL.
 */
export default function ScanChangesPage() {
  const { scanId } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const changeType = CHANGE_TYPES.includes(searchParams.get('change_type')) ? searchParams.get('change_type') : '';
  const entityType = ENTITY_TYPES.includes(searchParams.get('entity_type')) ? searchParams.get('entity_type') : '';
  const page = Math.max(1, Number.parseInt(searchParams.get('page') || '1', 10) || 1);
  const limit = [10, 25, 50, 100].includes(Number(searchParams.get('limit'))) ? Number(searchParams.get('limit')) : PAGE_SIZE;

  const query = useQuery(
    (signal) => fetchScanChanges({ scanId, changeType, entityType, page, limit }, signal),
    [scanId, changeType, entityType, page, limit],
    { keepPrevious: true },
  );
  const scansQuery = useQuery((signal) => fetchScans({ limit: 20 }, signal), []);

  const setParams = (patch) => {
    const next = new URLSearchParams(searchParams);
    for (const [key, value] of Object.entries(patch)) {
      if (value === '' || value === null || value === undefined) next.delete(key);
      else next.set(key, String(value));
    }
    if (!('page' in patch)) next.delete('page');
    setSearchParams(next, { replace: true });
  };
  const clearFilters = () => setParams({ change_type: '', entity_type: '' });

  const data = query.data;
  const scan = data?.scan;
  const summary = data?.summary;
  const filtered = Boolean(changeType || entityType);

  const back = (
    <Button variant="secondary" as={Link} to="/overview" icon={ArrowLeft}>
      Dashboard
    </Button>
  );

  const scanOptions = useMemo(() => {
    const scans = scansQuery.data ?? [];
    const options = scans.map((item, index) => ({
      value: item.id,
      label: `${formatDateTime(item.completedAt)} · ${formatNumber(item.totalChanges)} ${item.totalChanges === 1 ? 'change' : 'changes'}${index === 0 ? ' (latest)' : ''}`,
    }));
    if (scan && !options.some((option) => option.value === scan.id)) {
      options.unshift({ value: scan.id, label: formatDateTime(scan.completedAt) });
    }
    return options;
  }, [scansQuery.data, scan]);

  if (query.isError && !data) {
    const missing = query.error?.status === 404 || query.error?.status === 400;
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Scan changes" actions={back} />
        <Panel>
          {missing ? (
            <EmptyState
              icon={SearchX}
              title="Scan not found"
              description="It may be outside the accounts you have selected, or the link is out of date."
              action={
                <Button variant="secondary" size="sm" onClick={() => navigate(scanChangesRoute('latest'), { replace: true })}>
                  Show the latest scan
                </Button>
              }
            />
          ) : (
            <ErrorState error={query.error} onRetry={query.refetch} title="The scan's changes failed to load" />
          )}
        </Panel>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Scan changes"
        lede="What discovery found had changed since the previous scan, in the accounts and regions you have selected."
        actions={
          <>
            {scanOptions.length > 1 && (
              <label className="flex items-center gap-2 text-[12.5px] text-ink-3">
                <span className="hidden sm:inline">Scan</span>
                <Select
                  value={scan?.id ?? ''}
                  onChange={(event) => navigate(scanChangesRoute(event.target.value))}
                  options={scanOptions}
                  aria-label="Choose a scan"
                  className="max-w-[18rem]"
                />
              </label>
            )}
            {back}
          </>
        }
      />

      {!data ? (
        <Panel prominence="lead">
          <div className="grid gap-4 @min-[52rem]:grid-cols-4">
            {[0, 1, 2, 3].map((index) => (
              <Skeleton key={index} className="h-20 rounded-[var(--radius-control)]" />
            ))}
          </div>
          <span role="status" className="sr-only">
            Loading scan changes
          </span>
        </Panel>
      ) : (
        <SummaryPanel
          scan={scan}
          summary={summary}
          changeType={changeType}
          entityType={entityType}
          onChangeType={(value) => setParams({ change_type: value === changeType ? '' : value })}
          onEntityType={(value) => setParams({ entity_type: value === entityType ? '' : value })}
        />
      )}

      {(scansQuery.data?.length ?? 0) >= 2 && (
        <ScanHistory scans={scansQuery.data} currentId={scan?.id} onOpen={(id) => navigate(scanChangesRoute(id))} />
      )}

      <Panel prominence="lead" flush className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3 sm:px-5">
          <SegmentedControl
            label="Change type"
            value={changeType || 'all'}
            onChange={(value) => setParams({ change_type: value === 'all' ? '' : value })}
            options={[
              { value: 'all', label: `All${summary ? ` (${formatNumber(summary.total)})` : ''}` },
              ...CHANGE_TYPES.map((type) => ({
                value: type,
                label: `${CHANGE_META[type].filter}${summary ? ` (${formatNumber(summary.byChangeType[type] ?? 0)})` : ''}`,
              })),
            ]}
          />
          <div className="flex flex-wrap items-center gap-2">
            <Select
              size="sm"
              value={entityType}
              onChange={(event) => setParams({ entity_type: event.target.value })}
              aria-label="Entity type"
              className="min-w-[11rem]"
              options={[
                { value: '', label: 'All entity types' },
                ...ENTITY_TYPES.map((type) => ({
                  value: type,
                  label: `${ENTITY_META[type].plural}${summary ? ` (${formatNumber(summary.byEntityType[type] ?? 0)})` : ''}`,
                })),
              ]}
            />
            {filtered && (
              <Button variant="ghost" size="sm" onClick={clearFilters}>
                Clear filters
              </Button>
            )}
          </div>
        </div>

        <div
          aria-hidden="true"
          className={cn('hidden gap-x-4 border-b border-line bg-surface-2 px-5 py-2 text-[11px] font-medium text-ink-3 @min-[56rem]:grid', CHANGE_GRID)}
        >
          <span>Change</span>
          <span>Entity</span>
          <span>What changed</span>
          <span>ARN</span>
          <span>Detected</span>
          <span />
        </div>

        {!data ? (
          <div className="p-5">
            <ListSkeleton rows={6} />
          </div>
        ) : data.changes.length === 0 ? (
          <NoChanges scan={scan} summary={summary} filtered={filtered} onClear={clearFilters} />
        ) : (
          <ul aria-busy={query.isRefreshing || undefined} className={cn('transition-opacity', (query.isRefreshing || query.isLoading) && 'opacity-60')}>
            {data.changes.map((change) => (
              <ChangeRow key={change.id} change={change} />
            ))}
          </ul>
        )}

        {data && data.pagination.total > 0 && (
          <Pagination
            page={data.pagination.page}
            pageSize={data.pagination.limit}
            total={data.pagination.total}
            unit={data.pagination.total === 1 ? 'change' : 'changes'}
            onPageChange={(value) => setParams({ page: value })}
            onPageSizeChange={(value) => setParams({ limit: value === PAGE_SIZE ? '' : value, page: '' })}
          />
        )}
      </Panel>
    </div>
  );
}

function SummaryPanel({ scan, summary, changeType, entityType, onChangeType, onEntityType }) {
  const first = !scan?.previousScanId;
  const entityTotal = ENTITY_TYPES.reduce((sum, type) => sum + (summary.byEntityType[type] ?? 0), 0);

  return (
    <Panel prominence="lead" className="animate-rise">
      <div className="grid gap-6 @min-[60rem]:grid-cols-[minmax(0,16rem)_minmax(0,1fr)]">
        <div className="flex flex-col gap-4">
          <div>
            <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Scan completed</p>
            <p className="mt-1 text-[14px] font-semibold text-ink">{formatDateTime(scan.completedAt)}</p>
            <p className="text-[12px] text-ink-3">{formatRelative(scan.completedAt)}</p>
          </div>
          <div>
            <p className="flex items-center gap-1.5 text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">
              <GitCompareArrows aria-hidden="true" className="size-3.5" />
              Compared with
            </p>
            {first ? (
              <p className="mt-1 text-[12.5px] text-ink-2">Nothing - this is the first scan.</p>
            ) : (
              <>
                <p className="mt-1 text-[13px] font-medium text-ink">{formatDateTime(scan.previousScanAt)}</p>
                <p className="text-[12px] text-ink-3">the previous scan</p>
              </>
            )}
          </div>
        </div>

        <div className="flex min-w-0 flex-col gap-5">
          <div className="grid gap-3 @min-[40rem]:grid-cols-[minmax(0,0.8fr)_repeat(3,minmax(0,1fr))]">
            <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-4 py-3">
              <p className="text-[11px] font-medium text-ink-3">Total changes</p>
              <p data-numeric="" className="mt-1 text-[26px] leading-none font-bold text-ink">
                {formatNumber(summary.total)}
              </p>
            </div>
            {CHANGE_TYPES.map((type) => {
              const meta = CHANGE_META[type];
              const Icon = meta.icon;
              const style = STAT_STYLE[type];
              const count = summary.byChangeType[type] ?? 0;
              const active = changeType === type;
              return (
                <button
                  key={type}
                  type="button"
                  aria-pressed={active}
                  onClick={() => onChangeType(type)}
                  disabled={count === 0 && !active}
                  className={cn(
                    'rounded-[var(--radius-control)] border px-4 py-3 text-left transition-shadow enabled:hover:shadow-sm disabled:cursor-default',
                    style.box,
                    active && style.active,
                  )}
                  title={active ? 'Show every change type' : `Show only ${meta.label.toLowerCase()} changes`}
                >
                  <p className={cn('flex items-center gap-1.5 text-[11px] font-semibold', style.text)}>
                    <Icon aria-hidden="true" className="size-3.5" />
                    {meta.label}
                  </p>
                  <p data-numeric="" className={cn('mt-1 text-[26px] leading-none font-bold', style.text)}>
                    {formatNumber(count)}
                  </p>
                </button>
              );
            })}
          </div>

          <div>
            <p className="text-[11px] font-medium text-ink-3">By entity type</p>
            <div className="mt-2 flex h-2 w-full overflow-hidden rounded-full bg-line" aria-hidden="true">
              {entityTotal > 0 &&
                ENTITY_TYPES.map((type) =>
                  summary.byEntityType[type] ? (
                    <span key={type} className="h-full" style={{ width: `${(summary.byEntityType[type] / entityTotal) * 100}%`, background: ENTITY_COLOR[type] }} />
                  ) : null,
                )}
            </div>
            <ul className="mt-2.5 flex flex-wrap gap-x-1 gap-y-1">
              {ENTITY_TYPES.map((type) => {
                const count = summary.byEntityType[type] ?? 0;
                const active = entityType === type;
                return (
                  <li key={type}>
                    <button
                      type="button"
                      aria-pressed={active}
                      disabled={count === 0 && !active}
                      onClick={() => onEntityType(type)}
                      className={cn(
                        'inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[12px] text-ink-2 transition-colors enabled:hover:bg-surface-3 disabled:text-ink-3',
                        active && 'bg-surface-3 font-medium text-ink',
                      )}
                    >
                      <span aria-hidden="true" className="size-2 rounded-sm" style={{ background: ENTITY_COLOR[type] }} />
                      {ENTITY_META[type].plural}
                      <span data-numeric="" className="font-semibold text-ink">
                        {formatNumber(count)}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>
        </div>
      </div>
    </Panel>
  );
}

/**
 * Changes found by each recent scan, oldest to newest: a spike is drift worth
 * opening. The scan on screen is the accent bar, the rest are context; a bar
 * opens its scan.
 */
function ScanHistory({ scans, currentId, onOpen }) {
  const data = useMemo(
    () =>
      [...scans].reverse().map((item) => ({
        id: item.id,
        label: item.completedAt
          ? new Date(item.completedAt).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
          : item.id,
        at: item.completedAt,
        changes: item.totalChanges,
      })),
    [scans],
  );
  const total = data.reduce((sum, row) => sum + row.changes, 0);
  return (
    <Panel prominence="quiet" className="animate-rise">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="text-[13px] font-semibold text-ink">Changes per scan</p>
        <p className="text-[12px] text-ink-3" data-numeric="">
          {formatNumber(total)} changes across the last {formatNumber(data.length)} scans · select a bar to open that scan
        </p>
      </div>
      <div className="mt-3 h-[150px] w-full">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
            <CartesianGrid stroke="var(--t-chart-grid)" strokeDasharray="2 4" vertical={false} />
            <XAxis dataKey="label" tick={{ fill: 'var(--t-chart-axis)', fontSize: 10.5 }} tickLine={false} axisLine={{ stroke: 'var(--t-chart-grid)' }} interval="preserveStartEnd" minTickGap={28} />
            <YAxis tick={{ fill: 'var(--t-chart-axis)', fontSize: 10.5 }} tickLine={false} axisLine={false} width={34} allowDecimals={false} />
            <Tooltip
              cursor={{ fill: 'var(--t-chart-grid)', opacity: 0.5 }}
              content={({ active, payload }) => {
                if (!active || !payload?.length) return null;
                const row = payload[0].payload;
                return (
                  <TooltipCard
                    title={row.at ? formatDateTime(row.at) : row.id}
                    subtitle={row.id === currentId ? 'Shown below' : 'Select to open'}
                    rows={[{ label: 'Changes', value: formatNumber(row.changes), color: row.id === currentId ? SERIES_TOKENS[0] : NEUTRAL_SERIES }]}
                  />
                );
              }}
            />
            <Bar dataKey="changes" radius={[3, 3, 0, 0]} maxBarSize={30} onClick={(row) => row?.id && row.id !== currentId && onOpen(row.id)} style={{ cursor: 'pointer' }}>
              {data.map((row) => (
                <Cell key={row.id} fill={row.id === currentId ? SERIES_TOKENS[0] : NEUTRAL_SERIES} fillOpacity={row.id === currentId ? 1 : 0.55} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>
    </Panel>
  );
}

function NoChanges({ scan, summary, filtered, onClear }) {
  if (filtered && summary.total > 0) {
    return (
      <EmptyState
        icon={SearchX}
        title="No change matches these filters"
        description="This scan has changes, just none of this kind."
        action={
          <Button variant="secondary" size="sm" onClick={onClear}>
            Clear filters
          </Button>
        }
      />
    );
  }
  if (!scan?.previousScanId) {
    return (
      <EmptyState
        icon={History}
        title="First scan completed"
        description="There was nothing to compare it with. Changes appear here after the next scan."
      />
    );
  }
  return (
    <EmptyState
      icon={GitCompareArrows}
      title="No changes detected"
      description="Everything in your selected accounts and regions matched the previous scan."
    />
  );
}
