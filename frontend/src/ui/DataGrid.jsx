import { ArrowDown, ArrowUp, ChevronRight } from 'lucide-react';
import { useMediaQuery } from '../lib/hooks';
import { cn } from './cn';
import { GridSkeleton, LoadingAnnouncement } from './Skeleton';

/**
 * Data grid for dense, wide records.
 *
 * Columns: { key, header, width, align, sortable, cell(row), primary,
 *            priority: 'always' | 'wide' }
 *
 * `sortable` is only wired up where the full dataset is held client-side -
 * the Go API does not accept a sort parameter, so sorting a single server page
 * would misrepresent the data.
 *
 * Below the `md` breakpoint the same columns are re-composed as record cards
 * rather than being pushed into a horizontal scroll.
 */
export function DataGrid({
  columns,
  rows,
  rowKey,
  loading = false,
  refreshing = false,
  onRowClick,
  sort,
  onSortChange,
  emptyState,
  caption,
  skeletonRows = 8,
  rowActions,
  /** 'compact' | 'comfortable' - client-side only, a table-reading preference. */
  density = 'comfortable',
  /** Per-row accent colour, e.g. the identity's classification hue. */
  rowAccent,
  className,
}) {
  const isWide = useMediaQuery('(min-width: 768px)');
  const visibleColumns = isWide ? columns : columns.filter((column) => column.priority !== 'wide');

  if (loading) {
    return (
      <div className={className}>
        <LoadingAnnouncement label={caption ? `Loading ${caption}` : 'Loading records'} />
        <GridSkeleton columns={Math.min(visibleColumns.length, 6)} rows={skeletonRows} />
      </div>
    );
  }

  if (!rows || rows.length === 0) return <div className={className}>{emptyState}</div>;

  const cellPadding = density === 'compact' ? 'px-3 py-2' : 'px-3.5 py-3';

  const toggleSort = (column) => {
    if (!column.sortable || !onSortChange) return;
    const active = sort?.key === column.key;
    onSortChange({
      key: column.key,
      direction: active && sort.direction === 'desc' ? 'asc' : 'desc',
    });
  };

  if (!isWide) {
    const primary = columns.find((column) => column.primary) || columns[0];
    const rest = visibleColumns.filter((column) => column !== primary);

    return (
      <ul
        className={cn(
          'divide-y divide-line transition-opacity duration-200',
          refreshing && 'opacity-55',
          className,
        )}
      >
        {rows.map((row, index) => {
          const interactive = Boolean(onRowClick);
          return (
            <li key={rowKey(row, index)} className="px-4 py-3.5">
              <div className="flex items-start gap-2">
                <div className="min-w-0 flex-1">
                  <button
                    type="button"
                    disabled={!interactive}
                    onClick={interactive ? () => onRowClick(row) : undefined}
                    className={cn(
                      'block w-full text-left',
                      interactive && 'cursor-pointer',
                    )}
                  >
                    <div className="min-w-0">{primary.cell(row)}</div>
                  </button>
                  <dl className="mt-2.5 grid grid-cols-2 gap-x-3 gap-y-2">
                    {rest.map((column) => (
                      <div key={column.key} className="min-w-0">
                        <dt className="text-[10.5px] font-semibold tracking-[0.08em] text-ink-3 uppercase">
                          {column.header}
                        </dt>
                        <dd className="mt-0.5 min-w-0 text-[12.5px] text-ink-2">{column.cell(row)}</dd>
                      </div>
                    ))}
                  </dl>
                </div>
                {rowActions && <div className="shrink-0">{rowActions(row)}</div>}
                {!rowActions && interactive && (
                  <ChevronRight aria-hidden="true" className="mt-1 size-4 shrink-0 text-ink-3" />
                )}
              </div>
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <div className={cn('w-full overflow-x-auto', className)}>
      <table className="w-full min-w-full border-collapse text-left">
        {caption && <caption className="sr-only">{caption}</caption>}
        <thead className="grid-sticky-head">
          <tr className="border-b border-line">
            {visibleColumns.map((column) => {
              const active = sort?.key === column.key;
              const SortIcon = active && sort.direction === 'asc' ? ArrowUp : ArrowDown;
              return (
                <th
                  key={column.key}
                  scope="col"
                  style={column.width ? { width: column.width } : undefined}
                  aria-sort={active ? (sort.direction === 'asc' ? 'ascending' : 'descending') : undefined}
                  className={cn(
                    'px-3.5 py-2.5 text-[11px] font-semibold tracking-[0.07em] text-ink-3 uppercase',
                    column.align === 'right' && 'text-right',
                    column.align === 'center' && 'text-center',
                  )}
                >
                  {column.sortable && onSortChange ? (
                    <button
                      type="button"
                      onClick={() => toggleSort(column)}
                      className={cn(
                        /* Buttons get `text-transform: none` from the UA
                           stylesheet, so the header casing has to be restated. */
                        'inline-flex items-center gap-1.5 rounded uppercase tracking-[0.07em] transition-colors hover:text-ink',
                        active && 'text-ink',
                      )}
                    >
                      {column.header}
                      <SortIcon
                        aria-hidden="true"
                        className={cn('size-3', active ? 'opacity-100' : 'opacity-25')}
                      />
                    </button>
                  ) : (
                    column.header
                  )}
                </th>
              );
            })}
            {rowActions && (
              <th scope="col" className="w-12 px-2 py-2.5">
                <span className="sr-only">Actions</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody className={cn('transition-opacity duration-200', refreshing && 'opacity-55')}>
          {rows.map((row, index) => {
            const interactive = Boolean(onRowClick);
            return (
              <tr
                key={rowKey(row, index)}
                onClick={interactive ? () => onRowClick(row) : undefined}
                onKeyDown={
                  interactive
                    ? (event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault();
                          onRowClick(row);
                        }
                      }
                    : undefined
                }
                tabIndex={interactive ? 0 : undefined}
                aria-label={interactive ? 'Open record detail' : undefined}
                className={cn(
                  'grid-row border-b border-line/80 transition-colors duration-100 last:border-b-0',
                  interactive && 'cursor-pointer hover:bg-surface-2 focus-visible:bg-surface-2',
                )}
                style={rowAccent ? { '--t-brand': rowAccent(row) } : undefined}
              >
                {visibleColumns.map((column) => (
                  <td
                    key={column.key}
                    className={cn(
                      'max-w-0 align-middle text-[13px] text-ink-2',
                      cellPadding,
                      column.align === 'right' && 'text-right',
                      column.align === 'center' && 'text-center',
                    )}
                  >
                    {column.cell(row)}
                  </td>
                ))}
                {rowActions && (
                  <td className={cn('text-right align-middle', density === 'compact' ? 'px-2 py-2' : 'px-2 py-3')}>
                    <span className="row-actions inline-flex justify-end">{rowActions(row)}</span>
                  </td>
                )}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/** Primary cell composition: strong title over a muted, monospaced locator. */
export function CellStack({ title, meta, mono = false, icon: Icon, tone, badge }) {
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      {Icon && (
        <span
          className={cn(
            'grid size-7 shrink-0 place-items-center rounded-md border border-line bg-surface-2',
            tone,
          )}
        >
          <Icon aria-hidden="true" className="size-3.5" />
        </span>
      )}
      <span className="min-w-0">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-[13px] font-medium text-ink" title={title}>
            {title}
          </span>
          {badge}
        </span>
        {meta && (
          <span
            className={cn('mt-0.5 block truncate text-[11.5px] text-ink-3', mono && 'font-mono')}
            title={typeof meta === 'string' ? meta : undefined}
          >
            {meta}
          </span>
        )}
      </span>
    </div>
  );
}
