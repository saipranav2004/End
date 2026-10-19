import { useId, useRef, useState } from 'react';
import {
  Check,
  CheckCircle2,
  ChevronDown,
  Download,
  ExternalLink,
  EyeOff,
  FileText,
  Info,
  Link2,
  ShieldCheck,
  Users,
} from 'lucide-react';
import { downloadText } from '../../lib/csv';
import { verifyAwsConnection } from '../../lib/api/integrations';
import { Button } from '../../ui/Button';
import { CopyButton } from '../../ui/Copyable';
import { Field, Input } from '../../ui/Field';
import { InlineError } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';
import { cn } from '../../ui/cn';
import {
  AWS_CONSOLE,
  SETUP_STEPS,
  accountOfRoleArn,
  eventPatternJson,
  exampleRoleArn,
  roleArnProblem,
  setupGuideMarkdown,
  trustPolicyJson,
} from './awsSetup';
import { VerifyResult } from './CheckList';
import { RegionPicker } from './RegionPicker';
import { CodeBlock, CopyChip, Disclosure, Instructions, copyText } from './parts';

const reducedMotion = () => typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;

/* Which steps the admin has marked done, per workspace. A convenience only:
   the API's checks are what decide whether anything is connected. */
function readDone(key, fallback) {
  try {
    const raw = JSON.parse(localStorage.getItem(key) ?? 'null');
    if (Array.isArray(raw)) return new Set(raw.filter((n) => n === 1 || n === 2));
  } catch {
    /* unreadable storage: start from the fallback */
  }
  return new Set(fallback);
}

function writeDone(key, done) {
  try {
    localStorage.setItem(key, JSON.stringify([...done]));
  } catch {
    /* private window: progress lasts for this page */
  }
}

/**
 * "Connect AWS Account" as a guided timeline: two steps done in the AWS
 * console, then Verify - beside a panel holding everything to copy, what is
 * being granted, and a way to hand the whole setup to whoever owns the AWS
 * account.
 *
 * Every value in the instructions is this workspace's own, placed exactly
 * where the console asks for it and copied in one click. A failed check names
 * its fix and opens the step that makes it.
 *
 * `previous` is the account last checked and not connected: its ARN and
 * checks are restored so a reload does not lose where the admin got to.
 */
