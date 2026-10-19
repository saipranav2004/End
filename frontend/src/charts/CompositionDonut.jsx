import { useState } from 'react';
import { Cell, Pie, PieChart, ResponsiveContainer, Sector } from 'recharts';
import { formatNumber, formatPercent } from '../lib/format';
import { cn } from '../ui/cn';

/**
 * Composition of a whole. Slices follow the canonical category order (not
 * value order) so a colour always means the same category; the legend doubles
 * as the value table, which is what makes the slices readable at all.
 */
export function CompositionDonut({
  data,
  total,
  centerLabel = 'Total',
  onSelect,
  size = 184,
  className,
}) {
  const [activeKey, setActiveKey] = useState(null);
  const activeIndex = data.findIndex((item) => item.key === activeKey);
  const sum = total ?? data.reduce((acc, item) => acc + (Number(item.value) || 0), 0);
  const active = data.find((item) => item.key === activeKey) || null;

  return (
    <div className={cn('flex flex-col items-center gap-5 @min-[27rem]:flex-row @min-[27rem]:items-center', className)}>
      <div className="relative shrink-0" style={{ width: size, height: size }}>
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={data}
              dataKey="value"
              nameKey="label"
              innerRadius="66%"
              outerRadius="100%"
              paddingAngle={2}
              stroke="var(--t-surface)"
              strokeWidth={2}
              isAnimationActive
              animationDuration={720}
              animationEasing="ease-out"
              activeIndex={activeIndex >= 0 ? activeIndex : undefined}
              activeShape={(props) => (
                /* The hovered slice lifts out of the ring, which connects a
                   legend row to its slice without needing a click. */
                <Sector {...props} outerRadius={props.outerRadius + 5} />
              )}
              onMouseEnter={(_, index) => setActiveKey(data[index]?.key ?? null)}
              onMouseLeave={() => setActiveKey(null)}
              onClick={onSelect ? (_, index) => onSelect(data[index]) : undefined}
            >
              {data.map((item) => (
                <Cell
                  key={item.key}
                  fill={item.color}
                  opacity={activeKey && activeKey !== item.key ? 0.4 : 1}
                  style={{
                    cursor: onSelect ? 'pointer' : 'default',
                    transition: 'opacity 160ms ease-out',
                    outline: 'none',
                  }}
                />
              ))}
            </Pie>
          </PieChart>
        </ResponsiveContainer>

        <div className="pointer-events-none absolute inset-0 grid place-content-center text-center">
          <p
            data-numeric=""
            className="font-display text-[26px] leading-none font-extrabold tracking-[-0.03em] text-ink"
          >
            {formatNumber(active ? active.value : sum)}
          </p>
          <p className="mt-1 max-w-[7rem] text-[10.5px] leading-tight font-semibold tracking-[0.08em] text-ink-3 uppercase">
            {active ? active.label : centerLabel}
          </p>
        </div>
      </div>

      <ul className="flex w-full min-w-0 flex-col gap-0.5">
        {data.map((item) => {
          const Row = onSelect ? 'button' : 'div';
          const dimmed = activeKey && activeKey !== item.key;
          return (
            <li key={item.key}>
              <Row
                type={onSelect ? 'button' : undefined}
                onClick={onSelect ? () => onSelect(item) : undefined}
                onMouseEnter={() => setActiveKey(item.key)}
                onMouseLeave={() => setActiveKey(null)}
                onFocus={() => setActiveKey(item.key)}
                onBlur={() => setActiveKey(null)}
                className={cn(
                  'flex w-full items-center gap-2.5 rounded-md px-1.5 py-1.5 text-left transition-[background-color,opacity] duration-150',
                  onSelect && 'cursor-pointer hover:bg-surface-2',
                  dimmed && 'opacity-55',
                )}
              >
                <span
                  aria-hidden="true"
                  className="size-2.5 shrink-0 rounded-[3px]"
                  style={{ background: item.color }}
                />
                <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink-2" title={item.label}>
                  {item.label}
                </span>
                <span data-numeric="" className="text-[12.5px] font-semibold text-ink">
                  {formatNumber(item.value)}
                </span>
                <span data-numeric="" className="w-11 shrink-0 text-right text-[11.5px] text-ink-3">
                  {formatPercent(item.value, sum, 1)}
                </span>
              </Row>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
