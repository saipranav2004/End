import { AlertTriangle, Inbox, RotateCw, ShieldCheck, WifiOff } from 'lucide-react';
import { Button } from './Button';
import { cn } from './cn';

/**
 * Empty states answer three questions: what is missing, why it is missing,
 * and what the operator can do next.
 */
export function EmptyState({
  icon: Icon = Inbox,
  title,
  description,
  action,
  secondaryAction,
  tone = 'neutral',
  compact = false,
  className,
  headingLevel = 3,
}) {
  const Heading = `h${headingLevel}`;
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center text-center',
        compact ? 'gap-2 px-6 py-10' : 'gap-3 px-6 py-16',
        className,
      )}
    >
      <span
        className={cn(
          'grid place-items-center rounded-full border',
          compact ? 'size-10' : 'size-12',
          tone === 'positive'
            ? 'border-low/25 bg-low-soft text-low'
            : 'border-line bg-surface-2 text-ink-3',
        )}
      >
        <Icon aria-hidden="true" className={compact ? 'size-4.5' : 'size-5'} />
      </span>
      <Heading className="text-[14px] font-semibold text-ink">{title}</Heading>
      {description && (
        <p className="max-w-sm text-balance text-[12.5px] leading-relaxed text-ink-3">{description}</p>
      )}
      {(action || secondaryAction) && (
        <div className="mt-2 flex flex-wrap items-center justify-center gap-2">
          {action}
          {secondaryAction}
        </div>
      )}
    </div>
  );
}

/** Positive empty state - nothing found is the good outcome here. */
export function ClearState({ title, description, action, compact }) {
  return (
    <EmptyState
      icon={ShieldCheck}
      tone="positive"
      title={title}
      description={description}
      action={action}
      compact={compact}
    />
  );
}

export function ErrorState({ error, onRetry, title, compact = false, className }) {
  const offline = error?.code === 'NETWORK' || error?.code === 'ECONNABORTED';
  const Icon = offline ? WifiOff : AlertTriangle;

  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center gap-3 text-center',
        compact ? 'px-6 py-10' : 'px-6 py-14',
        className,
      )}
      role="alert"
    >
      <span className="grid size-12 place-items-center rounded-full border border-critical/25 bg-critical-soft text-critical">
        <Icon aria-hidden="true" className="size-5" />
      </span>
      <h3 className="text-[14px] font-semibold text-ink">
        {title || (offline ? 'Cannot reach the service' : 'This view failed to load')}
      </h3>
      <p className="max-w-sm text-balance text-[12.5px] leading-relaxed text-ink-3">
        {error?.message || 'An unexpected error occurred.'}
        {error?.status ? ` (HTTP ${error.status})` : ''}
      </p>
      {onRetry && (
        <Button variant="secondary" size="sm" icon={RotateCw} onClick={onRetry} className="mt-1">
          Try again
        </Button>
      )}
    </div>
  );
}

/** Compact inline variant for panels inside a composed dashboard. */
export function InlineError({ error, onRetry, label }) {
  return (
    <div
      role="alert"
      className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-[var(--radius-control)] border border-critical/25 bg-critical-soft px-3 py-2.5"
    >
      <AlertTriangle aria-hidden="true" className="size-4 shrink-0 text-critical" />
      <p className="min-w-0 flex-1 text-[12.5px] text-ink-2">
        <span className="font-medium text-critical">{label || 'Failed to load'}</span>
        {' - '}
        {error?.message || 'Unexpected error.'}
      </p>
      {onRetry && (
        <Button variant="ghost" size="sm" icon={RotateCw} onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  );
}
