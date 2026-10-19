import { Link } from 'react-router-dom';
import { ArrowUpRight } from 'lucide-react';
import { formatNumber, formatPercent, percentValue } from '../../lib/format';
import { Meter } from '../../ui/Meter';
import { Skeleton } from '../../ui/Skeleton';
import { cn, TONE_FG } from '../../ui/cn';

/**
 * Risk signals, ranked by share of their own population.
 *
 * Each row reads one KPI of the discovery overview. A row links into the
 * inventory only where the list API has a filter that means the same thing,
 * so a link never lands on a list that disagrees with the number beside it.
 *
 *   rows: [{ key, label, rationale, count, denominator, denominatorLabel, tone, to? }]
 */
export function SignalList({ rows, loading }) {
  if (loading) {
    return (
      <ul className="flex flex-col divide-y divide-line">
        {Array.from({ length: 5 }).map((_, index) => (
          <li key={index} className="flex items-center gap-4 py-3">
            <div className="min-w-0 flex-1">
              <Skeleton className="h-3 w-40 rounded" />
              <Skeleton className="mt-2.5 h-1.5 w-full rounded-full" />
            </div>
            <Skeleton className="h-6 w-12 rounded" />
          </li>
        ))}
      </ul>
    );
  }

  const ranked = [...rows]
    .map((row) => ({ ...row, share: percentValue(row.count, row.denominator) }))
    .sort((a, b) => b.share - a.share || b.count - a.count);

  return (
    <ul className="flex flex-col divide-y divide-line">
      {ranked.map((row) => {
        const inert = row.count === 0;
        const Row = row.to ? Link : 'div';
        return (
          <li key={row.key}>
            <Row
              {...(row.to
                ? { to: row.to, 'aria-label': `${row.count} ${row.label} - open the list` }
                : {})}
              className="group flex items-center gap-4 py-3 transition-opacity duration-150"
            >
              <span className="min-w-0 flex-1">
                <span className="flex items-baseline gap-2">
                  <span className={cn('truncate text-[13px] font-semibold', inert ? 'text-ink-2' : TONE_FG[row.tone])}>
                    {row.label}
                  </span>
                  {row.to && (
                    <ArrowUpRight
                      aria-hidden="true"
                      className="size-3.5 shrink-0 text-ink-3 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
                    />
                  )}
                </span>
                <span className="mt-1 block text-[11.5px] text-ink-3">{row.rationale}</span>
                <Meter
                  value={row.share}
                  tone={inert ? 'neutral' : row.tone}
                  height={4}
                  className="mt-2"
                  label={`${row.label} share`}
                />
              </span>

              <span className="w-24 shrink-0 text-right">
                <span
                  data-numeric=""
                  className={cn('block font-display text-[19px] leading-none font-extrabold', inert ? 'text-ink-3' : 'text-ink')}
                >
                  {formatNumber(row.count)}
                </span>
                <span className="mt-1 block text-[11px] text-ink-3" data-numeric="">
                  {row.denominator > 0 ? `${formatPercent(row.count, row.denominator, 1)} ${row.denominatorLabel}` : '-'}
                </span>
              </span>
            </Row>
          </li>
        );
      })}
    </ul>
  );
}
