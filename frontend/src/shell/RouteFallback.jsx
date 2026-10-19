import { Skeleton, StatStripSkeleton } from '../ui/Skeleton';

/**
 * Shown while a route's code chunk loads. It mirrors the standard page
 * composition - header, metric strip, primary panel - so the transition into
 * the real screen is a fill rather than a jump.
 */
export function RouteFallback() {
  return (
    <div className="flex flex-col gap-5" aria-busy="true">
      <div>
        <Skeleton className="h-2.5 w-20 rounded" />
        <Skeleton className="mt-2.5 h-7 w-72 rounded" />
        <Skeleton className="mt-3 h-3.5 w-full max-w-2xl rounded" />
      </div>
      <StatStripSkeleton count={4} />
      <Skeleton className="h-96 rounded-[var(--radius-panel)]" />
      <span role="status" className="sr-only">
        Loading screen
      </span>
    </div>
  );
}
