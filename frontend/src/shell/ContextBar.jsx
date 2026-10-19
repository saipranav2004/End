import { useLocation, useNavigate } from 'react-router-dom';
import { ArrowLeft, ChevronRight } from 'lucide-react';
import { useAccess } from '../app/useAccess';
import { ALL_NAV_ITEMS, BREADCRUMBS, BREADCRUMB_PREFIXES } from './navigation';
import { cn } from '../ui/cn';

/**
 * Context row: where am I, and how do I get back.
 *
 * Scan scope used to sit here too. It moved to the top bar, because it is
 * global state rather than a property of this screen - see `TopBar`. What is
 * left is location, which is what a context row is for. The back control exists
 * because a console is a place people navigate into, and the browser's own
 * button is not a UI.
 */
export function ContextBar() {
  const { pathname, key } = useLocation();
  const navigate = useNavigate();
  const { role } = useAccess();
  /* A screen named differently for a role (an analyst's "My work") is named
     the same way here as in the navigation. */
  const roleLabel = ALL_NAV_ITEMS.find((item) => item.to === pathname)?.roleLabels?.[role];
  const exact = BREADCRUMBS[pathname];
  const trail =
    (exact && roleLabel ? [...exact.slice(0, -1), roleLabel] : exact) ??
    BREADCRUMB_PREFIXES.find((entry) => pathname.startsWith(entry.prefix))?.trail;

  /* `key === 'default'` means this is the first entry in the history stack, so
     there is nowhere to go back to and the control would be a dead end. */
  const canGoBack = key !== 'default';

  return (
    <div className="flex h-10 items-center gap-2 border-b border-line bg-surface px-2 sm:px-4 lg:px-5">
      <button
        type="button"
        onClick={() => navigate(-1)}
        disabled={!canGoBack}
        aria-label="Go back"
        title="Go back"
        className={cn(
          'grid size-7 shrink-0 place-items-center rounded-md transition-colors',
          canGoBack
            ? 'text-ink-3 hover:bg-surface-3 hover:text-ink'
            : 'cursor-not-allowed text-ink-3/35',
        )}
      >
        <ArrowLeft aria-hidden="true" className="size-4" />
      </button>

      <span aria-hidden="true" className="h-4 w-px shrink-0 bg-line" />

      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1 text-[12px]">
        {(trail ?? ['Not found']).map((crumb, index, all) => {
          const last = index === all.length - 1;
          return (
            <span key={crumb} className="inline-flex min-w-0 items-center gap-1">
              {index > 0 && (
                <ChevronRight aria-hidden="true" className="size-3.5 shrink-0 text-ink-3/60" />
              )}
              <span
                aria-current={last ? 'page' : undefined}
                className={cn('truncate', last ? 'font-semibold text-ink' : 'text-ink-3')}
              >
                {crumb}
              </span>
            </span>
          );
        })}
      </nav>
    </div>
  );
}
