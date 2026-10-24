import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { AlertTriangle, CheckCircle2, Clock3, FlaskConical, KeyRound, LayoutDashboard, Plus, RotateCw, ShieldCheck, X } from 'lucide-react';
import { useAuth } from '../../app/AuthContext';
import { useWorkspace } from '../../app/WorkspaceContext';
import { useAccess } from '../../app/useAccess';
import { fetchAwsIntegration } from '../../lib/api/integrations';
import { INTEGRATIONS_PREVIEW, previewInventory, resetPreview } from '../../lib/api/integrationsPreview';
import { fetchFilters } from '../../lib/api/inventory';
import { useQuery } from '../../lib/hooks';
import { formatDateTime, formatRelative } from '../../lib/format';
import { PageHeader } from '../../shell/PageHeader';
import { SyncNowButton } from '../sync/SyncButton';
import { getEpoch, subscribeEpoch } from '../../lib/sync/engine';
import { Button, IconButton } from '../../ui/Button';
import { Panel } from '../../ui/Panel';
import { Skeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState } from '../../ui/States';
import { useToast } from '../../ui/Toast';
import { cn } from '../../ui/cn';
import { AccountDrawer } from './AccountDrawer';
import { AccountsTable } from './AccountsTable';
import { AWS_STATUS } from './awsSetup';
import { AwsConnectWizard } from './AwsConnectWizard';
import { ExposureDrawer } from './ExposureDrawer';
import { OverviewTiles, SourceCards } from './IntegrationsOverview';
import { AwsMark } from './parts';

const reducedMotion = () => typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;

/**
 * Integrations: where a workspace connects the AWS accounts its data comes
 * from, and switches on exposed-credential scanning. Two situations, one
 * screen:
 *
 *   Onboarding  No account has ever connected. `ConnectionGate` reduces the
 *               console to this page; the first verified account opens the
 *               dashboard.
 *   Manage      At least one account has connected: account health, regions,
 *               sync, "Connect another account" (?add=1 opens it directly)
 *               and exposed-credential scanning (?exposure=<account> opens it).
 *
 * The page reads the same answer the gate and the sidebar do
 * (`useWorkspace`), so a change made here - an account connected or removed,
 * scanning switched on - moves the navigation with it.
 */
