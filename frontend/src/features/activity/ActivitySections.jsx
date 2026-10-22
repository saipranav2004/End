import { useMemo } from 'react';
import { Activity, AlertTriangle, DatabaseZap, Globe, Network } from 'lucide-react';
import { fetchIdentity, fetchIdentityEvents } from '../../lib/api/inventory';
import { useQuery } from '../../lib/hooks';
import { rampColor, SERIES_TOKENS } from '../../lib/domain';
import { formatDate, formatDateTime, formatNumber, formatRelative, formatRelativeShort, percentValue } from '../../lib/format';
import { Panel, PanelHeader } from '../../ui/Panel';
import { MetricTile } from '../../ui/Stat';
import { ProportionBar } from '../../ui/Meter';
import { DataGrid } from '../../ui/DataGrid';
import { Pagination } from '../../ui/Pagination';
import { ListSkeleton, StatStripSkeleton } from '../../ui/Skeleton';
import { ClearState, EmptyState, ErrorState, InlineError } from '../../ui/States';
import { Code, Tag } from '../../ui/Tag';
import { bindingLabel, credentialTypeMeta } from '../identities/meta';
import { activeEventFilters, activityRange, eventQuery, eventRange, rangeQuery } from './eventState';

/**
 * The sections of `GET /api/v1/identities/{id}/events` other than the
 * timeline, and the summary that heads every activity view. Every figure is
 * the API's own aggregate over the selected window - nothing is counted from
 * a loaded page. Each section takes the whole event state and sends its range
 * and filters, so a section answers the same question as the event list.
 */

/** One cache key for the window and every filter. */
function filterKey(state) {
  return JSON.stringify(eventQuery(state));
}

function Unavailable() {
  return (
    <EmptyState
      compact
      icon={DatabaseZap}
      title="The activity log could not be read"
      description="The AWS activity log (CloudTrail) cannot be read right now, so this is unknown, not empty. Try again in a few minutes."
    />
  );
}

function SectionState({ query, children, rows = 5 }) {
  if (query.isError && !query.data) return <ErrorState compact error={query.error} onRetry={query.refetch} />;
  if (!query.data) return <ListSkeleton rows={rows} />;
  if (query.data.data_available === false) return <Unavailable />;
  return children;
}

/* ── Summary ─────────────────────────────────────────────────────────────── */

/**
 * Tiles and the shape of the window. Read versus mutating is counted exactly,
 * with the API's read_write filter, so it holds for the whole window rather
 * than for the rows on screen.
 */
