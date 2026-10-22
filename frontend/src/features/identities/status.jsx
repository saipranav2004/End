import { formatDateTime, formatNumber, formatRelative, formatRelativeShort } from '../../lib/format';
import { cn, TONE_BG, TONE_FG } from '../../ui/cn';
import { signalTone } from './meta';

/**
 * The API's own risk signals for one identity (or credential), in the
 * design's check-list form. A signal whose `status` is true is raised.
 * Raised ones lead, by severity.
 */
const SEVERITY_RANK = { critical: 0, high: 1, medium: 2, low: 3 };

/* The API names its signals in its own shorthand; the ones it sends today, in plain words. */
const SIGNAL_WORDS = {
  'Stale (no activity > 90d)': 'Unused for 90+ days',
  'Wildcard resource scope': 'Allowed on every resource (*)',
  'No owner attribution': 'No owner recorded',
  'Credential expired/expiring': 'Credential expired or expiring soon',
  'Credential never rotated': 'Credential key never changed (rotated)',
  'Cross-account trust': 'Another AWS account may use it',
  'Not in a VPC': 'Not in a private network (VPC)',
};

export function SignalBreakdown({ signals }) {
  const rows = [...(signals ?? [])].sort(
    (a, b) =>
      Number(b.status) - Number(a.status) ||
      (SEVERITY_RANK[String(a.severity).toLowerCase()] ?? 9) - (SEVERITY_RANK[String(b.severity).toLowerCase()] ?? 9),
  );
  if (rows.length === 0) {
    return <p className="py-2 text-[12.5px] text-ink-3">No risk checks were recorded for this identity.</p>;
  }
  return (
    <ul className="divide-y divide-line">
      {rows.map((row) => {
        const tone = row.status ? signalTone(row.severity) : 'low';
        return (
          <li key={row.signal} className="flex items-center gap-3 py-2">
            <span aria-hidden="true" className={cn('size-2 shrink-0 rounded-full', row.status ? TONE_BG[tone] : 'bg-line-strong')} />
            <span className={cn('min-w-0 flex-1 truncate text-[12.5px]', row.status ? 'text-ink' : 'text-ink-3')} title={row.signal}>
              {SIGNAL_WORDS[row.signal] ?? row.signal}
            </span>
            <span className={cn('shrink-0 text-[11.5px] font-semibold', row.status ? TONE_FG[tone] : 'text-low')}>
              {row.status ? `Found · ${String(row.severity || '').toLowerCase() || 'unrated'}` : 'Clear'}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

/** Last recorded activity, with idle days beneath. */
export function ActivityCell({ identity, activityAvailable = true }) {
  if (!activityAvailable) {
    return <span className="text-[12px] text-ink-3" title="The AWS activity log (CloudTrail) could not be read">Unknown</span>;
  }
  const idle = identity.idle_days;
  const tone = idle === null || idle === undefined ? 'text-ink-3' : idle > 90 ? 'text-high' : idle > 30 ? 'text-medium' : 'text-ink-3';
  return (
    <span
      className="block min-w-0"
      title={identity.last_activity_at ? `Last activity ${formatDateTime(identity.last_activity_at)}` : 'No call by this identity or its credentials in the AWS activity log (CloudTrail)'}
    >
      <span className="block text-[12.5px] whitespace-nowrap text-ink-2">
        {identity.last_activity_at ? formatRelativeShort(identity.last_activity_at) : 'Never used'}
      </span>
      <span className={cn('block text-[11px] whitespace-nowrap', tone)}>
        {idle === null || idle === undefined ? 'No calls recorded' : idle === 0 ? 'Active today' : `Idle ${formatNumber(idle)} ${idle === 1 ? 'day' : 'days'}`}
      </span>
    </span>
  );
}

/** Share of granted actions actually used (90 days), with a bar. */
export function UtilisationCell({ identity, activityAvailable = true }) {
  const pct = identity.permission_utilization_pct;
  const known = activityAvailable && pct !== null && pct !== undefined;
  const credentials = Number(identity.bound_credentials_count) || 0;
  return (
    <span
      className="block min-w-0"
      title={known ? `${pct}% of its allowed actions were used in the last 90 days` : 'Needs the AWS activity log (CloudTrail), which has no calls for it'}
    >
      <span className="flex items-baseline justify-between gap-2">
        <span data-numeric="" className="text-[12.5px] whitespace-nowrap text-ink-2">
          {known ? `${pct}% used` : 'Not known'}
        </span>
        <span data-numeric="" className="truncate text-[11px] text-ink-3">
          {formatNumber(credentials)} credential{credentials === 1 ? '' : 's'}
        </span>
      </span>
      <span className="mt-1.5 block h-[3px] w-full overflow-hidden rounded-full bg-track">
        {known && (
          <span
            className="block h-full rounded-full bg-brand transition-[width] duration-700 ease-[var(--ease-out-quint)]"
            style={{ width: `${Math.max(2, Math.min(100, pct))}%` }}
          />
        )}
      </span>
    </span>
  );
}

export function lastActivityText(at) {
  return at ? `${formatRelative(at)} (${formatDateTime(at)})` : 'No activity recorded';
}
