import { formatDateTime, formatNumber, formatRelative, formatRelativeShort } from '../../lib/format';
import { cn, TONE_BG, TONE_CLASSES, TONE_FG } from '../../ui/cn';
import { signalTone } from './meta';

/**
 * Identity signals, as a list row can state them.
 *
 * A row of `GET /api/v1/identities` carries enough to say three things in
 * plain words: whether the identity is reachable from the internet, whether it
 * has been used lately, and whether anyone owns it. The table shows one state
 * and the most severe reason; the full evaluation, with the API's own risk
 * signals, is on the record.
 *
 * The thresholds are the API's: "stale" is no activity for more than 90 days,
 * the same line the dashboard's stale count draws. Thirty days is shown as a
 * softer "idle" so the column separates a quiet identity from an abandoned
 * one. There is no score - the inventory API supplies none, and inventing a
 * weighting would be fabricated analytics people then act on.
 */
const CHECK_ORDER = ['exposure', 'activity', 'ownership'];

export function evaluateSignals(identity, activityAvailable = true) {
  const idle = identity.idle_days;
  const checks = {
    exposure: identity.is_publicly_accessible
      ? { state: 'fail', label: 'Publicly accessible' }
      : { state: 'pass', label: 'Not publicly accessible' },

    activity: !activityAvailable
      ? { state: 'na', label: 'Activity unknown - CloudTrail unavailable' }
      : idle === null || idle === undefined
        ? { state: 'warn', label: 'No activity recorded' }
        : idle > 90
          ? { state: 'fail', label: `Stale ${formatNumber(idle)} days` }
          : idle > 30
            ? { state: 'warn', label: `Idle ${formatNumber(idle)} ${idle === 1 ? 'day' : 'days'}` }
            : { state: 'pass', label: 'Recently active' },

    ownership: identity.owner
      ? { state: 'pass', label: 'Owner recorded' }
      : { state: 'warn', label: 'No owner recorded' },
  };

  const failing = CHECK_ORDER.filter((key) => checks[key].state === 'fail');
  const warning = CHECK_ORDER.filter((key) => checks[key].state === 'warn');
  const flagged = [...failing, ...warning];

  return {
    checks,
    state: failing.length > 0 ? 'critical' : warning.length > 0 ? 'attention' : 'healthy',
    leadReason: flagged.length > 0 ? checks[flagged[0]].label : 'Private, in use, owned',
    extra: Math.max(0, flagged.length - 1),
    detail: CHECK_ORDER.map((key) => checks[key].label).join(' · '),
  };
}

/**
 * The Risks column: only what is wrong, as short pills a reader can scan down
 * the table - "Public", "Stale 120d", "No owner" - worst first. Nothing
 * raised reads "No issues". The full sentence for each is in the tooltip and
 * on the record. Not a posture verdict - that is the Posture column.
 */
const PILL = {
  exposure: () => ({ word: 'Public', tone: 'critical' }),
  activity: (identity) => {
    const idle = identity.idle_days;
    if (idle === null || idle === undefined) return { word: 'No activity', tone: 'medium' };
    return idle > 90 ? { word: `Stale ${formatNumber(idle)}d`, tone: 'high' } : { word: `Idle ${formatNumber(idle)}d`, tone: 'medium' };
  },
  ownership: () => ({ word: 'No owner', tone: 'medium' }),
};