export default function IntegrationsPage() {
  const { user } = useAuth();
  const workspace = useWorkspace();
  const { notify } = useToast();
  const { can } = useAccess();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const data = workspace.integration;
  const manage = workspace.hasConnectedAccount && Boolean(data);

  const setParam = (key, value) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next, { replace: true });
  };
  const adding = searchParams.get('add') === '1';
  const exposureParam = searchParams.get('exposure');
  const accountParam = searchParams.get('account');
  const [editing, setEditing] = useState(null);
  const [removing, setRemoving] = useState(null);

  /* What each account brought in, for the table and the overview. A failure
     only blanks those counts. */
  const filters = useQuery((signal) => (manage && !INTEGRATIONS_PREVIEW ? fetchFilters(signal) : Promise.resolve(null)), [manage, data?.accounts?.length]);
  const inventory = useMemo(() => {
    if (INTEGRATIONS_PREVIEW) return previewInventory(data?.accounts ?? []);
    const rows = filters.data?.accounts;
    if (!Array.isArray(rows)) return null;
    return new Map(rows.map((row) => [String(row.account_id), { identities: Number(row.identity_count) || 0, credentials: Number(row.credential_count) || 0 }]));
  }, [filters.data, data?.accounts]);
  const regionNames = useMemo(() => new Map((data?.setup?.regions ?? []).map((region) => [region.code, region.name])), [data?.setup?.regions]);

  /* A new key per opening: every account needs its own role and rule, so the
     setup for the next account starts from step 1. */
  const [wizardKey, setWizardKey] = useState(0);
  const [justAdded, setJustAdded] = useState('');
  const [onboardingDone, setOnboardingDone] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const storageKey = `nhi.awsSetup.${user?.tenantId ?? user?.tenant_id ?? 'workspace'}`;
  const anotherKey = `${storageKey}.another`;

  const addSectionRef = useRef(null);
  useEffect(() => {
    if (adding && data) addSectionRef.current?.scrollIntoView({ behavior: reducedMotion() ? 'auto' : 'smooth', block: 'start' });
  }, [adding, data]);

  /* While a sync is queued or running, keep the page current on its own. */
  const syncing = (data?.accounts ?? []).some((account) => account.sync.status === 'queued' || account.sync.status === 'running');
  const refresh = workspace.refresh;
  useEffect(() => {
    if (!syncing) return undefined;
    const id = setInterval(() => refresh(), 8000);
    return () => clearInterval(id);
  }, [syncing, refresh]);

  /* A sync from this console ended: read the accounts' own sync state again. */
  const syncEpoch = useSyncExternalStore(subscribeEpoch, getEpoch);
  useEffect(() => {
    if (syncEpoch > 0) refresh();
  }, [syncEpoch, refresh]);

  const reload = async () => {
    setRefreshing(true);
    try {
      await refresh();
    } finally {
      setRefreshing(false);
    }
  };

  const onVerified = (outcome) => {
    if (outcome.status === 'connected') {
      if (!workspace.hasConnectedAccount) {
        /* The first account: celebrate here, and open the console only when
           the reader moves on - refreshing now would swap the shell under them. */
        setOnboardingDone(outcome.accountId);
        return;
      }
      setJustAdded(outcome.accountId);
      setParam('add', null);
      setWizardKey((value) => value + 1);
      try {
        localStorage.removeItem(anotherKey);
      } catch {
        /* progress is a convenience */
      }
    }
    if (outcome.saved) refresh();
  };

  /* Read the new state and move in the same tick, so the gate opens and the
     route changes together - no flash of the setup inside the full console. */
  const leaveOnboarding = async (to) => {
    try {
      const next = await fetchAwsIntegration();
      workspace.apply(next);
      navigate(to, { replace: true });
    } catch (error) {
      notify({ variant: 'error', title: 'Could not open the dashboard', description: error?.message });
    }
  };

  /* Previous unconnected attempt, so the setup reopens where it was left. */
  const previous = !manage ? ((data?.accounts ?? []).find((account) => account.status !== 'connected') ?? null) : null;

  if (!workspace.hasConnectedAccount && onboardingDone) {
    return (
      <OnboardingSuccess
        accountId={onboardingDone}
        onOpen={() => leaveOnboarding('/overview')}
        onAnother={() => leaveOnboarding('/integrations?add=1')}
      />
    );
  }

  const exposureAccounts = (data?.accounts ?? []).filter((account) => account.connectedAt);
  const openAccount = accountParam ? (data?.accounts ?? []).find((account) => account.accountId === accountParam) ?? null : null;
  const exposureOpen = manage && exposureParam !== null && data?.setup && data.canManage && exposureAccounts.length > 0;

  return (
    <div className="flex flex-col gap-6">
      {!manage ? (
        <OnboardingIntro name={user?.name} canManage={data?.canManage ?? true} />
      ) : (
        <PageHeader
          title="Integrations"
          lede="The cloud accounts and code repositories this console reads. Every connection is read-only."
          actions={
            <>
              <Button variant="secondary" icon={RotateCw} loading={refreshing} onClick={reload} title="Reload this page. It does not read your AWS accounts again - Sync now does.">
                Refresh
              </Button>
              {data.setup && <SyncNowButton />}
              {data.canManage && data.setup && (
                <Button variant="primary" icon={Plus} onClick={() => setParam('add', '1')} disabled={adding}>
                  Connect AWS account
                </Button>
              )}
            </>
          }
        />
      )}

      {INTEGRATIONS_PREVIEW && manage && (
        <PreviewBanner
          onReset={async () => {
            resetPreview();
            await refresh();
            notify({ variant: 'info', title: 'Sample accounts restored' });
          }}
        />
      )}

      {!data && workspace.status === 'loading' && <PageSkeleton />}

      {!data && workspace.status === 'error' && (
        <Panel>
          <ErrorState
            error={workspace.error}
            onRetry={reload}
            title={workspace.error?.code === 'AWS_PLATFORM_NOT_CONFIGURED' ? 'AWS connections are not available yet' : undefined}
          />
        </Panel>
      )}

      {data && (
        <>
          {!manage && <ProviderHero data={data} manage={manage} />}

          {manage && justAdded && <AddedBanner accountId={justAdded} onDismiss={() => setJustAdded('')} />}

          {manage && <AttentionBanner accounts={data.accounts} onOpen={(account) => setParam('account', account.accountId)} />}

          {manage && <OverviewTiles accounts={data.accounts} inventory={inventory} regionNames={regionNames} />}

          {manage && (
            <AccountsTable
              accounts={data.accounts}
              catalogue={data.setup?.regions ?? []}
              inventory={inventory}
              canManage={data.canManage}
              onChanged={refresh}
              onRemoved={(accountId) => {
                if (accountParam === accountId) setParam('account', null);
                refresh();
              }}
              onOpen={(account) => setParam('account', account.accountId)}
              onExposure={data.canManage && data.setup ? (account) => setParam('exposure', account.accountId) : null}
              editing={editing}
              setEditing={setEditing}
              removing={removing}
              setRemoving={setRemoving}
            />
          )}

          {manage && adding && data.setup && (
            <section ref={addSectionRef} className="scroll-mt-24" aria-label="Connect another AWS account">
              <AwsConnectWizard
                key={wizardKey}
                setup={data.setup}
                storageKey={anotherKey}
                title="Connect another AWS account"
                onVerified={onVerified}
                onCancel={() => setParam('add', null)}
              />
            </section>
          )}

          {manage && (
            <SourceCards
              accounts={data.accounts}
              exposureEnabled={data.exposureEnabled}
              canManage={data.canManage && Boolean(data.setup)}
              canSeeFindings={can('exposure.view')}
              onConnect={() => setParam('add', '1')}
              onExposure={(accountId) => setParam('exposure', accountId || exposureAccounts[0]?.accountId || '')}
            />
          )}

          {!manage &&
            (data.canManage && data.setup ? (
              <AwsConnectWizard setup={data.setup} previous={previous} storageKey={storageKey} onVerified={onVerified} />
            ) : (
              <Panel>
                <EmptyState
                  icon={Clock3}
                  title={data.setup ? 'Waiting for an admin to connect AWS' : 'AWS connections are not set up for your workspace yet'}
                  description={
                    data.setup
                      ? 'Connecting an account needs the admin role. Once an admin has connected one, your dashboard opens.'
                      : 'Our team finishes this when your workspace is created. Contact support if it has been more than a day.'
                  }
                  action={
                    <Button variant="secondary" size="sm" icon={RotateCw} loading={refreshing} onClick={reload}>
                      Check again
                    </Button>
                  }
                />
              </Panel>
            ))}

          {manage && openAccount && (
            <AccountDrawer
              key={openAccount.accountId}
              account={openAccount}
              setup={data.setup}
              catalogue={data.setup?.regions ?? []}
              counts={inventory?.get(openAccount.accountId) ?? null}
              canManage={data.canManage}
              onClose={() => setParam('account', null)}
              onChanged={refresh}
              onEditRegions={() => setEditing(openAccount)}
              onRemove={() => setRemoving(openAccount)}
              onExposure={data.canManage && data.setup && openAccount.connectedAt ? () => setParam('exposure', openAccount.accountId) : null}
            />
          )}

          {exposureOpen && (
            <ExposureDrawer
              accounts={exposureAccounts}
              accountId={exposureParam || exposureAccounts[0].accountId}
              setup={data.setup}
              onClose={() => setParam('exposure', null)}
              onSaved={() => refresh()}
            />
          )}
        </>
      )}
    </div>
  );
}

