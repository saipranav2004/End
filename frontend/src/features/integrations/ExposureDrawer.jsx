import { useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle, ArrowRight, Check, ExternalLink, EyeOff, Info, KeyRound, ShieldCheck } from 'lucide-react';
import { setAwsExposure } from '../../lib/api/integrations';
import { formatDateTime, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Field, Input, Select } from '../../ui/Field';
import { Drawer } from '../../ui/Overlay';
import { SectionLabel } from '../../ui/Panel';
import { InlineError } from '../../ui/States';
import { useToast } from '../../ui/Toast';
import { cn } from '../../ui/cn';
import {
  AWS_CONSOLE,
  EXPOSURE_CHECKS,
  GITHUB_NEW_TOKEN,
  codeCommitCli,
  codeCommitPolicyJson,
  githubCli,
  githubSecretPolicyJson,
  kmsDecryptPolicyJson,
  roleNameOfArn,
  secretArnProblem,
  suggestedSecretName,
} from './awsSetup';
import { CheckResult } from './CheckList';
import { exposureState } from './ExposureBadges';
import { CodeBlock, CodeCommitMark, CopyChip, Disclosure, GitHubMark, Instructions } from './parts';

/**
 * Turn exposed-credential scanning on or off for one connected AWS account,
 * per platform: GitHub, CodeCommit, or both.
 *
 * Each platform needs one grant on the account's role, the same way the
 * read-only connection did:
 *
 *   CodeCommit  the AWS-managed AWSCodeCommitReadOnly policy (or the same
 *               reads as an inline policy).
 *   GitHub      a read-only fine-grained token kept in the client's own
 *               Secrets Manager, and a policy letting the role read only that
 *               secret. The token never passes through this console.
 *
 * Saving runs the platform's checks through the role and switches it on only
 * when they pass; the result is shown here, check by check, with the fix.
 *
 *   accounts    connected accounts this can be set for (connectedAt set)
 *   accountId   the one to open on
 */
export function ExposureDrawer({ accounts, accountId, setup, onClose, onSaved }) {
  const [selectedId, setSelectedId] = useState(accountId ?? accounts[0]?.accountId ?? '');
  const account = accounts.find((entry) => entry.accountId === selectedId) ?? accounts[0];

  return (
    <Drawer
      open
      onClose={onClose}
      eyebrow="Exposed credentials"
      title="Scan code for leaked credentials"
      subtitle="Find access keys, tokens and passwords committed to your repositories, before someone else does."
      width="xl"
    >
      {account ? (
        /* Keyed by account: switching accounts starts from that account's own settings. */
        <ExposureForm
          key={account.accountId}
          account={account}
          accounts={accounts}
          onSelectAccount={setSelectedId}
          setup={setup}
          onClose={onClose}
          onSaved={onSaved}
        />
      ) : (
        <p className="px-5 py-6 text-[13px] text-ink-3">Connect an AWS account first.</p>
      )}
    </Drawer>
  );
}

