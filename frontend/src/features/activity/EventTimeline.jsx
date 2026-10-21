import { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, CalendarClock, DatabaseZap, Eye, List, PenLine, SearchX } from 'lucide-react';
import { fetchIdentity, fetchIdentityEvents } from '../../lib/api/inventory';
import { useDebouncedValue, useQuery } from '../../lib/hooks';
import { formatDateTime, formatNumber, formatRelativeShort } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Select } from '../../ui/Field';
import { AppliedFilters } from '../../ui/FacetRail';
import { RecordBar, ResultCount } from '../../ui/WorkArea';
import { RefreshButton, TableToolbar } from '../../ui/TableTools';
import { SegmentedControl } from '../../ui/Tabs';
import { CellStack, DataGrid } from '../../ui/DataGrid';
import { Pagination } from '../../ui/Pagination';
import { EmptyState, ErrorState } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { cn } from '../../ui/cn';
import { ActivityFeed } from './ActivityFeed';
import { EventDetailDrawer } from './EventDetailDrawer';
import { activeEventFilters, activityRange, EVENT_RANGES, eventQuery, eventRange, rangeQuery, todayUtc, validDates } from './eventState';

const VIEWS = [
  { value: 'table', label: 'Table', icon: List },
  { value: 'timeline', label: 'Timeline', icon: CalendarClock },
];

const RESULT_OPTIONS = [
  { value: 'all', label: 'Any result' },
  { value: 'success', label: 'Succeeded' },
  { value: 'error', label: 'Failed' },
];

const RW_OPTIONS = [
  { value: 'all', label: 'Reads and writes' },
  { value: 'read', label: 'Read-only' },
  { value: 'write', label: 'Mutating' },
];

/**
 * An identity's CloudTrail events: `section=timeline`, filtered and paged on
 * the server. Every filter is one parameter of the events endpoint (see
 * `EventFilters`).
 */