/* ── Onboarding ──────────────────────────────────────────────────────────── */

function OnboardingIntro({ name, canManage }) {
  const word = String(name ?? '').trim().split(/\s+/)[0];
  const first = word ? word[0].toUpperCase() + word.slice(1) : '';
  return (
    <header className="pt-2">
      <p className="text-[12.5px] font-medium text-brand">{first ? `Welcome, ${first}` : 'Welcome'}</p>
      <h1 className="mt-1 text-[26px] leading-tight font-extrabold tracking-[-0.026em] text-ink sm:text-[28px]">
        {canManage ? 'Connect your first AWS account' : 'Your workspace is almost ready'}
      </h1>
      <p className="mt-2 max-w-2xl text-[13.5px] leading-relaxed text-ink-2">
        {canManage
          ? 'We discover the non-human identities and credentials in your AWS accounts. Connect one to begin - your dashboard opens as soon as the connection is verified.'
          : 'Your dashboard opens once an AWS account is connected to this workspace.'}
      </p>
    </header>
  );
}

/**
 * The moment the first account connects. Opens the dashboard on its own after
 * a short, visible countdown that can be stopped; "Connect another account"
 * is offered here because teams with several accounts usually do them in one
 * sitting.
 */
function OnboardingSuccess({ accountId, onOpen, onAnother }) {
  const [remaining, setRemaining] = useState(8);
  const [paused, setPaused] = useState(false);
  const [leaving, setLeaving] = useState('');
  const openRef = useRef(onOpen);
  useEffect(() => {
    openRef.current = onOpen;
  });

  useEffect(() => {
    if (paused || leaving) return undefined;
    const id = setInterval(() => setRemaining((value) => Math.max(0, value - 1)), 1000);
    return () => clearInterval(id);
  }, [paused, leaving]);

  /* At zero the dashboard opens; the ref keeps a re-render from opening it twice. */
  const autoOpened = useRef(false);
  useEffect(() => {
    if (!paused && !leaving && remaining === 0 && !autoOpened.current) {
      autoOpened.current = true;
      openRef.current();
    }
  }, [remaining, paused, leaving]);

  return (
    <div className="grid min-h-[calc(100dvh-10rem)] place-items-center">
      <section
        role="status"
        className="animate-rise w-full max-w-xl rounded-[var(--radius-panel)] border border-line-strong bg-surface p-6 text-center shadow-[var(--t-shadow-lg)] sm:p-8"
      >
        <span className="animate-pop mx-auto grid size-16 place-items-center rounded-full bg-low-soft shadow-[0_0_0_8px_color-mix(in_srgb,var(--t-low)_10%,transparent)]">
          <CheckCircle2 aria-hidden="true" className="size-8 text-low" />
        </span>
        <h1 className="mt-5 text-[22px] font-extrabold tracking-[-0.02em] text-ink">AWS is connected</h1>
        <p className="mt-1.5 text-[13.5px] text-ink-2">
          Account <span className="font-mono font-semibold text-ink">{accountId}</span> passed all three checks. We now discover its
          identities and credentials; your dashboard fills in after the first scan.
        </p>
        <div className="mt-6 flex flex-col items-stretch justify-center gap-2 sm:flex-row sm:items-center">
          <Button
            variant="primary"
            size="lg"
            icon={LayoutDashboard}
            loading={leaving === 'open'}
            disabled={Boolean(leaving)}
            onClick={() => {
              setLeaving('open');
              onOpen();
            }}
          >
            Open the dashboard
          </Button>
          <Button
            variant="secondary"
            size="lg"
            icon={Plus}
            loading={leaving === 'another'}
            disabled={Boolean(leaving)}
            onClick={() => {
              setLeaving('another');
              onAnother();
            }}
          >
            Connect another account
          </Button>
        </div>
        <p className="mt-4 text-[12px] text-ink-3" aria-live="off">
          {paused ? (
            'Staying on this page.'
          ) : leaving || remaining === 0 ? (
            'Opening…'
          ) : (
            <>
              Opening the dashboard in <span data-numeric="">{remaining}</span>s ·{' '}
              <button type="button" onClick={() => setPaused(true)} className="font-medium text-brand hover:underline">
                Stay here
              </button>
            </>
          )}
        </p>
        <p className="mt-5 border-t border-line pt-4 text-[12px] leading-relaxed text-ink-3">
          Want us to look for credentials leaked into GitHub or CodeCommit too? Switch it on from Integrations at any time.
        </p>
      </section>
    </div>
  );
}