export function RiskCell({ identity, activityAvailable }) {
  const result = evaluateSignals(identity, activityAvailable);
  const raised = CHECK_ORDER.filter((key) => result.checks[key].state === 'fail').concat(
    CHECK_ORDER.filter((key) => result.checks[key].state === 'warn'),
  );
  if (raised.length === 0) {
    return (
      <span className="inline-flex items-center gap-1.5 text-[12px] text-ink-3" title={result.detail}>
        <span aria-hidden="true" className={cn('size-1.5 shrink-0 rounded-full', TONE_BG.low)} />
        No issues
      </span>
    );
  }
  const shown = raised.slice(0, 2);
  const label = raised.map((key) => result.checks[key].label).join(' · ');
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-1" title={label} aria-label={label}>
      {shown.map((key) => {
        const pill = PILL[key](identity);
        return (
          <span
            key={key}
            className={cn('inline-flex shrink-0 items-center rounded-full border px-1.5 py-px text-[11px] font-medium whitespace-nowrap', TONE_CLASSES[pill.tone])}
            data-numeric=""
          >
            {pill.word}
          </span>
        );
      })}
      {raised.length > shown.length && (
        <span data-numeric="" className="shrink-0 text-[11px] font-semibold text-ink-3">
          +{raised.length - shown.length}
        </span>
      )}
    </span>
  );
}

/**
 * The API's own risk signals for one identity (or credential), in the
 * design's check-list form. A signal whose `status` is true is raised.
 * Raised ones lead, by severity.
 */
const SEVERITY_RANK = { critical: 0, high: 1, medium: 2, low: 3 };

export function SignalBreakdown({ signals }) {
  const rows = [...(signals ?? [])].sort(
    (a, b) =>
      Number(b.status) - Number(a.status) ||
      (SEVERITY_RANK[String(a.severity).toLowerCase()] ?? 9) - (SEVERITY_RANK[String(b.severity).toLowerCase()] ?? 9),
  );
  if (rows.length === 0) {
    return <p className="py-2 text-[12.5px] text-ink-3">The API returned no risk signals for this record.</p>;
  }
  return (
    <ul className="divide-y divide-line">
      {rows.map((row) => {
        const tone = row.status ? signalTone(row.severity) : 'low';
        return (
          <li key={row.signal} className="flex items-center gap-3 py-2">
            <span aria-hidden="true" className={cn('size-2 shrink-0 rounded-full', row.status ? TONE_BG[tone] : 'bg-line-strong')} />
            <span className={cn('min-w-0 flex-1 truncate text-[12.5px]', row.status ? 'text-ink' : 'text-ink-3')}>{row.signal}</span>
            <span className={cn('shrink-0 text-[11.5px] font-semibold', row.status ? TONE_FG[tone] : 'text-low')}>
              {row.status ? `Raised · ${String(row.severity || '').toLowerCase() || 'unrated'}` : 'Clear'}
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
    return <span className="text-[12px] text-ink-3" title="CloudTrail data could not be read">Unknown</span>;
  }
  const idle = identity.idle_days;
  const tone = idle === null || idle === undefined ? 'text-ink-3' : idle > 90 ? 'text-high' : idle > 30 ? 'text-medium' : 'text-ink-3';
  return (
    <span
      className="block min-w-0"
      title={identity.last_activity_at ? `Last activity ${formatDateTime(identity.last_activity_at)}` : 'No CloudTrail event recorded for this identity or its credentials'}
    >
      <span className="block text-[12.5px] whitespace-nowrap text-ink-2">
        {identity.last_activity_at ? formatRelativeShort(identity.last_activity_at) : 'Never seen'}
      </span>
      <span className={cn('block text-[11px] whitespace-nowrap', tone)}>
        {idle === null || idle === undefined ? 'No activity recorded' : idle === 0 ? 'Active today' : `Idle ${formatNumber(idle)} ${idle === 1 ? 'day' : 'days'}`}
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
      title={known ? `${pct}% of granted actions were used in the last 90 days` : 'Utilisation needs CloudTrail data'}
    >
      <span className="flex items-baseline justify-between gap-2">
        <span data-numeric="" className="text-[12.5px] whitespace-nowrap text-ink-2">
          {known ? `${pct}% used` : '-'}
        </span>
        <span data-numeric="" className="truncate text-[11px] text-ink-3">
          {formatNumber(credentials)} cred{credentials === 1 ? '' : 's'}
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
