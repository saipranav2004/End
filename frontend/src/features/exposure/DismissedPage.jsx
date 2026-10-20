import { useAccess } from '../../app/useAccess';
import { useCallback, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  ArrowLeft,
  CalendarClock,
  List,
  RotateCcw,
  SearchX,
  ShieldOff,
  UserCheck,
} from 'lucide-react';
import { fetchAllowlist, restoreFinding } from '../../lib/api/exposure';
import { useMutation, useQuery } from '../../lib/hooks';
import {
  formatDate,
  formatDateTime,
  formatNumber,
  formatRelative,
  humanizeToken,
  parseDate,
} from '../../lib/format';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { DetailList, DetailRow, Panel } from '../../ui/Panel';
import { SearchInput } from '../../ui/Field';
import { AppliedFilters, FacetRail, useFacetRail } from '../../ui/FacetRail';
import { RecordBar, ResultCount, ShowFiltersButton, WorkArea } from '../../ui/WorkArea';
import { Pagination } from '../../ui/Pagination';
import { SegmentedControl } from '../../ui/Tabs';
import { RefreshButton, TableSettings, TableToolbar } from '../../ui/TableTools';
import { CellStack, DataGrid } from '../../ui/DataGrid';
import { Modal } from '../../ui/Overlay';
import { MetricTile } from '../../ui/Stat';
import { StatStripSkeleton } from '../../ui/Skeleton';
import { useToast } from '../../ui/Toast';
import { EmptyState, ErrorState } from '../../ui/States';
import { describeExposureError } from './scannerState';

/**
 * The scanner's allowlist - findings a reviewer accepted.
 *
 * This is an audit surface, so it is organised the way an auditor reads it:
 * who accepted what, when, and on what stated grounds. Entries dismissed with
 * no recorded reason are the audit risk, so that is a first-class facet.
 *
 * The whole allowlist arrives in one response, so every figure here is exact
 * and every filter is client-side.
 */
const VIEWS = [
  { value: 'table', label: 'Table', icon: List },
  { value: 'timeline', label: 'Review timeline', icon: CalendarClock },
];

/**
 * Rows per page.
 *
 * `GET /api/v1/credential-exposure/allowlist` returns every entry in one response, so paging happens
 * here. An allowlist only grows - every accepted exposure stays on it until
 * somebody restores it - so this is the screen that gets long first.
 */
const DEFAULT_PAGE_SIZE = 25;

const entryKey = (entry) =>
  [entry.client_id, entry.file_path, entry.detector, entry.redacted].join('|');

