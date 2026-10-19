import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ArrowRight, Download, Gauge, Loader2, SearchX, ShieldCheck, Sparkles, Wrench } from 'lucide-react';
import { useAccess } from '../../app/useAccess';
import {
  exportPostureCsv,
  fetchPostureFacets,
  fetchPostureIdentities,
  fetchPostureOverview,
  normaliseWindow,
  POSTURE_WINDOWS,
  DEFAULT_WINDOW,
  windowPhrase,
} from '../../lib/api/posture';
import { severityMeta } from '../../lib/domain';
import { formatDateTime, formatNumber, titleCaseEnum } from '../../lib/format';
import { useDebouncedValue, useQuery } from '../../lib/hooks';
import { BANDS, bandMeta } from '../../lib/posture';
import { PageHeader } from '../../shell/PageHeader';
import { TrendChart } from '../../charts/TrendChart';
import { Button, IconButton } from '../../ui/Button';
import { DataGrid } from '../../ui/DataGrid';
import { SearchInput } from '../../ui/Field';
import { AppliedFilters, FacetRail, useFacetRail } from '../../ui/FacetRail';
import { Pagination } from '../../ui/Pagination';
import { Panel, PanelHeader } from '../../ui/Panel';
import { ChartSkeleton, ListSkeleton, Skeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState, InlineError } from '../../ui/States';
import { SegmentedControl } from '../../ui/Tabs';
import { RefreshButton, TableToolbar } from '../../ui/TableTools';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';
import { RecordBar, ResultCount, ShowFiltersButton, WorkArea } from '../../ui/WorkArea';
import { cn, TONE_VAR } from '../../ui/cn';
import { CheckDrawer } from './CheckDrawer';
import { BandTag, Delta, formatScore, identityTypeLabel, pillarLabel, PillarBars, ScoreRing } from './parts';
import { useOpenPostureIdentity } from './useOpenIdentity';

const WINDOW_OPTIONS = POSTURE_WINDOWS.map((value) => ({ value, label: value, title: `Compare with ${windowPhrase(value)} ago` }));

/* The service's band keys, in the console's band order. */
const BAND_KEYS = BANDS.map((band) => band.key.toUpperCase());
const SEVERITY_KEYS = ['Critical', 'High', 'Medium', 'Low'];
const CHANGED = {
  down: 'Went down',
  up: 'Went up',
  same: 'Unchanged',
};

/** "Identities with" keys the overview counts. Shown as counts only: the list has no filter for them. */
const RISK_LABELS = {
  over_permissioned: 'Over-permissioned',
  stale_credentials: 'With a stale credential',
  escalation_paths: 'With an escalation path',
  exposed: 'Exposed to the internet',
  dormant: 'Dormant (unused)',
  trust_issues: 'With a risky trust policy',
};

const LIST_KEYS = ['band', 'severity', 'category', 'account', 'type', 'pillar', 'failing_check'];
const SORTS = ['score', '-score', 'failed', 'change', 'name'];
const PAGE_SIZES = [10, 25, 50, 100];

const categoryLabel = (value) => (value ? titleCaseEnum(value) : 'Unclassified');

/**
 * Posture - identity security posture management.
 *
 * Every identity scored against the same checks in pillars, so the page
 * answers three questions in order: how is the estate doing and which way is
 * it moving (the score and its trend), where is it weakest (distribution,
 * pillars, categories, accounts), and what single fix buys the most (quick
 * wins). The table below is where any of those leads.
 *
 * Everything is the posture service's latest scan. The list, its facet counts
 * and the CSV export take the same filters, so a count and the rows its filter
 * shows always agree.
 */
