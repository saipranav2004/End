import { useEffect, useState } from 'react';
import { AlertTriangle, Loader2, RefreshCw } from 'lucide-react';
import { formatDateTime, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { cn } from '../../ui/cn';
import { useSync } from './SyncContext';

const STEP_OF = { start: 1, discovery: 2, posture: 3, refresh: 4 };

/**
 * The scan control in the top bar, where it is on every screen:
 *
 *   idle       "Sync now" with when the data was last synced beside it
 *   running    "Syncing · step 2 of 4": opens the progress panel
 *   cooling    the next sync's countdown (the scanner allows one every 10 minutes)
 *   failed     "Sync didn't finish": opens the panel
 *
 * A role that may not start scans still sees when the data is from.
 */
export function SyncButton() {
  const { run, cooldownUntil, lastScanAt, canScan, openPanel, start } = useSync();
  const [now, setNow] = useState(() => Date.now());
  const cooling = cooldownUntil > now;
  useEffect(() => {
    if (!cooling) return undefined;
    const id = setInterval(() => setNow(Date.now()), 15000);
    return () => clearInterval(id);
  }, [cooling]);

  const running = run && run.stage !== 'done' && run.stage !== 'failed';
  const failed = run?.stage === 'failed';
  const base = 'hidden h-9 shrink-0 items-center gap-2 rounded-[var(--radius-control)] border px-3 text-[13px] transition-colors sm:flex';

  if (running) {
    return (
      <button type="button" onClick={openPanel} className={cn(base, 'border-brand/40 bg-info-soft text-brand hover:border-brand')} aria-label="Sync in progress. Show progress.">
        <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin" />
        <span>Syncing · step {STEP_OF[run.stage] ?? 1} of 4</span>
      </button>
    );
  }
  if (failed) {
    return (
      <button type="button" onClick={openPanel} className={cn(base, 'border-critical/40 bg-critical-soft text-critical hover:border-critical')}>
        <AlertTriangle aria-hidden="true" className="size-4" />
        <span>Sync didn&apos;t finish</span>
      </button>
    );
  }

  const last = lastScanAt ? (
    <span className="hidden text-[12px] text-topbar-muted xl:inline" title={formatDateTime(lastScanAt)}>
      Synced {formatRelative(lastScanAt)}
    </span>
  ) : null;

  if (!canScan) return <div className="hidden sm:flex">{last}</div>;
  const minutes = Math.ceil((cooldownUntil - now) / 60000);
  return (
    <>
      {last}
      <button
        type="button"
        onClick={cooling ? openPanel : start}
        className={cn(base, 'border-topbar-line bg-topbar-field text-topbar-ink hover:border-ink-3/45')}
        title={cooling ? `A sync can run every 10 minutes. The next one can start in ${minutes} min.` : 'Read your AWS accounts, score what is found and refresh every page'}
      >
        <RefreshCw aria-hidden="true" className="size-4" />
        <span>{cooling ? `Next sync in ${minutes} min` : 'Sync now'}</span>
      </button>
    </>
  );
}


/**
 * The same control for a page body (Integrations): one workspace-wide Sync now,
 * with the run's state in its label. Hidden for roles that cannot start one.
 */
export function SyncNowButton() {
  const { run, cooldownUntil, canScan, openPanel, start } = useSync();
  const [now, setNow] = useState(() => Date.now());
  const cooling = cooldownUntil > now;
  useEffect(() => {
    if (!cooling) return undefined;
    const id = setInterval(() => setNow(Date.now()), 15000);
    return () => clearInterval(id);
  }, [cooling]);

  if (!canScan) return null;
  const running = run && run.stage !== 'done' && run.stage !== 'failed';
  if (running) {
    return (
      <Button variant="secondary" icon={RefreshCw} loading onClick={openPanel} aria-label="Sync in progress. Show progress.">
        Syncing · step {STEP_OF[run.stage] ?? 1} of 4
      </Button>
    );
  }
  if (run?.stage === 'failed') {
    return (
      <Button variant="secondary" icon={AlertTriangle} onClick={openPanel}>
        Sync didn&apos;t finish
      </Button>
    );
  }
  const minutes = Math.ceil((cooldownUntil - now) / 60000);
  return (
    <Button
      variant="secondary"
      icon={RefreshCw}
      onClick={cooling ? openPanel : start}
      title={cooling ? `A sync can run every 10 minutes. The next one can start in ${minutes} min.` : 'Read every connected AWS account, score what is found and refresh every page'}
    >
      {cooling ? `Next sync in ${minutes} min` : 'Sync now'}
    </Button>
  );
}
