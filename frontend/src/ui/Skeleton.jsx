import { cn } from './cn';

export function Skeleton({ className, style }) {
  return <span aria-hidden="true" className={cn('skeleton block', className)} style={style} />;
}

/** Mirrors the metric strip so the layout never reflows when data lands. */
export function StatStripSkeleton({ count = 4 }) {
  return (
    <div className="grid gap-3 @min-[30rem]:grid-cols-2 @min-[54rem]:grid-cols-4" aria-hidden="true">
      {Array.from({ length: count }).map((_, index) => (
        <div key={index} className="rounded-[var(--radius-panel)] border border-line bg-surface p-4">
          <Skeleton className="h-3 w-24 rounded" />
          <Skeleton className="mt-3 h-8 w-20 rounded" />
          <Skeleton className="mt-3 h-1.5 w-full rounded-full" />
        </div>
      ))}
    </div>
  );
}

export function ChartSkeleton({ height = 240, bars = 9 }) {
  return (
    <div aria-hidden="true" className="flex items-end gap-2" style={{ height }}>
      {Array.from({ length: bars }).map((_, index) => (
        <Skeleton
          key={index}
          className="flex-1 rounded-t-md"
          style={{ height: `${34 + ((index * 37) % 62)}%` }}
        />
      ))}
    </div>
  );
}

export function DonutSkeleton({ size = 180 }) {
  return (
    <div aria-hidden="true" className="flex flex-col items-center gap-4">
      <Skeleton className="rounded-full" style={{ width: size, height: size }} />
      <div className="flex w-full flex-col gap-2">
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="flex items-center gap-2">
            <Skeleton className="size-2.5 rounded-sm" />
            <Skeleton className="h-3 flex-1 rounded" />
            <Skeleton className="h-3 w-8 rounded" />
          </div>
        ))}
      </div>
    </div>
  );
}

/** Row skeletons matched to the real grid's column template. */
export function GridSkeleton({ columns = 5, rows = 8 }) {
  return (
    <div aria-hidden="true" className="divide-y divide-line">
      {Array.from({ length: rows }).map((_, rowIndex) => (
        <div
          key={rowIndex}
          className="grid items-center gap-4 px-4 py-3.5"
          style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}
        >
          {Array.from({ length: columns }).map((__, colIndex) => (
            <Skeleton
              key={colIndex}
              className="h-3.5 rounded"
              style={{ width: colIndex === 0 ? '82%' : `${44 + ((rowIndex + colIndex) % 4) * 12}%` }}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

export function ListSkeleton({ rows = 5 }) {
  return (
    <ul aria-hidden="true" className="flex flex-col gap-3">
      {Array.from({ length: rows }).map((_, index) => (
        <li key={index} className="flex items-start gap-3">
          <Skeleton className="size-8 shrink-0 rounded-full" />
          <div className="min-w-0 flex-1">
            <Skeleton className="h-3.5 w-1/2 rounded" />
            <Skeleton className="mt-2 h-3 w-4/5 rounded" />
          </div>
        </li>
      ))}
    </ul>
  );
}

export function DetailSkeleton({ rows = 6 }) {
  return (
    <div aria-hidden="true" className="divide-y divide-line">
      {Array.from({ length: rows }).map((_, index) => (
        <div key={index} className="grid gap-2 py-3 sm:grid-cols-[150px_1fr] sm:gap-4">
          <Skeleton className="h-3 w-24 rounded" />
          <Skeleton className="h-3 rounded" style={{ width: `${52 + ((index * 13) % 40)}%` }} />
        </div>
      ))}
    </div>
  );
}

/** Screen-reader announcement to pair with any visual skeleton. */
export function LoadingAnnouncement({ label }) {
  return (
    <span role="status" aria-live="polite" className="sr-only">
      {label}
    </span>
  );
}
