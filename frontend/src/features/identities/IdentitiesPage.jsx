import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ArrowDownWideNarrow, ArrowUpNarrowWide, Copy, Fingerprint, SearchX } from 'lucide-react';
import { countIdentities, fetchFilters, fetchIdentities, fetchTopOwners } from '../../lib/api/inventory';
import { fetchPostureScoreIndex } from '../../lib/api/posture';
import { ScoreRing } from '../posture/parts';
import { LatestChangesNotice, RecentChangeMarker } from '../changes/parts';
import { bandMeta } from '../../lib/posture';
import { useDebouncedValue, useQuery } from '../../lib/hooks';
import { useAccess } from '../../app/useAccess';
import { formatDate, formatNumber } from '../../lib/format';
import { exportRowsToCsv, timestampedName } from '../../lib/csv';
import { PageHeader } from '../../shell/PageHeader';
import { Button, IconButton } from '../../ui/Button';
import { Panel } from '../../ui/Panel';
import { SearchInput, Select } from '../../ui/Field';
import { AppliedFilters, FacetRail, useFacetRail } from '../../ui/FacetRail';
import { RecordBar, ResultCount, ShowFiltersButton, WorkArea } from '../../ui/WorkArea';
import { Tabs } from '../../ui/Tabs';
import { OverflowMenu, RefreshButton, TableSettings, TableToolbar } from '../../ui/TableTools';
import { DataGrid } from '../../ui/DataGrid';
import { Pagination } from '../../ui/Pagination';
import { EmptyState, ErrorState } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';
import { IdentityDrawer } from './IdentityDrawer';
import { ActivityCell, UtilisationCell } from './status';
import {
  API_ENVIRONMENTS,
  API_FUNCTIONAL_CLASSES,
  API_IDENTITY_STATUSES,
  API_IDENTITY_TYPES,
  environmentLabel,
  functionalClassMeta,
  identityTypeLabel,
  lifecycleMeta,
} from './meta';
import { FILTER_PARAMS, SIGNALS, SORTABLE, SORT_FIELDS, describeFilters, readFilters, readSort, splitList } from './filters';

/**
 * Identity explorer - the workbench every inventory signal leads into.
 *
 * Filter, sort and page live in the URL, so a dashboard drill-through, a
 * bookmark and a shared link all resolve to the same list. View tabs are
 * single-parameter presets that *replace* the current filters; the facet rail
 * refines additively. Both only ever emit parameters `GET /api/v1/identities`
 * accepts, and paging, sorting and search all run on the server.
 */
const VIEW_PRESETS = [
  { key: 'all', label: 'All identities', params: {}, countKey: 'all' },
  { key: 'public', label: 'Publicly accessible', params: { is_publicly_accessible: 'true' }, countKey: 'public' },
  { key: 'stale', label: 'Stale 90+', params: { min_idle_days: '90' }, countKey: 'stale' },
  { key: 'idle', label: 'Idle 30+', params: { min_idle_days: '30' }, countKey: 'idle' },
];

const DENSITY_KEY = 'dna.grid.density';

