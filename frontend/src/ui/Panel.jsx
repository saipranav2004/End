import { cn } from './cn';

/**
 * The single container primitive.
 *
 * `prominence` is what stops a screen reading as a wall of equal cards. A
 * dashboard where every panel carries the same border, radius and background
 * gives the eye nowhere to land, so each screen names one panel `lead` - the
 * thing the operator came for - and demotes the supporting ones to `quiet`.
 * The difference is carried by surface, border and elevation rather than by
 * colour, so the severity palette keeps its meaning.
 *
 *   lead    the work on this screen. Raised, stronger border, roomier.
 *   default supporting detail. Hairline border on the base surface.
 *   quiet   reference material. Recessed onto the canvas tint, no elevation.
 */
const PROMINENCE = {
  lead: 'border-line-strong bg-surface shadow-[var(--t-shadow-md)]',
  default: 'border-line bg-surface',
  quiet: 'border-line bg-surface-2',
};

const PADDING = {
  lead: 'p-4 sm:p-5 lg:p-6',
  default: 'p-4 sm:p-5',
  quiet: 'p-3.5 sm:p-4',
};

export function Panel({
  as: Tag = 'section',
  className,
  flush = false,
  prominence = 'default',
  children,
  ...rest
}) {
  return (
    <Tag
      className={cn(
        /* min-w-0: a panel is usually a grid item, and grid items default to
           min-width:auto - without this a long line inside would widen the
           whole column past the viewport on small screens. */
        /* A query container too: what a panel holds should respond to the
           panel's width, not the window's. A legend or a two-up split inside a
           420px panel has no business consulting the viewport. */
        '@container min-w-0 rounded-[var(--radius-panel)] border',
        PROMINENCE[prominence] ?? PROMINENCE.default,
        !flush && (PADDING[prominence] ?? PADDING.default),
        className,
      )}
      {...rest}
    >
      {children}
    </Tag>
  );
}

const TITLE_SIZE = {
  lead: 'text-[17px] font-bold tracking-[-0.012em]',
  default: 'text-[14.5px] font-semibold',
  quiet: 'text-[13px] font-semibold',
};

export function PanelHeader({
  title,
  subtitle,
  icon: Icon,
  actions,
  className,
  children,
  id,
  prominence = 'default',
}) {
  return (
    <header
      className={cn(
        'flex flex-wrap items-start justify-between gap-x-4 gap-y-2',
        className,
      )}
    >
      <div className="min-w-0">
        <h2
          id={id}
          className={cn(
            'flex items-center gap-2 leading-tight text-ink',
            TITLE_SIZE[prominence] ?? TITLE_SIZE.default,
          )}
        >
          {Icon && <Icon aria-hidden="true" className="size-4 shrink-0 text-brand" />}
          <span className="truncate">{title}</span>
        </h2>
        {subtitle && <p className="mt-1 max-w-prose text-[12.5px] text-ink-3">{subtitle}</p>}
        {children}
      </div>
      {actions && <div className="ml-auto flex shrink-0 items-center gap-2">{actions}</div>}
    </header>
  );
}

/** Small uppercase divider used to group fields and detail rows. */
export function SectionLabel({ children, className }) {
  return (
    <p
      className={cn(
        'text-[10.5px] font-semibold tracking-[0.13em] text-ink-3 uppercase',
        className,
      )}
    >
      {children}
    </p>
  );
}

/** Label/value row used throughout detail views. */
export function DetailRow({ label, children, mono = false, className }) {
  return (
    <div className={cn('grid gap-0.5 py-2 sm:grid-cols-[minmax(0,150px)_minmax(0,1fr)] sm:gap-4', className)}>
      <dt className="text-[12.5px] text-ink-3">{label}</dt>
      <dd
        className={cn(
          'min-w-0 text-[13px] break-words text-ink',
          mono && 'font-mono text-[12.5px]',
        )}
      >
        {children}
      </dd>
    </div>
  );
}

export function DetailList({ className, children }) {
  return <dl className={cn('divide-y divide-line', className)}>{children}</dl>;
}
