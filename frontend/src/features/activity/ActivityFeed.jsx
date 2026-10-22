import { AlertTriangle, Eye, PenLine } from 'lucide-react';
import { formatDateTime, formatRelative } from '../../lib/format';
import { ListSkeleton } from '../../ui/Skeleton';
import { EmptyState, InlineError } from '../../ui/States';
import { cn } from '../../ui/cn';

/**
 * CloudTrail events as a timeline (`section=timeline` rows). `is_read_only`
 * separates a look from a change, so it drives the icon and the accent; a
 * failed call is marked as such - everything else is supporting detail.
 */
export function ActivityFeed({ events, loading, error, onRetry, limit, emptyHint, onSelect }) {
  if (loading) return <ListSkeleton rows={limit || 5} />;
  if (error) return <InlineError error={error} onRetry={onRetry} label="Activity unavailable" />;

  if (!events || events.length === 0) {
    return (
      <EmptyState
        compact
        icon={Eye}
        title="No calls in this period"
        description={emptyHint || 'Nothing was recorded for this identity in the selected time range.'}
      />
    );
  }

  const rows = limit ? events.slice(0, limit) : events;

  return (
    <ol className="flex flex-col">
      {rows.map((event, index) => {
        const failed = event.status === 'error' || Boolean(event.error_code);
        const mutating = !event.is_read_only;
        const Icon = failed ? AlertTriangle : mutating ? PenLine : Eye;
        const Wrapper = onSelect ? 'button' : 'div';

        return (
          <li key={event.event_id || index} className="relative flex gap-3 pb-4 last:pb-0">
            <span className="relative flex shrink-0 flex-col items-center">
              <span
                className={cn(
                  'grid size-7 place-items-center rounded-full border',
                  failed
                    ? 'border-critical/30 bg-critical-soft text-critical'
                    : mutating
                      ? 'border-high/30 bg-high-soft text-high'
                      : 'border-line bg-surface-2 text-ink-3',
                )}
              >
                <Icon aria-hidden="true" className="size-3.5" />
              </span>
              {index < rows.length - 1 && <span aria-hidden="true" className="mt-1 w-px flex-1 bg-line" />}
            </span>

            <Wrapper
              {...(onSelect ? { type: 'button', onClick: () => onSelect(event) } : {})}
              className={cn(
                'min-w-0 flex-1 text-left',
                onSelect && '-m-1.5 rounded-md p-1.5 transition-colors hover:bg-surface-2 focus-visible:bg-surface-2',
              )}
            >
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
                <p className="min-w-0 truncate text-[13px] font-semibold text-ink" title={event.event_name}>
                  {event.event_name || 'Unnamed call'}
                  <span className="ml-2 font-mono text-[11.5px] font-normal text-ink-3">{event.event_source}</span>
                </p>
                <time
                  className="shrink-0 text-[11.5px] text-ink-3"
                  dateTime={event.event_time || undefined}
                  title={formatDateTime(event.event_time)}
                >
                  {formatRelative(event.event_time)}
                </time>
              </div>

              <p className="mt-0.5 truncate text-[12px] text-ink-2" title={event.resource_arn || undefined}>
                {event.resource_name || event.resource_arn || 'No resource recorded'}
                {event.credential_name && <span className="text-ink-3"> · as {event.credential_name}</span>}
              </p>

              <p className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11.5px] text-ink-3">
                {event.source_ip && <span className="font-mono">{event.source_ip}</span>}
                {event.aws_region && <span>{event.aws_region}</span>}
                <span className={mutating ? 'font-medium text-high' : ''}>{mutating ? 'Made changes' : 'Read-only'}</span>
                {failed && <span className="font-medium text-critical">{event.error_code || 'Failed'}</span>}
                {event.is_cross_account && <span className="font-medium text-medium">Another AWS account</span>}
              </p>
            </Wrapper>
          </li>
        );
      })}
    </ol>
  );
}