/* ── Provider header ─────────────────────────────────────────────────────── */

function StatusPill({ status }) {
  const meta = AWS_STATUS[status] ?? AWS_STATUS.not_connected;
  const live = status === 'connected';
  const dot = { low: 'bg-low', medium: 'bg-medium', neutral: 'bg-ink-3' }[meta.tone] ?? 'bg-ink-3';
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[12px] font-medium',
        meta.tone === 'low'
          ? 'border-low/30 bg-low-soft text-low'
          : meta.tone === 'medium'
            ? 'border-medium/30 bg-medium-soft text-medium'
            : 'border-line-strong bg-surface-2 text-ink-2',
      )}
    >
      <span className="relative flex size-2">
        {live && <span className={cn('absolute inline-flex size-full rounded-full opacity-60 motion-safe:animate-ping', dot)} />}
        <span className={cn('relative inline-flex size-2 rounded-full', dot)} />
      </span>
      {meta.label}
    </span>
  );
}

/**
 * Which provider, its state, and the three facts a reviewer asks about first.
 * Once connected, the right side carries the account health at a glance.
 */
function ProviderHero({ data, manage }) {
  const total = data.accounts.length;
  const healthy = data.accounts.filter((account) => account.status === 'connected').length;
  const lastChecked = data.accounts
    .map((account) => account.lastVerifiedAt)
    .filter(Boolean)
    .sort()
    .at(-1);

  return (
    <section
      aria-label="Amazon Web Services"
      className="relative overflow-hidden rounded-[var(--radius-panel)] border border-line-strong bg-surface shadow-[var(--t-shadow-md)]"
    >
      {/* Ambient light from the top right and a dot grid fading in from the
          right edge: depth without a second accent colour. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(90%_140%_at_100%_0%,color-mix(in_srgb,var(--t-brand)_11%,transparent)_0%,transparent_60%)]"
      />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-y-0 right-0 w-1/2 bg-[radial-gradient(var(--t-line-strong)_1px,transparent_1px)] [mask-image:linear-gradient(to_left,black,transparent)] bg-[size:14px_14px] opacity-50"
      />
      <div className="relative flex flex-wrap items-center gap-x-6 gap-y-5 p-5 sm:p-6">
        <AwsMark size="lg" />
        <div className="min-w-0 flex-1 basis-[22rem]">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
            <h2 className="text-[19px] font-bold tracking-[-0.015em] text-ink">Amazon Web Services</h2>
            {/* Before the first connection, a failed attempt is still setup, not a fault. */}
            <StatusPill status={manage ? data.status : 'not_connected'} />
          </div>
          <p className="mt-1 max-w-xl text-[13px] leading-relaxed text-ink-2">
            Discovers the non-human identities and credentials in your accounts.
          </p>
          <ul className="mt-3 flex flex-wrap gap-x-5 gap-y-1.5 text-[12px] text-ink-2">
            <li className="inline-flex items-center gap-1.5">
              <ShieldCheck aria-hidden="true" className="size-3.5 text-low" />
              Read-only access
            </li>
            <li className="inline-flex items-center gap-1.5">
              <KeyRound aria-hidden="true" className="size-3.5 text-low" />
              No access keys shared
            </li>
            <li className="inline-flex items-center gap-1.5">
              <Clock3 aria-hidden="true" className="size-3.5 text-ink-3" />
              About 10 minutes per account
            </li>
          </ul>
        </div>

        {manage && total > 0 && (
          <dl className="flex shrink-0 divide-x divide-line rounded-[var(--radius-control)] border border-line bg-surface/80 backdrop-blur-sm">
            <HeroStat label="Accounts" value={total} />
            <HeroStat label="Healthy" value={`${healthy}/${total}`} tone={healthy === total ? 'text-low' : 'text-medium'} />
            <HeroStat label="Last checked" value={lastChecked ? formatRelative(lastChecked) : '-'} small title={formatDateTime(lastChecked)} />
          </dl>
        )}
      </div>
    </section>
  );
}

