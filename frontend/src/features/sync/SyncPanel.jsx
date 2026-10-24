import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { CheckCircle2, Circle, Loader2, MinusCircle, Play, RefreshCw, XCircle } from 'lucide-react';
import * as engine from '../../lib/sync/engine';
import { formatDateTime, formatNumber } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Drawer } from '../../ui/Overlay';
import { cn } from '../../ui/cn';
import { useSync } from './SyncContext';

/** A clock that ticks while something is on screen that needs it. */
function useNow(active) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return undefined;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [active]);
  return now;
}

const clock = (ms) => {
  const total = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return m > 0 ? `${m} min ${String(s).padStart(2, '0')} s` : `${s} s`;
};

/** The four steps as a person reads them, and what each one found. */
function buildSteps(run) {
  const d = run.discovery;
  const p = run.posture;
  const failed = run.stage === 'failed' ? run.failedStep : null;
  const discoveryDone = d.state === 'completed';
  const postureDone = ['completed', 'partial', 'skipped'].includes(p.state);
  const done = run.stage === 'done';
  const stateOf = (key, finished, active) => (failed === key ? 'failed' : finished ? 'done' : active ? 'active' : 'pending');

  const fields = (rows) => rows.filter((row) => row.value !== null && row.value !== undefined && row.value !== '');
  return [
    {
      key: 'start',
      title: 'Start the sync',
      state: stateOf('start', run.stage !== 'start', run.stage === 'start'),
      text: d.attached ? 'A sync of your workspace was already running, so we are following it.' : 'Asking the scanner to read your AWS accounts.',
      fields: fields([
        { label: 'Scan ID', value: d.scanId, mono: true },
        { label: 'Workspace', value: d.schema, mono: true },
      ]),
    },
    {
      key: 'discovery',
      title: 'Read your AWS accounts',
      state: stateOf('discovery', discoveryDone, run.stage === 'discovery'),
      text: discoveryDone ? 'Finished reading your accounts.' : 'Finding identities, their credentials and what changed. This usually takes a few minutes.',
      fields: discoveryDone
        ? fields([
            { label: 'Identities found', value: d.identities, number: true },
            { label: 'Credentials found', value: d.credentials, number: true },
            { label: 'Changes since last scan', value: d.changes, number: true },
            { label: 'Finished', value: d.scanEnd ? formatDateTime(d.scanEnd) : '' },
          ])
        : fields([{ label: 'Scanner status', value: d.status }]),
    },
    {
      key: 'posture',
      title: 'Score the security posture',
      state: stateOf('posture', postureDone, run.stage === 'posture'),
      text:
        p.state === 'skipped'
          ? p.reason
          : postureDone
            ? p.state === 'partial'
              ? 'Scored, but some checks could not run.'
              : 'Every identity and credential has been checked against the security rules.'
            : 'Checking each identity and credential against the security rules.',
      fields: postureDone && p.state !== 'skipped'
        ? fields([
            { label: 'New findings', value: p.newFindings, number: true },
            { label: 'Resolved', value: p.resolvedFindings, number: true },
            { label: 'Still open', value: p.openFindings, number: true },
          ])
        : fields([{ label: 'Scoring job', value: p.jobId, mono: true }]),
    },
    {
      key: 'refresh',
      title: 'Update your dashboard',
      state: done ? 'done' : 'pending',
      text: done ? 'Dashboard, identities, credentials, access graph, genome and posture now show this sync.' : 'Reloading every page with the new data.',
      fields: [],
    },
  ];
}

const ICON = { done: CheckCircle2, active: Loader2, failed: XCircle, pending: Circle, skipped: MinusCircle };
const ICON_TONE = { done: 'text-low', active: 'text-brand', failed: 'text-critical', pending: 'text-ink-3/60', skipped: 'text-ink-3' };

function Step({ step, last }) {
  const Icon = ICON[step.state];
  return (
    <li className="relative flex gap-3 pb-5 last:pb-0">
      {!last && <span aria-hidden="true" className={cn('absolute top-7 bottom-0 left-[11px] w-0.5 rounded-full', step.state === 'done' ? 'bg-low/50' : 'bg-line')} />}
      <Icon aria-hidden="true" className={cn('relative mt-0.5 size-6 shrink-0 bg-surface', ICON_TONE[step.state], step.state === 'active' && 'motion-safe:animate-spin')} />
      <div className="min-w-0 flex-1">
        <p className={cn('text-[13.5px] font-medium', step.state === 'pending' ? 'text-ink-3' : 'text-ink')}>{step.title}</p>
        {step.state !== 'pending' && <p className="mt-0.5 text-[12.5px] leading-relaxed text-ink-2">{step.text}</p>}
        {step.state !== 'pending' && step.fields.length > 0 && (
          <dl className="mt-2 grid gap-x-6 gap-y-1.5 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2.5 sm:grid-cols-2">
            {step.fields.map((field) => (
              <div key={field.label} className="min-w-0">
                <dt className="text-[11px] text-ink-3">{field.label}</dt>
                <dd className={cn('truncate text-[13px] text-ink', field.mono && 'font-mono text-[12px]')} data-numeric={field.number ? '' : undefined}>
                  {field.number ? formatNumber(field.value) : field.value}
                </dd>
              </div>
            ))}
          </dl>
        )}
      </div>
    </li>
  );
}

