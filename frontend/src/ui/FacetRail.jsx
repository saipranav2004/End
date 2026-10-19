import { useCallback, useState } from 'react';
import { ChevronDown, Lock, PanelLeftClose, SlidersHorizontal, X } from 'lucide-react';
import { formatNumber } from '../lib/format';
import { Button } from './Button';
import { cn } from './cn';

const RAIL_KEY = 'dna.facets.open';

/** Whether the desktop rail is showing. Persisted per viewer. */
export function useFacetRail() {
  const [open, setOpen] = useState(() => {
    try {
      return localStorage.getItem(RAIL_KEY) !== '0';
    } catch {
      return true;
    }
  });

  const toggle = useCallback(() => {
    setOpen((value) => {
      const next = !value;
      try {
        localStorage.setItem(RAIL_KEY, next ? '1' : '0');
      } catch {
        /* per-viewer convenience only - safe to lose */
      }
      return next;
    });
  }, []);

  return { railOpen: open, toggleRail: toggle };
}

/**
 * Persistent filter rail - the defining control of a triage console.
 *
 * Counts come from the backend's own aggregates (the scan summary, or the
 * full client-side finding set), so an operator knows the size of a filter
 * before applying it. A group with no counts available simply omits them
 * rather than showing a guess.
 *
 * The rail closes. Filtering is a phase of work, not a permanent state, and an
 * operator reading a wide table wants those 232px back - so the header carries
 * a close control and the work area gives the records the full width. The
 * choice is remembered, and any applied filters stay applied and stay visible
 * as chips above the records, so closing the rail never hides active scoping.
 */
export function FacetRail({
  groups,
  appliedCount,
  onClearAll,
  onClose,
  className,
  mobileTitle = 'Filters',
}) {
  const [openOnMobile, setOpenOnMobile] = useState(false);

  const body = (
    <>
      <div className="flex items-center gap-2 border-b border-line bg-surface-2 px-3.5 py-2.5">
        <SlidersHorizontal aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
        <span className="text-[10.5px] font-semibold tracking-[0.12em] text-ink-3 uppercase">
          Filters
        </span>
        {appliedCount > 0 && (
          <span
            data-numeric=""
            className="rounded-full bg-info-soft px-1.5 py-0.5 text-[10.5px] font-semibold text-brand"
          >
            {appliedCount}
          </span>
        )}
        {appliedCount > 0 && (
          <Button variant="link" size="sm" onClick={onClearAll} className="ml-auto text-[11.5px]">
            Clear all
          </Button>
        )}
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            aria-label="Hide filters"
            title="Hide filters"
            className={cn(
              'hidden size-6 place-items-center rounded-md text-ink-3 transition-colors hover:bg-surface-3 hover:text-ink lg:grid',
              appliedCount > 0 ? '' : 'ml-auto',
            )}
          >
            <PanelLeftClose aria-hidden="true" className="size-3.5" />
          </button>
        )}
      </div>

      <div className="divide-y divide-line">
        {groups.map((group) => (
          <FacetGroup key={group.key} group={group} />
        ))}
      </div>
    </>
  );

  return (
    <>
      {/* Mobile: the rail becomes a disclosure above the table. */}
      <div className={cn('lg:hidden', className)}>
        <button
          type="button"
          onClick={() => setOpenOnMobile((value) => !value)}
          aria-expanded={openOnMobile}
          className="flex w-full items-center gap-2 rounded-[var(--radius-panel)] border border-line bg-surface px-3.5 py-2.5 text-left"
        >
          <SlidersHorizontal aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
          <span className="text-[13px] font-medium text-ink">{mobileTitle}</span>
          {appliedCount > 0 && (
            <span
              data-numeric=""
              className="rounded-full bg-info-soft px-1.5 py-0.5 text-[10.5px] font-semibold text-brand"
            >
              {appliedCount}
            </span>
          )}
          <ChevronDown
            aria-hidden="true"
            className={cn(
              'ml-auto size-4 shrink-0 text-ink-3 transition-transform duration-200',
              openOnMobile && 'rotate-180',
            )}
          />
        </button>
        {openOnMobile && (
          <div className="animate-fade mt-2 overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface">
            {body}
          </div>
        )}
      </div>

      <aside
        aria-label="Filters"
        className={cn(
          'sticky top-[calc(104px/var(--content-zoom))] hidden overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface lg:block',
          className,
        )}
      >
        {body}
      </aside>
    </>
  );
}