export default function PosturePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { lock } = useAccess();
  const { notify } = useToast();
  const { railOpen, toggleRail } = useFacetRail();
  const { open, opening } = useOpenPostureIdentity();

  const window = normaliseWindow(searchParams.get('window') || DEFAULT_WINDOW);
  const q = searchParams.get('q') || '';
  const sort = SORTS.includes(searchParams.get('sort')) ? searchParams.get('sort') : 'score';
  const pageSize = PAGE_SIZES.includes(Number(searchParams.get('size'))) ? Number(searchParams.get('size')) : 25;
  const changed = CHANGED[searchParams.get('changed')] ? searchParams.get('changed') : '';
  const lists = useMemo(() => {
    const out = {};
    for (const key of LIST_KEYS) out[key] = (searchParams.get(key) || '').split(',').map((v) => v.trim()).filter(Boolean);
    return out;
  }, [searchParams]);

  const filters = useMemo(() => ({ window, ...lists, changed, q }), [window, lists, changed, q]);
  const filterKey = JSON.stringify(filters);

  const setParams = useCallback(
    (patch) => {
      const next = new URLSearchParams(searchParams);
      for (const [key, value] of Object.entries(patch)) {
        const text = Array.isArray(value) ? value.join(',') : value;
        if (text) next.set(key, text);
        else next.delete(key);
      }
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const toggleValue = useCallback(
    (key, value) => {
      const current = lists[key];
      setParams({ [key]: current.includes(value) ? current.filter((v) => v !== value) : [...current, value] });
    },
    [lists, setParams],
  );

  const clearAll = useCallback(() => {
    const next = new URLSearchParams();
    if (window !== DEFAULT_WINDOW) next.set('window', window);
    if (searchParams.get('sort')) next.set('sort', searchParams.get('sort'));
    if (searchParams.get('size')) next.set('size', searchParams.get('size'));
    setSearchParams(next, { replace: true });
    setSearchText('');
  }, [searchParams, setSearchParams, window]);

  /* Search: typed freely, sent once it settles and is 2-100 characters. */
  const [searchText, setSearchText] = useState(q);
  const debounced = useDebouncedValue(searchText, 350);
  const searchRunes = [...searchText.trim()].length;
  const searchInvalid = searchRunes === 1 || searchRunes > 100;
  useEffect(() => {
    const value = debounced.trim();
    const runes = [...value].length;
    if (runes === 1 || runes > 100) return;
    if (value !== q) setParams({ q: value });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced]);

  const overviewQuery = useQuery((signal) => fetchPostureOverview({ window }, signal), [window]);
  const facetsQuery = useQuery((signal) => fetchPostureFacets(filters, signal), [filterKey]);

  /* Keyset paging: the cursors of the pages visited so far. Any change to the
     filters, sort or page size starts again from the first page. */
  const listKey = `${filterKey}|${sort}|${pageSize}`;
  const [paging, setPaging] = useState({ key: listKey, cursors: [null], page: 1 });
  const current = paging.key === listKey ? paging : { key: listKey, cursors: [null], page: 1 };
  const cursor = current.cursors[current.page - 1] ?? null;
  const listQuery = useQuery(
    (signal) => fetchPostureIdentities(filters, { sort, limit: pageSize, cursor }, signal),
    [listKey, cursor],
  );

  const overview = overviewQuery.data?.overview;
  const overviewMeta = overviewQuery.data?.meta;
  const facets = facetsQuery.data?.facets;
  const rows = listQuery.data?.items ?? [];
  const nextCursor = listQuery.data?.nextCursor ?? null;

  const pillarNames = useMemo(
    () => Object.fromEntries((overview?.pillars ?? []).map((pillar) => [pillar.pillar, pillar.name])),
    [overview],
  );
  const accountNames = useMemo(
    () => Object.fromEntries(Object.entries(facets?.account ?? {}).map(([id, entry]) => [id, entry?.name || id])),
    [facets],
  );
  const quickWinTitle = (ruleId) => overview?.quick_wins?.find((win) => win.rule_id === ruleId)?.title ?? ruleId;

  const goToPage = (page) => {
    setPaging(() => {
      const base = current;
      if (page > base.page) {
        if (!nextCursor) return base;
        const cursors = [...base.cursors.slice(0, base.page), nextCursor];
        return { ...base, cursors, page: base.page + 1 };
      }
      return { ...base, page: Math.max(1, page) };
    });
  };

  const [drawer, setDrawer] = useState(null);
  /* A filter applied from the overview changes the list far below it; bring
     the list into view so the change is seen. */
  const listRef = useRef(null);
  const showList = useCallback(() => {
    requestAnimationFrame(() => listRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }));
  }, []);
  const [exporting, setExporting] = useState(false);

  const onExport = async () => {
    setExporting(true);
    try {
      const { filename } = await exportPostureCsv(filters);
      notify({
        variant: 'success',
        title: 'Export downloaded',
        description: `${filename}${Number.isFinite(facets?.total) ? ` - ${formatNumber(facets.total)} identities` : ''}, with the filters shown.`,
      });
    } catch (error) {
      notify({
        variant: 'error',
        title: 'Export failed',
        description: error?.status === 429 ? `Too many requests. Try again in ${error.retryAfter ?? 'a few'} seconds.` : error?.message,
      });
    } finally {
      setExporting(false);
    }
  };

  const refresh = () => {
    overviewQuery.refetch();
    facetsQuery.refetch();
    listQuery.refetch();
  };

  /* ── Facets ─────────────────────────────────────────────────────────── */

  const optionsFrom = (key, counts, label, order) => {
    const keys = order ?? Object.keys(counts ?? {}).sort((a, b) => label(a).localeCompare(label(b)));
    const selected = lists[key];
    const all = [...keys, ...selected.filter((value) => !keys.includes(value))];
    return all.map((value) => ({
      value,
      label: label(value),
      active: selected.includes(value),
      count: facets ? Number(typeof counts?.[value] === 'object' ? counts[value]?.count : counts?.[value]) || 0 : undefined,
    }));
  };

  const facetGroups = [
    {
      key: 'band',
      label: 'Score band',
      options: optionsFrom('band', facets?.band, (value) => bandMeta(value.toLowerCase()).label, BAND_KEYS),
      onToggle: (value) => toggleValue('band', value),
    },
    {
      key: 'severity',
      label: 'Failing severity',
      options: optionsFrom('severity', facets?.severity, (value) => severityMeta(value).label, SEVERITY_KEYS),
      onToggle: (value) => toggleValue('severity', value),
      note: 'Identities failing at least one check of this severity.',
    },
    {
      key: 'changed',
      label: `Score over ${windowPhrase(window)}`,
      options: Object.entries(CHANGED).map(([value, label]) => ({
        value,
        label,
        active: changed === value,
        count: facets ? Number(facets.changed?.[value]) || 0 : undefined,
      })),
      onToggle: (value) => setParams({ changed: changed === value ? '' : value }),
    },
    {
      key: 'category',
      label: 'Category',
      options: optionsFrom('category', facets?.category, categoryLabel),
      onToggle: (value) => toggleValue('category', value),
    },
    {
      key: 'type',
      label: 'Identity type',
      options: optionsFrom('type', facets?.type, identityTypeLabel),
      onToggle: (value) => toggleValue('type', value),
      defaultOpen: false,
    },
    {
      key: 'pillar',
      label: 'Weakest area',
      options: optionsFrom('pillar', facets?.pillar, (value) => pillarLabel(value, pillarNames)),
      onToggle: (value) => toggleValue('pillar', value),
    },
    {
      key: 'account',
      label: 'Account',
      options: optionsFrom('account', facets?.account, (value) => accountNames[value] ?? value),
      onToggle: (value) => toggleValue('account', value),
    },
  ].filter(Boolean);

  const chipLabel = {
    band: ['Band', (value) => bandMeta(value.toLowerCase()).label],
    severity: ['Failing', (value) => severityMeta(value).label],
    category: ['Category', categoryLabel],
    type: ['Type', identityTypeLabel],
    pillar: ['Weakest area', (value) => pillarLabel(value, pillarNames)],
    account: ['Account', (value) => accountNames[value] ?? value],
    failing_check: ['Failing check', quickWinTitle],
  };
  const chips = [
    ...LIST_KEYS.flatMap((key) =>
      lists[key].map((value) => ({ key: `${key}:${value}`, label: chipLabel[key][0], value: chipLabel[key][1](value) })),
    ),
    changed && { key: 'changed', label: 'Score', value: `${CHANGED[changed].toLowerCase()} over ${window}` },
    q && { key: 'q', label: 'Search', value: q },
  ].filter(Boolean);

  const removeChip = (chipKey) => {
    if (chipKey === 'q') {
      setSearchText('');
      setParams({ q: '' });
      return;
    }
    if (chipKey === 'changed') {
      setParams({ changed: '' });
      return;
    }
    const at = chipKey.indexOf(':');
    toggleValue(chipKey.slice(0, at), chipKey.slice(at + 1));
  };

  /* ── Table ──────────────────────────────────────────────────────────── */

  const gridSort =
    sort === 'name'
      ? { key: 'identity', direction: 'asc' }
      : sort === 'failed'
        ? { key: 'failed', direction: 'desc' }
        : sort === 'change'
          ? { key: 'change', direction: 'asc' }
          : { key: 'score', direction: sort === '-score' ? 'desc' : 'asc' };

  const onSortChange = (next) => {
    const value =
      next.key === 'score'
        ? gridSort.key === 'score' && sort === 'score'
          ? '-score'
          : 'score'
        : next.key === 'identity'
          ? 'name'
          : next.key;
    setParams({ sort: value === 'score' ? '' : value });
  };

  const columns = [
    {
      key: 'identity',
      header: 'Identity',
      primary: true,
      sortable: true,
      width: '20%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate font-mono text-[12.5px] font-semibold text-ink" title={row.name}>
            {row.name}
          </span>
          <span className="block truncate text-[11.5px] text-ink-3">
            {identityTypeLabel(row.type)} · {categoryLabel(row.functional_class)}
          </span>
        </span>
      ),
    },
    {
      key: 'account',
      header: 'Account',
      width: '12%',
      priority: 'wide',
      cell: (row) => (
        <span className="block truncate text-[12.5px] text-ink-2" title={row.account?.id}>
          {row.account?.name || row.account?.id || '-'}
        </span>
      ),
    },
    {
      key: 'score',
      header: 'Score',
      width: '14%',
      sortable: true,
      cell: (row) => (
        <span className="flex min-w-0 items-center gap-2.5">
          <ScoreRing score={row.score} band={row.band} size={34} grade={row.grade} />
          <span className="min-w-0 truncate text-[12px] font-medium" style={{ color: TONE_VAR[bandMeta(String(row.band || '').toLowerCase()).tone] }}>
            {bandMeta(String(row.band || '').toLowerCase()).label}
          </span>
        </span>
      ),
    },
    {
      key: 'pillar',
      header: 'Weakest area',
      width: '17%',
      priority: 'wide',
      cell: (row) =>
        row.weakest_pillar ? (
          <span className="block truncate text-[12.5px] text-ink-2" title={pillarLabel(row.weakest_pillar, pillarNames)}>
            {pillarLabel(row.weakest_pillar, pillarNames)}
          </span>
        ) : (
          <span className="text-[12.5px] text-ink-3">None</span>
        ),
    },
    {
      key: 'failed',
      header: 'Failed checks',
      width: '14%',
      sortable: true,
      cell: (row) => <FailedCell failed={row.failed} />,
    },
    {
      key: 'credential',
      header: 'Credential',
      width: '13%',
      priority: 'wide',
      cell: (row) =>
        row.credential ? (
          <span className="block min-w-0">
            <span className="block truncate text-[12.5px] text-ink-2">{titleCaseEnum(row.credential.type)}</span>
            {Number.isFinite(row.credential.age_days) && (
              <span className="block truncate text-[11px] text-ink-3">{formatNumber(row.credential.age_days)} days old</span>
            )}
          </span>
        ) : (
          <span className="text-[12.5px] text-ink-3">None</span>
        ),
    },
    {
      key: 'change',
      header: `Change (${window})`,
      width: '10%',
      sortable: true,
      cell: (row) => <Delta value={row.change?.value} emptyLabel="-" />,
    },
  ];

  const total = facets?.total;
  const listError = listQuery.isError && !listQuery.data;
  const noData = overviewMeta && overviewMeta.data_available === false;
  const activeCheck = lists.failing_check.length === 1 ? lists.failing_check[0] : null;
  const activeWin = activeCheck ? overview?.quick_wins?.find((win) => win.rule_id === activeCheck) : null;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Posture"
        lede="Every non-human identity starts at 100 and loses points for each security check it fails: critical −25, high −15, medium −8, low −3. Fix a check to win its points back. Covers every connected account, whatever scope the dashboard is set to."
        actions={
          <>
            <SegmentedControl
              label="Comparison window"
              options={WINDOW_OPTIONS}
              value={window}
              onChange={(value) => setParams({ window: value === DEFAULT_WINDOW ? '' : value })}
            />
            <Button
              variant="secondary"
              icon={Download}
              onClick={onExport}
              loading={exporting}
              disabled={listError || total === 0}
              locked={lock('data.export')}
              title="Download the identities with the filters shown, as CSV"
            >
              Export
            </Button>
          </>
        }
      />

      {overviewQuery.isError && !overview ? (
        <Panel>
          <ErrorState error={overviewQuery.error} onRetry={overviewQuery.refetch} title="The posture overview failed to load" />
        </Panel>
      ) : overviewQuery.isLoading && !overview ? (
        <OverviewSkeleton />
      ) : noData ? (
        <Panel prominence="lead">
          <EmptyState
            icon={Gauge}
            title="No posture scan has been scored yet"
            description="Identities are scored after each discovery scan. When the first one is scored, the fleet score, its trend and the quick wins appear here."
            action={
              <Button variant="secondary" size="sm" onClick={refresh}>
                Check again
              </Button>
            }
          />
        </Panel>
      ) : (
        overview && (
          <>
            <div className="grid gap-4 @min-[44rem]:grid-cols-2 @min-[44rem]:[&>*:last-child]:col-span-2 @min-[64rem]:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)_minmax(0,1fr)] @min-[64rem]:[&>*:last-child]:col-span-1">
              <FleetScore overview={overview} meta={overviewMeta} window={window} />
              <Distribution
                overview={overview}
                selected={lists.band}
                onBand={(key) => {
                  toggleValue('band', key);
                  showList();
                }}
              />
              <QuickWins overview={overview} active={lists.failing_check} onPick={(win) => setDrawer({ quickWin: win })} pillarNames={pillarNames} />
            </div>

            <div className="grid gap-4 @min-[44rem]:grid-cols-2 @min-[44rem]:[&>*:last-child]:col-span-2 @min-[64rem]:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)_minmax(0,1fr)] @min-[64rem]:[&>*:last-child]:col-span-1">
              <Panel prominence="quiet" className="animate-rise">
                <PanelHeader prominence="quiet" title="Score by security area" subtitle="Average score in each area. Hover an area's name to see what it checks." />
                <div className="mt-4">
                  {(overview.pillars ?? []).length === 0 ? (
                    <p className="text-[12.5px] text-ink-3">No area scores in this scan.</p>
                  ) : (
                    <PillarBars
                      pillars={overview.pillars}
                      detail={(entry) => `${formatNumber(entry.failing)} of ${formatNumber(entry.evaluated)} evaluated failing`}
                    />
                  )}
                </div>
              </Panel>
              <WeakestIdentities overview={overview} open={open} opening={opening} />
              <Breakdowns
                overview={overview}
                window={window}
                facets={facets}
                onCategory={(value) => setParams({ category: [value] })}
                onAccount={(id) => setParams({ account: [id] })}
              />
            </div>
          </>
        )
      )}

      {/* Before the first scored scan there is nothing to list or filter; the
          empty state above already says so. */}
      {!noData && (
      <WorkArea
        railOpen={railOpen}
        rail={
          facetsQuery.isError && !facets ? (
            <Panel prominence="quiet" className="lg:sticky lg:top-[calc(104px/var(--content-zoom))]">
              <InlineError error={facetsQuery.error} onRetry={facetsQuery.refetch} label="Filters unavailable" />
            </Panel>
          ) : (
            <FacetRail
              groups={facetGroups}
              appliedCount={chips.length}
              onClearAll={clearAll}
              onClose={toggleRail}
              mobileTitle="Filter identities"
            />
          )
        }
      >
        <div ref={listRef} className="scroll-mt-20">
        <Panel prominence="lead" flush className="animate-rise overflow-hidden">
          <RecordBar
            trailing={
              <TableToolbar>
                <SearchInput
                  value={searchText}
                  onChange={setSearchText}
                  placeholder="Search name or ARN…"
                  size="sm"
                  className="w-full sm:w-64"
                  aria-invalid={searchInvalid || undefined}
                />
                {!railOpen && <ShowFiltersButton onClick={toggleRail} appliedCount={chips.length} />}
                <RefreshButton onRefresh={refresh} refreshing={listQuery.isRefreshing} label="Refresh posture" />
              </TableToolbar>
            }
          >
            <ResultCount
              shown={formatNumber(total)}
              total={formatNumber(overview?.fleet?.identities_scored)}
              unit="identities"
              filtered={chips.length > 0}
              loading={facetsQuery.isLoading && !facets}
            />
            <span className="hidden text-[11.5px] text-ink-3 xl:inline">
              {sort === 'score' ? 'Lowest score first. ' : ''}Open a row to see why and what to fix.
            </span>
          </RecordBar>

          {searchInvalid && (
            <p className="border-b border-line bg-surface px-3 py-1.5 text-[11.5px] text-ink-3" role="status">
              {searchRunes === 1 ? 'Type at least 2 characters to search.' : 'Search is limited to 100 characters.'}
            </p>
          )}

          <AppliedFilters filters={chips} onRemove={removeChip} onClearAll={clearAll} />

          {activeCheck && (
            <div className="flex flex-wrap items-center gap-3 border-b border-line bg-info-soft/60 px-4 py-2.5">
              <Wrench aria-hidden="true" className="size-4 shrink-0 text-brand" />
              <p className="min-w-0 flex-1 text-[12.5px] text-ink-2">
                {Number.isFinite(total) ? `${formatNumber(total)} ${total === 1 ? 'identity fails' : 'identities fail'}` : 'Identities failing'} “
                {quickWinTitle(activeCheck)}”.
                {activeWin ? ` Fixing it everywhere adds ${formatScore(activeWin.fleet_gain)} to the fleet score.` : ''}
              </p>
              <Button
                variant="secondary"
                size="sm"
                icon={Wrench}
                onClick={() => setDrawer({ quickWin: activeWin ?? { rule_id: activeCheck, title: quickWinTitle(activeCheck) } })}
              >
                How to fix it
              </Button>
            </div>
          )}

          {listError ? (
            <ErrorState error={listQuery.error} onRetry={listQuery.refetch} title="The identity list failed to load" />
          ) : (
            <DataGrid
              caption="Identity posture"
              columns={columns}
              rows={rows}
              rowKey={(row) => row.identity_id}
              loading={listQuery.isLoading && !listQuery.data}
              refreshing={listQuery.isRefreshing || (listQuery.isLoading && Boolean(listQuery.data))}
              onRowClick={(row) => open({ postureId: row.identity_id, name: row.name, arn: row.arn })}
              sort={gridSort}
              onSortChange={onSortChange}
              rowActions={(row) =>
                opening === row.identity_id ? (
                  <span className="grid size-8 place-items-center" role="status" aria-label={`Opening ${row.name}`}>
                    <Loader2 aria-hidden="true" className="size-4 animate-spin text-ink-3" />
                  </span>
                ) : (
                  <IconButton
                    icon={ArrowRight}
                    size="sm"
                    label={`Open ${row.name}`}
                    onClick={(event) => {
                      event.stopPropagation();
                      open({ postureId: row.identity_id, name: row.name, arn: row.arn });
                    }}
                  />
                )
              }
              emptyState={
                chips.length > 0 ? (
                  <EmptyState
                    icon={SearchX}
                    title="No identity matches those filters"
                    description="Every scored identity is outside the current selection."
                    action={
                      <Button variant="secondary" size="sm" onClick={clearAll}>
                        Clear filters
                      </Button>
                    }
                  />
                ) : (
                  <EmptyState
                    icon={ShieldCheck}
                    title="No identities scored yet"
                    description="The posture service has not scored any identity in this workspace yet. Identities appear here after the next scan is scored."
                  />
                )
              }
            />
          )}

          {!listError && rows.length > 0 && (
            Number.isFinite(total) && total > 0 ? (
              <Pagination
                page={current.page}
                pageSize={pageSize}
                total={total}
                unit="identities"
                onPageChange={goToPage}
                onPageSizeChange={(next) => setParams({ size: next === 25 ? '' : String(next) })}
              />
            ) : (
              nextCursor && (
                <div className="flex justify-center border-t border-line px-4 py-3">
                  <Button variant="secondary" size="sm" onClick={() => goToPage(current.page + 1)}>
                    Next page
                  </Button>
                </div>
              )
            )
          )}
        </Panel>
        </div>
      </WorkArea>
      )}

      {drawer?.quickWin && (
        <CheckDrawer
          ruleId={drawer.quickWin.rule_id}
          quickWin={drawer.quickWin}
          pillarNames={pillarNames}
          onClose={() => setDrawer(null)}
          onFilter={() => {
            setParams({ failing_check: [drawer.quickWin.rule_id] });
            setDrawer(null);
            showList();
          }}
        />
      )}
    </div>
  );
}

