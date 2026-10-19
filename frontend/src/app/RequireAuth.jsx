import { Navigate, useLocation } from 'react-router-dom';
import { useAuth } from './AuthContext';
import { Skeleton } from '../ui/Skeleton';

/**
 * Gate for every authenticated route. While the stored token is being
 * verified the shell's silhouette is shown rather than a spinner, so the first
 * paint already has the shape of the destination.
 */
export function RequireAuth({ children }) {
  const { isAuthenticated, status } = useAuth();
  const location = useLocation();

  if (!isAuthenticated && status === 'verifying') return <ShellSkeleton />;
  if (!isAuthenticated) return <Navigate to="/login" replace state={{ from: location }} />;
  return children;
}

function ShellSkeleton() {
  return (
    <div className="flex min-h-dvh bg-canvas" aria-busy="true">
      <div className="rail-gradient hidden w-[236px] shrink-0 flex-col gap-2 p-4 lg:flex">
        <Skeleton className="h-6 w-36 rounded bg-white/10" />
        <div className="mt-6 flex flex-col gap-2">
          {Array.from({ length: 7 }).map((_, index) => (
            <Skeleton key={index} className="h-8 rounded bg-white/[0.06]" />
          ))}
        </div>
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-14 items-center gap-3 border-b border-line px-5">
          <Skeleton className="h-8 w-48 rounded" />
          <div className="flex-1" />
          <Skeleton className="h-8 w-24 rounded" />
        </div>
        <div className="flex flex-col gap-4 p-5">
          <Skeleton className="h-8 w-64 rounded" />
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className="h-28 rounded-[var(--radius-panel)]" />
            ))}
          </div>
          <Skeleton className="h-72 rounded-[var(--radius-panel)]" />
        </div>
      </div>
      <span role="status" className="sr-only">
        Restoring your session
      </span>
    </div>
  );
}
