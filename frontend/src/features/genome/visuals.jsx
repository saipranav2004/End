import { formatNumber } from '../../lib/format';
import { cn } from '../../ui/cn';

/**
 * The purpose-built visuals the Genome screens need. Each one exists because a
 * generic chart would say less:
 *
 *   ScheduleGrid   7 x 24 of activity. "Off-hours" is only meaningful next to
 *                  the hours that are normal for this identity.
 *   ApiShareList   ranked call mix.
 *
 * All of them draw marks in the data hue (`--t-series-1`, the same blue
 * `TrendChart` defaults to) and reserve the severity tiers for severity.
 */

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
