import { ChevronLeft, ChevronRight } from 'lucide-react';
import { Select } from './Field';
import { IconButton } from './Button';
import { formatNumber } from '../lib/format';
import { cn } from './cn';

const PAGE_SIZES = [10, 25, 50, 100];

/**
 * Server-backed pagination. Page size is capped at 100 because the API
 * silently falls back to its default above that.
 */
export function Pagination({
  page,
  pageSize,
  total,
  onPageChange,
  onPageSizeChange,
  unit = 'records',
  className,
}) {
  const totalPages = Math.max(1, Math.ceil((total || 0) / (pageSize || 1)));
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = Math.min(total, page * pageSize);

  return (
    <div
      className={cn(
        'flex flex-wrap items-center justify-between gap-x-4 gap-y-3 border-t border-line px-4 py-3',
        className,
      )}
    >
      <p className="text-[12.5px] text-ink-3" data-numeric="" aria-live="polite">
        <span className="font-medium text-ink-2">
          {formatNumber(from)}-{formatNumber(to)}
        </span>{' '}
        of {formatNumber(total)} {unit}
      </p>

      <div className="flex items-center gap-3">
        {onPageSizeChange && (
          <label className="flex items-center gap-2 text-[12.5px] text-ink-3">
            <span className="hidden sm:inline">Rows</span>
            <Select
              size="sm"
              value={String(pageSize)}
              onChange={(event) => onPageSizeChange(Number(event.target.value))}
              options={PAGE_SIZES.map((size) => ({ value: String(size), label: String(size) }))}
              className="w-18"
              aria-label="Rows per page"
            />
          </label>
        )}

        {/* Full-size targets. These are the most-clicked controls on a record
            screen, and at `sm` the chevrons were a 14px glyph in a 32px box -
            below the 40px touch target every platform guideline asks for. */}
        <div className="flex items-center gap-1.5">
          <IconButton
            icon={ChevronLeft}
            label="Previous page"
            variant="secondary"
            disabled={page <= 1}
            onClick={() => onPageChange(page - 1)}
          />
          <span
            className="min-w-[6.5rem] px-1 text-center text-[13px] font-medium whitespace-nowrap text-ink-2"
            data-numeric=""
          >
            Page {formatNumber(page)} of {formatNumber(totalPages)}
          </span>
          <IconButton
            icon={ChevronRight}
            label="Next page"
            variant="secondary"
            disabled={page >= totalPages}
            onClick={() => onPageChange(page + 1)}
          />
        </div>
      </div>
    </div>
  );
}