function HeroStat({ label, value, tone, small, title }) {
  return (
    <div className="flex min-w-[92px] flex-col gap-1 px-4 py-2.5" title={title}>
      <dt className="text-[11px] text-ink-3">{label}</dt>
      <dd data-numeric="" className={cn('leading-none font-bold tracking-[-0.02em] text-ink', small ? 'text-[14px]' : 'text-[22px]', tone)}>
        {value}
      </dd>
    </div>
  );
}

/**
 * Shown on every Integrations view of a preview build, so sample accounts are
 * never mistaken for connected ones.
 */
function PreviewBanner({ onReset }) {
  return (
    <div role="note" className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-[var(--radius-panel)] border border-info/35 bg-info-soft px-4 py-3">
      <FlaskConical aria-hidden="true" className="size-4.5 shrink-0 text-brand" />
      <p className="min-w-0 flex-1 text-[13px] text-ink-2">
        <span className="font-semibold text-ink">Preview: these AWS accounts are samples.</span> They live in this browser tab only - connecting,
        syncing or removing one changes nothing on the server or in AWS. Every other page shows your real data.
      </p>
      <Button variant="secondary" size="sm" icon={RotateCw} onClick={onReset}>
        Reset sample
      </Button>
    </div>
  );
}

/** The accounts whose connection needs fixing, with the failing check named. */
function AttentionBanner({ accounts, onOpen }) {
  const broken = accounts.filter((account) => account.status === 'action_needed');
  if (broken.length === 0) return null;
  return (
    <div role="status" className="animate-rise flex flex-col gap-2 rounded-[var(--radius-panel)] border border-medium/40 bg-medium-soft px-4 py-3">
      {broken.map((account) => {
        const failing = account.checks.find((check) => check.state === 'fail' || check.state === 'error');
        return (
          <div key={account.accountId} className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
            <AlertTriangle aria-hidden="true" className="size-4.5 shrink-0 text-medium" />
            <p className="min-w-0 flex-1 text-[13px] text-ink-2">
              <span className="font-semibold text-ink">
                Account <span className="font-mono">{account.accountId}</span> needs attention
              </span>
              {failing ? ` - "${failing.label}" did not pass. ` : '. '}
              {failing?.fix ? <span className="text-ink-3">{failing.fix}</span> : 'Its data stops updating until this is fixed.'}
            </p>
            <Button variant="secondary" size="sm" onClick={() => onOpen(account)}>
              Open account
            </Button>
          </div>
        );
      })}
    </div>
  );
}

