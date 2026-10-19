import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { KeyRound, SearchX } from 'lucide-react';
import { countCredentials, fetchCredentials, fetchFilters } from '../../lib/api/inventory';
import { useDebouncedValue, useQuery } from '../../lib/hooks';
import { useAccess } from '../../app/useAccess';
import { formatDateTime, formatNumber, formatRelative, percentValue } from '../../lib/format';
import { exportRowsToCsv, timestampedName } from '../../lib/csv';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { Panel, PanelHeader } from '../../ui/Panel';
import { SearchInput } from '../../ui/Field';
import { AppliedFilters, FacetRail, useFacetRail } from '../../ui/FacetRail';
import { RecordBar, ResultCount, ShowFiltersButton, WorkArea } from '../../ui/WorkArea';
import { OverflowMenu, RefreshButton, TableSettings, TableToolbar } from '../../ui/TableTools';
import { MetricTile } from '../../ui/Stat';
import { StatStripSkeleton } from '../../ui/Skeleton';
import { useToast } from '../../ui/Toast';
import { CellStack, DataGrid } from '../../ui/DataGrid';
import { Pagination } from '../../ui/Pagination';
import { Tag } from '../../ui/Tag';
import { EmptyState, ErrorState, InlineError } from '../../ui/States';
import { cn } from '../../ui/cn';
import { API_CREDENTIAL_STATUSES, API_CREDENTIAL_TYPES, credentialTypeMeta, lifecycleMeta } from '../identities/meta';
import { splitList } from '../identities/filters';
import { RecentChangeMarker } from '../changes/parts';
import { CredentialDrawer } from './CredentialDrawer';

/**
 * Credential register: one row per credential - roles, keys, secrets,
 * certificates - which is the shape an operator needs when the question is
 * "what needs rotating" rather than "who owns what".
 *
 * Search, filters, sort and page run on the server and live in the URL
 * (`/credentials?search=<name>` is how other screens link here; `credential=`
 * opens one in the drawer). Every count is the API's: one filtered request
 * each, never counted from the loaded page.
 */

/* Risk flags: each is exactly one parameter of `GET /api/v1/credentials`. */
const FLAGS = [
  { key: 'expired', label: 'Already expired', param: 'max_days_until_expiry', value: '0', tone: 'critical', query: { maxDaysUntilExpiry: 0 } },
  { key: 'expiring', label: 'Expires within 30 days', param: 'max_days_until_expiry', value: '30', tone: 'high', query: { maxDaysUntilExpiry: 30 }, note: 'includes expired' },
  { key: 'overdue', label: 'Rotation overdue', param: 'rotation_overdue', value: 'true', tone: 'high', query: { rotationOverdue: 'true' } },
  { key: 'stale', label: 'Unused 90+ days', param: 'min_idle_days', value: '90', tone: 'medium', query: { minIdleDays: 90 } },
  { key: 'orphaned', label: 'No owner identity', param: 'is_orphaned', value: 'true', tone: 'medium', query: { isOrphaned: 'true' } },
  { key: 'external', label: 'External trust', param: 'has_external_trust', value: 'true', tone: 'medium', query: { hasExternalTrust: 'true' } },
];

const FILTER_PARAMS = ['search', 'credential_type', 'status', ...new Set(FLAGS.map((flag) => flag.param))];

const SORTABLE = {
  credential: 'name',
  status: 'status',
  bound: 'bound_identities_count',
  age: 'age_days',
  rotation: 'days_until_expiry',
  lastUsed: 'last_used_at',
};

const DENSITY_KEY = 'dna.grid.density';

