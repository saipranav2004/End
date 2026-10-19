import { cn } from '../ui/cn';

/**
 * Shared tooltip surface for every Recharts chart, so the hover layer looks
 * and reads the same everywhere.
 */
export function TooltipCard({ title, subtitle, rows, className }) {
  return (
    <div
      className={cn(
        'pointer-events-none min-w-44 rounded-[var(--radius-control)] border border-line bg-surface p-2.5 shadow-lg',
        className,
      )}
    >
      <p className="text-[12.5px] font-semibold text-ink">{title}</p>
      {subtitle && <p className="mt-0.5 text-[11.5px] text-ink-3">{subtitle}</p>}
      {rows?.length > 0 && (
        <dl className="mt-2 flex flex-col gap-1.5">
          {rows.map((row) => (
            <div key={row.label} className="flex items-center justify-between gap-4">
              <dt className="flex min-w-0 items-center gap-1.5 text-[11.5px] text-ink-2">
                {row.color && (
                  <span
                    aria-hidden="true"
                    className="size-2 shrink-0 rounded-[2px]"
                    style={{ background: row.color }}
                  />
                )}
                <span className="truncate">{row.label}</span>
              </dt>
              <dd data-numeric="" className="text-[12px] font-semibold text-ink">
                {row.value}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}