export function AwsConnectWizard({ setup, previous, storageKey, title = 'Connect an AWS account', onVerified, onCancel }) {
  const [done, setDone] = useState(() => readDone(storageKey, previous ? [1, 2] : []));
  const [openStep, setOpenStep] = useState(() => [1, 2].find((n) => !done.has(n)) ?? null);
  const [roleArn, setRoleArn] = useState(previous?.roleArn ?? '');
  const [regions, setRegions] = useState(previous?.regions ?? []);
  const [regionsError, setRegionsError] = useState('');
  const regionsRef = useRef(null);
  const [touched, setTouched] = useState(false);
  const [serverProblem, setServerProblem] = useState('');
  const [requestError, setRequestError] = useState(null);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState(
    previous ? { status: previous.status, checks: previous.checks, verifiedAt: previous.lastVerifiedAt } : null,
  );
  const stepRefs = useRef({});
  const inputRef = useRef(null);

  const scrollToStep = (number) =>
    stepRefs.current[number]?.scrollIntoView({ behavior: reducedMotion() ? 'auto' : 'smooth', block: 'start' });

  const showStep = (number) => {
    setOpenStep(number === 3 ? null : number);
    scrollToStep(number);
  };

  const setStepDone = (number, value) => {
    const next = new Set(done);
    if (value) next.add(number);
    else next.delete(number);
    setDone(next);
    writeDone(storageKey, next);
    if (value) {
      const following = [1, 2].find((n) => n > number && !next.has(n));
      setOpenStep(following ?? null);
      scrollToStep(following ?? 3);
      if (!following) inputRef.current?.focus({ preventScroll: true });
    }
  };

  const localProblem = roleArnProblem(roleArn, setup.accountId);
  const fieldError = serverProblem || (touched ? localProblem : '');
  const detectedAccount = !localProblem ? accountOfRoleArn(roleArn) : '';
  const connected = result?.status === 'connected';
  const completed = done.size + (connected ? 1 : 0);
  const minutesLeft = SETUP_STEPS.filter((step) => (step.number === 3 ? !connected : !done.has(step.number))).reduce(
    (sum, step) => sum + step.minutes,
    0,
  );

  const verify = async (event) => {
    event.preventDefault();
    setTouched(true);
    if (localProblem) {
      inputRef.current?.focus();
      return;
    }
    if (regions.length === 0) {
      setRegionsError('Choose at least one region to read from.');
      regionsRef.current?.scrollIntoView({ behavior: reducedMotion() ? 'auto' : 'smooth', block: 'center' });
      return;
    }
    setRunning(true);
    setServerProblem('');
    setRegionsError('');
    setRequestError(null);
    try {
      const outcome = await verifyAwsConnection({ roleArn: roleArn.trim(), regions });
      setResult(outcome);
      onVerified?.(outcome);
    } catch (error) {
      /* A 400 is about the input itself, so it belongs under that field. */
      if (error?.status === 400 && error?.code === 'INVALID_REGIONS') setRegionsError(error.message);
      else if (error?.status === 400) setServerProblem(error.message);
      else setRequestError(error);
    } finally {
      setRunning(false);
    }
  };

  const firstOpen = [1, 2].find((n) => !done.has(n));
  const stateOf = (number) => {
    if (number === 3) return connected ? 'done' : firstOpen ? 'todo' : 'current';
    if (done.has(number)) return 'done';
    return firstOpen === number ? 'current' : 'todo';
  };

  return (
    <div className="grid items-start gap-5 @min-[56rem]:grid-cols-[minmax(0,1fr)_288px] @min-[80rem]:grid-cols-[minmax(0,1fr)_336px]">
      <section aria-labelledby="aws-setup-title" className="min-w-0">
        <div className="mb-4 flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
          <div>
            <h2 id="aws-setup-title" className="text-[17px] font-bold tracking-[-0.012em] text-ink">
              {title}
            </h2>
            <p className="mt-0.5 text-[12.5px] text-ink-3">Two read-only changes in your AWS console, then one check from here.</p>
          </div>
          <div className="flex min-w-[220px] flex-col gap-1.5">
            <div className="flex items-baseline justify-between gap-3 text-[12px]">
              <span className="font-medium text-ink-2" data-numeric="">
                {completed} of 3 complete
              </span>
              <span className="text-ink-3">{connected ? 'Done' : `About ${minutesLeft} min left`}</span>
            </div>
            <div className="grid grid-cols-3 gap-1" aria-hidden="true">
              {[1, 2, 3].map((n) => (
                <span
                  key={n}
                  className={cn(
                    'h-1.5 rounded-full transition-colors duration-300',
                    stateOf(n) === 'done' ? 'bg-low' : stateOf(n) === 'current' ? 'bg-brand' : 'bg-surface-3',
                  )}
                />
              ))}
            </div>
          </div>
        </div>

        <ol className="flex flex-col">
          <TimelineStep
            step={SETUP_STEPS[0]}
            state={stateOf(1)}
            open={openStep === 1}
            onToggle={() => setOpenStep(openStep === 1 ? null : 1)}
            stepRef={(node) => {
              stepRefs.current[1] = node;
            }}
            summary={`${setup.roleName} · ${setup.managedPolicies.join(' + ')}`}
            consoleLink={{ href: AWS_CONSOLE.iamRoles, label: 'Open IAM console' }}
            onDone={(value) => setStepDone(1, value)}
          >
            <Instructions
              items={[
                <>
                  Go to <b>Roles</b> and choose <b>Create role</b>.
                </>,
                <>
                  Trusted entity: <b>AWS account</b>, then <b>Another AWS account</b>.
                </>,
                <>
                  Account ID <CopyChip value={setup.accountId} label="our account ID" />
                </>,
                <>
                  Tick <b>Require external ID</b> and paste <CopyChip value={setup.externalId} label="External ID" />
                </>,
                <>
                  Search for and tick{' '}
                  {setup.managedPolicies.map((policy, index) => (
                    <span key={policy}>
                      {index > 0 && ' and '}
                      <CopyChip value={policy} />
                    </span>
                  ))}
                </>,
                <>
                  Role name <CopyChip value={setup.roleName} label="role name" />, then <b>Create role</b>.
                </>,
                <>
                  Open the role and copy its <b>ARN</b> - you will paste it in step 3.
                </>,
              ]}
            />
            <Disclosure label="Prefer JSON? Trust policy for “Edit trust relationship”">
              <CodeBlock code={trustPolicyJson(setup)} copyLabel="Copy trust policy" />
            </Disclosure>
          </TimelineStep>

          <TimelineStep
            step={SETUP_STEPS[1]}
            state={stateOf(2)}
            open={openStep === 2}
            onToggle={() => setOpenStep(openStep === 2 ? null : 2)}
            stepRef={(node) => {
              stepRefs.current[2] = node;
            }}
            summary={`Rule ${setup.ruleName} → our event bus`}
            consoleLink={{ href: AWS_CONSOLE.eventBridgeRules, label: 'Open EventBridge console' }}
            onDone={(value) => setStepDone(2, value)}
          >
            <Instructions
              items={[
                <>
                  Go to <b>Rules</b> and choose <b>Create rule</b>.
                </>,
                <>
                  Name <CopyChip value={setup.ruleName} label="rule name" />, event bus <b>default</b>, type{' '}
                  <b>Rule with an event pattern</b>.
                </>,
                <>
                  Event pattern: <b>Custom pattern (JSON)</b>, and paste
                  <CodeBlock code={eventPatternJson(setup)} copyLabel="Copy event pattern" />
                </>,
                <>
                  Target: <b>EventBridge event bus in a different account or Region</b>.
                </>,
                <>
                  Event bus ARN <CopyChip value={setup.eventBusArn} label="event bus ARN" />
                </>,
                <>
                  Keep <b>Create a new role for this specific resource</b>, then <b>Create rule</b>.
                </>,
              ]}
            />
            <p className="mt-3.5 flex items-start gap-2 rounded-[var(--radius-control)] bg-info-soft px-3 py-2 text-[12px] leading-relaxed text-ink-2">
              <Info aria-hidden="true" className="mt-0.5 size-3.5 shrink-0 text-info" />
              {regions.length > 0 ? (
                <span>
                  Create this rule in each region you allow in step 3 (
                  <span className="font-mono">{regions.join(', ')}</span>)
                  {regions.includes('us-east-1') ? '.' : ', and in us-east-1 for IAM events.'}
                </span>
              ) : (
                'Create this rule in every region you will allow in step 3, and in us-east-1 for IAM events.'
              )}
            </p>
          </TimelineStep>

          <TimelineStep
            step={SETUP_STEPS[2]}
            state={stateOf(3)}
            open
            last
            stepRef={(node) => {
              stepRefs.current[3] = node;
            }}
          >
            <form onSubmit={verify} noValidate className="flex flex-col gap-2">
              <Field label="Role ARN" htmlFor="aws-role-arn" error={fieldError || undefined}>
                <Input
                  ref={inputRef}
                  id="aws-role-arn"
                  name="role_arn"
                  size="lg"
                  icon={Link2}
                  autoComplete="off"
                  spellCheck={false}
                  value={roleArn}
                  placeholder={exampleRoleArn(setup)}
                  onChange={(event) => {
                    setRoleArn(event.target.value);
                    setServerProblem('');
                  }}
                  onBlur={() => roleArn.trim() && setTouched(true)}
                  invalid={Boolean(fieldError)}
                  className="font-mono text-[13px]"
                />
              </Field>
              <div className="flex min-h-7 flex-wrap items-center gap-2 text-[12px]">
                {detectedAccount ? (
                  <span className="inline-flex items-center gap-1.5 rounded-full bg-low-soft px-2.5 py-1 font-medium text-low">
                    <CheckCircle2 aria-hidden="true" className="size-3.5" />
                    AWS account <span className="font-mono">{detectedAccount}</span>
                  </span>
                ) : (
                  !fieldError && <span className="text-ink-3">The ARN of the role from step 1.</span>
                )}
              </div>
              <div ref={regionsRef} className="mb-2 scroll-mt-24">
                <RegionPicker
                  catalogue={setup.regions}
                  value={regions}
                  onChange={(next) => {
                    setRegions(next);
                    setRegionsError('');
                  }}
                  error={regionsError || undefined}
                />
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Button type="submit" variant="primary" size="lg" icon={ShieldCheck} loading={running}>
                  {running ? 'Checking…' : connected ? 'Verify again' : 'Verify connection'}
                </Button>
                {onCancel && (
                  <Button variant="ghost" size="lg" onClick={onCancel} disabled={running}>
                    Cancel
                  </Button>
                )}
              </div>
            </form>

            {requestError && (
              <div className="mt-3">
                <InlineError error={requestError} label="Could not verify" />
              </div>
            )}

            {(running || result) && (
              <div className="mt-4">
                <VerifyResult running={running} result={running ? null : result} setup={setup} onShowStep={showStep} />
              </div>
            )}
          </TimelineStep>
        </ol>
      </section>

      <SetupAside setup={setup} regions={regions} />
    </div>
  );
}

