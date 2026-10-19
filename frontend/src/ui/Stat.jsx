import { ArrowUpRight } from 'lucide-react';
import { formatNumber } from '../lib/format';
import { Meter } from './Meter';
import { cn, TONE_FG, TONE_VAR } from './cn';

/**
 * Metric tile. The number is the loudest thing in the tile; the label,
 * denominator and meter exist to make it interpretable at a glance.
 * Tiles that lead somewhere render as links and say so on hover.
 *
 * Its type and padding run about 10% tighter than the rest of the console on
 * purpose: four of these sit in one row, and at that count the uppercase label
 * is what decides how narrow a readable tile can be.
 */
export function MetricTile({
  label,
  value,
  unit,
  caption,
  tone = 'brand',
  meter,
  meterLabel,
  /** Numbers for an inline history line, revealed on hover. Supplied only
      where the backend actually carries history for this measure. */
  sparkline,
  icon: Icon,
  as: Tag = 'div',
  className,
  ...rest
}) {
  /* The number is printed, not counted up to. An operator reads these figures
     dozens of times a day and needs to compare them against what they saw an
     hour ago; a 620ms roll-up delays every one of those reads and makes the
     digits unstable while it runs. Counting up is a first-impression effect on
     a screen nobody sees for the first time twice. */
  // null means "not known", which must not read as zero (Number(null) is 0).
  const numeric = value === null || value === undefined || value === '' ? NaN : Number(value);
  const display = Number.isFinite(numeric) ? formatNumber(numeric) : '-';
  const interactive = Tag !== 'div';

  return (
    <Tag
      className={cn(
        'group relative flex flex-col overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface p-3.5 text-left',
        interactive &&
          'transition-[border-color,box-shadow,transform] duration-200 hover:-translate-y-px hover:border-line-strong hover:shadow-md',
        className,
      )}
      {...rest}
    >
      <span
        aria-hidden="true"
        className="absolute inset-y-0 left-0 w-[3px]"
        style={{ background: TONE_VAR[tone] }}
      />

      <div className="flex items-start justify-between gap-3">
        <span className="text-[10.5px] font-semibold tracking-[0.085em] text-ink-3 uppercase">
          {label}
        </span>
        {Icon && <Icon aria-hidden="true" className={cn('size-4 shrink-0', TONE_FG[tone])} />}
        {interactive && !Icon && (
          <ArrowUpRight
            aria-hidden="true"
            className="size-4 shrink-0 text-ink-3 opacity-0 transition-opacity duration-150 group-hover:opacity-100"
          />
        )}
      </div>

      <div className="mt-2 flex items-baseline gap-1.5">
        <span
          data-numeric=""
          className="font-display text-[27px] leading-none font-extrabold tracking-[-0.03em] text-ink"
        >
          {display}
        </span>
        {unit && <span className="text-[12px] font-medium text-ink-3">{unit}</span>}
      </div>

      {caption && <p className="mt-1.5 text-[11.5px] leading-snug text-ink-3">{caption}</p>}

      {sparkline?.length > 1 && (
        <Sparkline values={sparkline} tone={tone} className="mt-3" />
      )}

      {meter !== undefined && meter !== null && (
        <Meter value={meter} tone={tone} height={4} className="mt-3" label={meterLabel || label} />
      )}
    </Tag>
  );
}

/**
 * History line for a metric tile. Recedes until the tile is hovered, so the
 * headline number stays the loudest thing in the tile. Rendered only where the
 * backend actually carries history for that measure.
 */
function Sparkline({ values, tone, className }) {
  const numbers = values.filter((value) => Number.isFinite(Number(value))).map(Number);
  if (numbers.length < 2) return null;

  const max = Math.max(...numbers);
  const min = Math.min(...numbers);
  const span = max - min || 1;
  const points = numbers
    .map((value, index) => {
      const x = (index / (numbers.length - 1)) * 100;
      const y = 22 - ((value - min) / span) * 20;
      return `${x.toFixed(2)},${y.toFixed(2)}`;
    })
    .join(' ');

  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 100 24"
      preserveAspectRatio="none"
      className={cn(
        'h-6 w-full opacity-40 transition-opacity duration-200 group-hover:opacity-100',
        className,
      )}
    >
      <polyline
        points={points}
        fill="none"
        stroke={TONE_VAR[tone]}
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