function FacetGroup({ group }) {
  const [open, setOpen] = useState(group.defaultOpen !== false);
  const activeInGroup = group.options.filter((option) => option.active).length;

  return (
    <div className="px-3.5 py-3">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className="flex w-full items-center gap-2 text-left"
      >
        <span className="text-[11.5px] font-semibold text-ink-2">{group.label}</span>
        {activeInGroup > 0 && (
          <span
            data-numeric=""
            className="rounded-full bg-info-soft px-1.5 text-[10px] font-semibold text-brand"
          >
            {activeInGroup}
          </span>
        )}
        <ChevronDown
          aria-hidden="true"
          className={cn(
            'ml-auto size-3.5 shrink-0 text-ink-3 transition-transform duration-200',
            !open && '-rotate-90',
          )}
        />
      </button>

      {open && (
        <>
          <div className="mt-2 flex flex-col gap-0.5">
            {group.options.length === 0 ? (
              <p className="py-1 text-[11.5px] text-ink-3">Nothing to filter on in this scan.</p>
            ) : (
              group.options.map((option) => (
                <label
                  key={option.value}
                  className={cn(
                    'flex cursor-pointer items-center gap-2.5 rounded-md py-1.5 pr-1 text-[12.5px] transition-colors',
                    option.active ? 'font-semibold text-brand' : 'text-ink-2 hover:text-ink',
                  )}
                >
                  <input
                    type="checkbox"
                    checked={option.active}
                    onChange={() => group.onToggle(option.value)}
                    className="size-3.5 shrink-0 accent-[var(--t-brand)]"
                  />
                  <span className="min-w-0 flex-1 truncate" title={option.label}>
                    {option.label}
                  </span>
                  {Number.isFinite(option.count) && (
                    <span
                      data-numeric=""
                      className={cn(
                        'shrink-0 text-[11.5px]',
                        option.active ? 'text-brand' : 'text-ink-3',
                      )}
                    >
                      {formatNumber(option.count)}
                    </span>
                  )}
                </label>
              ))
            )}
          </div>
          {group.note && (
            <p className="mt-2 text-[11px] leading-relaxed text-ink-3">{group.note}</p>
          )}
        </>
      )}
    </div>
  );
}

/** Applied-filter summary shown above a grid, with per-chip removal. */
export function AppliedFilters({ filters, onRemove, onClearAll }) {
  if (!filters || filters.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1.5 border-b border-line bg-surface px-3 py-2">
      {filters.map((filter) =>
        /* A locked chip states a scope the screen is defined by - it is shown
           so the scoping is never invisible, but it cannot be removed. */
        filter.locked ? (
          <span
            key={filter.key}
            title="This screen is scoped to these records"
            className="inline-flex max-w-full items-center gap-1.5 rounded-full border border-line-strong bg-surface-3 px-2.5 py-1 text-[11.5px] font-medium text-ink-2"
          >
            <Lock aria-hidden="true" className="size-3 shrink-0 text-ink-3" />
            <span className="text-ink-3">{filter.label}</span>
            <span className="truncate">{filter.value}</span>
          </span>
        ) : (
          <button
            key={filter.key}
            type="button"
            onClick={() => onRemove(filter.key)}
            className="group inline-flex max-w-full items-center gap-1.5 rounded-full border border-brand/25 bg-info-soft px-2.5 py-1 text-[11.5px] font-medium text-brand transition-colors hover:border-brand/50"
          >
            <span className="text-brand/70">{filter.label}</span>
            <span className="truncate">{filter.value}</span>
            <X aria-hidden="true" className="size-3 shrink-0 opacity-60 group-hover:opacity-100" />
            <span className="sr-only">Remove filter</span>
          </button>
        ),
      )}
      {filters.filter((filter) => !filter.locked).length > 1 && (
        <Button variant="link" size="sm" onClick={onClearAll} className="text-[11.5px]">
          Clear all
        </Button>
      )}
    </div>
  );
}