function readQuery(searchParams) {
  const raw = searchParams.get('sort') || '';
  const desc = raw.startsWith('-');
  const column = desc ? raw.slice(1) : raw;
  const sortKey = Object.keys(SORTABLE).find((key) => SORTABLE[key] === column);
  const pageSize = Number(searchParams.get('page_size'));
  const intParam = (name) => (/^-?\d+$/.test(searchParams.get(name) || '') ? Number(searchParams.get(name)) : undefined);
  const boolParam = (name) => (['true', 'false'].includes(searchParams.get(name)) ? searchParams.get(name) : undefined);
  return {
    sort: sortKey ? { key: sortKey, direction: desc ? 'desc' : 'asc' } : null,
    query: {
      search: searchParams.get('search') || '',
      credentialType: splitList(searchParams.get('credential_type'), API_CREDENTIAL_TYPES),
      status: splitList(searchParams.get('status'), new Set(API_CREDENTIAL_STATUSES)),
      rotationOverdue: boolParam('rotation_overdue'),
      isOrphaned: boolParam('is_orphaned'),
      hasExternalTrust: boolParam('has_external_trust'),
      maxDaysUntilExpiry: intParam('max_days_until_expiry'),
      minIdleDays: intParam('min_idle_days'),
      sortBy: sortKey ? column : undefined,
      sortOrder: sortKey ? (desc ? 'desc' : 'asc') : undefined,
      page: Math.max(1, Number(searchParams.get('page')) || 1),
      pageSize: [10, 25, 50, 100].includes(pageSize) ? pageSize : 25,
    },
  };
}

