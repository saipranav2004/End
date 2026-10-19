import { useId } from 'react';
import { formatNumber } from '../../lib/format';
import { cn } from '../../ui/cn';

/**
 * The purpose-built visuals the Genome screens need. Each one exists because a
 * generic chart would say less:
 *
 *   Fingerprint    every dimension at once. A bar chart would lose the shape,
 *                  which is the thing being read.
 *   ScheduleGrid   7 x 24 of activity. "Off-hours" is only meaningful next to
 *                  the hours that are normal for this identity.
 *   ApiShareList   ranked call mix.
 *
 * All of them draw marks in the data hue (`--t-series-1`, the same blue
 * `TrendChart` defaults to) and reserve the severity tiers for severity.
 */

/* ── Behavioural fingerprint ──────────────────────────────────────────────── */

const SIZE = 210;
const CENTRE = SIZE / 2;
const RADIUS = 74;

function axisPoint(index, count, value) {
  /* Start at twelve o'clock so the first axis is where the eye lands. */
  const angle = (Math.PI * 2 * index) / count - Math.PI / 2;
  const distance = (Math.max(0, Math.min(100, value)) / 100) * RADIUS;
  return [CENTRE + Math.cos(angle) * distance, CENTRE + Math.sin(angle) * distance];
}

function polygon(values, axes) {
  return axes
    .map((axis, index) => axisPoint(index, axes.length, values[axis.key] ?? 0).map((n) => n.toFixed(1)).join(','))
    .join(' ');
}

/**
 * `axes` is `[{ key, label }]` (three or more), `series` is
 * `[{ key, label, values, tone }]` with values 0-100 keyed by axis. Two series
 * is the useful maximum: overlapping polygons stop being readable.
 */
export function Fingerprint({ series, axes, className, valueSuffix = '' }) {
  const titleId = useId();

  return (
    <div className={cn('flex flex-col items-center gap-3', className)}>
      <svg
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        role="img"
        aria-labelledby={titleId}
        className="w-full max-w-[240px]"
      >
        <title id={titleId}>
          {series.map((entry) => `${entry.label}: ${axes.map((axis) => `${axis.label} ${Math.round(entry.values[axis.key] ?? 0)}${valueSuffix}`).join(', ')}`).join('. ')}
        </title>

        {/* Rings at 25% steps, so a reader can estimate a value without a scale. */}
        {[0.25, 0.5, 0.75, 1].map((step) => (
          <polygon
            key={step}
            points={polygon(
              Object.fromEntries(axes.map((axis) => [axis.key, step * 100])),
              axes,
            )}
            fill="none"
            stroke="var(--t-line)"
            strokeWidth="1"
          />
        ))}

        {axes.map((axis, index) => {
          const [x, y] = axisPoint(index, axes.length, 100);
          return <line key={axis.key} x1={CENTRE} y1={CENTRE} x2={x} y2={y} stroke="var(--t-line)" strokeWidth="1" />;
        })}

        {series.map((entry) => (
          <polygon
            key={entry.key}
            points={polygon(entry.values, axes)}
            fill={entry.tone}
            fillOpacity={entry.fillOpacity ?? 0.16}
            stroke={entry.tone}
            strokeWidth="1.75"
            strokeDasharray={entry.dashed ? '4 3' : undefined}
          />
        ))}
      </svg>

      <dl className="grid w-full grid-cols-2 gap-x-4 gap-y-1">
        {axes.map((axis) => (
          <div key={axis.key} className="flex items-baseline justify-between gap-2">
            <dt className="truncate text-[11.5px] text-ink-3" title={axis.label}>
              {axis.label}
            </dt>
            <dd className="flex shrink-0 items-baseline gap-1.5">
              {series.map((entry) => (
                <span
                  key={entry.key}
                  data-numeric=""
                  className="text-[11.5px] font-semibold"
                  style={{ color: entry.tone }}
                  title={entry.label}
                >
                  {Math.round(entry.values[axis.key] ?? 0)}
                  {valueSuffix}
                </span>
              ))}
            </dd>
          </div>
        ))}
      </dl>

      {series.length > 1 && (
      <ul className="flex flex-wrap items-center justify-center gap-x-4 gap-y-1">
        {series.map((entry) => (
          <li key={entry.key} className="flex items-center gap-1.5 text-[11.5px] text-ink-3">
            <span
              aria-hidden="true"
              className="h-0.5 w-4 rounded-full"
              style={{ background: entry.tone }}
            />
            {entry.label}
          </li>
        ))}
      </ul>
      )}
    </div>
  );
}