/** Shown after an additional account connects; the first one gets the onboarding success screen. */
function AddedBanner({ accountId, onDismiss }) {
  return (
    <div role="status" className="animate-rise flex items-center gap-3 rounded-[var(--radius-panel)] border border-low/35 bg-low-soft px-4 py-3">
      <CheckCircle2 aria-hidden="true" className="size-5 shrink-0 text-low" />
      <p className="min-w-0 flex-1 text-[13px] text-ink-2">
        <span className="font-semibold text-ink">
          Account <span className="font-mono">{accountId}</span> connected.
        </span>{' '}
        It is included from the next scan.
      </p>
      <IconButton icon={X} label="Dismiss" size="sm" onClick={onDismiss} />
    </div>
  );
}

function PageSkeleton() {
  return (
    <div className="flex flex-col gap-5" aria-busy="true">
      <div className="flex items-center gap-5 rounded-[var(--radius-panel)] border border-line bg-surface p-6">
        <Skeleton className="size-14 rounded-[14px]" />
        <div className="flex-1">
          <Skeleton className="h-5 w-56 rounded" />
          <Skeleton className="mt-2.5 h-3 w-96 max-w-full rounded" />
          <Skeleton className="mt-3 h-3 w-72 max-w-full rounded" />
        </div>
      </div>
      <Skeleton className="h-72 rounded-[var(--radius-panel)]" />
      <span role="status" className="sr-only">
        Loading your AWS connection
      </span>
    </div>
  );
}

export { IntegrationsPage };
