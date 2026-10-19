import { useEffect, useState } from 'react';
import { formatNumber, formatPercent } from '../lib/format';
import { cn } from '../ui/cn';

/**
 * Ranked magnitude list. A bar list beats a bar chart here: the category names
 * are long, arbitrary strings from the backend and a rotated axis would be
 * unreadable. One hue only - length carries the value, colour carries nothing.
 */
export function BarList({ items, total, onSelect, valueSuffix, max: providedMax, className }) {
  const [grown, setGrown] = useState(false);

  useEffect(() => {
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduced) {
      setGrown(true);
      return undefined;
    }
    const id = requestAnimationFrame(() => setGrown(true));
    return () => cancelAnimationFrame(id);
  }, [items]);

  const max = providedMax ?? Math.max(1, ...items.map((item) => Number(item.value) || 0));
  const sum = total ?? items.reduce((acc, item) => acc + (Number(item.value) || 0), 0);

  return (
    <ul className={cn('flex flex-col', className)}>
      {items.map((item, index) => {
        const value = Number(item.value) || 0;
        const width = (value / max) * 100;
        const Row = onSelect ? 'button' : 'div';

        return (
          <li key={item.key} className="border-b border-line/70 last:border-b-0">
            <Row
              type={onSelect ? 'button' : undefined}
              onClick={onSelect ? () => onSelect(item) : undefined}
              className={cn(
                'group flex w-full items-center gap-3 py-2.5 text-left',
                onSelect && 'cursor-pointer',
              )}
            >
              <span className="flex min-w-0 flex-1 flex-col gap-1.5">
                <span className="flex items-baseline justify-between gap-3">
                  <span
                    className="truncate text-[12.5px] font-medium text-ink"
                    title={item.label}
                  >
                    {item.label}
                  </span>
                  <span className="shrink-0 text-[11.5px] text-ink-3" data-numeric="">
                    {formatPercent(value, sum, 1)}
                  </span>
                </span>
                <span className="h-1.5 w-full overflow-hidden rounded-full bg-track">
                  <span
                    className="block h-full rounded-full transition-[width] duration-[900ms] ease-[var(--ease-out-quint)] group-hover:brightness-110"
                    style={{
                      width: grown ? `${Math.max(width, 1.5)}%` : '0%',
                      background: 'var(--t-ramp-4)',
                      transitionDelay: `${Math.min(index * 45, 360)}ms`,
                    }}
                  />
                </span>
              </span>
              <span
                data-numeric=""
                className="w-14 shrink-0 text-right font-display text-[15px] font-bold text-ink"
              >
                {formatNumber(value)}
                {valueSuffix}
              </span>
            </Row>
          </li>
        );
      })}
    </ul>
  );
}