export function ActivitySummary({ identityId, state }) {
  const range = eventRange(state);
  const windowQuery = rangeQuery(state);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const activityParams = useMemo(() => activityRange(state), [state.range, state.from, state.to]);
  const countsQuery = useQuery(
    async (signal) => {
      const [all, reads, errors, ips] = await Promise.all([
        fetchIdentityEvents(identityId, 'timeline', { ...windowQuery, pageSize: 1 }, signal),
        fetchIdentityEvents(identityId, 'timeline', { ...windowQuery, readWrite: 'read', pageSize: 1 }, signal),
        fetchIdentityEvents(identityId, 'errors', { ...windowQuery, pageSize: 1 }, signal),
        fetchIdentityEvents(identityId, 'ips', windowQuery, signal),
      ]);
      return {
        available: all.data_available !== false,
        total: all.total_events ?? 0,
        reads: reads.total_events ?? 0,
        errors: errors.summary?.total_errors ?? 0,
        ips: ips.total_unique_ips ?? 0,
        publicIps: (ips.ips ?? []).filter((ip) => ip.kind === 'public').length,
      };
    },
    [identityId, state.range, state.from, state.to],
    { enabled: Boolean(identityId) },
  );
  const shapeQuery = useQuery(
    (signal) => fetchIdentity(identityId, 'activity', { ...activityParams, page_size: 1 }, signal),
    [identityId, activityParams],
    { enabled: Boolean(identityId) },
  );

  const counts = countsQuery.data;
  if (countsQuery.isError && !counts) {
    return <InlineError error={countsQuery.error} onRetry={countsQuery.refetch} label="Activity summary unavailable" />;
  }
  if (!counts) return <StatStripSkeleton count={4} />;
  if (!counts.available) {
    return (
      <Panel>
        <Unavailable />
      </Panel>
    );
  }

  const writes = Math.max(0, counts.total - counts.reads);
  const analytics = shapeQuery.data?.analytics;
  const services = analytics?.top_services ?? [];
  const volume = shapeQuery.data?.volume_chart ?? [];
  const peak = volume.reduce((max, point) => Math.max(max, Number(point.count) || 0), 0);

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-3 @min-[30rem]:grid-cols-2 @min-[54rem]:grid-cols-4">
        <MetricTile
          label="Calls"
          value={counts.total}
          tone="brand"
          icon={Activity}
          caption={`${range.title}${analytics?.usage_pattern ? ` · ${analytics.usage_pattern.toLowerCase()} use` : ''}`}
          className="animate-rise"
        />
        <MetricTile
          label="Made changes"
          value={writes}
          tone={writes > 0 ? 'high' : 'low'}
          caption={`Of ${formatNumber(counts.total)} calls in this period`}
          meter={percentValue(writes, counts.total)}
          meterLabel="Share of calls that changed something"
          className="animate-rise"
          data-stagger=""
          style={{ '--stagger': 1 }}
        />
        <MetricTile
          label="Failed calls"
          value={counts.errors}
          tone={counts.errors > 0 ? 'critical' : 'low'}
          caption={
            [
              typeof analytics?.error_rate_pct === 'number' ? `${analytics.error_rate_pct}% of calls` : null,
              typeof analytics?.access_denied_rate_pct === 'number' ? `${analytics.access_denied_rate_pct}% refused by a policy` : null,
            ]
              .filter(Boolean)
              .join(' · ') || 'Calls answered with an error'
          }
          className="animate-rise"
          data-stagger=""
          style={{ '--stagger': 2 }}
        />
        <MetricTile
          label="Source IPs"
          value={counts.ips}
          tone={counts.publicIps > 0 ? 'medium' : 'neutral'}
          caption={counts.publicIps > 0 ? `${formatNumber(counts.publicIps)} from the public internet` : 'All from private or AWS addresses'}
          className="animate-rise"
          data-stagger=""
          style={{ '--stagger': 3 }}
        />
      </div>

      <Panel prominence="quiet" className="animate-rise">
        <PanelHeader
          prominence="quiet"
          title="How it was used"
          subtitle={`${range.title}, counted from the AWS activity log (CloudTrail).`}
        />
        {counts.total === 0 ? (
          <p className="mt-3 text-[12.5px] text-ink-3">No calls were recorded in this period. Try a longer range.</p>
        ) : (
          <div className="mt-3.5 grid gap-6 @min-[46rem]:grid-cols-3">
            <div>
              <ProportionBar
                height={12}
                total={counts.total}
                ariaLabel={`${writes} calls made changes and ${counts.reads} only read`}
                segments={[
                  { key: 'mutating', label: 'Made changes', value: writes, color: 'var(--t-high)' },
                  { key: 'read', label: 'Read-only', value: counts.reads, color: 'var(--t-info)' },
                ]}
              />
              <dl className="mt-3 flex flex-wrap gap-x-7 gap-y-2">
                <div className="flex items-center gap-2">
                  <span aria-hidden="true" className="size-2.5 rounded-[3px] bg-high" />
                  <dt className="text-[12px] text-ink-3">Made changes</dt>
                  <dd data-numeric="" className="text-[12.5px] font-semibold text-ink">
                    {formatNumber(writes)}
                  </dd>
                </div>
                <div className="flex items-center gap-2">
                  <span aria-hidden="true" className="size-2.5 rounded-[3px] bg-info" />
                  <dt className="text-[12px] text-ink-3">Read-only</dt>
                  <dd data-numeric="" className="text-[12.5px] font-semibold text-ink">
                    {formatNumber(counts.reads)}
                  </dd>
                </div>
              </dl>
            </div>

            <div>
              <p className="text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">Busiest services</p>
              {shapeQuery.isError ? (
                <p className="mt-2 text-[12px] text-ink-3">Not available just now.</p>
              ) : (
                <ul className="mt-2 flex flex-col gap-1.5">
                  {services.slice(0, 5).map((entry) => (
                    <li key={entry.service} className="flex items-center gap-3">
                      <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink-2">{entry.service}</span>
                      <span className="h-1.5 w-20 overflow-hidden rounded-full bg-track">
                        <span
                          className="block h-full rounded-full bg-[var(--t-ramp-4)] transition-[width] duration-700 ease-[var(--ease-out-quint)]"
                          style={{ width: `${percentValue(entry.count, services[0].count)}%` }}
                        />
                      </span>
                      <span data-numeric="" className="w-10 shrink-0 text-right text-[12px] font-semibold text-ink">
                        {formatNumber(entry.count)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            <div>
              <p className="text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">Calls per day</p>
              {volume.length === 0 ? (
                <p className="mt-2 text-[12px] text-ink-3">{shapeQuery.data ? 'No daily figures.' : ''}</p>
              ) : (
                <div
                  className="mt-2 flex h-16 items-end gap-[2px]"
                  role="img"
                  aria-label={`Daily calls, peak ${formatNumber(peak)} on one day`}
                >
                  {volume.map((point) => (
                    <span
                      key={point.date}
                      title={`${formatDate(point.date)}: ${formatNumber(point.count)} calls`}
                      className="min-w-[2px] flex-1 rounded-t-[2px] bg-[var(--t-ramp-4)]"
                      style={{ height: `${Math.max(4, percentValue(point.count, peak))}%` }}
                    />
                  ))}
                </div>
              )}
              {volume.length > 0 && (
                <p className="mt-1 flex justify-between text-[10.5px] text-ink-3">
                  <span>{formatDate(volume[0].date)}</span>
                  <span>{formatDate(volume[volume.length - 1].date)}</span>
                </p>
              )}
            </div>
          </div>
        )}
      </Panel>
    </div>
  );
}

/* ── Heatmap ─────────────────────────────────────────────────────────────── */

export function HeatmapSection({ identityId, state }) {
  const query = useQuery((signal) => fetchIdentityEvents(identityId, 'heatmap', eventQuery(state), signal), [identityId, filterKey(state)]);
  const grid = useMemo(() => {
    const cells = query.data?.heatmap ?? [];
    const byAction = new Map();
    for (const cell of cells) {
      const row = byAction.get(cell.action) ?? { action: cell.action, total: 0, hours: Array(24).fill(0) };
      row.hours[cell.hour] = Number(cell.count) || 0;
      row.total += Number(cell.count) || 0;
      byAction.set(cell.action, row);
    }
    const rows = [...byAction.values()].sort((a, b) => b.total - a.total);
    const max = rows.reduce((m, row) => Math.max(m, ...row.hours), 0);
    return { rows, max };
  }, [query.data]);

  return (
    <SectionState query={query}>
      {grid.rows.length === 0 ? (
        <EmptyState compact icon={Activity} title="No calls in this period" description="Nothing to place on the clock. Try a longer range." />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[720px] border-separate border-spacing-[2px] text-left">
            <caption className="sr-only">Calls per action by hour of day, UTC</caption>
            <thead>
              <tr>
                <th scope="col" className="w-48 pb-1 text-[10.5px] font-semibold tracking-[0.08em] text-ink-3 uppercase">
                  Action
                </th>
                {Array.from({ length: 24 }, (_, hour) => (
                  <th key={hour} scope="col" className="pb-1 text-center text-[10px] font-medium text-ink-3">
                    {hour % 3 === 0 ? String(hour).padStart(2, '0') : ''}
                    <span className="sr-only">{`${hour}:00 UTC`}</span>
                  </th>
                ))}
                <th scope="col" className="pb-1 pl-2 text-right text-[10.5px] font-semibold tracking-[0.08em] text-ink-3 uppercase">
                  Total
                </th>
              </tr>
            </thead>
            <tbody>
              {grid.rows.map((row) => (
                <tr key={row.action}>
                  <th scope="row" className="max-w-48 truncate pr-2 font-mono text-[11.5px] font-normal text-ink-2" title={row.action}>
                    {row.action}
                  </th>
                  {row.hours.map((count, hour) => (
                    <td
                      key={hour}
                      title={`${row.action} at ${String(hour).padStart(2, '0')}:00 UTC: ${formatNumber(count)} calls`}
                      className="h-6 rounded-[3px]"
                      style={{ background: count > 0 ? rampColor(count, grid.max + 1) : 'var(--t-track)' }}
                    >
                      <span className="sr-only">{count}</span>
                    </td>
                  ))}
                  <td data-numeric="" className="pl-2 text-right text-[12px] font-semibold text-ink">
                    {formatNumber(row.total)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="mt-2 text-[11.5px] text-ink-3">When each action is called, by hour of day (UTC). Darker cells mean more calls in that hour across the period.</p>
        </div>
      )}
    </SectionState>
  );
}

/* ── Resources ───────────────────────────────────────────────────────────── */

export function ResourcesSection({ identityId, state, onFilter }) {
  const query = useQuery((signal) => fetchIdentityEvents(identityId, 'resources', eventQuery(state), signal), [identityId, filterKey(state)]);
  const rows = query.data?.resources_accessed ?? [];
  return (
    <SectionState query={query}>
      {typeof query.data?.total_resources === 'number' && (
        <p className="mb-3 text-[12.5px] text-ink-3">
          <span data-numeric="" className="font-semibold text-ink">
            {formatNumber(query.data.total_resources)}
          </span>{' '}
          {query.data.total_resources === 1 ? 'resource' : 'resources'} used {eventRange(state).phrase}.
        </p>
      )}
      <DataGrid
        caption="Resources this identity used"
        rows={rows}
        rowKey={(row) => row.resource_arn}
        onRowClick={onFilter ? (row) => onFilter({ resource: row.resource_arn.slice(0, 256) }) : undefined}
        density="compact"
        columns={[
          {
            key: 'resource',
            header: 'Resource',
            primary: true,
            width: '26%',
            cell: (row) => (
              <span className="block min-w-0" title={row.resource_arn}>
                <span className="block truncate text-[12.5px] font-medium text-ink">{row.resource_name || row.resource_arn}</span>
                <span className="block truncate text-[11px] text-ink-3">{(row.resource_type || '').replace(/^AWS::/, '') || 'Resource'}</span>
              </span>
            ),
          },
          {
            key: 'account',
            header: 'Account',
            width: '14%',
            cell: (row) => (
              <span className="flex flex-wrap items-center gap-1.5">
                <span className="font-mono text-[12px] text-ink-2">{row.account_id || 'Not recorded'}</span>
                {row.is_cross_account && (
                  <Tag tone="medium" size="sm">
                    Another account
                  </Tag>
                )}
              </span>
            ),
          },
          {
            key: 'actions',
            header: 'Actions',
            width: '16%',
            priority: 'wide',
            cell: (row) => <ActionList actions={row.actions} />,
          },
          {
            key: 'first',
            header: 'First used',
            width: '10%',
            priority: 'wide',
            cell: (row) => (
              <span className="text-[12px] whitespace-nowrap text-ink-2" title={formatDateTime(row.first_access)}>
                {formatRelativeShort(row.first_access)}
              </span>
            ),
          },
          { key: 'reads', header: 'Reads', align: 'right', width: '8%', cell: (row) => <Num value={row.read_count} /> },
          { key: 'writes', header: 'Changes', align: 'right', width: '8%', cell: (row) => <Num value={row.write_count} tone={row.write_count > 0 ? 'text-high' : undefined} /> },
          { key: 'errors', header: 'Failed', align: 'right', width: '8%', cell: (row) => <Num value={row.error_count} tone={row.error_count > 0 ? 'text-critical' : undefined} /> },
          {
            key: 'last',
            header: 'Last used',
            width: '10%',
            cell: (row) => (
              <span className="text-[12px] whitespace-nowrap text-ink-2" title={formatDateTime(row.last_access)}>
                {formatRelativeShort(row.last_access)}
              </span>
            ),
          },
        ]}
        emptyState={<EmptyState compact icon={Network} title="No resources used" description="No call in this period named a resource." />}
      />
    </SectionState>
  );
}

/* ── Source IPs ──────────────────────────────────────────────────────────── */

const IP_KIND = {
  private: { label: 'Private', tone: 'neutral' },
  public: { label: 'Public internet', tone: 'medium' },
  aws_service: { label: 'AWS service', tone: 'info' },
  unknown: { label: 'Not an IP address', tone: 'neutral' },
};

export function IpsSection({ identityId, state, onFilter }) {
  const query = useQuery((signal) => fetchIdentityEvents(identityId, 'ips', eventQuery(state), signal), [identityId, filterKey(state)]);
  const rows = query.data?.ips ?? [];
  return (
    <SectionState query={query}>
      <DataGrid
        caption="Where calls came from"
        rows={rows}
        rowKey={(row) => row.source_ip}
        onRowClick={onFilter ? (row) => (row.kind === 'private' || row.kind === 'public' ? onFilter({ ip: row.source_ip }) : undefined) : undefined}
        density="compact"
        columns={[
          {
            key: 'ip',
            header: 'Source',
            primary: true,
            width: '34%',
            cell: (row) => (
              <span className="flex min-w-0 items-center gap-2">
                <Globe aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
                <span className="truncate font-mono text-[12.5px] text-ink">{row.source_ip}</span>
                <Tag tone={(IP_KIND[row.kind] ?? IP_KIND.unknown).tone} size="sm">
                  {(IP_KIND[row.kind] ?? IP_KIND.unknown).label}
                </Tag>
              </span>
            ),
          },
          { key: 'events', header: 'Calls', align: 'right', width: '14%', cell: (row) => <Num value={row.event_count} /> },
          { key: 'geo', header: 'Location', width: '18%', priority: 'wide', cell: (row) => <span className="text-[12px] text-ink-3">{row.geo_location || 'Location unknown'}</span> },
          { key: 'first', header: 'First seen', width: '17%', cell: (row) => <When at={row.first_seen} /> },
          { key: 'last', header: 'Last seen', width: '17%', cell: (row) => <When at={row.last_seen} /> },
        ]}
        emptyState={<EmptyState compact icon={Globe} title="No calls in this period" description="No source address was recorded." />}
      />
    </SectionState>
  );
}

/* ── Errors ──────────────────────────────────────────────────────────────── */

/* Paged with the shared state, so a new filter or range goes back to page 1. */
export function ErrorsSection({ identityId, state, patch }) {
  const query = useQuery(
    (signal) => fetchIdentityEvents(identityId, 'errors', eventQuery(state, { page: state.page, pageSize: state.size }), signal),
    [identityId, filterKey(state), state.page, state.size],
  );
  const data = query.data;
  const summary = data?.summary;
  const rows = data?.events ?? [];
  const codes = Object.entries(summary?.by_error_code ?? {}).sort((a, b) => b[1] - a[1]);

  return (
    <SectionState query={query}>
      {summary?.total_errors === 0 ? (
        <ClearState
          compact
          title="No failed calls"
          description={activeEventFilters(state).length > 0 ? 'No failed call in this period matches the filters applied.' : 'Every call in this period succeeded.'}
        />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="grid gap-3 rounded-[var(--radius-control)] border border-line bg-surface-2 p-4 sm:grid-cols-3">
            <div>
              <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Failed calls</p>
              <p data-numeric="" className="mt-1 text-[22px] font-bold text-critical">
                {formatNumber(summary?.total_errors)}
              </p>
              <ul className="mt-1 flex flex-wrap gap-1.5">
                {codes.map(([code, count]) => (
                  <li key={code}>
                    <Tag tone="critical" size="sm">
                      {code} · {formatNumber(count)}
                    </Tag>
                  </li>
                ))}
              </ul>
            </div>
            <div className="min-w-0">
              <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Most often refused action</p>
              <p className="mt-1 truncate font-mono text-[13px] text-ink">{summary?.most_denied_action?.action || 'None'}</p>
              {summary?.most_denied_action && (
                <p className="text-[11.5px] text-ink-3">Refused {formatNumber(summary.most_denied_action.count)} times</p>
              )}
            </div>
            <div className="min-w-0">
              <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Most often refused resource</p>
              <p className="mt-1 truncate text-[13px] text-ink" title={summary?.most_denied_resource?.resource_arn}>
                {summary?.most_denied_resource?.resource_name || summary?.most_denied_resource?.resource_arn || 'None'}
              </p>
              {summary?.most_denied_resource && (
                <p className="text-[11.5px] text-ink-3">Refused {formatNumber(summary.most_denied_resource.count)} times</p>
              )}
            </div>
          </div>

          <div className="overflow-hidden rounded-[var(--radius-control)] border border-line">
            <DataGrid
              caption="Failed calls"
              rows={rows}
              rowKey={(row, index) => `${row.event_time}-${row.event_name}-${index}`}
              refreshing={query.isRefreshing}
              density="compact"
              columns={[
                {
                  key: 'event',
                  header: 'Call',
                  primary: true,
                  width: '22%',
                  cell: (row) => (
                    <span className="flex min-w-0 items-center gap-2">
                      <AlertTriangle aria-hidden="true" className="size-3.5 shrink-0 text-critical" />
                      <span className="min-w-0">
                        <span className="block truncate text-[12.5px] font-medium text-ink">{row.event_name}</span>
                        <span className="block truncate text-[11px] text-ink-3">{row.service}</span>
                      </span>
                    </span>
                  ),
                },
                {
                  key: 'error',
                  header: 'Error',
                  width: '24%',
                  cell: (row) => (
                    <span className="block min-w-0" title={row.error_message}>
                      <span className="block truncate text-[12.5px] font-medium text-critical">{row.error_code}</span>
                      <span className="block truncate text-[11px] text-ink-3">{row.error_message || 'No message'}</span>
                    </span>
                  ),
                },
                {
                  key: 'resource',
                  header: 'Resource',
                  width: '16%',
                  priority: 'wide',
                  cell: (row) => (
                    <span className="block truncate text-[12px] text-ink-2" title={row.resource_arn}>
                      {row.resource_name || row.resource_arn || 'No resource'}
                    </span>
                  ),
                },
                {
                  key: 'credential',
                  header: 'Credential',
                  width: '13%',
                  priority: 'wide',
                  cell: (row) => <span className="block truncate font-mono text-[12px] text-ink-2">{row.credential_name || 'Not matched'}</span>,
                },
                {
                  key: 'source',
                  header: 'Source',
                  width: '13%',
                  priority: 'wide',
                  cell: (row) => (
                    <span className="block min-w-0">
                      <span className="block truncate font-mono text-[12px] text-ink-2">{row.source_ip || 'Not recorded'}</span>
                      <span className="block truncate text-[11px] text-ink-3">{row.aws_region || ''}</span>
                    </span>
                  ),
                },
                { key: 'when', header: 'When', width: '12%', cell: (row) => <When at={row.event_time} /> },
              ]}
              emptyState={null}
            />
            {(data?.pagination?.total_count ?? 0) > 0 && (
              <Pagination
                page={state.page}
                pageSize={state.size}
                total={data.pagination.total_count}
                unit="failed calls"
                onPageChange={(page) => patch({ page })}
                onPageSizeChange={(size) => patch({ size })}
              />
            )}
          </div>
        </div>
      )}
    </SectionState>
  );
}

/* ── Cross-account ───────────────────────────────────────────────────────── */

export function CrossAccountSection({ identityId, state }) {
  const query = useQuery((signal) => fetchIdentityEvents(identityId, 'cross_account', eventQuery(state), signal), [identityId, filterKey(state)]);
  const rows = query.data?.cross_account_activity ?? [];
  return (
    <SectionState query={query}>
      {rows.length === 0 ? (
        <ClearState
          compact
          title="No calls into other AWS accounts"
          description={`Every call in this period stayed in ${query.data?.identity_account ? `account ${query.data.identity_account}` : 'its own account'}.`}
        />
      ) : (
        <>
          <p className="mb-3 text-[12.5px] text-ink-3">
            {formatNumber(query.data.total_cross_account_events)} calls from account{' '}
            <span className="font-mono text-ink-2">{query.data.identity_account}</span> reached resources in another AWS account.
          </p>
          <DataGrid
            caption="Calls into other accounts"
            rows={rows}
            rowKey={(row) => `${row.target_account_id}-${row.target_resource_arn}`}
            density="compact"
            columns={[
              {
                key: 'resource',
                header: 'Resource',
                primary: true,
                width: '34%',
                cell: (row) => (
                  <span className="block min-w-0" title={row.target_resource_arn}>
                    <span className="block truncate text-[12.5px] font-medium text-ink">{row.target_resource_name || row.target_resource_arn}</span>
                    <span className="block truncate font-mono text-[11px] text-ink-3">{row.target_resource_arn}</span>
                  </span>
                ),
              },
              { key: 'account', header: 'Account', width: '14%', cell: (row) => <span className="font-mono text-[12px] text-ink-2">{row.target_account_id}</span> },
              { key: 'actions', header: 'Actions', width: '26%', priority: 'wide', cell: (row) => <ActionList actions={row.actions} /> },
              { key: 'calls', header: 'Calls', align: 'right', width: '10%', cell: (row) => <Num value={row.event_count} /> },
              { key: 'last', header: 'Last seen', width: '14%', cell: (row) => <When at={row.last_seen} /> },
            ]}
            emptyState={null}
          />
        </>
      )}
    </SectionState>
  );
}

/* ── Sessions ────────────────────────────────────────────────────────────── */

export function SessionsSection({ identityId, state }) {
  const query = useQuery((signal) => fetchIdentityEvents(identityId, 'sessions', eventQuery(state), signal), [identityId, filterKey(state)]);
  const rows = query.data?.sessions ?? [];
  return (
    <SectionState query={query}>
      {typeof query.data?.total_sessions === 'number' && (
        <p className="mb-3 text-[12.5px] text-ink-3">
          <span data-numeric="" className="font-semibold text-ink">
            {formatNumber(query.data.total_sessions)}
          </span>{' '}
          {query.data.total_sessions === 1 ? 'session' : 'sessions'} {eventRange(state).phrase}.
        </p>
      )}
      {query.data?.truncated && (
        <p className="mb-3 rounded-[var(--radius-control)] border border-medium/25 bg-medium-soft px-3 py-2 text-[12px] text-ink-2">
          This period held more calls than are read to build sessions, so the oldest sessions may be incomplete. Choose a shorter
          time range for a complete picture.
        </p>
      )}
      {rows.length > 0 && <SessionLanes sessions={rows} />}
      <DataGrid
        caption="Sessions"
        rows={rows}
        rowKey={(row, index) => `${row.start_time}-${row.session_name}-${index}`}
        density="compact"
        columns={[
          {
            key: 'session',
            header: 'Session',
            primary: true,
            width: '20%',
            cell: (row) => (
              <span className="block min-w-0">
                <span className="block truncate font-mono text-[12.5px] text-ink">{row.session_name || 'Unnamed session'}</span>
                {(row.credential_name || row.credential_arn) && (
                  <span className="block truncate text-[11px] text-ink-3" title={row.credential_arn}>
                    as {row.credential_name || row.credential_arn}
                  </span>
                )}
              </span>
            ),
          },
          { key: 'start', header: 'Started', width: '14%', cell: (row) => <When at={row.start_time} /> },
          {
            key: 'duration',
            header: 'Duration',
            width: '10%',
            cell: (row) => <span className="text-[12px] text-ink-2">{row.duration_minutes > 0 ? `${formatNumber(row.duration_minutes)} min` : 'Under 1 min'}</span>,
          },
          { key: 'events', header: 'Calls', align: 'right', width: '8%', cell: (row) => <Num value={row.event_count} /> },
          { key: 'services', header: 'Services', width: '14%', cell: (row) => <ServiceChips services={row.services} /> },
          {
            key: 'what',
            header: 'Actions',
            width: '16%',
            priority: 'wide',
            cell: (row) => <ActionList actions={row.actions} />,
          },
          {
            key: 'ips',
            header: 'From',
            width: '14%',
            priority: 'wide',
            cell: (row) => <span className="block truncate font-mono text-[12px] text-ink-2">{(row.source_ips ?? []).join(', ') || 'Not recorded'}</span>,
          },
        ]}
        emptyState={<EmptyState compact icon={Activity} title="No sessions in this period" description="No call was recorded. Try a longer range." />}
      />
    </SectionState>
  );
}

/**
 * Sessions as blocks on one time axis, one lane per credential: overlapping
 * sessions, quiet gaps and an unusually long session read at a glance, which
 * the table below cannot show. The table stays the record of each session.
 */
function SessionLanes({ sessions }) {
  const view = useMemo(() => {
    const parsed = sessions
      .map((row) => {
        const start = Date.parse(row.start_time);
        const end = Date.parse(row.end_time || row.start_time);
        return Number.isFinite(start) ? { row, start, end: Number.isFinite(end) && end > start ? end : start } : null;
      })
      .filter(Boolean);
    if (parsed.length === 0) return null;
    const from = Math.min(...parsed.map((s) => s.start));
    const to = Math.max(...parsed.map((s) => s.end), from + 60000);
    const lanes = new Map();
    for (const item of parsed) {
      const key = item.row.credential_name || item.row.credential_arn || item.row.session_name || 'Unknown credential';
      if (!lanes.has(key)) lanes.set(key, []);
      lanes.get(key).push(item);
    }
    const ticks = Array.from({ length: 5 }, (_, i) => from + ((to - from) * i) / 4);
    return { from, to, lanes: [...lanes.entries()], ticks };
  }, [sessions]);
  if (!view) return null;
  const span = view.to - view.from;
  const at = (t) => `${((t - view.from) / span) * 100}%`;

  return (
    <div className="mb-5 rounded-[var(--radius-control)] border border-line bg-surface-2/50 p-3.5">
      <p className="text-[11px] font-semibold tracking-[0.08em] text-ink-3 uppercase">Session timeline</p>
      <div className="mt-3 flex flex-col gap-2">
        {view.lanes.map(([lane, items], index) => (
          <div key={lane} className="grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)] items-center gap-3">
            <span className="truncate font-mono text-[11.5px] text-ink-2" title={lane}>
              {lane}
            </span>
            <div className="relative h-5 rounded bg-track">
              {items.map((item) => (
                <span
                  key={`${item.start}-${item.row.session_name}`}
                  className="absolute top-0.5 bottom-0.5 min-w-[4px] rounded-[3px] opacity-90 hover:opacity-100"
                  style={{
                    left: at(item.start),
                    width: `max(4px, ${((item.end - item.start) / span) * 100}%)`,
                    background: SERIES_TOKENS[index % SERIES_TOKENS.length],
                  }}
                  title={`${item.row.session_name || 'Session'} · ${formatDateTime(item.row.start_time)} · ${
                    item.row.duration_minutes > 0 ? `${formatNumber(item.row.duration_minutes)} min` : 'under 1 min'
                  } · ${formatNumber(item.row.event_count)} calls${item.row.actions?.length ? ` · ${item.row.actions.slice(0, 4).join(', ')}` : ''}`}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="mt-1.5 grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)] gap-3">
        <span />
        <div className="flex justify-between text-[10.5px] text-ink-3">
          {view.ticks.map((tick) => (
            <span key={tick}>{new Date(tick).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</span>
          ))}
        </div>
      </div>
    </div>
  );
}

/* ── Credentials used ────────────────────────────────────────────────────── */

export function CredentialsUsedSection({ identityId, state, onFilter }) {
  const query = useQuery((signal) => fetchIdentityEvents(identityId, 'credentials', eventQuery(state), signal), [identityId, filterKey(state)]);
  const rows = query.data?.credentials_used ?? [];
  return (
    <SectionState query={query}>
      <DataGrid
        caption="Credentials used"
        rows={rows}
        rowKey={(row) => row.credential_arn}
        onRowClick={onFilter ? (row) => onFilter({ cred: row.credential_arn }) : undefined}
        density="compact"
        columns={[
          {
            key: 'credential',
            header: 'Credential',
            primary: true,
            width: '28%',
            cell: (row) => (
              <span className="block min-w-0" title={row.credential_arn}>
                <span className="block truncate font-mono text-[12.5px] text-ink">{row.credential_name || row.credential_arn}</span>
                <span className="block truncate text-[11px] text-ink-3">
                  {credentialTypeMeta(row.credential_type).label}
                  {row.binding_type ? ` · ${bindingLabel(row.binding_type)}` : ''}
                </span>
              </span>
            ),
          },
          { key: 'calls', header: 'Calls', align: 'right', width: '9%', cell: (row) => <Num value={row.event_count} /> },
          { key: 'resources', header: 'Resources', align: 'right', width: '10%', cell: (row) => <Num value={row.unique_resources_count} /> },
          { key: 'actions', header: 'Actions', width: '27%', priority: 'wide', cell: (row) => <ActionList actions={row.actions_performed} /> },
          { key: 'first', header: 'First used', width: '13%', cell: (row) => <When at={row.first_used} /> },
          { key: 'last', header: 'Last used', width: '13%', cell: (row) => <When at={row.last_used} /> },
        ]}
        emptyState={<EmptyState compact title="No credential used in this period" description="No call was recorded. Try a longer range." />}
      />
    </SectionState>
  );
}

/* ── Small parts ─────────────────────────────────────────────────────────── */

function Num({ value, tone }) {
  return (
    <span data-numeric="" className={`text-[12.5px] font-semibold ${tone || 'text-ink'}`}>
      {formatNumber(value)}
    </span>
  );
}

function When({ at }) {
  return (
    <span className="text-[12px] whitespace-nowrap text-ink-2" title={formatDateTime(at)}>
      {formatRelative(at)}
    </span>
  );
}

function ServiceChips({ services }) {
  const list = services ?? [];
  if (list.length === 0) return <span className="text-[12px] text-ink-3">None recorded</span>;
  return (
    <span className="flex min-w-0 flex-wrap gap-1" title={list.join(', ')}>
      {list.slice(0, 3).map((service) => (
        <Tag key={service} tone="neutral" size="sm">
          {service}
        </Tag>
      ))}
      {list.length > 3 && <span className="self-center text-[11px] text-ink-3">+{list.length - 3}</span>}
    </span>
  );
}

function ActionList({ actions }) {
  const list = actions ?? [];
  if (list.length === 0) return <span className="text-[12px] text-ink-3">None recorded</span>;
  return (
    <span className="flex min-w-0 flex-wrap gap-1" title={list.join(', ')}>
      {list.slice(0, 2).map((action) => (
        <Code key={action}>{action}</Code>
      ))}
      {list.length > 2 && <span className="self-center text-[11px] text-ink-3">+{list.length - 2}</span>}
    </span>
  );
}