const STATE_TAG = {
  done: { tone: 'low', label: 'Done' },
  current: { tone: 'brand', label: 'Next' },
  todo: { tone: 'neutral', label: 'To do' },
};

/** One step on the timeline: a node on the rail, and a card that opens. */
function TimelineStep({ step, state, open, onToggle, last = false, stepRef, summary, consoleLink, onDone, children }) {
  const panelId = useId();
  const collapsible = Boolean(onToggle);
  const tag = STATE_TAG[state];

  const header = (
    <>
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
          <span className="text-[14px] font-semibold text-ink">{step.title}</span>
          <Tag tone={tag.tone} size="sm">
            {tag.label}
          </Tag>
        </span>
        <span className="mt-0.5 block truncate text-[12px] text-ink-3">
          {open || !summary ? `${step.where} · about ${step.minutes} min` : summary}
        </span>
      </span>
      {collapsible && (
        <ChevronDown
          aria-hidden="true"
          className={cn('size-4 shrink-0 text-ink-3 transition-transform duration-200', open ? 'rotate-180' : 'rotate-0')}
        />
      )}
    </>
  );

  return (
    <li ref={stepRef} className={cn('relative scroll-mt-32 pl-11', !last && 'pb-4')}>
      {/* The rail turns green below a finished step, so progress reads down the page. */}
      {!last && (
        <span
          aria-hidden="true"
          className={cn(
            'absolute top-11 bottom-0 left-[15px] w-0.5 rounded-full transition-colors duration-300',
            state === 'done' ? 'bg-low/60' : 'bg-line',
          )}
        />
      )}
      <span
        aria-hidden="true"
        data-numeric=""
        className={cn(
          'absolute top-3 left-0 grid size-8 place-items-center rounded-full text-[13px] font-semibold transition-colors duration-300',
          state === 'done'
            ? 'bg-low text-white'
            : state === 'current'
              ? 'bg-surface text-brand shadow-[0_0_0_2px_var(--t-brand),0_0_0_6px_color-mix(in_srgb,var(--t-brand)_14%,transparent)]'
              : 'border border-line-strong bg-surface text-ink-3',
        )}
      >
        {state === 'done' ? <Check className="size-4" strokeWidth={3} /> : step.number}
      </span>

      <div
        className={cn(
          'rounded-[var(--radius-panel)] border bg-surface transition-[border-color,box-shadow] duration-200',
          open ? 'border-line-strong shadow-[var(--t-shadow-md)]' : 'border-line shadow-[var(--t-shadow-sm)] hover:border-line-strong',
        )}
      >
        <h3>
          {collapsible ? (
            <button
              type="button"
              onClick={onToggle}
              aria-expanded={open}
              aria-controls={panelId}
              className="flex w-full items-center gap-3 rounded-[var(--radius-panel)] px-4 py-3.5 text-left"
            >
              {header}
            </button>
          ) : (
            <span className="flex w-full items-center gap-3 px-4 pt-3.5">{header}</span>
          )}
        </h3>

        {open && (
          <div id={panelId} className="animate-fade">
              <div className={cn('px-4 pb-4', collapsible ? 'border-t border-line pt-3.5' : 'pt-3')}>
                {consoleLink && (
                  <Button
                    as="a"
                    href={consoleLink.href}
                    target="_blank"
                    rel="noopener noreferrer"
                    variant="secondary"
                    size="sm"
                    iconRight={ExternalLink}
                    className="mb-3.5"
                  >
                    {consoleLink.label}
                    <span className="sr-only">(opens in a new tab)</span>
                  </Button>
                )}
                {children}
                {onDone && (
                  <div className="mt-4 flex items-center justify-between gap-3 border-t border-line pt-3.5">
                    <span className="text-[12px] text-ink-3">{state === 'done' ? 'Marked as done.' : 'Finished in the console?'}</span>
                    {state === 'done' ? (
                      <Button variant="ghost" size="sm" onClick={() => onDone(false)}>
                        Mark as not done
                      </Button>
                    ) : (
                      <Button variant="primary" size="sm" icon={Check} onClick={() => onDone(true)}>
                        Mark as done
                      </Button>
                    )}
                  </div>
                )}
              </div>
          </div>
        )}
      </div>
    </li>
  );
}