/**
 * Failed checks in two short lines: the count with its worst severity, and a
 * bar split by severity. The full breakdown is in the tooltip and the
 * identity's posture page.
 */
function FailedCell({ failed }) {
  const total = Number(failed?.total) || 0;
  if (total === 0) return <span className="text-[12.5px] text-ink-3">None</span>;
  const parts = [
    ['CRITICAL', Number(failed.critical) || 0],
    ['HIGH', Number(failed.high) || 0],
    ['MEDIUM', Number(failed.medium) || 0],
    ['LOW', Number(failed.low) || 0],
  ].filter(([, count]) => count > 0);
  const breakdown = parts.map(([key, count]) => `${count} ${severityMeta(key).label.toLowerCase()}`).join(', ');
  const [worstKey, worstCount] = parts[0] ?? [];
  const worst = worstKey ? severityMeta(worstKey) : null;
  return (
    <span className="block min-w-0" title={breakdown} aria-label={`${total} failed: ${breakdown}`}>
      <span className="flex min-w-0 items-baseline gap-1.5 whitespace-nowrap">
        <span data-numeric="" className="text-[13px] font-semibold text-ink">
          {formatNumber(total)}
        </span>
        {worst && (
          <span className="truncate text-[11.5px] font-medium" data-numeric="" style={{ color: TONE_VAR[worst.tone] }}>
            {worstCount} {worst.label.toLowerCase()}
          </span>
        )}
      </span>
      <span aria-hidden="true" className="mt-1.5 flex h-1.5 w-full max-w-[96px] gap-px overflow-hidden rounded-full bg-line">
        {parts.map(([key, count]) => (
          <span key={key} className="h-full" style={{ width: `${(count / total) * 100}%`, background: TONE_VAR[severityMeta(key).tone] }} />
        ))}
      </span>
    </span>
  );
}

