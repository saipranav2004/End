import { SlidersHorizontal } from 'lucide-react';
import { cn } from './cn';

/**
 * Two-column work area: facet rail beside the record surface.
 *
 * `railOpen === false` drops the rail column entirely so the records get the
 * full width - a hidden rail that still reserved its track would defeat the
 * point of hiding it. Below `lg` there is only ever one column; the rail
 * renders itself as a disclosure above the records.
 */
export function WorkArea({ rail, railOpen = true, children, className }) {
  return (
    <div
      className={cn(
        'grid min-w-0 gap-3.5 lg:items-start',
        railOpen && 'lg:grid-cols-[232px_minmax(0,1fr)]',
        className,
      )}
    >
      {railOpen ? rail : <div className="lg:hidden">{rail}</div>}
      <div className="min-w-0">{children}</div>
    </div>
  );
}

/**
 * Reopens a hidden facet rail. It sits in the record bar, where the rail's
 * own controls live, and carries the applied-filter count so a closed rail can
 * never hide active scoping.
 */
export function ShowFiltersButton({ onClick, appliedCount = 0, className }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title="Show filters"
      className={cn(
        'hidden h-8 shrink-0 items-center gap-2 rounded-[var(--radius-control)] border border-line-strong bg-surface px-2.5 text-[12.5px] font-medium text-ink-2 transition-colors hover:border-ink-3/50 hover:bg-surface-2 hover:text-ink lg:inline-flex',
        className,
      )}
    >
      <SlidersHorizontal aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
      Filters
      {appliedCount > 0 && (
        <span
          data-numeric=""
          className="rounded-full bg-info-soft px-1.5 text-[10.5px] font-semibold text-brand"
        >
          {appliedCount}
        </span>
      )}
    </button>
  );
}

/**
 * Header strip that sits on top of a record surface: result count on the left,
 * search and view controls on the right. Distinct from the page title strip -
 * this one describes the *result set*, not the screen.
 */
export function RecordBar({ children, trailing, className }) {
  return (
    <div
      className={cn(
        'flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line bg-surface-2/70 px-3 py-2.5',
        className,
      )}
    >
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-2">{children}</div>
      {trailing && <div className="flex shrink-0 items-center gap-2">{trailing}</div>}
    </div>
  );
}

/** Live "N of M" result readout - the count is the operator's feedback loop. */
export function ResultCount({ shown, total, unit, filtered, paged = false, loading }) {
  if (loading) {
    return <span className="text-[12.5px] text-ink-3">Counting…</span>;
  }
  return (
    <span className="text-[12.5px] text-ink-3" data-numeric="" aria-live="polite">
      <span className="font-semibold text-ink">{shown}</span>
      {/* A page of a longer list says so even unfiltered: "25 identities"
          over a 41-identity list would read as the whole. */}
      {(filtered || paged) && total !== shown ? <> of {total}</> : null} {unit}
    </span>
  );
}