export default function IdentitiesPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { notify } = useToast();
  const { can } = useAccess();
  const [selected, setSelected] = useState(null);
  const [searchDraft, setSearchDraft] = useState(() => searchParams.get('search') || '');
  const debouncedSearch = useDebouncedValue(searchDraft, 320);
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

  /* A link from elsewhere (`/identities?search=<arn>`) changes the URL while
     the page is mounted; the box follows it. */
  const urlSearch = searchParams.get('search') || '';
  useEffect(() => {
    setSearchDraft((draft) => (draft.trim() === urlSearch ? draft : urlSearch));
  }, [urlSearch]);

  useEffect(() => {
    const value = debouncedSearch.trim();
    const current = searchParams.get('search') || '';
    if (value === current) return;
    const next = new URLSearchParams(searchParams);
    if (value) next.set('search', value);
    else next.delete('search');
    next.delete('page');
    setSearchParams(next, { replace: true });
    // Only a settled draft writes the URL; the URL writing back is handled above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedSearch]);

  const filters = useMemo(() => readFilters(searchParams), [searchParams]);
  const sort = useMemo(() => readSort(searchParams), [searchParams]);
  const chips = useMemo(() => describeFilters(searchParams), [searchParams]);

  const query = useQuery((signal) => fetchIdentities(filters, signal), [JSON.stringify(filters)]);

  /* Facet values and their counts come from the API's own aggregate for the
     user's scope, so a filter never offers a value the inventory lacks. */
  const facetsQuery = useQuery((signal) => fetchFilters(signal), []);
  const ownersQuery = useQuery((signal) => fetchTopOwners(signal), []);
  /* Posture scores by ARN, so each row shows the same score as the Posture page. */
  const postureIndex = useQuery((signal) => fetchPostureScoreIndex(signal), []);
  const facets = facetsQuery.data;

  /* Preset and signal sizes are exact: one filtered request each, one row
     requested, the total read. Never derived from the loaded page. */
  const countsQuery = useQuery(
    async (signal) => {
      const [all, isPublic, stale, idle] = await Promise.all([
        countIdentities({}, signal),
        countIdentities({ isPublic: 'true' }, signal),
        countIdentities({ minIdleDays: 90 }, signal),
        countIdentities({ minIdleDays: 30 }, signal),
      ]);
      return { all, public: isPublic, stale, idle };
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

  /* Enum facets hold comma lists: a click adds or removes one value. */
  const toggleListValue = useCallback(
    (param, value) =>
      update((next) => {
        const values = splitList(next.get(param)).map((entry) => entry.toLowerCase());
        const updated = values.includes(value) ? values.filter((entry) => entry !== value) : [...values, value];
        if (updated.length) next.set(param, updated.join(','));
        else next.delete(param);
      }),
    [update],
  );

  const toggleParam = useCallback(
    (param, value) =>
      update((next) => {
        if (next.get(param) === value) next.delete(param);
        else next.set(param, value);
      }),
    [update],
  );

  const removeChip = useCallback(
    (key) => {
      if (key === 'search') setSearchDraft('');
      const [param, value] = key.split('=');
      if (value !== undefined) toggleListValue(param, value);
      else update((next) => next.delete(param));
    },
    [toggleListValue, update],
  );

  const clearAll = useCallback(() => {
    update((next) => FILTER_PARAMS.forEach((param) => next.delete(param)));
    setSearchDraft('');
  }, [update]);

  const applyPreset = useCallback(
    (key) => {
      const preset = VIEW_PRESETS.find((entry) => entry.key === key);
      if (!preset) return;
      update((next) => {
        FILTER_PARAMS.forEach((param) => next.delete(param));
        Object.entries(preset.params).forEach(([param, value]) => next.set(param, value));
      });
      setSearchDraft('');
    },
    [update],
  );

  /* A preset is active only when the URL holds exactly its parameters. */
  const activePreset = useMemo(() => {
    const applied = FILTER_PARAMS.filter((param) => searchParams.get(param));
    const match = VIEW_PRESETS.find((preset) => {
      const keys = Object.keys(preset.params);
      return keys.length === applied.length && keys.every((key) => searchParams.get(key) === preset.params[key]);
    });
    return match?.key ?? null;
  }, [searchParams]);

  const writeSort = useCallback(
    (column, direction) => {
      const params = new URLSearchParams(searchParams);
      params.set('sort', direction === 'desc' ? `-${column}` : column);
      params.delete('page');
      setSearchParams(params, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const setSort = useCallback(
    (next) => {
      const column = SORTABLE[next.key];
      if (column) writeSort(column, next.direction);
    },
    [writeSort],
  );

  const facetGroups = useMemo(() => {
    const listGroup = (key, label, param, entries, valueKey, allowed, describe, note) => ({
      key,
      label,
      note,
      options: (entries ?? [])
        .filter((entry) => allowed.has(String(entry[valueKey]).toLowerCase()) && Number(entry.count) > 0)
        .map((entry) => ({
          value: entry[valueKey],
          label: describe(entry[valueKey]),
          count: Number(entry.count),
          active: splitList(searchParams.get(param)).some((value) => value.toLowerCase() === String(entry[valueKey]).toLowerCase()),
        })),
      onToggle: (value) => toggleListValue(param, value),
    });
    return [
      {
        key: 'signals',
        label: 'Risk signals',
        options: SIGNALS.map((signal) => ({
          value: signal.key,
          label: signal.label,
          count: counts ? counts[signal.key] : undefined,
          active: searchParams.get(signal.param) === signal.value,
        })),
        onToggle: (value) => {
          const signal = SIGNALS.find((entry) => entry.key === value);
          if (signal) toggleParam(signal.param, signal.value);
        },
        note: 'Idle and stale are measured from the last CloudTrail event of the identity or any credential bound to it.',
      },
      listGroup('type', 'Identity type', 'identity_type', facets?.identity_types, 'type', API_IDENTITY_TYPES, identityTypeLabel),
      listGroup(
        'class',
        'Category',
        'functional_class',
        facets?.functional_classes,
        'class',
        API_FUNCTIONAL_CLASSES,
        (value) => functionalClassMeta(value).label,
      ),
      listGroup('env', 'Environment', 'environment', facets?.environments, 'environment', API_ENVIRONMENTS, environmentLabel),
      listGroup('status', 'Lifecycle status', 'status', facets?.statuses, 'status', API_IDENTITY_STATUSES, (value) => lifecycleMeta(value).label),
      {
        key: 'owner',
        label: 'Owner',
        options: (ownersQuery.data ?? []).map((entry) => ({
          value: entry.owner,
          label: entry.owner,
          count: Number(entry.count),
          active: searchParams.get('owner') === entry.owner,
        })),
        onToggle: (value) => toggleParam('owner', value),
        note: 'The owners holding the most identities. Identities with no owner tag cannot be filtered on; the Owner column reads Unassigned for them.',
        defaultOpen: false,
      },
    ];
  }, [facets, ownersQuery.data, counts, searchParams, toggleListValue, toggleParam]);

  const rows = query.data?.rows ?? [];
  const total = query.data?.total ?? 0;
  const activityAvailable = query.data?.activityAvailable !== false;
  const filtered = chips.length > 0;

  const onExport = () => {
    exportRowsToCsv({
      filename: timestampedName('identities'),
      columns: [
        { header: 'Name', value: (row) => row.name },
        { header: 'ARN', value: (row) => row.arn },
        { header: 'Identity type', value: (row) => row.identity_type },
        { header: 'Category', value: (row) => row.functional_class },
        { header: 'Account', value: (row) => row.account_id },
        { header: 'Region', value: (row) => row.region },
        { header: 'Environment', value: (row) => row.environment },
        { header: 'Owner', value: (row) => row.owner },
        { header: 'Status', value: (row) => row.status },
        { header: 'Publicly accessible', value: (row) => (row.is_publicly_accessible ? 'yes' : 'no') },
        { header: 'VPC', value: (row) => row.vpc_id },
        { header: 'Bound credentials', value: (row) => row.bound_credentials_count },
        { header: 'Permission utilisation %', value: (row) => row.permission_utilization_pct },
        { header: 'Last activity', value: (row) => row.last_activity_at },
        { header: 'Idle days', value: (row) => row.idle_days },
        { header: 'Created (AWS)', value: (row) => row.created_at_aws },
        { header: 'Created by', value: (row) => row.created_by },
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
      key: 'identity',
      header: 'Identity',
      primary: true,
      sortable: true,
      width: '35%',
      cell: (row) => {
        const meta = functionalClassMeta(row.functional_class);
        return (
          <span className="flex min-w-0 items-center gap-3">
            <span
              aria-hidden="true"
              className="grid size-8 shrink-0 place-items-center rounded-[9px] border text-[9.5px] font-bold tracking-tight"
              style={{
                borderColor: `color-mix(in srgb, ${meta.color} 34%, transparent)`,
                background: `color-mix(in srgb, ${meta.color} 10%, transparent)`,
                color: `color-mix(in srgb, ${meta.color} 62%, var(--t-ink))`,
              }}
            >
              {meta.badge}
            </span>
            <span className="min-w-0">
              <span className="flex min-w-0 items-start gap-1.5">
                {/* Two lines before truncating, so a marker beside it never costs the name. */}
                <span className="line-clamp-2 min-w-0 text-[13px] leading-snug font-semibold text-ink [overflow-wrap:anywhere]" title={row.arn}>
                  {row.name || row.arn}
                </span>
                {/* Beside the name, where the eye reads the row; it opens this identity's Changes. */}
                <RecentChangeMarker change={row.recent_change} to={`/identities/${encodeURIComponent(row.id)}?tab=changes`} />
              </span>
              <span className="mt-0.5 flex min-w-0 items-center gap-1.5">
                <span className="truncate text-[11px] text-ink-3">
                  {identityTypeLabel(row.identity_type)} · {meta.label}
                </span>
                {row.is_publicly_accessible && (
                  <span className="shrink-0" title="Reachable from the internet">
                    <Tag tone="critical" size="sm">
                      Public
                    </Tag>
                  </span>
                )}
              </span>
            </span>
          </span>
        );
      },
    },
    {
      key: 'posture',
      header: 'Posture',
      width: '12%',
      cell: (row) => <PostureCell entry={postureIndex.data?.get(row.arn)} loading={!postureIndex.data && !postureIndex.isError} />,
    },
    {
      key: 'owner',
      header: 'Owner',
      width: '14%',
      sortable: true,
      cell: (row) => (
        <span className="block min-w-0">
          <span
            className={row.owner ? 'block truncate text-[12.5px] text-ink-2' : 'block truncate text-[12.5px] text-ink-3 italic'}
            title={row.owner || undefined}
          >
            {row.owner || 'Unassigned'}
          </span>
          {/* Who created it and how long ago, on one line: age reads best beside its creator. */}
          {(row.created_by || Number.isFinite(row.age_days)) && (
            <span
              className="block truncate text-[11px] text-ink-3"
              title={[row.created_by, row.created_at_aws && `Created ${formatDate(row.created_at_aws)}`].filter(Boolean).join(' · ')}
            >
              {[
                row.created_by && `Created by ${row.created_by.split('/').pop()}`,
                Number.isFinite(row.age_days) &&
                  `${row.created_by ? '' : 'Age '}${formatNumber(row.age_days)} day${row.age_days === 1 ? '' : 's'}`,
              ]
                .filter(Boolean)
                .join(' · ')}
            </span>
          )}
        </span>
      ),
    },
    {
      key: 'place',
      header: 'Where',
      width: '14%',
      sortable: true,
      priority: 'wide',
      cell: (row) => (
        <span className="block min-w-0">
          {/* Region first: most identities carry no environment tag, and
              "No environment" would take the line that says where it runs. */}
          <span className="block truncate text-[12.5px] text-ink-2">
            {[row.environment && environmentLabel(row.environment), row.region].filter(Boolean).join(' · ') || '-'}
          </span>
          <span className="block truncate text-[11px] text-ink-3" title={`Account ${row.account_id}, ${row.region}`}>
            <span className="font-mono tabular-nums">{row.account_id}</span>
            {row.status && row.status !== 'active' && row.status !== 'running' ? ` · ${lifecycleMeta(row.status).label}` : ''}
          </span>
        </span>
      ),
    },
    {
      key: 'access',
      header: 'Permissions used',
      width: '12%',
      sortable: true,
      priority: 'wide',
      cell: (row) => <UtilisationCell identity={row} activityAvailable={activityAvailable} />,
    },
    {
      key: 'activity',
      header: 'Activity',
      width: '13%',
      sortable: true,
      cell: (row) => <ActivityCell identity={row} activityAvailable={activityAvailable} />,
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Identity explorer"
        tabs={
          <Tabs
            size="sm"
            value={activePreset ?? 'custom'}
            onChange={applyPreset}
            tabs={[
              ...VIEW_PRESETS.map((preset) => ({
                value: preset.key,
                label: preset.label,
                count: counts ? counts[preset.countKey] : undefined,
              })),
              ...(activePreset === null ? [{ value: 'custom', label: 'Custom filter' }] : []),
            ]}
          />
        }
      />

      <LatestChangesNotice entityType="identity" noun={{ one: 'identity', many: 'identities' }} />

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
                {!railOpen && <ShowFiltersButton onClick={toggleRail} appliedCount={chips.length} />}
                <RefreshButton
                  onRefresh={() => {
                    query.refetch();
                    countsQuery.refetch();
                    facetsQuery.refetch();
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
            <SearchInput
              size="sm"
              value={searchDraft}
              onChange={setSearchDraft}
              placeholder="Search name, ARN or tag…"
              className="w-full min-w-0 sm:max-w-sm"
            />
            <ResultCount
              shown={formatNumber(rows.length)}
              total={formatNumber(total)}
              unit="identities"
              filtered={filtered}
              paged
              loading={query.isLoading && !query.data}
            />
            {/* Every field the API sorts by, including those without a column. */}
            <span className="flex items-center gap-1">
              <Select
                size="sm"
                aria-label="Sort by"
                value={sort.sortBy}
                onChange={(event) => writeSort(event.target.value, sort.direction)}
                options={SORT_FIELDS.map((field) => ({ value: field.value, label: `Sort: ${field.label}` }))}
              />
              <IconButton
                size="sm"
                icon={sort.direction === 'desc' ? ArrowDownWideNarrow : ArrowUpNarrowWide}
                label={sort.direction === 'desc' ? 'Sorted descending - switch to ascending' : 'Sorted ascending - switch to descending'}
                onClick={() => writeSort(sort.sortBy, sort.direction === 'desc' ? 'asc' : 'desc')}
              />
            </span>
          </RecordBar>

          <AppliedFilters filters={chips} onRemove={removeChip} onClearAll={clearAll} />

          {!activityAvailable && query.data && (
            <p role="status" className="border-b border-line bg-medium-soft px-4 py-2 text-[12px] text-ink-2 sm:px-5">
              CloudTrail data could not be read just now, so last activity, idle days and permission use are unknown rather
              than zero. The inventory itself is complete.
            </p>
          )}

          {query.isError && !query.data ? (
            <ErrorState error={query.error} onRetry={query.refetch} />
          ) : (
            <>
              <DataGrid
                caption="Discovered identities"
                columns={columns}
                rows={rows}
                rowKey={(row) => row.id ?? row.arn}
                loading={query.isLoading && !query.data}
                refreshing={query.isRefreshing || (query.isLoading && Boolean(query.data))}
                onRowClick={setSelected}
                sort={{ key: sort.key, direction: sort.direction }}
                onSortChange={setSort}
                density={density}
                rowAccent={(row) => functionalClassMeta(row.functional_class).color}
                skeletonRows={10}
                rowActions={(row) => (
                  <IconButton
                    icon={Copy}
                    label={`Copy ARN for ${row.name || row.arn}`}
                    size="sm"
                    onClick={(event) => {
                      event.stopPropagation();
                      navigator.clipboard?.writeText(row.arn);
                      notify({ variant: 'success', title: 'ARN copied', duration: 2200 });
                    }}
                  />
                )}
                emptyState={
                  filtered ? (
                    <EmptyState
                      icon={SearchX}
                      title="No identities match these filters"
                      description="The inventory holds identities, but none satisfy every filter currently applied."
                      action={
                        <Button variant="secondary" size="sm" onClick={clearAll}>
                          Clear filters
                        </Button>
                      }
                    />
                  ) : (
                    <EmptyState
                      icon={Fingerprint}
                      title="No identities discovered"
                      description="Discovery found nothing in the accounts and regions you have selected. Check the AWS connection on the Integrations screen, or widen your scope."
                    />
                  )
                }
              />

              {total > 0 && (
                <Pagination
                  page={filters.page}
                  pageSize={filters.pageSize}
                  total={total}
                  unit="identities"
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

      <IdentityDrawer
        key={selected?.id ?? 'none'}
        identity={selected}
        activityAvailable={activityAvailable}
        open={Boolean(selected)}
        onClose={() => setSelected(null)}
      />
    </div>
  );
}

/** The identity's posture score and band, as the Posture page states them. */
function PostureCell({ entry, loading }) {
  if (!entry) {
    return <span className="text-[12px] text-ink-3">{loading ? '…' : 'Not scored'}</span>;
  }
  const band = bandMeta(String(entry.band || '').toLowerCase());
  return (
    <span className="flex min-w-0 items-center gap-1.5" title={`Posture score ${entry.score} of 100 - ${band.label}`}>
      <ScoreRing score={entry.score} band={entry.band} size={28} />
      <span className="min-w-0 truncate text-[12px] font-medium" style={{ color: `var(--t-${band.tone})` }}>
        {band.label}
      </span>
    </span>
  );
}