/* ── Activity schedule ────────────────────────────────────────────────────── */

const DAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
const LEVEL_OPACITY = [0.06, 0.28, 0.58, 1];

/** Busiest hour sets the top of the scale; any call at all is level 1. */
function levelOf(calls, peak) {
  if (!calls || !peak) return 0;
  return Math.max(1, Math.min(3, Math.ceil((calls / peak) * 3)));
}

/**
 * `cells` is `[{ day_of_week 1-7 (Mon=1), hour 0-23 UTC, calls }]`; hours
 * missing from it had no calls.
 */
export function ScheduleGrid({ cells, className }) {
  const grid = DAYS.map(() => Array(24).fill(0));
  for (const cell of cells) {
    const day = Number(cell.day_of_week) - 1;
    const hour = Number(cell.hour);
    if (day >= 0 && day < 7 && hour >= 0 && hour < 24) grid[day][hour] += Number(cell.calls) || 0;
  }
  const peak = Math.max(0, ...grid.flat());

  return (
    <div className={cn('min-w-0', className)}>
      <div className="flex gap-1.5">
        <div className="flex shrink-0 flex-col gap-[3px] pt-[15px]">
          {DAYS.map((day) => (
            <span key={day} className="h-3 text-[10px] leading-3 text-ink-3">
              {day}
            </span>
          ))}
        </div>

        <div className="min-w-0 flex-1">
          <div className="mb-1 flex justify-between text-[10px] text-ink-3">
            {['00', '06', '12', '18', '23'].map((hour) => (
              <span key={hour}>{hour}:00</span>
            ))}
          </div>
          <div className="flex flex-col gap-[3px]" role="img" aria-label={`Calls by weekday and hour, UTC. Busiest hour ${formatNumber(peak)} calls.`}>
            {grid.map((hours, dayIndex) => (
              <div key={DAYS[dayIndex]} className="flex gap-[3px]">
                {hours.map((calls, hourIndex) => (
                  <span
                    key={hourIndex}
                    title={`${DAYS[dayIndex]} ${String(hourIndex).padStart(2, '0')}:00 UTC - ${formatNumber(calls)} ${calls === 1 ? 'call' : 'calls'}`}
                    className="h-3 min-w-0 flex-1 rounded-[2px]"
                    style={{ background: 'var(--t-series-1)', opacity: LEVEL_OPACITY[levelOf(calls, peak)] }}
                  />
                ))}
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="mt-2.5 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] text-ink-3">
        <span className="flex items-center gap-1.5">
          Quiet
          {LEVEL_OPACITY.map((opacity) => (
            <span
              key={opacity}
              aria-hidden="true"
              className="size-2.5 rounded-[2px]"
              style={{ background: 'var(--t-series-1)', opacity }}
            />
          ))}
          Busy
        </span>
        <span>Busiest hour: {formatNumber(peak)} calls. Times are UTC.</span>
      </div>
    </div>
  );
}

/* ── Ranked API share ─────────────────────────────────────────────────────── */

/** `actions` is `[{ api, share (0-100), calls }]`, already ranked. */
export function ApiShareList({ actions, className }) {
  return (
    <ul className={cn('flex flex-col', className)}>
      {actions.map((action) => (
        <li key={action.api} className="border-b border-line py-2 last:border-0">
          <div className="flex items-baseline justify-between gap-3">
            <span className="min-w-0 truncate font-mono text-[12px] text-ink-2" title={action.api}>
              {action.api}
            </span>
            <span data-numeric="" className="shrink-0 text-[12px] font-semibold text-ink">
              {formatShare(action.share)}
            </span>
          </div>
          <div className="mt-1.5 flex items-center gap-2">
            <span className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-track">
              <span
                className="block h-full rounded-full"
                style={{ width: `${Math.max(0, Math.min(100, action.share))}%`, background: 'var(--t-series-1)' }}
              />
            </span>
            <span data-numeric="" className="shrink-0 text-[11px] text-ink-3">
              {formatNumber(action.calls)} {action.calls === 1 ? 'call' : 'calls'}
            </span>
          </div>
        </li>
      ))}
    </ul>
  );
}

/** Shares under 1% keep a decimal, so a rare call does not read as zero. */
export function formatShare(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return '-';
  if (n > 0 && n < 1) return `${n.toFixed(1)}%`;
  return `${Math.round(n)}%`;
}
