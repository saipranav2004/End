import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { formatNumber } from '../lib/format';
import { TooltipCard } from './ChartTooltip';
import { cn } from '../ui/cn';

/**
 * One measure over the sequence of completed scans.
 *
 * Deliberately one series per chart: identities, events and credentials differ by
 * orders of magnitude, and a shared axis would flatten two of them while a
 * second axis would invite false comparisons. Small multiples instead.
 */
export function TrendChart({
  data,
  dataKey,
  label,
  color = 'var(--t-series-1)',
  height = 132,
  showXAxis = true,
  /** Charts sharing a syncId share one cursor, so a single hover reads all
      three measures at the same scan. */
  syncId,
  valueFormatter = formatNumber,
  /** Y-axis range; recharts' automatic range when omitted. */
  yDomain,
  className,
}) {
  const gradientId = `trend-${dataKey}`;

  return (
    <div className={cn('w-full', className)} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} syncId={syncId} margin={{ top: 6, right: 6, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.24} />
              <stop offset="100%" stopColor={color} stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke="var(--t-chart-grid)" strokeDasharray="2 4" vertical={false} />
          {/* Small multiples share one x scale, so only the last chart in a
              stack draws the labels. */}
          <XAxis
            dataKey="label"
            hide={!showXAxis}
            tick={{ fill: 'var(--t-chart-axis)', fontSize: 10.5 }}
            tickLine={false}
            axisLine={{ stroke: 'var(--t-chart-grid)' }}
            interval="preserveStartEnd"
            minTickGap={24}
          />
          <YAxis
            tick={{ fill: 'var(--t-chart-axis)', fontSize: 10.5 }}
            tickLine={false}
            axisLine={false}
            width={38}
            domain={yDomain ?? ['auto', 'auto']}
            allowDecimals={false}
            tickFormatter={(value) => formatNumber(value)}
          />
          <Tooltip
            cursor={{ stroke: 'var(--t-chart-axis)', strokeWidth: 1, strokeDasharray: '3 3' }}
            content={({ active, payload }) => {
              if (!active || !payload?.length) return null;
              const point = payload[0].payload;
              return (
                <TooltipCard
                  title={point.label}
                  subtitle={point.subtitle}
                  rows={[{ label, value: valueFormatter(point[dataKey]), color }]}
                />
              );
            }}
          />
          <Area
            type="monotone"
            dataKey={dataKey}
            stroke={color}
            strokeWidth={2}
            fill={`url(#${gradientId})`}
            dot={false}
            activeDot={{ r: 4, strokeWidth: 2, stroke: 'var(--t-surface)' }}
            isAnimationActive
            animationDuration={760}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