/* ── Side panel ──────────────────────────────────────────────────────────── */

/**
 * Everything the admin reaches for while working in the other tab: the
 * values, what is being granted (the questions a security reviewer asks
 * first), and the setup as a document for whoever owns the AWS account.
 */
function SetupAside({ setup, regions }) {
  const { notify } = useToast();
  const guide = () => setupGuideMarkdown(setup, regions);
  return (
    <aside
      aria-label="Connection details"
      className="flex flex-col gap-4 @min-[56rem]:sticky @min-[56rem]:top-20 @min-[56rem]:max-h-[calc(100dvh-6rem)] @min-[56rem]:overflow-y-auto @min-[56rem]:overscroll-contain @min-[56rem]:pb-1"
    >
      <AsideCard title="Values to copy">
        <dl className="-my-1 divide-y divide-line">
          <ValueRow label="Our account ID" value={setup.accountId} />
          <ValueRow label="External ID" value={setup.externalId} note="Unique to your workspace" />
          <ValueRow label="Event bus ARN" value={setup.eventBusArn} />
          <ValueRow label="Region" value={setup.region} />
        </dl>
      </AsideCard>

      <AsideCard title="What you are granting">
        <ul className="flex flex-col gap-2">
          {setup.managedPolicies.map((policy) => (
            <li key={policy} className="flex items-center justify-between gap-2">
              <span className="font-mono text-[12.5px] text-ink">{policy}</span>
              <span className="text-[11px] text-ink-3">AWS managed · read-only</span>
            </li>
          ))}
        </ul>
        <div className="mt-3 border-t border-line pt-3">
          <p className="text-[12px] text-ink-2">
            Events from <span className="font-semibold text-ink">{setup.eventSources.length} services</span>
          </p>
          <div className="mt-1.5 flex flex-wrap gap-1">
            {setup.eventSources.map((source) => (
              <span key={source} className="rounded-md bg-surface-3 px-1.5 py-0.5 font-mono text-[10.5px] text-ink-2">
                {source.replace(/^aws\./, '')}
              </span>
            ))}
          </div>
        </div>
        <p className="mt-3 flex items-start gap-2 border-t border-line pt-3 text-[12px] leading-relaxed text-ink-3">
          <EyeOff aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
          No write access, no secret values, no object contents. Revoke any time by deleting the role.
        </p>
      </AsideCard>

      <AsideCard title="Not the AWS admin?" icon={Users}>
        <p className="text-[12px] leading-relaxed text-ink-3">
          Send the setup, with your values filled in, to whoever manages the account.
        </p>
        <div className="mt-3 flex flex-wrap gap-2">
          <Button
            variant="secondary"
            size="sm"
            icon={FileText}
            onClick={async () => {
              const ok = await copyText(guide());
              notify(
                ok
                  ? { variant: 'success', title: 'Setup guide copied', description: 'Paste it into an email, chat or ticket.' }
                  : { variant: 'error', title: 'Could not copy', description: 'Download it instead.' },
              );
            }}
          >
            Copy guide
          </Button>
          <Button
            variant="ghost"
            size="sm"
            icon={Download}
            onClick={() => downloadText(guide(), 'aws-connection-setup.md', 'text/markdown')}
          >
            Download
          </Button>
        </div>
      </AsideCard>
    </aside>
  );
}

function AsideCard({ title, icon: Icon, children }) {
  return (
    <section className="rounded-[var(--radius-panel)] border border-line bg-surface p-4 shadow-[var(--t-shadow-sm)]">
      <h3 className="mb-3 flex items-center gap-2 text-[13px] font-semibold text-ink">
        {Icon && <Icon aria-hidden="true" className="size-4 text-ink-3" />}
        {title}
      </h3>
      {children}
    </section>
  );
}

function ValueRow({ label, value, note }) {
  return (
    <div className="flex items-start gap-2 py-2">
      <div className="min-w-0 flex-1">
        <dt className="text-[11.5px] text-ink-3">
          {label}
          {note && <span className="ml-1 text-[11px] text-medium">· {note}</span>}
        </dt>
        <dd className="mt-0.5 font-mono text-[12.5px] break-all text-ink">{value}</dd>
      </div>
      <CopyButton value={value} label={`Copy ${label}`} className="mt-1" />
    </div>
  );
}