function OverviewSkeleton() {
  return (
    <div className="grid gap-4 @min-[64rem]:grid-cols-3" aria-busy="true">
      {[0, 1, 2].map((index) => (
        <Panel key={index} prominence="quiet">
          <Skeleton className="h-4 w-32 rounded" />
          <div className="mt-4">{index === 0 ? <ChartSkeleton height={150} /> : <ListSkeleton rows={4} />}</div>
        </Panel>
      ))}
      <span role="status" className="sr-only">
        Loading posture
      </span>
    </div>
  );
}

function trendLabel(at, perScan) {
  const date = new Date(at.length === 10 ? `${at}T00:00:00` : at);
  if (Number.isNaN(date.getTime())) return at;
  return perScan
    ? date.toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
    : date.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
}

function FleetScore({ overview, meta, window }) {
  const fleet = overview.fleet ?? {};
  const perScan = ['24h', '3d', '7d'].includes(window);
  const trend = (overview.trend ?? []).map((point) => ({ ...point, label: trendLabel(point.at, perScan) }));
  const lowest = trend.length ? Math.min(...trend.map((point) => point.score)) : 0;
  const change = fleet.change ?? {};
  const band = bandMeta(String(fleet.band || '').toLowerCase());
  return (
    <Panel prominence="lead" className="animate-rise">
      <PanelHeader
        prominence="lead"
        title="Fleet posture score"
        subtitle={`The average score of ${formatNumber(fleet.identities_scored)} scored ${fleet.identities_scored === 1 ? 'identity' : 'identities'}.`}
      />
      <div className="mt-4 flex flex-wrap items-center gap-5">
        <ScoreRing score={fleet.score} band={fleet.band} size={112} grade={fleet.grade} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <BandTag score={fleet.score} band={fleet.band} size="md" />
            {fleet.grade && (
              <Tag tone="neutral" size="md">
                Grade {fleet.grade}
              </Tag>
            )}
          </div>
          <p className="mt-2 flex flex-wrap items-baseline gap-1.5 text-[12.5px] text-ink-2">
            {change.delta === null || change.delta === undefined ? (
              <span className="text-ink-3">No score from {windowPhrase(window)} ago to compare with yet.</span>
            ) : (
              <>
                <Delta value={change.delta} />
                <span>
                  vs {windowPhrase(window)} ago ({formatScore(change.previous)})
                </span>
              </>
            )}
          </p>
          {meta?.computed_at && (
            <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-3">Scored {formatDateTime(meta.computed_at)}.</p>
          )}
        </div>
      </div>
      {trend.length < 2 ? (
        <p className="mt-4 rounded-[var(--radius-control)] border border-dashed border-line px-3 py-6 text-center text-[12px] text-ink-3">
          {trend.length === 0 ? 'No fleet score recorded' : 'Only one fleet score recorded'} in the last {windowPhrase(window)}, so
          there is no trend to draw yet.
        </p>
      ) : (
        <TrendChart
          className="mt-4"
          data={trend}
          dataKey="score"
          label="Fleet score"
          height={176}
          color={TONE_VAR[band.tone]}
          valueFormatter={formatScore}
          yDomain={[Math.max(0, Math.floor((lowest - 5) / 5) * 5), 100]}
        />
      )}
    </Panel>
  );
}