export function EventTimeline({ identityId, state, patch, clearFilters, compact = false, onCount, leading }) {
  const range = eventRange(state);

  const query = useQuery(
    (signal) =>
      fetchIdentityEvents(identityId, 'timeline', eventQuery(state, { page: state.page, pageSize: state.size }), signal),
    [identityId, state.range, state.from, state.to, state.service, state.action, state.result, state.rw, state.resource, state.ip, state.cred, state.page, state.size],
    { enabled: Boolean(identityId) },
  );

  const data = query.data;
  const rows = data?.events ?? [];
  const total = data?.pagination?.total_count ?? data?.total_events ?? 0;
  const unavailable = data && data.data_available === false;
  const loading = query.isLoading && !data;

  useEffect(() => {
    if (onCount && data) onCount(data.total_events);
  }, [onCount, data]);

  const columns = [
    {
      key: 'event',
      header: 'Event',
      primary: true,
      width: '24%',
      cell: (row) => {
        const failed = row.status === 'error' || Boolean(row.error_code);
        return (
          <CellStack
            icon={failed ? AlertTriangle : row.is_read_only ? Eye : PenLine}
            tone={
              failed
                ? 'border-critical/30 bg-critical-soft text-critical'
                : row.is_read_only
                  ? undefined
                  : 'border-high/30 bg-high-soft text-high'
            }
            title={row.event_name || 'Unnamed event'}
            meta={row.event_source || '-'}
          />
        );
      },
    },
    {
      key: 'resource',
      header: 'Resource',
      width: '22%',
      cell: (row) => (
        <span className="block min-w-0" title={row.resource_arn || undefined}>
          <span className="block truncate text-[12.5px] text-ink-2">{row.resource_name || row.resource_arn || '-'}</span>
          <span className="block truncate text-[11px] text-ink-3">
            {row.resource_type ? row.resource_type.replace(/^AWS::/, '') : 'No resource recorded'}
            {row.is_cross_account ? ' · cross-account' : ''}
          </span>
        </span>
      ),
    },
    {
      key: 'result',
      header: 'Result',
      width: '12%',
      cell: (row) => {
        const failed = row.status === 'error' || Boolean(row.error_code);
        return (
          <Tag tone={failed ? 'critical' : row.is_read_only ? 'neutral' : 'high'} size="sm" className="max-w-full">
            <span className="truncate" title={failed ? row.error_message || row.error_code : undefined}>
              {failed ? row.error_code || 'Failed' : row.is_read_only ? 'Read-only' : 'Mutating'}
            </span>
          </Tag>
        );
      },
    },
    {
      key: 'credential',
      header: 'Credential',
      width: '16%',
      priority: 'wide',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate font-mono text-[12px] text-ink-2" title={row.caller_arn}>
            {row.credential_name || '-'}
          </span>
          <span className="block truncate text-[11px] text-ink-3">{row.caller_type || '-'}</span>
        </span>
      ),
    },
    {
      key: 'source',
      header: 'Source',
      width: '14%',
      priority: 'wide',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate font-mono text-[12px] text-ink-2">{row.source_ip || '-'}</span>
          <span className="block truncate text-[11px] text-ink-3" title={row.user_agent}>
            {row.aws_region || '-'}
          </span>
        </span>
      ),
    },
    {
      key: 'time',
      header: 'When',
      width: '10%',
      cell: (row) => (
        <span className="block text-[12.5px] whitespace-nowrap text-ink-2" title={formatDateTime(row.event_time)}>
          {formatRelativeShort(row.event_time)}
        </span>
      ),
    },
  ];

  const filtered = activeEventFilters(state).length > 0;
  const openEvent = (event) => patch({ event: event.event_id });

  return (
    <>
      <RecordBar
        trailing={
          <TableToolbar>
            {!compact && <SegmentedControl label="View mode" options={VIEWS} value={state.view} onChange={(view) => patch({ view })} />}
            <RefreshButton onRefresh={query.refetch} refreshing={query.isRefreshing} label="Refresh events" />
          </TableToolbar>
        }
      >
        {leading}
        <RangePicker state={state} patch={patch} />
        <ResultCount shown={formatNumber(rows.length)} total={formatNumber(total)} unit="events" filtered={filtered} loading={loading} />
      </RecordBar>

      <EventFilters identityId={identityId} state={state} patch={patch} clearFilters={clearFilters} compact={compact} />

      {query.isError && !data ? (
        <ErrorState error={query.error} onRetry={query.refetch} />
      ) : unavailable ? (
        <EmptyState
          icon={DatabaseZap}
          title="Event store unavailable"
          description="CloudTrail events cannot be read right now, so this identity's activity is unknown - not empty. The inventory is unaffected. Try again shortly."
          action={
            <Button variant="secondary" size="sm" onClick={query.refetch}>
              Try again
            </Button>
          }
        />
      ) : state.view === 'timeline' && !compact ? (
        <div className={cn('p-4 sm:p-5', query.isRefreshing && 'opacity-55')}>
          <ActivityFeed
            events={rows}
            loading={loading}
            onSelect={openEvent}
            emptyHint={
              filtered
                ? 'No event in this window matches every filter applied.'
                : `No CloudTrail event was recorded for this identity ${range.phrase}.`
            }
          />
        </div>
      ) : compact ? (
        <div className={cn('p-4 sm:p-5', query.isRefreshing && 'opacity-55')}>
          <ActivityFeed
            events={rows}
            loading={loading}
            onSelect={openEvent}
            emptyHint={`No CloudTrail event was recorded for this identity ${range.phrase}.`}
          />
        </div>
      ) : (
        <DataGrid
          caption="CloudTrail events"
          columns={columns}
          rows={rows}
          rowKey={(row, index) => row.event_id || index}
          loading={loading}
          refreshing={query.isRefreshing || (query.isLoading && Boolean(data))}
          onRowClick={openEvent}
          density="compact"
          skeletonRows={10}
          emptyState={
            filtered ? (
              <EmptyState
                icon={SearchX}
                title="No events match these filters"
                description={`Events exist for this identity, but none ${range.phrase} satisfy every filter applied.`}
                action={
                  <Button variant="secondary" size="sm" onClick={clearFilters}>
                    Clear filters
                  </Button>
                }
              />
            ) : (
              <EmptyState
                icon={Eye}
                title="No CloudTrail events in this window"
                description={`Nothing was recorded for this identity or its credentials ${range.phrase}. Try a longer range.`}
              />
            )
          }
        />
      )}

      {total > 0 && !unavailable && (
        <Pagination
          page={state.page}
          pageSize={state.size}
          total={total}
          unit="events"
          onPageChange={(page) => patch({ page })}
          onPageSizeChange={(size) => patch({ size })}
        />
      )}

      <EventDetailDrawer identityId={identityId} eventId={state.event} onClose={() => patch({ event: '' })} />
    </>
  );
}

/**
 * The time range: the fixed windows, or two UTC days chosen with the
 * browser's date inputs (start on or before end, end not after today). A
 * chosen pair is applied only once it is valid; until then the inputs say why.
 */
