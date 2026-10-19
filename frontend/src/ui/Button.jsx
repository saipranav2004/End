import { forwardRef } from 'react';
import { Loader2, Lock } from 'lucide-react';
import { cn } from './cn';

const BASE =
  'relative inline-flex select-none items-center justify-center gap-2 whitespace-nowrap rounded-[var(--radius-control)] ' +
  'font-medium transition-[background-color,border-color,color,box-shadow,transform] duration-150 ' +
  'active:translate-y-[0.5px] disabled:pointer-events-none disabled:opacity-55';

const VARIANTS = {
  primary:
    'bg-brand text-white border border-brand-strong/70 shadow-sm hover:bg-brand-strong ' +
    'dark:text-ink-inverse',
  accent:
    'text-white border border-transparent shadow-sm ' +
    'bg-[linear-gradient(135deg,var(--t-accent)_0%,var(--t-brand)_100%)] hover:brightness-[1.06]',
  secondary:
    'bg-surface text-ink border border-line-strong hover:bg-surface-2 hover:border-ink-3/50 shadow-sm',
  ghost: 'bg-transparent text-ink-2 border border-transparent hover:bg-surface-3 hover:text-ink',
  danger: 'bg-critical text-white border border-critical/60 shadow-sm hover:brightness-110',
  link: 'bg-transparent border-0 text-brand hover:underline underline-offset-4 px-0',
  /* Sign-in only. The supplied design's action is a flat #1492c4 - sampling
     across its full width returned the same value at both ends, so the
     gradient this used to carry was invention. */
  auth:
    'text-white border border-transparent shadow-sm bg-[var(--t-auth-action)] ' +
    'hover:bg-[var(--t-auth-action-hover)]',
};

const SIZES = {
  sm: 'h-8 px-3 text-[12.5px]',
  md: 'h-9.5 px-3.5 text-[13.5px]',
  lg: 'h-11 px-5 text-[14.5px]',
  auth: 'h-[clamp(46px,3.7vw,70px)] px-6 text-[clamp(14px,1.06vw,20px)] font-semibold',
};

export const Button = forwardRef(function Button(
  {
    as: Tag = 'button',
    variant = 'secondary',
    size = 'md',
    loading = false,
    icon: Icon,
    iconRight: IconRight,
    /* Lets a caller scale the glyph with the box - an icon-only button needs a
       bigger glyph than the same box carrying a label beside it. */
    iconClassName,
    className,
    children,
    disabled,
    /* A reason string when the signed-in role may not use this control. The
       button stays visible and focusable - so the reader learns the action
       exists and who can take it - but does nothing, shows a lock, and says
       why on hover and to assistive tech. A plain `disabled` button would
       swallow the tooltip in most browsers. */
    locked,
    ...rest
  },
  ref,
) {
  if (locked) {
    const { onClick: _onClick, to: _to, href: _href, type: _type, ...safe } = rest;
    return (
      <button
        {...safe}
        ref={ref}
        type="button"
        aria-disabled="true"
        title={locked}
        onClick={(event) => event.preventDefault()}
        className={cn(
          BASE,
          VARIANTS[variant],
          variant !== 'link' && SIZES[size],
          'cursor-not-allowed opacity-55 hover:brightness-100 active:translate-y-0',
          className,
        )}
      >
        <Lock aria-hidden="true" className={cn('size-4 shrink-0', iconClassName)} />
        {children}
        <span className="sr-only">. {locked}</span>
      </button>
    );
  }
  return (
    <Tag
      ref={ref}
      className={cn(BASE, VARIANTS[variant], variant !== 'link' && SIZES[size], className)}
      disabled={Tag === 'button' ? disabled || loading : undefined}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading ? (
        <Loader2 aria-hidden="true" className={cn('size-4 animate-spin', iconClassName)} />
      ) : (
        Icon && <Icon aria-hidden="true" className={cn('size-4 shrink-0', iconClassName)} />
      )}
      {children}
      {IconRight && !loading && (
        <IconRight aria-hidden="true" className={cn('size-4 shrink-0', iconClassName)} />
      )}
    </Tag>
  );
});

export const IconButton = forwardRef(function IconButton(
  { icon: Icon, label, variant = 'ghost', size = 'md', className, ...rest },
  ref,
) {
  const dimension = size === 'sm' ? 'size-8' : 'size-10';
  const glyph = size === 'sm' ? 'size-4' : 'size-[17px]';
  return (
    <Button
      ref={ref}
      variant={variant}
      iconClassName={glyph}
      /* The size's own side padding would otherwise win over px-0 (both are
         plain utilities, so the emitted order decides) and squeeze the glyph
         to a dot inside the square. */
      className={cn(dimension, '!px-0', className)}
      aria-label={label}
      title={label}
      {...rest}
    >
      {!rest.locked && <Icon aria-hidden="true" className={cn(glyph, 'shrink-0')} />}
    </Button>
  );
});
