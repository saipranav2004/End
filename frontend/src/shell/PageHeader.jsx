import { cn } from '../ui/cn';

/**
 * Title strip. Full-bleeds to the content edges so it reads as shell rather
 * than as a card, and hosts the screen's primary actions plus (optionally) its
 * view tabs - the console pattern of breadcrumb → title → views → work area.
 */
export function PageHeader({ title, lede, actions, tabs, meta, className }) {
  return (
    <header
      className={cn(
        '-mx-3 -mt-4 border-b border-line bg-surface px-3 pt-4 sm:-mx-5 sm:px-5 lg:-mx-6 lg:px-6',
        className,
      )}
    >
      <div className="mx-auto w-full max-w-[1760px]">
        <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3 pb-3.5">
          <div className="min-w-0">
            <h1 className="text-[21px] leading-tight font-extrabold tracking-[-0.026em] text-ink sm:text-[23px]">
              {title}
            </h1>
            {lede && (
              <p className="mt-1.5 max-w-2xl text-balance text-[12.5px] leading-relaxed text-ink-2">
                {lede}
              </p>
            )}
          </div>
          {/* Allowed to wrap and shrink: three actions at 360px would otherwise
              push the page wider than the viewport. */}
          {actions && (
            <div className="flex min-w-0 flex-wrap items-center gap-2 sm:shrink-0">{actions}</div>
          )}
        </div>
        {meta && <div className="pb-3.5">{meta}</div>}
        {tabs}
      </div>
    </header>
  );
}