export function RangePicker({ state, patch }) {
  /* An invalid pair is held here; a valid one goes straight to the state. */
  const [pending, setPending] = useState(null);
  const draft = pending ?? { from: state.from, to: state.to };
  const today = todayUtc();

  const choose = (value) => {
    setPending(null);
    if (value !== 'custom') return patch({ range: value, from: '', to: '' });
    const from = new Date(Date.now() - 6 * 86400000).toISOString().slice(0, 10);
    return patch({ range: 'custom', from, to: today });
  };
  const edit = (key, value) => {
    const next = { ...draft, [key]: value };
    const valid = validDates(next.from, next.to);
    setPending(valid ? null : next);
    if (valid) patch({ range: 'custom', ...next });
  };
  const problem =
    state.range !== 'custom' || validDates(draft.from, draft.to)
      ? null
      : !draft.from || !draft.to
        ? 'Choose both days.'
        : draft.to > today
          ? 'The end cannot be after today.'
          : 'The start must be on or before the end.';

  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <SegmentedControl
        label="Time range"
        options={EVENT_RANGES.map((entry) => ({ value: entry.value, label: entry.label, title: entry.title }))}
        value={state.range}
        onChange={choose}
      />
      {state.range === 'custom' && (
        <span className="flex min-w-0 flex-wrap items-center gap-1.5" title="Days are UTC; the end day is included">
          <input
            type="date"
            aria-label="From day (UTC)"
            value={draft.from}
            max={draft.to || today}
            onChange={(event) => edit('from', event.target.value)}
            className={cn(DATE_INPUT, problem && 'border-critical')}
          />
          <span className="text-[12px] text-ink-3">to</span>
          <input
            type="date"
            aria-label="To day (UTC)"
            value={draft.to}
            min={draft.from || undefined}
            max={today}
            onChange={(event) => edit('to', event.target.value)}
            className={cn(DATE_INPUT, problem && 'border-critical')}
          />
          {problem && (
            <span role="alert" className="text-[11.5px] text-critical">
              {problem}
            </span>
          )}
        </span>
      )}
    </div>
  );
}

const DATE_INPUT =
  'h-8 min-w-0 rounded-[var(--radius-control)] border border-line-strong bg-surface px-2 text-[12.5px] text-ink focus:border-brand';

/**
 * The filters of the events endpoint, and the chips of those applied. The
 * same filters narrow every section of the identity's events, so they are
 * shown above each one. The choices for service and action come from the
 * identity's own activity summary for the same window, the source IPs and
 * credentials from their sections - so a filter only offers a value that
 * occurs, and the match (exact, as the API applies it) can always succeed.
 * Resource is a free-text "contains" match, which is what the API does with
 * it. `compact` keeps only the chips.
 */
