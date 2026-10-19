import { useEffect, useState } from 'react';
import { AlertTriangle, ArrowRight, CheckCircle2, CircleDashed, Loader2, XCircle } from 'lucide-react';
import { formatRelative } from '../../lib/format';
import { cn } from '../../ui/cn';
import { AWS_CHECKS } from './awsSetup';
import { CopyChip } from './parts';

const STATE = {
  pass: { icon: CheckCircle2, color: 'text-low', dot: 'bg-low', word: 'Passed' },
  fail: { icon: XCircle, color: 'text-critical', dot: 'bg-critical', word: 'Failed' },
  error: { icon: AlertTriangle, color: 'text-medium', dot: 'bg-medium', word: 'Not run' },
  skipped: { icon: CircleDashed, color: 'text-ink-3', dot: 'bg-surface-3', word: 'Waiting' },
  running: { icon: Loader2, color: 'text-brand', dot: 'bg-brand', word: 'Checking' },
};

const stateOf = (check) => (check && Object.hasOwn(STATE, check.state) ? check.state : 'skipped');

/** One dot per check, for a dense row. Hovering names each result. */
export function CheckDots({ checks, definitions = AWS_CHECKS }) {
  const byKey = new Map((checks ?? []).map((check) => [check.key, check]));
  return (
    <span
      className="inline-flex items-center gap-1"
      role="img"
      aria-label={definitions.map((meta) => `${meta.label}: ${STATE[stateOf(byKey.get(meta.key))].word}`).join(', ')}
    >
      {definitions.map((meta) => {
        const state = stateOf(byKey.get(meta.key));
        return <span key={meta.key} title={`${meta.label}: ${STATE[state].word}`} className={cn('h-1.5 w-4 rounded-full', STATE[state].dot)} />;
      })}
    </span>
  );
}

/** Compact list: label and state, plus the API's fix when a check failed. */
export function CheckList({ checks, definitions = AWS_CHECKS }) {
  const byKey = new Map((checks ?? []).map((check) => [check.key, check]));
  return (
    <ul className="flex flex-col">
      {definitions.map((meta) => {
        const check = byKey.get(meta.key);
        const state = stateOf(check);
        const { icon: Icon, color, word } = STATE[state];
        return (
          <li key={meta.key} className="flex items-start gap-2.5 border-b border-line py-1.5 last:border-b-0">
            <Icon aria-hidden="true" className={cn('mt-0.5 size-4 shrink-0', color)} />
            <div className="min-w-0 flex-1">
              <p className="text-[12.5px] text-ink">{check?.label || meta.label}</p>
              {check?.message && state !== 'pass' && <p className="mt-0.5 text-[12px] text-ink-3">{check.message}</p>}
              {state === 'fail' && check?.fix && (
                <p className="mt-0.5 text-[12px] text-ink-2">
                  <span className="font-semibold text-critical">Fix:</span> {check.fix}
                </p>
              )}
            </div>
            <span className={cn('shrink-0 text-[12px] font-medium', color)}>{word}</span>
          </li>
        );
      })}
    </ul>
  );
}

function Elapsed() {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const started = Date.now();
    const id = setInterval(() => setSeconds(Math.floor((Date.now() - started) / 1000)), 250);
    return () => clearInterval(id);
  }, []);
  return <span data-numeric="">{seconds}s</span>;
}

/**
 * The outcome of a check run, as a card.
 *
 * While the request runs, every check shows what it is about to prove and an
 * elapsed timer counts - the API answers for all of them at once, so nothing
 * pretends to know which one it is on. A failed check opens with the exact
 * place to fix it (`renderFix`), the values filled in.
 *
 *   definitions   [{ key, label, proves, step? }] in the order the API runs them
 *   ok            whether the run as a whole succeeded
 *   copy          { running, ok, okDetail } headline strings
 */