function Distribution({ overview, selected, onBand }) {
  const distribution = overview.distribution ?? {};
  const entries = BANDS.map((band) => ({ ...band, apiKey: band.key.toUpperCase(), count: Number(distribution[band.key]) || 0 }));
  const total = entries.reduce((sum, entry) => sum + entry.count, 0);
  const risks = Object.entries(overview.identities_with ?? {});
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
      <PanelHeader prominence="quiet" title="Identities by score" subtitle="Healthy 80+, Fair 60–79, Poor 40–59, Critical below 40. Select a band to list them." />
      <div
        className="mt-4 flex h-2.5 w-full gap-[2px] overflow-hidden rounded-full bg-track"
        role="img"
        aria-label={entries.map((entry) => `${entry.label}: ${entry.count}`).join(', ')}
      >
        {total > 0 &&
          entries.map((entry) =>
            entry.count > 0 ? (
              <span
                key={entry.key}
                className="h-full first:rounded-l-full last:rounded-r-full"
                style={{ width: `${(entry.count / total) * 100}%`, background: TONE_VAR[entry.tone] }}
              />
            ) : null,
          )}
      </div>
      <ul className="mt-3 grid grid-cols-2 gap-2">
        {entries.map((entry) => {
          const active = selected.includes(entry.apiKey);
          return (
            <li key={entry.key}>
              <button
                type="button"
                onClick={() => onBand(entry.apiKey)}
                aria-pressed={active}
                title={`List ${entry.label.toLowerCase()} identities`}
                className={cn(
                  'flex w-full items-center gap-2 rounded-[var(--radius-control)] border px-2.5 py-2 text-left transition-colors',
                  active ? 'border-brand bg-info-soft' : 'border-line hover:bg-surface-2',
                )}
              >
                <span aria-hidden="true" className="size-2 shrink-0 rounded-full" style={{ background: TONE_VAR[entry.tone] }} />
                <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink-2">{entry.label}</span>
                <span data-numeric="" className="text-[13px] font-semibold text-ink">
                  {formatNumber(entry.count)}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
      {risks.length > 0 && (
        <>
          <p className="mt-4 text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Identities with</p>
          <ul className="mt-2 flex flex-wrap gap-1.5">
            {risks.map(([key, count]) => (
              <li key={key} className="inline-flex items-center gap-1.5 rounded-md bg-surface-2 px-2 py-1 text-[12px] text-ink-2">
                {RISK_LABELS[key] ?? titleCaseEnum(key)}
                <span data-numeric="" className="font-semibold text-ink">
                  {formatNumber(count)}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </Panel>
  );
}

function QuickWins({ overview, active, onPick, pillarNames }) {
  const wins = overview.quick_wins ?? [];
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 2 }}>
      <PanelHeader
        prominence="quiet"
        icon={Sparkles}
        title="Quick wins"
        subtitle="Fix one check on every identity that fails it, and the overall score rises by this much."
      />
      {wins.length === 0 ? (
        <p className="mt-4 text-[12.5px] text-ink-3">No check stands out: nothing fails widely enough to be a quick win.</p>
      ) : (
        <ul className="mt-3 flex flex-col gap-1.5">
          {wins.map((win) => (
            <li key={win.rule_id}>
              <button
                type="button"
                onClick={() => onPick(win)}
                aria-pressed={active.includes(win.rule_id)}
                className={cn(
                  'flex w-full items-center gap-3 rounded-[var(--radius-control)] border px-3 py-2.5 text-left transition-colors',
                  active.includes(win.rule_id) ? 'border-brand bg-info-soft' : 'border-line hover:bg-surface-2',
                )}
              >
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[12.5px] font-medium text-ink" title={win.title}>
                    {win.title || win.rule_id}
                  </span>
                  <span className="block truncate text-[11px] text-ink-3">
                    {win.pillar ? `${pillarLabel(win.pillar, pillarNames)} · ` : ''}
                    {formatNumber(win.identities)} {win.identities === 1 ? 'identity' : 'identities'}
                  </span>
                </span>
                <span data-numeric="" className="shrink-0 text-[13px] font-bold text-low">
                  +{formatScore(win.fleet_gain)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {wins.length > 0 && (
        <p className="mt-3 text-[11px] leading-relaxed text-ink-3">Choose one to see the fix and the identities it applies to.</p>
      )}
    </Panel>
  );
}

function WeakestIdentities({ overview, open, opening }) {
  const weakest = overview.weakest_identities ?? [];
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
      <PanelHeader prominence="quiet" icon={ShieldCheck} title="Weakest identities" subtitle="The identities with the lowest scores. Start here." />
      {weakest.length === 0 ? (
        <p className="mt-3 text-[12.5px] text-ink-3">No identity scored yet.</p>
      ) : (
        <ul className="mt-3 divide-y divide-line">
          {weakest.map((row) => (
            <li key={row.identity_id}>
              <button
                type="button"
                onClick={() => open({ postureId: row.identity_id, name: row.name, arn: row.arn })}
                className="flex w-full items-center gap-3 py-2 text-left transition-colors hover:text-brand"
              >
                <ScoreRing score={row.score} band={row.band} size={36} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-mono text-[12.5px] font-semibold text-ink">{row.name}</span>
                  <span className="block truncate text-[11px] text-ink-3">
                    {categoryLabel(row.functional_class)} · {row.account || 'Unknown account'} · {formatNumber(row.checks_failed)} failed
                  </span>
                </span>
                {opening === row.identity_id ? (
                  <Loader2 aria-label="Opening" className="size-4 animate-spin text-ink-3" />
                ) : (
                  <BandTag score={row.score} band={row.band} />
                )}
              </button>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

function Breakdowns({ overview, window, facets, onCategory, onAccount }) {
  /* The overview names accounts; the list filters by account id. */
  const accountId = (account) => {
    const entries = Object.entries(facets?.account ?? {});
    return entries.find(([id, entry]) => id === account || entry?.name === account)?.[0] ?? null;
  };
  const categories = (overview.by_category ?? []).map((item) => ({
    key: item.functional_class,
    label: categoryLabel(item.functional_class),
    count: item.count,
    mean: item.mean,
    change: item.change,
    onPick: item.functional_class ? () => onCategory(item.functional_class) : null,
  }));
  const accounts = (overview.by_account ?? []).map((item) => {
    const id = accountId(item.account);
    return {
      key: item.account,
      label: item.account,
      count: item.count,
      mean: item.mean,
      change: item.change,
      onPick: id ? () => onAccount(id) : null,
    };
  });
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 2 }}>
      <PanelHeader prominence="quiet" title="By category and account" subtitle={`Mean score, and its change over ${windowPhrase(window)}.`} />
      <BreakdownList title="Category" items={categories} />
      <BreakdownList title="Account" items={accounts} />
    </Panel>
  );
}

function BreakdownList({ title, items }) {
  return (
    <div className="mt-4">
      <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{title}</p>
      {items.length === 0 ? (
        <p className="mt-1.5 text-[12px] text-ink-3">Nothing scored.</p>
      ) : (
        <ul className="mt-1.5 flex flex-col">
          {items.map((item) => {
            const Element = item.onPick ? 'button' : 'div';
            return (
              <li key={item.key ?? item.label}>
                <Element
                  type={item.onPick ? 'button' : undefined}
                  onClick={item.onPick ?? undefined}
                  title={item.onPick ? `List identities in ${item.label}` : undefined}
                  className={cn(
                    'grid w-full grid-cols-[minmax(0,1fr)_3rem_3.25rem] items-center gap-2 rounded px-1 py-1 text-left',
                    item.onPick && 'transition-colors hover:bg-surface-2',
                  )}
                >
                  <span className="truncate text-[12.5px] text-ink-2">
                    {item.label} <span className="text-ink-3">({formatNumber(item.count)})</span>
                  </span>
                  <span className="flex items-center justify-end gap-1.5">
                    <span aria-hidden="true" className="size-1.5 rounded-full" style={{ background: TONE_VAR[bandForMean(item.mean)] }} />
                    <span data-numeric="" className="text-[12.5px] font-semibold text-ink">
                      {formatScore(item.mean)}
                    </span>
                  </span>
                  <span className="text-right">
                    <Delta value={item.change} emptyLabel="-" />
                  </span>
                </Element>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

/* The dot beside a mean uses the console's bands; the service bands identities, not means. */
function bandForMean(mean) {
  const n = Number(mean);
  return (BANDS.find((band) => n >= band.min) ?? BANDS[BANDS.length - 1]).tone;
}