export function EventFilters({ identityId, state, patch, clearFilters, compact = false }) {
  const [resourceDraft, setResourceDraft] = useState(state.resource);
  const debouncedResource = useDebouncedValue(resourceDraft.trim(), 400);

  useEffect(() => setResourceDraft(state.resource), [state.resource]);
  useEffect(() => {
    if (debouncedResource !== state.resource) patch({ resource: debouncedResource });
    // Only the settled draft writes the state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedResource]);

  /* Filter choices for the same window, never narrowed by the filters. */
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const activityParams = useMemo(() => activityRange(state), [state.range, state.from, state.to]);
  const windowQuery = rangeQuery(state);
  const summaryQuery = useQuery(
    (signal) => fetchIdentity(identityId, 'activity', { ...activityParams, page_size: 1 }, signal),
    [identityId, activityParams],
    { enabled: Boolean(identityId) && !compact },
  );
  const ipsQuery = useQuery((signal) => fetchIdentityEvents(identityId, 'ips', windowQuery, signal), [identityId, state.range, state.from, state.to], {
    enabled: Boolean(identityId) && !compact,
  });
  const credsQuery = useQuery(
    (signal) => fetchIdentityEvents(identityId, 'credentials', windowQuery, signal),
    [identityId, state.range, state.from, state.to],
    { enabled: Boolean(identityId) && !compact },
  );

  const withCurrent = (options, current, label) =>
    current && !options.some((option) => option.value === current) ? [...options, { value: current, label: label(current) }] : options;

  const serviceOptions = withCurrent(
    (summaryQuery.data?.analytics?.top_services ?? []).map((entry) => ({ value: entry.service, label: `${entry.service} (${formatNumber(entry.count)})` })),
    state.service,
    (value) => value,
  );
  const actionOptions = withCurrent(
    (summaryQuery.data?.analytics?.top_actions ?? []).map((entry) => ({ value: entry.action, label: `${entry.action} (${formatNumber(entry.count)})` })),
    state.action,
    (value) => value,
  );
  const ipOptions = withCurrent(
    (ipsQuery.data?.ips ?? []).map((entry) => ({ value: entry.source_ip, label: `${entry.source_ip} (${formatNumber(entry.event_count)})` })),
    state.ip,
    (value) => value,
  );
  const credOptions = withCurrent(
    (credsQuery.data?.credentials_used ?? []).map((entry) => ({
      value: entry.credential_arn,
      label: `${entry.credential_name || entry.credential_arn} (${formatNumber(entry.event_count)})`,
    })),
    state.cred,
    (value) => value.split('/').pop(),
  );
  const credName = (arn) =>
    (credsQuery.data?.credentials_used ?? []).find((entry) => entry.credential_arn === arn)?.credential_name || arn.split('/').pop();

  const chips = useMemo(() => {
    const labels = {
      service: ['Service', state.service],
      action: ['Action', state.action],
      result: ['Result', state.result === 'error' ? 'Failed' : 'Succeeded'],
      rw: ['Mode', state.rw === 'read' ? 'Read-only' : 'Mutating'],
      resource: ['Resource contains', state.resource],
      ip: ['Source IP', state.ip],
      cred: ['Credential', state.cred ? credName(state.cred) : ''],
    };
    return activeEventFilters(state).map((key) => ({ key, label: labels[key][0], value: labels[key][1] }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, credsQuery.data]);

  return (
    <>
      {!compact && (
        <div className="grid gap-2 border-b border-line px-4 py-3 sm:grid-cols-2 sm:px-5 lg:grid-cols-4 xl:grid-cols-7">
          <FilterSelect label="Service" value={state.service} onChange={(value) => patch({ service: value })} options={serviceOptions} placeholder="Any service" loading={summaryQuery.isLoading && !summaryQuery.data} />
          <FilterSelect label="Action" value={state.action} onChange={(value) => patch({ action: value })} options={actionOptions} placeholder="Any action" loading={summaryQuery.isLoading && !summaryQuery.data} />
          <FilterSelect label="Result" value={state.result} onChange={(value) => patch({ result: value })} options={RESULT_OPTIONS} />
          <FilterSelect label="Mode" value={state.rw} onChange={(value) => patch({ rw: value })} options={RW_OPTIONS} />
          <FilterSelect label="Source IP" value={state.ip} onChange={(value) => patch({ ip: value })} options={ipOptions} placeholder="Any source IP" loading={ipsQuery.isLoading && !ipsQuery.data} />
          <FilterSelect label="Credential" value={state.cred} onChange={(value) => patch({ cred: value })} options={credOptions} placeholder="Any credential" loading={credsQuery.isLoading && !credsQuery.data} />
          <label className="flex min-w-0 flex-col gap-1">
            <span className="text-[10.5px] font-semibold tracking-[0.08em] text-ink-3 uppercase">Resource contains</span>
            <input
              type="search"
              value={resourceDraft}
              maxLength={256}
              onChange={(event) => setResourceDraft(event.target.value)}
              placeholder="ARN or name part…"
              className="h-8 w-full min-w-0 rounded-[var(--radius-control)] border border-line-strong bg-surface px-2.5 text-[12.5px] text-ink placeholder:text-ink-3/85 focus:border-brand"
            />
          </label>
        </div>
      )}

      <AppliedFilters
        filters={chips}
        onRemove={(key) => {
          if (key === 'resource') setResourceDraft('');
          patch({ [key]: key === 'result' || key === 'rw' ? 'all' : '' });
        }}
        onClearAll={() => {
          setResourceDraft('');
          clearFilters();
        }}
      />
    </>
  );
}

function FilterSelect({ label, value, onChange, options, placeholder, loading }) {
  return (
    <label className="flex min-w-0 flex-col gap-1">
      <span className="text-[10.5px] font-semibold tracking-[0.08em] text-ink-3 uppercase">{label}</span>
      <Select
        size="sm"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        options={options}
        placeholder={placeholder}
        disabled={loading}
        aria-label={label}
        className="h-8 w-full min-w-0 text-[12.5px]"
      />
    </label>
  );
}