export default function DismissedPage() {
  const { lock } = useAccess();
  const { notify } = useToast();
  const query = useQuery((signal) => fetchAllowlist(signal), []);
  const restoration = useMutation((input) => restoreFinding(input));

  const [search, setSearch] = useState('');
  const [detector, setDetector] = useState('');
  const [reviewer, setReviewer] = useState('');
  const [reasonState, setReasonState] = useState('');
  const [view, setView] = useState('table');
  const { railOpen, toggleRail } = useFacetRail();
  const [density, setDensity] = useState('comfortable');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [pendingRestore, setPendingRestore] = useState(null);
  const [restored, setRestored] = useState(() => new Set());

  const all = useMemo(
    () => (query.data ?? []).filter((entry) => !restored.has(entryKey(entry))),
    [query.data, restored],
  );

  const stats = useMemo(() => {
    const detectors = new Map();
    const reviewers = new Map();
    let withoutReason = 0;
    let oldest = null;

    for (const entry of all) {
      if (entry.detector) detectors.set(entry.detector, (detectors.get(entry.detector) || 0) + 1);
      const who = entry.dismissed_by || 'Unattributed';
      reviewers.set(who, (reviewers.get(who) || 0) + 1);
      if (!entry.reason || !String(entry.reason).trim()) withoutReason += 1;
      const when = parseDate(entry.dismissed_at);
      if (when && (!oldest || when < oldest)) oldest = when;
    }

    return {
      total: all.length,
      detectors: [...detectors.entries()].sort((a, b) => b[1] - a[1]),
      reviewers: [...reviewers.entries()].sort((a, b) => b[1] - a[1]),
      withoutReason,
      oldest,
    };
  }, [all]);

  const entries = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return all.filter((entry) => {
      if (detector && entry.detector !== detector) return false;
      if (reviewer && (entry.dismissed_by || 'Unattributed') !== reviewer) return false;
      const hasReason = Boolean(entry.reason && String(entry.reason).trim());
      if (reasonState === 'with' && !hasReason) return false;
      if (reasonState === 'without' && hasReason) return false;
      if (!needle) return true;
      return [entry.file_path, entry.detector, entry.reason, entry.dismissed_by, entry.client_id]
        .filter(Boolean)
        .some((field) => String(field).toLowerCase().includes(needle));
    });
  }, [all, search, detector, reviewer, reasonState]);

  /* Clamped, not trusted: restoring the last entry on the last page, or a
     filter that shrinks the set, would otherwise leave the reader on a page
     past the end looking at an empty table. */
  const safePage = Math.min(page, Math.max(1, Math.ceil(entries.length / pageSize)));
  const pageStart = (safePage - 1) * pageSize;
  const pageEntries = useMemo(
    () => entries.slice(pageStart, pageStart + pageSize),
    [entries, pageStart, pageSize],
  );

  /* Grouped by the day it was accepted - the order an auditor walks it in.
     Grouped from the current page, so the dates on screen and the rows under
     them are the same set: a day heading counting entries the reader cannot
     see on this page would be a lie about the page. */
  const timeline = useMemo(() => {
    const groups = new Map();
    for (const entry of pageEntries) {
      const when = parseDate(entry.dismissed_at);
      const key = when ? when.toISOString().slice(0, 10) : 'unknown';
      if (!groups.has(key)) groups.set(key, { key, when, entries: [] });
      groups.get(key).entries.push(entry);
    }
    return [...groups.values()].sort((a, b) => (b.when?.getTime() || 0) - (a.when?.getTime() || 0));
  }, [pageEntries]);

  const chips = useMemo(() => {
    const list = [];
    if (search.trim()) list.push({ key: 'search', label: 'Search', value: search.trim() });
    if (detector) list.push({ key: 'detector', label: 'Detector', value: humanizeToken(detector) });
    if (reviewer) list.push({ key: 'reviewer', label: 'Reviewer', value: reviewer });
    if (reasonState) {
      list.push({
        key: 'reason',
        label: 'Reason',
        value: reasonState === 'with' ? 'Recorded' : 'Not recorded',
      });
    }
    return list;
  }, [search, detector, reviewer, reasonState]);

  const clearAll = () => {
    setSearch('');
    setDetector('');
    setReviewer('');
    setReasonState('');
  };

  const removeChip = (key) => {
    if (key === 'search') setSearch('');
    if (key === 'detector') setDetector('');
    if (key === 'reviewer') setReviewer('');
    if (key === 'reason') setReasonState('');
  };

  const facetGroups = useMemo(
    () => [
      {
        key: 'detector',
        label: 'Detector',
        options: stats.detectors.map(([key, count]) => ({
          value: key,
          label: humanizeToken(key),
          count,
          active: detector === key,
        })),
        onToggle: (value) => {
          setPage(1);
          setDetector((current) => (current === value ? '' : value));
        },
      },
      {
        key: 'reviewer',
        label: 'Reviewed by',
        options: stats.reviewers.map(([key, count]) => ({
          value: key,
          label: key,
          count,
          active: reviewer === key,
        })),
        onToggle: (value) => {
          setPage(1);
          setReviewer((current) => (current === value ? '' : value));
        },
      },
      {
        key: 'reason',
        label: 'Stated reason',
        options: [
          {
            value: 'with',
            label: 'Reason recorded',
            count: stats.total - stats.withoutReason,
            active: reasonState === 'with',
          },
          {
            value: 'without',
            label: 'No reason recorded',
            count: stats.withoutReason,
            active: reasonState === 'without',
          },
        ],
        onToggle: (value) => {
          setPage(1);
          setReasonState((current) => (current === value ? '' : value));
        },
        note: 'An entry accepted with no stated reason is the one an auditor will ask about.',
      },
    ],
    [stats, detector, reviewer, reasonState],
  );

  const onRestore = useCallback(
    async (entry) => {
      const result = await restoration.mutate({
        clientId: entry.client_id,
        filePath: entry.file_path,
        detector: entry.detector,
        redacted: entry.redacted,
      });

      setPendingRestore(null);

      if (!result.ok) {
        notify({
          variant: 'error',
          title: 'Could not restore',
          description: describeExposureError(result.error).message,
        });
        return;
      }

      /* `{ ok: false }` is not an error - it means nothing matched, so the
         entry was already gone. */
      if (result.result?.ok === false) {
        notify({
          variant: 'info',
          title: 'Nothing to restore',
          description: 'That allowlist entry no longer exists on the scanner.',
        });
      } else {
        notify({
          variant: 'success',
          title: 'Removed from allowlist',
          description: `${entry.file_path} will appear in findings again if the scanner still holds the record.`,
        });
      }

      setRestored((current) => new Set(current).add(entryKey(entry)));
    },
    [restoration, notify],
  );

  if (query.isError && !query.data) {
    const detail = describeExposureError(query.error);
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Accepted exposures" />
        <Panel>
          <ErrorState error={{ message: detail.message, code: query.error?.code }} title={detail.title} onRetry={query.refetch} />
        </Panel>
      </div>
    );
  }

  const loading = query.isLoading && !query.data;

  const columns = [
    {
      key: 'entry',
      header: 'Secret',
      primary: true,
      width: '30%',
      cell: (row) => (
        <CellStack icon={ShieldOff} title={humanizeToken(row.detector)} meta={row.file_path} mono />
      ),
    },
    {
      key: 'redacted',
      header: 'Masked value',
      width: '18%',
      cell: (row) => (
        <span className="block truncate font-mono text-[12px] text-ink-2" title={row.redacted}>
          {row.redacted || '-'}
        </span>
      ),
    },
    {
      key: 'reason',
      header: 'Stated reason',
      width: '22%',
      cell: (row) =>
        row.reason && String(row.reason).trim() ? (
          <span className="block truncate text-[12.5px] text-ink-2" title={row.reason}>
            {row.reason}
          </span>
        ) : (
          <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-medium">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-medium" />
            Not recorded
          </span>
        ),
    },
    {
      key: 'by',
      header: 'Reviewed',
      width: '18%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate text-[12.5px] text-ink-2">{row.dismissed_by || '-'}</span>
          <span
            className="block truncate text-[11px] text-ink-3"
            title={formatDateTime(row.dismissed_at)}
          >
            {formatRelative(row.dismissed_at)}
          </span>
        </span>
      ),
    },
    {
      key: 'restore',
      header: '',
      width: '12%',
      align: 'right',
      cell: (row) => (
        <Button
          variant="ghost"
          size="sm"
          icon={RotateCcw}
          locked={lock('exposure.review')}
          onClick={(event) => {
            event.stopPropagation();
            setPendingRestore(row);
          }}
        >
          Restore
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Accepted exposures"
        lede="Findings marked as safe. They no longer appear in Exposed credentials; restore one to bring it back."
        actions={
          <>
            <Button as={Link} to="/exposure" variant="ghost" icon={ArrowLeft}>
              Back to findings
            </Button>
          </>
        }
      />

      {loading ? (
        <StatStripSkeleton count={4} />
      ) : (
        <div className="grid gap-3 @min-[30rem]:grid-cols-2 @min-[54rem]:grid-cols-4">
          <MetricTile
            label="Allowlisted"
            value={stats.total}
            tone="brand"
            icon={ShieldOff}
            caption="Marked safe and removed from the live list"
            className="animate-rise"
          />
          <MetricTile
            label="No stated reason"
            value={stats.withoutReason}
            tone={stats.withoutReason > 0 ? 'medium' : 'low'}
            caption="Accepted without a recorded justification"
            className="animate-rise"
            data-stagger=""
            style={{ '--stagger': 1 }}
          />
          <MetricTile
            label="Reviewers"
            value={stats.reviewers.length}
            tone="info"
            icon={UserCheck}
            caption={
              stats.reviewers[0] ? `Most active: ${stats.reviewers[0][0]}` : 'No reviewers recorded'
            }
            className="animate-rise"
            data-stagger=""
            style={{ '--stagger': 2 }}
          />
          <MetricTile
            label="Detector types"
            value={stats.detectors.length}
            tone="neutral"
            caption={
              stats.oldest ? `Oldest entry ${formatRelative(stats.oldest)}` : 'Nothing accepted yet'
            }
            className="animate-rise"
            data-stagger=""
            style={{ '--stagger': 3 }}
          />
        </div>
      )}

      <WorkArea
        railOpen={railOpen}
        rail={
          <FacetRail
            groups={facetGroups}
            appliedCount={chips.length}
            onClearAll={clearAll}
            onClose={toggleRail}
            mobileTitle="Filter allowlist"
          />
        }
      >
        <Panel prominence="lead" flush className="animate-rise overflow-hidden">
          <RecordBar
            trailing={
              <TableToolbar>
                <SegmentedControl
                  label="View mode"
                  options={VIEWS}
                  value={view}
                  onChange={(next) => {
                    setPage(1);
                    setView(next);
                  }}
                />
                {!railOpen && (
                  <ShowFiltersButton onClick={toggleRail} appliedCount={chips.length} />
                )}
                <RefreshButton onRefresh={query.refetch} refreshing={query.isRefreshing} />
                {/* Density only means something in the table view. */}
                {view === 'table' && (
                  <TableSettings density={density} onDensityChange={setDensity} />
                )}
              </TableToolbar>
            }
          >
            <SearchInput
              size="sm"
              value={search}
              onChange={(next) => {
                setPage(1);
                setSearch(next);
              }}
              placeholder="Search file, detector, reason or reviewer…"
              className="w-full min-w-0 sm:max-w-sm"
            />
            <ResultCount
              shown={formatNumber(entries.length)}
              total={formatNumber(stats.total)}
              unit={stats.total === 1 ? 'accepted exposure' : 'accepted exposures'}
              filtered={chips.length > 0}
              loading={loading}
            />
          </RecordBar>

          <AppliedFilters filters={chips} onRemove={removeChip} onClearAll={clearAll} />

          {view === 'table' ? (
            <DataGrid
              caption="Accepted exposures"
              columns={columns}
              rows={pageEntries}
              rowKey={(row, index) => `${entryKey(row)}-${index}`}
              loading={loading}
              refreshing={query.isRefreshing}
              density={density}
              skeletonRows={6}
              emptyState={emptyState({ chips, clearAll, search })}
            />
          ) : entries.length === 0 ? (
            emptyState({ chips, clearAll, search })
          ) : (
            <ol className="divide-y divide-line">
              {timeline.map((group) => (
                <li key={group.key} className="px-3.5 py-3.5">
                  <div className="flex items-baseline gap-2.5">
                    <CalendarClock aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
                    <p className="text-[13px] font-semibold text-ink">
                      {group.when ? formatDate(group.when) : 'Date not recorded'}
                    </p>
                    <span
                      data-numeric=""
                      className="rounded-full bg-surface-3 px-1.5 py-0.5 text-[10.5px] font-semibold text-ink-3"
                    >
                      {formatNumber(group.entries.length)}
                    </span>
                  </div>

                  <ul className="mt-2.5 flex flex-col gap-1.5">
                    {group.entries.map((entry, index) => {
                      const hasReason = Boolean(entry.reason && String(entry.reason).trim());
                      return (
                        <li
                          key={`${entryKey(entry)}-${index}`}
                          className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md px-2 py-1.5 transition-colors hover:bg-surface-2"
                        >
                          <span
                            aria-hidden="true"
                            className={`size-1.5 shrink-0 rounded-full ${hasReason ? 'bg-low' : 'bg-medium'}`}
                          />
                          <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink-2">
                            {entry.file_path}
                          </span>
                          <span className="shrink-0 text-[11.5px] text-ink-3">
                            {humanizeToken(entry.detector)}
                          </span>
                          <span
                            className={`shrink-0 text-[11.5px] ${hasReason ? 'text-ink-2' : 'font-medium text-medium'}`}
                          >
                            {hasReason ? entry.reason : 'No reason recorded'}
                          </span>
                          <span className="shrink-0 text-[11.5px] text-ink-3">
                            {entry.dismissed_by || 'Unattributed'}
                          </span>
                          <Button
                            variant="ghost"
                            size="sm"
                            icon={RotateCcw}
                            locked={lock('exposure.review')}
                            onClick={() => setPendingRestore(entry)}
                          >
                            Restore
                          </Button>
                        </li>
                      );
                    })}
                  </ul>
                </li>
              ))}
            </ol>
          )}

          {!loading && entries.length > pageSize && (
            <Pagination
              page={safePage}
              pageSize={pageSize}
              total={entries.length}
              unit="accepted exposures"
              onPageChange={setPage}
              onPageSizeChange={(next) => {
                setPage(1);
                setPageSize(next);
              }}
            />
          )}
        </Panel>
      </WorkArea>

      <Modal
        open={Boolean(pendingRestore)}
        onClose={() => setPendingRestore(null)}
        icon={RotateCcw}
        tone="medium"
        title="Restore this finding?"
        description="The allowlist entry is deleted. The finding reappears in the live set if the scanner still holds the underlying record."
        footer={
          <>
            <Button variant="ghost" onClick={() => setPendingRestore(null)}>
              Cancel
            </Button>
            <Button variant="primary" loading={restoration.pending} onClick={() => onRestore(pendingRestore)}>
              Restore finding
            </Button>
          </>
        }
      >
        {pendingRestore && (
          <DetailList>
            <DetailRow label="File" mono>
              {pendingRestore.file_path}
            </DetailRow>
            <DetailRow label="Detector">{humanizeToken(pendingRestore.detector)}</DetailRow>
            <DetailRow label="Masked value" mono>
              {pendingRestore.redacted}
            </DetailRow>
            <DetailRow label="Stated reason">
              {pendingRestore.reason && String(pendingRestore.reason).trim()
                ? pendingRestore.reason
                : 'None recorded'}
            </DetailRow>
            <DetailRow label="Accepted">
              {formatDateTime(pendingRestore.dismissed_at)}
              {pendingRestore.dismissed_by ? ` by ${pendingRestore.dismissed_by}` : ''}
            </DetailRow>
          </DetailList>
        )}
      </Modal>
    </div>
  );
}

function emptyState({ chips, clearAll, search }) {
  if (chips.length > 0) {
    return (
      <EmptyState
        icon={SearchX}
        title="No accepted entry matches these filters"
        description={
          search.trim()
            ? `Nothing in the allowlist matches "${search.trim()}" with the current filters.`
            : 'Nothing in the allowlist satisfies every filter applied.'
        }
        action={
          <Button variant="secondary" size="sm" onClick={clearAll}>
            Clear filters
          </Button>
        }
      />
    );
  }
  return (
    <EmptyState
      icon={ShieldOff}
      title="Nothing has been accepted"
      description="Findings you mark as safe on Exposed credentials appear here, and can be restored at any time."
      action={
        <Button as={Link} to="/exposure" variant="secondary" size="sm">
          Go to findings
        </Button>
      }
    />
  );
}