/**
 * The sync, step by step. Closing it never stops the run: the top bar keeps
 * showing where it is, and a notice appears when it ends.
 */
export function SyncPanel({ open, onClose }) {
  const sync = useSync();
  const navigate = useNavigate();
  const { run, cooldownUntil, canScan } = sync;
  const ticking = open && (Boolean(run) || cooldownUntil > 0);
  const now = useNow(ticking);
  const waitMs = Math.max(0, cooldownUntil - now);

  const go = (path) => {
    onClose();
    navigate(path);
  };

  const steps = run ? buildSteps(run) : [];
  const running = run && run.stage !== 'done' && run.stage !== 'failed';
  const failed = run?.stage === 'failed';
  const finished = run?.stage === 'done';
  const doneStep = steps.filter((step) => step.state === 'done').length;

  let subtitle = 'Reads your AWS accounts, then scores what it finds. About 5 to 10 minutes.';
  if (running) subtitle = `Step ${Math.min(4, doneStep + 1)} of 4 · running for ${clock(now - run.startedAt)}`;
  if (failed) subtitle = 'The sync did not finish.';
  if (finished) subtitle = `Finished in ${clock((run.finishedAt ?? now) - run.startedAt)}.`;

  const tryAgain = async () => {
    if (run?.postureRetry) {
      await engine.retryPosture();
    } else if (run?.resumable) {
      engine.resume();
    } else {
      engine.dismiss();
      await engine.start();
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="md"
      eyebrow="Sync"
      title="Sync your AWS accounts"
      subtitle={subtitle}
      footer={
        <>
          {running && <p className="mr-auto text-[12px] text-ink-3">You can close this. The sync keeps running.</p>}
          <Button variant="secondary" onClick={onClose}>
            Close
          </Button>
          {failed && canScan && (
            <Button variant="primary" icon={RefreshCw} onClick={tryAgain} disabled={!run.postureRetry && !run.resumable && waitMs > 0} title={!run.postureRetry && !run.resumable && waitMs > 0 ? `A sync can run again in ${clock(waitMs)}` : undefined}>
              {run.postureRetry ? 'Score again' : run.resumable ? 'Check again' : 'Try again'}
            </Button>
          )}
          {finished && (
            <>
              <Button variant="secondary" onClick={() => go('/scans/latest/changes')}>
                See what changed
              </Button>
              <Button variant="primary" onClick={() => go('/overview')}>
                View dashboard
              </Button>
            </>
          )}
          {!run && canScan && (
            <Button variant="primary" icon={Play} onClick={() => sync.start()} disabled={waitMs > 0}>
              Start sync
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-5 px-4 py-4 sm:px-5">
        {!run && (
          <>
            <p className="text-[13px] leading-relaxed text-ink-2">
              One sync reads every connected AWS account, then checks what it found against the security rules. When it finishes, every page shows the new results.
            </p>
            {waitMs > 0 && (
              <p className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-3.5 py-3 text-[12.5px] text-ink-2" role="status">
                A sync finished recently. To keep your AWS accounts from being read too often, the next sync can start in <b className="font-semibold text-ink">{clock(waitMs)}</b>.
              </p>
            )}
            {!canScan && <p className="text-[12.5px] text-ink-3">Your role can see the results but not start a sync.</p>}
            <ol className="flex flex-col">
              {buildSteps({ stage: 'start', discovery: {}, posture: {}, startedAt: now }).map((step, index, list) => (
                <Step key={step.key} step={{ ...step, state: 'pending' }} last={index === list.length - 1} />
              ))}
            </ol>
          </>
        )}

        {run && (
          <>
            {failed && (
              <p className="rounded-[var(--radius-control)] border border-critical/30 bg-critical-soft px-3.5 py-3 text-[12.5px] leading-relaxed text-ink" role="alert">
                {run.error}
              </p>
            )}
            <ol className="flex flex-col">
              {steps.map((step, index) => (
                <Step key={step.key} step={step} last={index === steps.length - 1} />
              ))}
            </ol>
            {finished && run.posture?.state === 'partial' && (
              <p className="text-[12.5px] text-ink-3">Some posture checks could not run, so a few scores may be missing until the next sync.</p>
            )}
          </>
        )}
      </div>
    </Drawer>
  );
}