function ExposureForm({ account, accounts, onSelectAccount, setup, onClose, onSaved }) {
  const { notify } = useToast();
  const [current, setCurrent] = useState(account);
  const initial = useMemo(
    () => ({
      github: exposureState(account.exposure.github) !== 'off',
      codecommit: exposureState(account.exposure.codecommit) !== 'off',
    }),
    [account],
  );
  const [want, setWant] = useState(initial);
  const [secretArn, setSecretArn] = useState(account.exposure.github.secretArn || '');
  const [touched, setTouched] = useState(false);
  const [serverSecretError, setServerSecretError] = useState('');
  const [requestError, setRequestError] = useState(null);
  const [running, setRunning] = useState(false);
  /* Which platforms' results to show: what the last save checked, or - on
     opening - any platform whose last check failed, so the fix is in view. */
  const [ran, setRan] = useState(() => {
    const failing = {
      github: exposureState(account.exposure.github) === 'failing',
      codecommit: exposureState(account.exposure.codecommit) === 'failing',
    };
    return failing.github || failing.codecommit ? failing : null;
  });
  const secretRef = useRef(null);
  const resultRef = useRef(null);

  const roleName = roleNameOfArn(current.roleArn) || setup.roleName;
  const prefix = setup.exposure.github.secretPrefix;
  const localSecretProblem = want.github ? secretArnProblem(secretArn, current.accountId, prefix) : '';
  const secretError = serverSecretError || (touched ? localSecretProblem : '');

  const turningOff = ['github', 'codecommit'].filter((key) => initial[key] && !want[key]);
  const changed = want.github !== initial.github || want.codecommit !== initial.codecommit || (want.github && secretArn.trim() !== (account.exposure.github.secretArn || ''));
  const anyOn = want.github || want.codecommit;
  const canSubmit = anyOn || turningOff.length > 0;

  const submit = async (event) => {
    event?.preventDefault();
    setTouched(true);
    if (localSecretProblem) {
      secretRef.current?.focus();
      return;
    }
    setRunning(true);
    setRequestError(null);
    setServerSecretError('');
    setRan(want.github || want.codecommit ? { ...want } : null);
    requestAnimationFrame(() => resultRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' }));
    try {
      const outcome = await setAwsExposure({
        accountId: current.accountId,
        github: want.github,
        codecommit: want.codecommit,
        githubSecretArn: secretArn.trim(),
      });
      setCurrent(outcome.account);
      onSaved?.(outcome);
      const next = outcome.account.exposure;
      const failed = ['github', 'codecommit'].filter((key) => want[key] && !next[key].connected);
      if (!anyOn) {
        notify({
          variant: 'success',
          title: `Scanning off for ${current.accountId}`,
          description: 'Findings already listed stay until you resolve them.',
        });
      } else if (failed.length === 0) {
        notify({
          variant: 'success',
          title: 'Exposed-credential scanning is on',
          description: `${platformList(want)} for ${current.accountId}. Findings appear under Exposed credentials after the first scan.`,
        });
      } else {
        notify({
          variant: 'error',
          title: `${platformList(Object.fromEntries(failed.map((key) => [key, true])))} not turned on`,
          description: 'A check did not pass. The fix is shown under it.',
        });
      }
    } catch (error) {
      setRan(null);
      if (error?.code === 'INVALID_SECRET_ARN') {
        setServerSecretError(error.message);
        secretRef.current?.focus();
      } else {
        setRequestError(error);
      }
    } finally {
      setRunning(false);
    }
  };

  const toggle = (key) => {
    setWant((prev) => ({ ...prev, [key]: !prev[key] }));
    setRan(null);
  };

  const github = current.exposure.github;
  const codecommit = current.exposure.codecommit;
  const allPassed = ran && ['github', 'codecommit'].every((key) => !ran[key] || current.exposure[key].connected);

  return (
    <form onSubmit={submit} noValidate className="flex min-h-full flex-col">
      <div className="flex flex-1 flex-col gap-6 px-4 py-5 sm:px-5">
        {/* Which account */}
        <div className="flex flex-wrap items-end gap-x-4 gap-y-2">
          {accounts.length > 1 ? (
            <Field label="AWS account" htmlFor="exposure-account" className="min-w-[16rem] flex-1">
              <Select
                id="exposure-account"
                value={current.accountId}
                onChange={(event) => onSelectAccount(event.target.value)}
                disabled={running}
                options={accounts.map((entry) => ({ value: entry.accountId, label: entry.accountId }))}
              />
            </Field>
          ) : (
            <div className="min-w-0 flex-1">
              <SectionLabel>AWS account</SectionLabel>
              <p className="mt-1 font-mono text-[13.5px] font-semibold text-ink">{current.accountId}</p>
            </div>
          )}
          <p className="min-w-0 text-[12px] text-ink-3">
            Uses role <span className="font-mono text-ink-2">{roleName}</span>
            {current.exposure.updatedBy ? ` · last changed by ${current.exposure.updatedBy}` : ''}
          </p>
        </div>

        {/* What to scan */}
        <fieldset className="flex flex-col gap-2.5">
          <legend className="mb-2.5 text-[13.5px] font-semibold text-ink">Where should we look?</legend>
          <div className="grid gap-2.5 @min-[38rem]:grid-cols-2">
            <PlatformChoice
              mark={<GitHubMark size="sm" />}
              name="GitHub"
              detail="Repositories a read-only token can see."
              checked={want.github}
              state={exposureState(github)}
              verifiedAt={github.verifiedAt}
              disabled={running}
              onToggle={() => toggle('github')}
            />
            <PlatformChoice
              mark={<CodeCommitMark size="sm" />}
              name="AWS CodeCommit"
              detail={`Repositories in ${current.accountId}, in the regions it reads.`}
              checked={want.codecommit}
              state={exposureState(codecommit)}
              verifiedAt={codecommit.verifiedAt}
              disabled={running}
              onToggle={() => toggle('codecommit')}
            />
          </div>
          <p className="flex items-start gap-2 text-[12px] leading-relaxed text-ink-3">
            <EyeOff aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
            Read-only. We never write to a repository, and found secrets are shown redacted.
          </p>
        </fieldset>

        {want.codecommit && (
          <SetupSection
            mark={<CodeCommitMark size="sm" />}
            title="Allow the role to read CodeCommit"
            state={exposureState(codecommit)}
            minutes={2}
          >
            <Instructions
              items={[
                <>
                  In the AWS console for account <CopyChip value={current.accountId} label="account ID" />, open IAM →{' '}
                  <b>Roles</b> → <CopyChip value={roleName} label="role name" />.
                </>,
                <>
                  <b>Add permissions</b> → <b>Attach policies</b>, tick{' '}
                  <CopyChip value={setup.exposure.codecommit.managedPolicy} label="policy name" /> and choose <b>Add permissions</b>.
                </>,
              ]}
            />
            <ConsoleLink href={AWS_CONSOLE.iamRoles}>Open IAM roles</ConsoleLink>
            <Disclosure label="Prefer the AWS CLI?">
              <CodeBlock code={codeCommitCli(setup, roleName)} copyLabel="Copy the command" />
            </Disclosure>
            <Disclosure label="Only the exact reads? Use this inline policy instead">
              <p className="mt-2 text-[12px] text-ink-3">
                Role → <b className="font-semibold text-ink-2">Add permissions</b> → <b className="font-semibold text-ink-2">Create inline policy</b> → JSON.
              </p>
              <CodeBlock code={codeCommitPolicyJson(setup)} copyLabel="Copy the inline policy" />
            </Disclosure>
            {current.regions.length > 0 && (
              <p className="mt-3.5 flex items-start gap-2 rounded-[var(--radius-control)] bg-info-soft px-3 py-2 text-[12px] leading-relaxed text-ink-2">
                <Info aria-hidden="true" className="mt-0.5 size-3.5 shrink-0 text-info" />
                <span>
                  Repositories are read in the regions this account reads:{' '}
                  <span className="font-mono">{current.regions.join(', ')}</span>.
                </span>
              </p>
            )}
          </SetupSection>
        )}

        {want.github && (
          <SetupSection
            mark={<GitHubMark size="sm" />}
            title="Give us a read-only GitHub token"
            state={exposureState(github)}
            minutes={5}
          >
            <Instructions
              items={[
                <>
                  In GitHub, create a <b>fine-grained personal access token</b>. Resource owner: your organisation. Repository access: <b>All repositories</b>, or the ones to scan.
                  Permissions:{' '}
                  {setup.exposure.github.tokenPermissions.map((permission, index) => (
                    <span key={permission}>
                      {index > 0 && ', '}
                      <b>{permission}</b>
                    </span>
                  ))}
                  . Nothing else.
                </>,
                <>
                  In AWS Secrets Manager in account <CopyChip value={current.accountId} label="account ID" />, choose <b>Store a new secret</b> →{' '}
                  <b>Other type of secret</b> → <b>Plaintext</b>, and paste the token. Name it{' '}
                  <CopyChip value={suggestedSecretName(prefix)} label="secret name" /> - the name must start with{' '}
                  <span className="font-mono text-ink">{prefix}</span>.
                </>,
                <>
                  Let the role read only that secret: IAM → <b>Roles</b> → <CopyChip value={roleName} label="role name" /> →{' '}
                  <b>Add permissions</b> → <b>Create inline policy</b> → JSON, paste this and name it{' '}
                  <CopyChip value="nhi-github-secret-read" label="policy name" />.
                  <CodeBlock code={githubSecretPolicyJson(setup, current.accountId)} copyLabel="Copy the secret read policy" />
                </>,
                <>
                  Copy the secret&rsquo;s <b>Secret ARN</b> from its page and paste it below.
                </>,
              ]}
            />
            <div className="mt-3.5 flex flex-wrap gap-2">
              <ConsoleLink href={GITHUB_NEW_TOKEN} inline>
                Create a GitHub token
              </ConsoleLink>
              <ConsoleLink href={AWS_CONSOLE.secretsManager} inline>
                Open Secrets Manager
              </ConsoleLink>
            </div>
            <Disclosure label="Secret encrypted with your own KMS key?">
              <p className="mt-2 text-[12px] leading-relaxed text-ink-3">
                Secrets Manager&rsquo;s default key needs nothing more. With a customer managed key, add this to the same inline policy, with
                your key&rsquo;s ARN.
              </p>
              <CodeBlock code={kmsDecryptPolicyJson(current.accountId)} copyLabel="Copy the KMS statement" />
            </Disclosure>
            <Disclosure label="Prefer the AWS CLI?">
              <CodeBlock
                code={githubCli(setup, current.accountId, roleName, current.regions[0] || setup.region)}
                copyLabel="Copy the commands"
              />
            </Disclosure>
            <p className="mt-3.5 flex items-start gap-2 text-[12px] leading-relaxed text-ink-3">
              <Info aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
              <span>
                If your organisation approves fine-grained tokens, an owner approves it under Settings → Personal access tokens. When the
                token expires the check fails and this page tells you - replace the secret&rsquo;s value; the ARN stays the same.
              </span>
            </p>

            <Field
              label="Secret ARN"
              htmlFor="exposure-secret-arn"
              required
              className="mt-4"
              error={secretError || undefined}
              hint="We read the token through your role when we scan, and never store it."
            >
              <Input
                ref={secretRef}
                id="exposure-secret-arn"
                icon={KeyRound}
                autoComplete="off"
                spellCheck={false}
                value={secretArn}
                disabled={running}
                placeholder={`arn:aws:secretsmanager:${current.regions[0] || setup.region}:${current.accountId}:secret:${suggestedSecretName(prefix)}-AbCdEf`}
                onChange={(event) => {
                  setSecretArn(event.target.value);
                  setServerSecretError('');
                }}
                onBlur={() => secretArn.trim() && setTouched(true)}
                invalid={Boolean(secretError)}
                className="font-mono text-[12.5px]"
              />
            </Field>
          </SetupSection>
        )}

        {!anyOn && turningOff.length === 0 && (
          <p className="rounded-[var(--radius-control)] border border-dashed border-line-strong px-4 py-6 text-center text-[12.5px] text-ink-3">
            Choose GitHub, CodeCommit or both to see what to set up.
          </p>
        )}

        {turningOff.length > 0 && (
          <p className="flex items-start gap-2 rounded-[var(--radius-control)] border border-medium/35 bg-medium-soft px-3.5 py-2.5 text-[12.5px] leading-relaxed text-ink-2">
            <AlertTriangle aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-medium" />
            <span>
              Saving turns off <b className="font-semibold text-ink">{platformList(Object.fromEntries(turningOff.map((key) => [key, true])))}</b> for{' '}
              {current.accountId}. Findings already listed stay; you can remove the role&rsquo;s extra permissions afterwards.
            </span>
          </p>
        )}

        <div ref={resultRef} className="flex scroll-mt-4 flex-col gap-3">
          {requestError && <InlineError error={requestError} label="Not saved" />}
          {(running ? want : ran)?.codecommit && (
            <CheckResult
              running={running}
              checks={running ? null : codecommit.checks}
              verifiedAt={codecommit.verifiedAt}
              ok={codecommit.connected}
              definitions={EXPOSURE_CHECKS.codecommit}
              copy={{ running: 'Checking CodeCommit…', ok: 'CodeCommit scanning is on', okDetail: firstMessage(codecommit) }}
            />
          )}
          {(running ? want : ran)?.github && (
            <CheckResult
              running={running}
              checks={running ? null : github.checks}
              verifiedAt={github.verifiedAt}
              ok={github.connected}
              definitions={EXPOSURE_CHECKS.github}
              copy={{ running: 'Checking GitHub…', ok: 'GitHub scanning is on', okDetail: lastMessage(github) }}
            />
          )}
          {!running && allPassed && (
            <div className="animate-rise flex flex-wrap items-center gap-3 rounded-[var(--radius-panel)] border border-low/35 bg-low-soft px-4 py-3">
              <Check aria-hidden="true" className="size-5 shrink-0 text-low" />
              <p className="min-w-0 flex-1 text-[12.5px] text-ink-2">
                <span className="font-semibold text-ink">Exposed credentials is now in your sidebar.</span> The first scan runs shortly; findings
                appear there as they are found.
              </p>
              <Button as={Link} to="/exposure" variant="secondary" size="sm" iconRight={ArrowRight}>
                Open Exposed credentials
              </Button>
            </div>
          )}
        </div>
      </div>

      <footer className="sticky bottom-0 flex flex-wrap items-center justify-end gap-2 border-t border-line bg-surface-2 px-4 py-3 sm:px-5">
        <Button variant="ghost" onClick={onClose} disabled={running}>
          {ran || !changed ? 'Close' : 'Cancel'}
        </Button>
        {!anyOn && turningOff.length > 0 ? (
          <Button type="submit" variant="danger" loading={running}>
            Turn off scanning
          </Button>
        ) : (
          <Button type="submit" variant="primary" icon={ShieldCheck} loading={running} disabled={!canSubmit}>
            {running ? 'Checking…' : changed ? 'Verify and turn on' : 'Check again'}
          </Button>
        )}
      </footer>
    </form>
  );
}

function platformList(want) {
  const names = [want.github && 'GitHub', want.codecommit && 'CodeCommit'].filter(Boolean);
  return names.join(' and ');
}

function firstMessage(platform) {
  return platform.checks.find((check) => check.state === 'pass')?.message || 'The role can read your repositories.';
}

function lastMessage(platform) {
  return [...platform.checks].reverse().find((check) => check.state === 'pass')?.message || 'GitHub accepts the token.';
}

const CHOICE_STATE = {
  on: { label: 'On', className: 'text-low' },
  failing: { label: 'Check failed', className: 'text-medium' },
  off: { label: 'Off', className: 'text-ink-3' },
};

/** A platform as a large checkbox: what it covers, and its state now. */
function PlatformChoice({ mark, name, detail, checked, state, verifiedAt, disabled, onToggle }) {
  const meta = CHOICE_STATE[state];
  return (
    <label
      className={cn(
        'relative flex cursor-pointer items-start gap-3 rounded-[var(--radius-panel)] border px-3.5 py-3 transition-[border-color,box-shadow,background-color]',
        checked
          ? 'border-brand bg-info-soft/40 shadow-[0_0_0_3px_color-mix(in_srgb,var(--t-brand)_14%,transparent)]'
          : 'border-line bg-surface hover:border-line-strong',
        disabled && 'cursor-not-allowed opacity-70',
      )}
    >
      <input type="checkbox" checked={checked} onChange={onToggle} disabled={disabled} className="sr-only" />
      {mark}
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="text-[13.5px] font-semibold text-ink">{name}</span>
          <span className={cn('text-[11.5px] font-medium', meta.className)} title={verifiedAt ? `Checked ${formatDateTime(verifiedAt)}` : undefined}>
            · {meta.label}
            {verifiedAt && state !== 'off' ? ` ${formatRelative(verifiedAt)}` : ''}
          </span>
        </span>
        <span className="mt-0.5 block text-[12px] leading-relaxed text-ink-3">{detail}</span>
      </span>
      <span
        aria-hidden="true"
        className={cn(
          'mt-0.5 grid size-5 shrink-0 place-items-center rounded-md border transition-colors',
          checked ? 'border-brand bg-brand text-white' : 'border-line-strong bg-surface',
        )}
      >
        {checked && <Check className="size-3.5" strokeWidth={3} />}
      </span>
    </label>
  );
}

const SECTION_TAG = {
  on: { label: 'On', className: 'border-low/30 bg-low-soft text-low' },
  failing: { label: 'Check failed', className: 'border-medium/35 bg-medium-soft text-medium' },
  off: null,
};

function SetupSection({ mark, title, state, minutes, children }) {
  const tag = SECTION_TAG[state];
  return (
    <section className="animate-rise rounded-[var(--radius-panel)] border border-line-strong bg-surface shadow-[var(--t-shadow-sm)]">
      <header className="flex items-center gap-3 border-b border-line px-4 py-3">
        {mark}
        <div className="min-w-0 flex-1">
          <h3 className="text-[14px] font-semibold text-ink">{title}</h3>
          <p className="text-[12px] text-ink-3">About {minutes} minutes · in your own accounts</p>
        </div>
        {tag && <span className={cn('rounded-full border px-2 py-0.5 text-[11px] font-medium', tag.className)}>{tag.label}</span>}
      </header>
      <div className="px-4 py-4">{children}</div>
    </section>
  );
}

function ConsoleLink({ href, children, inline = false }) {
  return (
    <Button
      as="a"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      variant="secondary"
      size="sm"
      iconRight={ExternalLink}
      className={inline ? undefined : 'mt-3.5'}
    >
      {children}
      <span className="sr-only">(opens in a new tab)</span>
    </Button>
  );
}