export default function CredentialsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { notify } = useToast();
  const { can } = useAccess();
  const [selectedRow, setSelectedRow] = useState(null);
  const { railOpen, toggleRail } = useFacetRail();
  const [density, setDensity] = useState(() => {
    try {
      return localStorage.getItem(DENSITY_KEY) || 'comfortable';
    } catch {
      return 'comfortable';
    }
  });
  const setDensityPref = (value) => {
    setDensity(value);
    try {
      localStorage.setItem(DENSITY_KEY, value);
    } catch {
      /* per-viewer convenience only */
    }
  };

  const [searchDraft, setSearchDraft] = useState(() => searchParams.get('search') || '');
  const debouncedSearch = useDebouncedValue(searchDraft, 320);
  const urlSearch = searchParams.get('search') || '';
  useEffect(() => {
    setSearchDraft((draft) => (draft.trim() === urlSearch ? draft : urlSearch));
  }, [urlSearch]);
  useEffect(() => {
    const value = debouncedSearch.trim();
    if (value === (searchParams.get('search') || '')) return;
    const next = new URLSearchParams(searchParams);
    if (value) next.set('search', value);
    else next.delete('search');
    next.delete('page');
    setSearchParams(next, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch]);

  const { query: filters, sort } = useMemo(() => readQuery(searchParams), [searchParams]);
  const query = useQuery((signal) => fetchCredentials(filters, signal), [JSON.stringify(filters)]);
  const facetsQuery = useQuery((signal) => fetchFilters(signal), []);

  const countsQuery = useQuery(
    async (signal) => {
      const flagCounts = await Promise.all(FLAGS.map((flag) => countCredentials(flag.query, signal)));
      const statusCounts = await Promise.all(API_CREDENTIAL_STATUSES.map((status) => countCredentials({ status: [status] }, signal)));
      const total = await countCredentials({}, signal);
      return {
        total,
        flags: Object.fromEntries(FLAGS.map((flag, index) => [flag.key, flagCounts[index]])),
        statuses: Object.fromEntries(API_CREDENTIAL_STATUSES.map((status, index) => [status, statusCounts[index]])),
      };
    },
    [],
  );
  const counts = countsQuery.data;

  const update = useCallback(
    (mutate) => {
      const next = new URLSearchParams(searchParams);
      mutate(next);
      next.delete('page');
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const toggleList = (param, value) =>
    update((next) => {
      const values = splitList(next.get(param)).map((entry) => entry.toLowerCase());
      const updated = values.includes(value) ? values.filter((entry) => entry !== value) : [...values, value];
      if (updated.length) next.set(param, updated.join(','));
      else next.delete(param);
    });

  const toggleFlag = (flag) =>
    update((next) => {
      if (next.get(flag.param) === flag.value) next.delete(flag.param);
      else next.set(flag.param, flag.value);
    });

  const clearAll = () => {
    update((next) => FILTER_PARAMS.forEach((param) => next.delete(param)));
    setSearchDraft('');
  };

  const openCredential = (row) => {
    setSelectedRow(row);
    const next = new URLSearchParams(searchParams);
    next.set('credential', row.id);
    setSearchParams(next, { replace: true });
  };
  const closeCredential = () => {
    setSelectedRow(null);
    const next = new URLSearchParams(searchParams);
    next.delete('credential');
    setSearchParams(next, { replace: true });
  };
  const openId = searchParams.get('credential') || '';

  const chips = useMemo(() => {
    const list = [];
    if (filters.search) list.push({ key: 'search', label: 'Search', value: filters.search });
    filters.credentialType.forEach((value) => list.push({ key: `credential_type=${value}`, label: 'Type', value: credentialTypeMeta(value).label }));
    filters.status.forEach((value) => list.push({ key: `status=${value}`, label: 'Status', value: lifecycleMeta(value).label }));
    FLAGS.forEach((flag) => {
      if (searchParams.get(flag.param) === flag.value) list.push({ key: flag.param, label: 'Flag', value: flag.label });
    });
    return list;
  }, [filters, searchParams]);

  const removeChip = (key) => {
    if (key === 'search') setSearchDraft('');
    const [param, value] = key.split('=');
    if (value !== undefined) toggleList(param, value);
    else update((next) => next.delete(param));
  };

  const facetGroups = [
    {
      key: 'flags',
      label: 'Needs attention',
      options: FLAGS.map((flag) => ({
        value: flag.key,
        label: flag.label,
        count: counts?.flags[flag.key],
        active: searchParams.get(flag.param) === flag.value,
      })),
      onToggle: (value) => toggleFlag(FLAGS.find((flag) => flag.key === value)),
      note: 'Unused is measured from the last CloudTrail event that used the credential.',
    },
    {
      key: 'type',
      label: 'Credential type',
      options: (facetsQuery.data?.credential_types ?? [])
        .filter((entry) => API_CREDENTIAL_TYPES.has(String(entry.type).toLowerCase()) && Number(entry.count) > 0)
        .map((entry) => ({
          value: entry.type,
          label: credentialTypeMeta(entry.type).label,
          count: Number(entry.count),
          active: filters.credentialType.includes(entry.type),
        })),
      onToggle: (value) => toggleList('credential_type', value),
      note: 'Roles that a workload uses are listed here as credentials. Stand-alone roles, such as cross-account roles, also appear on Identities.',
    },
    {
      key: 'status',
      label: 'Status',
      options: API_CREDENTIAL_STATUSES.filter((status) => (counts?.statuses[status] ?? 0) > 0).map((status) => ({
        value: status,
        label: lifecycleMeta(status).label,
        count: counts?.statuses[status],
        active: filters.status.includes(status),
      })),
      onToggle: (value) => toggleList('status', value),
    },
  ];

  const setSort = (next) => {
    const column = SORTABLE[next.key];
    if (!column) return;
    const params = new URLSearchParams(searchParams);
    params.set('sort', next.direction === 'desc' ? `-${column}` : column);
    params.delete('page');
    setSearchParams(params, { replace: true });
  };

  const rows = query.data?.rows ?? [];
  const total = query.data?.total ?? 0;
  const activityAvailable = query.data?.activityAvailable !== false;
  const maxAge = rows.reduce((max, row) => Math.max(max, Number(row.age_days) || 0), 0);
  const selected = selectedRow?.id === openId ? selectedRow : rows.find((row) => row.id === openId) ?? null;

  const onExport = () => {
    exportRowsToCsv({
      filename: timestampedName('credentials'),
      columns: [
        { header: 'Name', value: (row) => row.name },
        { header: 'ARN', value: (row) => row.arn },
        { header: 'Type', value: (row) => row.credential_type },
        { header: 'Account', value: (row) => row.account_id },
        { header: 'Region', value: (row) => row.region },
        { header: 'Status', value: (row) => row.status },
        { header: 'Bound identities', value: (row) => row.bound_identities_count },
        { header: 'Bound to no identity', value: (row) => (row.is_orphaned ? 'yes' : 'no') },
        { header: 'Owner principal', value: (row) => row.owner_principal_arn },
        { header: 'Age days', value: (row) => row.age_days },
        { header: 'Last used', value: (row) => row.last_used_at },
        { header: 'Idle days', value: (row) => row.idle_days },
        { header: 'Rotation enabled', value: (row) => (row.rotation_enabled ? 'yes' : 'no') },
        { header: 'Rotation overdue', value: (row) => (row.rotation_overdue ? 'yes' : 'no') },
        { header: 'Last rotated', value: (row) => row.last_rotated_at },
        { header: 'Expires', value: (row) => row.expires_at },
        { header: 'Days until expiry', value: (row) => row.days_until_expiry },
        { header: 'External trust', value: (row) => (row.has_external_trust ? 'yes' : 'no') },
        { header: 'Permission boundary', value: (row) => row.permission_boundary_arn },
      ],
      rows,
    });
    notify({
      variant: 'success',
      title: 'Exported this page',
      description: `${formatNumber(rows.length)} rows written to CSV. Only the loaded page is included; widen the page size to export more.`,
    });
  };

  const columns = [
    {
      key: 'credential',
      header: 'Credential',
      primary: true,
      sortable: true,
      width: '25%',
      cell: (row) => (
        <CellStack
          icon={KeyRound}
          title={credentialTypeMeta(row.credential_type).label}
          meta={row.name || row.arn}
          mono
          badge={<RecentChangeMarker change={row.recent_change} />}
        />
      ),
    },
    {
      key: 'bound',
      header: 'Bound to',
      sortable: true,
      width: '15%',
      cell: (row) =>
        row.is_orphaned || row.bound_identities_count === 0 ? (
          <span className="block min-w-0">
            <Tag tone="high" size="sm">
              Orphaned
            </Tag>
            <span className="mt-0.5 block truncate text-[11px] text-ink-3">No identity bound</span>
          </span>
        ) : (
          <span className="block min-w-0">
            <span className="block text-[12.5px] text-ink-2">
              {formatNumber(row.bound_identities_count)} identit{row.bound_identities_count === 1 ? 'y' : 'ies'}
            </span>
            <span className="block truncate font-mono text-[11px] text-ink-3" title={`Account ${row.account_id}`}>
              {row.account_id}
              {row.has_external_trust ? ' · external trust' : ''}
            </span>
          </span>
        ),
    },
    {
      key: 'status',
      header: 'Status',
      sortable: true,
      width: '10%',
      cell: (row) => {
        const meta = lifecycleMeta(row.status);
        return (
          <Tag tone={meta.tone} size="sm" dot>
            {meta.label}
          </Tag>
        );
      },
    },
    {
      key: 'age',
      header: 'Age',
      sortable: true,
      width: '13%',
      cell: (row) => {
        const age = Number(row.age_days);
        if (!Number.isFinite(age) || row.age_days === null) return <span className="text-[12.5px] text-ink-3">-</span>;
        const share = maxAge > 0 ? Math.max(2, (age / maxAge) * 100) : 0;
        /* Only a long-lived credential's age is overdue-ness. A role that has
           existed for two years is not two years overdue for anything. */
        const longLived = credentialTypeMeta(row.credential_type).longLived;
        const tone = !longLived ? 'bg-brand' : age > 365 ? 'bg-critical' : age > 180 ? 'bg-high' : age > 90 ? 'bg-medium' : 'bg-brand';
        return (
          <span className="block min-w-0">
            <span data-numeric="" className="block text-[12.5px] whitespace-nowrap text-ink-2">
              {formatNumber(age)} days
            </span>
            <span className="mt-1.5 block h-[3px] w-full overflow-hidden rounded-full bg-track">
              <span className={`block h-full rounded-full transition-[width] duration-700 ease-[var(--ease-out-quint)] ${tone}`} style={{ width: `${share}%` }} />
            </span>
          </span>
        );
      },
    },
    {
      key: 'rotation',
      header: 'Rotation & expiry',
      sortable: true,
      width: '18%',
      priority: 'wide',
      cell: (row) => <RotationCell row={row} />,
    },
    {
      key: 'lastUsed',
      header: 'Last used',
      sortable: true,
      width: '13%',
      cell: (row) =>
        !activityAvailable ? (
          <span className="text-[12px] text-ink-3">Unknown</span>
        ) : (
          <span className="block min-w-0" title={row.last_used_at ? formatDateTime(row.last_used_at) : undefined}>
            <span className="block text-[12.5px] text-ink-2">{row.last_used_at ? formatRelative(row.last_used_at) : 'Never seen'}</span>
            <span className={cn('block text-[11px]', row.idle_days > 90 ? 'text-high' : 'text-ink-3')}>
              {row.idle_days === null || row.idle_days === undefined ? 'No use recorded' : row.idle_days === 0 ? 'Used today' : `Idle ${formatNumber(row.idle_days)} days`}
            </span>
          </span>
        ),
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Credential register" />

      {countsQuery.isError && !counts ? (
        <InlineError error={countsQuery.error} onRetry={countsQuery.refetch} label="Counts unavailable" />
      ) : !counts ? (
        <StatStripSkeleton count={4} />
      ) : (
        <div className="grid gap-3 @min-[30rem]:grid-cols-2 @min-[54rem]:grid-cols-4">
          <MetricTile label="Credentials in scope" value={counts.total} tone="brand" caption="Roles, keys, secrets and certificates in your accounts" className="animate-rise" />
          <MetricTile
            as={Link}
            to="?rotation_overdue=true"
            label="Rotation overdue"
            value={counts.flags.overdue}
            tone="high"
            caption="Past their rotation schedule"
            meter={percentValue(counts.flags.overdue, counts.total)}
            meterLabel="Share of all credentials"
            className="animate-rise"
          />
          <MetricTile
            as={Link}
            to="?max_days_until_expiry=30"
            label="Expiring in 30 days"
            value={counts.flags.expiring}
            tone="critical"
            caption={counts.flags.expired > 0 ? `${formatNumber(counts.flags.expired)} already expired` : 'None expired yet'}
            className="animate-rise"
          />
          <MetricTile
            as={Link}
            to="?is_orphaned=true"
            label="Orphaned"
            value={counts.flags.orphaned}
            tone="medium"
            caption="Bound to no identity"
            className="animate-rise"
          />
        </div>
      )}

      <Panel prominence="quiet" className="animate-rise">
        <PanelHeader prominence="quiet" title="Rotation queue" subtitle="What to act on first. A credential can sit in more than one row." />
        {!counts ? (
          <div className="mt-3 h-24" />
        ) : (
          <ul className="mt-3 grid gap-x-8 gap-y-2.5 @min-[40rem]:grid-cols-2">
            {FLAGS.map((flag) => {
              const value = counts.flags[flag.key] ?? 0;
              const active = searchParams.get(flag.param) === flag.value;
              return (
                <li key={flag.key}>
                  <button
                    type="button"
                    onClick={() => toggleFlag(flag)}
                    aria-pressed={active}
                    className={cn(
                      'group grid w-full grid-cols-[minmax(0,1fr)_3rem] items-center gap-x-3 gap-y-1 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-surface-3',
                      active && 'bg-info-soft',
                    )}
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <span aria-hidden="true" className="size-2.5 shrink-0 rounded-[3px]" style={{ background: `var(--t-${flag.tone})` }} />
                      <span className="truncate text-[12.5px] text-ink-2 group-hover:text-ink">
                        {flag.label}
                        {flag.note && <span className="text-ink-3"> ({flag.note})</span>}
                      </span>
                    </span>
                    <span data-numeric="" className="text-right text-[13px] font-semibold text-ink">
                      {formatNumber(value)}
                    </span>
                    <span className="col-span-2 block h-1.5 overflow-hidden rounded-full bg-track">
                      <span
                        className="block h-full rounded-full transition-[width] duration-700 ease-[var(--ease-out-quint)]"
                        style={{ width: `${value > 0 ? Math.max(2, percentValue(value, counts.total)) : 0}%`, background: `var(--t-${flag.tone})` }}
                      />
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </Panel>

      <WorkArea
        railOpen={railOpen}
        rail={<FacetRail groups={facetGroups} appliedCount={chips.length} onClearAll={clearAll} onClose={toggleRail} mobileTitle="Filter credentials" />}
      >
        <Panel prominence="lead" flush className="animate-rise overflow-hidden">
          <RecordBar
            trailing={
              <TableToolbar>
                {!railOpen && <ShowFiltersButton onClick={toggleRail} appliedCount={chips.length} />}
                <RefreshButton
                  onRefresh={() => {
                    query.refetch();
                    countsQuery.refetch();
                  }}
                  refreshing={query.isRefreshing}
                />
                <TableSettings density={density} onDensityChange={setDensityPref} />
                {can('data.export') && (
                  <OverflowMenu
                    items={[
                      {
                        key: 'export',
                        label: 'Export this page',
                        hint: `CSV of the ${rows.length} rows shown`,
                        onSelect: onExport,
                        disabled: rows.length === 0,
                        disabledHint: 'Nothing to export - no rows match',
                      },
                    ]}
                  />
                )}
              </TableToolbar>
            }
          >
            <SearchInput size="sm" value={searchDraft} onChange={setSearchDraft} placeholder="Search name or ARN…" className="w-full min-w-0 sm:max-w-sm" />
            <ResultCount shown={formatNumber(rows.length)} total={formatNumber(total)} unit="credentials" filtered={chips.length > 0} paged loading={query.isLoading && !query.data} />
          </RecordBar>

          <AppliedFilters filters={chips} onRemove={removeChip} onClearAll={clearAll} />

          {!activityAvailable && query.data && (
            <p role="status" className="border-b border-line bg-medium-soft px-4 py-2 text-[12px] text-ink-2 sm:px-5">
              CloudTrail data could not be read just now, so last use and idle days are unknown rather than zero.
            </p>
          )}

          {query.isError && !query.data ? (
            <ErrorState error={query.error} onRetry={query.refetch} />
          ) : (
            <>
              <DataGrid
                caption="Credential register"
                columns={columns}
                rows={rows}
                rowKey={(row) => row.id}
                loading={query.isLoading && !query.data}
                refreshing={query.isRefreshing || (query.isLoading && Boolean(query.data))}
                onRowClick={openCredential}
                sort={sort ?? undefined}
                onSortChange={setSort}
                density={density}
                skeletonRows={10}
                emptyState={
                  chips.length > 0 ? (
                    <EmptyState
                      icon={SearchX}
                      title="No credentials match these filters"
                      description="Try a broader type or flag, or clear the search term."
                      action={
                        <Button variant="secondary" size="sm" onClick={clearAll}>
                          Clear filters
                        </Button>
                      }
                    />
                  ) : (
                    <EmptyState icon={KeyRound} title="No credentials discovered" description="Discovery found no credential in the accounts and regions you have selected." />
                  )
                }
              />
              {total > 0 && (
                <Pagination
                  page={filters.page}
                  pageSize={filters.pageSize}
                  total={total}
                  unit="credentials"
                  onPageChange={(page) => {
                    const next = new URLSearchParams(searchParams);
                    next.set('page', String(page));
                    setSearchParams(next, { replace: true });
                  }}
                  onPageSizeChange={(size) => {
                    const next = new URLSearchParams(searchParams);
                    next.set('page_size', String(size));
                    next.delete('page');
                    setSearchParams(next, { replace: true });
                  }}
                />
              )}
            </>
          )}
        </Panel>
      </WorkArea>

      <CredentialDrawer key={openId || 'none'} credentialId={openId} row={selected} onClose={closeCredential} />
    </div>
  );
}

function RotationCell({ row }) {
  const longLived = credentialTypeMeta(row.credential_type).longLived;
  const expiry = row.days_until_expiry;
  let line;
  if (row.rotation_overdue) line = <span className="font-semibold text-high">Rotation overdue</span>;
  else if (row.rotation_enabled) line = <span className="text-ink-2">Rotates on schedule</span>;
  else if (longLived) line = <span className="text-ink-2">{row.last_rotated_at ? `Rotated ${formatRelative(row.last_rotated_at)}` : 'Never rotated'}</span>;
  else line = <span className="text-ink-3">Session credentials</span>;
  return (
    <span className="block min-w-0 text-[12.5px]">
      <span className="block truncate">{line}</span>
      <span
        className={cn(
          'block truncate text-[11px]',
          expiry === null || expiry === undefined ? 'text-ink-3' : expiry < 0 ? 'font-medium text-critical' : expiry <= 30 ? 'font-medium text-high' : 'text-ink-3',
        )}
        title={row.expires_at ? formatDateTime(row.expires_at) : undefined}
      >
        {expiry === null || expiry === undefined ? 'No expiry' : expiry < 0 ? `Expired ${formatNumber(-expiry)} days ago` : `Expires in ${formatNumber(expiry)} days`}
      </span>
    </span>
  );
}