export function CheckResult({ running, checks, verifiedAt, ok, definitions, copy, renderFix, onShowStep }) {
  if (!running && !checks) return null;
  const byKey = new Map((checks ?? []).map((check) => [check.key, check]));
  const passed = (checks ?? []).filter((check) => check.state === 'pass').length;
  const success = !running && ok;
  const tone = running ? 'running' : success ? 'success' : 'attention';
  const failing = definitions.length - passed;

  return (
    <div
      aria-live="polite"
      aria-busy={running || undefined}
      className={cn(
        'animate-rise overflow-hidden rounded-[var(--radius-panel)] border',
        tone === 'success' ? 'border-low/40' : tone === 'attention' ? 'border-medium/40' : 'border-line',
      )}
    >
      <div
        className={cn(
          'flex items-center gap-3 px-4 py-3',
          tone === 'success' ? 'bg-low-soft' : tone === 'attention' ? 'bg-medium-soft' : 'bg-surface-2',
        )}
      >
        {running ? (
          <Loader2 aria-hidden="true" className="size-5 shrink-0 animate-spin text-brand" />
        ) : success ? (
          <CheckCircle2 aria-hidden="true" className="animate-pop size-5 shrink-0 text-low" />
        ) : (
          <AlertTriangle aria-hidden="true" className="size-5 shrink-0 text-medium" />
        )}
        <div className="min-w-0 flex-1">
          <p className="text-[13.5px] font-semibold text-ink">
            {running ? copy.running : success ? copy.ok : `${passed} of ${definitions.length} checks passed`}
          </p>
          <p className="text-[12px] text-ink-2">
            {running ? (
              <>
                Usually under 10 seconds · <Elapsed />
              </>
            ) : success ? (
              copy.okDetail
            ) : (
              <>
                Fix the {failing === 1 ? 'item' : 'items'} below, then check again
                {verifiedAt ? ` · checked ${formatRelative(verifiedAt)}` : ''}.
              </>
            )}
          </p>
        </div>
      </div>

      <ol className="divide-y divide-line bg-surface">
        {definitions.map((meta) => {
          const check = byKey.get(meta.key);
          const state = running ? 'running' : stateOf(check);
          const { icon: Icon, color, word } = STATE[state];
          return (
            <li key={`${meta.key}-${running ? 'r' : 'd'}`} className="animate-fade px-4 py-3">
              <div className="flex items-start gap-3">
                <Icon aria-hidden="true" className={cn('mt-0.5 size-[18px] shrink-0', color, state === 'running' && 'animate-spin')} />
                <div className="min-w-0 flex-1">
                  <p className="text-[13px] font-medium text-ink">{check?.label || meta.label}</p>
                  <p className="mt-0.5 text-[12px] text-ink-3">{running || !check?.message ? meta.proves : check.message}</p>
                </div>
                <span className={cn('shrink-0 text-[12px] font-semibold', color)}>{word}</span>
              </div>
              {state === 'fail' && (check?.fix || renderFix) && (
                <div className="mt-2.5 ml-[30px] rounded-[var(--radius-control)] border border-critical/20 bg-critical-soft/50 px-3 py-2.5">
                  {check?.fix && <p className="text-[12.5px] font-semibold text-ink">{check.fix}</p>}
                  {renderFix && <div className="mt-1.5 text-[12.5px] leading-relaxed text-ink-2">{renderFix(meta.key, check)}</div>}
                  {onShowStep && meta.step && (
                    <button
                      type="button"
                      onClick={() => onShowStep(meta.step)}
                      className="mt-2 inline-flex items-center gap-1 text-[12.5px] font-medium text-brand hover:underline"
                    >
                      Open step {meta.step}
                      <ArrowRight aria-hidden="true" className="size-3.5" />
                    </button>
                  )}
                </div>
              )}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

/** The AWS connection checks' result, with the console steps that fix each failure. */
export function VerifyResult({ running, result, setup, onShowStep }) {
  return (
    <CheckResult
      running={running}
      checks={result?.checks}
      verifiedAt={result?.verifiedAt}
      ok={result?.status === 'connected'}
      definitions={AWS_CHECKS}
      copy={{
        running: 'Checking your account…',
        ok: 'Connection verified',
        okDetail: 'Read access works and your events are arriving.',
      }}
      renderFix={setup ? (key) => connectionFix(key, setup) : undefined}
      onShowStep={onShowStep}
    />
  );
}

function connectionFix(key, setup) {
  switch (key) {
    case 'assume_role':
      return (
        <>
          In IAM, open <b className="font-semibold text-ink">{setup.roleName}</b> → Trust relationships. It must trust{' '}
          <CopyChip value={setup.accountId} label="our account ID" /> and require External ID{' '}
          <CopyChip value={setup.externalId} label="External ID" />. Also check you pasted the role&rsquo;s ARN.
        </>
      );
    case 'read_access':
      return (
        <>
          Open the role → Permissions → Add permissions, and attach{' '}
          {setup.managedPolicies.map((policy, index) => (
            <span key={policy}>
              {index > 0 && ' and '}
              <CopyChip value={policy} />
            </span>
          ))}
          .
        </>
      );
    case 'events':
      return (
        <>
          In EventBridge, open rule <CopyChip value={setup.ruleName} label="rule name" /> on the default bus. Its target must be{' '}
          <CopyChip value={setup.eventBusArn} label="event bus ARN" />. Just created it? Make a small change in that region (for
          example, tag an EC2 instance), wait a minute, then verify again.
        </>
      );
    default:
      return null;
  }
}
