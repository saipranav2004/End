import { Link } from 'react-router-dom';
import { AlertTriangle, ArrowRight, CheckCircle2, Clock3, Globe2, Layers, Plus, Settings2, ShieldCheck } from 'lucide-react';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { cn } from '../../ui/cn';
import { exposureState } from './ExposureBadges';
import { AwsMark, CodeCommitMark, GitHubMark } from './parts';
import { useSync } from '../sync/SyncContext';

/*
 * The top of the Integrations page once an account is connected: four facts a
 * security lead checks first (are the connections healthy, what do they cover,
 * what did they bring in, how fresh is it) and the three sources, each with
 * its state and the one action it needs.
 */

const TONES = {
  low: 'var(--t-low)',
  medium: 'var(--t-medium)',
  info: 'var(--t-brand)',
  neutral: 'var(--t-line-strong)',
};

function Tile({ icon: Icon, label, value, unit, caption, tone = 'info', title }) {
  return (
    <div className="relative flex flex-col overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface p-3.5" title={title}>
      <span aria-hidden="true" className="absolute inset-y-0 left-0 w-[3px]" style={{ background: TONES[tone] }} />
      <span className="flex items-center gap-1.5 text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">
        {Icon && <Icon aria-hidden="true" className="size-3.5" />}
        {label}
      </span>
      <span className="mt-1.5 flex items-baseline gap-1.5">
        <span data-numeric="" className="text-[22px] leading-none font-bold tracking-[-0.02em] text-ink">
          {value}
        </span>
        {unit && <span className="text-[12px] text-ink-3">{unit}</span>}
      </span>
      {caption && <span className="mt-1.5 text-[12px] leading-snug text-ink-3">{caption}</span>}
    </div>
  );
}

/** Account health, region coverage, what was discovered, and data freshness. */
export function OverviewTiles({ accounts, inventory, regionNames }) {
  const connected = accounts.filter((account) => account.status === 'connected');
  const attention = accounts.filter((account) => account.status === 'action_needed');
  const regions = [...new Set(accounts.flatMap((account) => account.regions))].sort();
  const identities = inventory ? accounts.reduce((sum, account) => sum + (inventory.get(account.accountId)?.identities ?? 0), 0) : null;
  const credentials = inventory ? accounts.reduce((sum, account) => sum + (inventory.get(account.accountId)?.credentials ?? 0), 0) : null;

  const pending = accounts.filter((account) => account.sync.status === 'queued' || account.sync.status === 'running');
  const failed = accounts.filter((account) => account.sync.status === 'failed');
  const { run, lastScanAt } = useSync();
  const running = Boolean(run) && run.stage !== 'done' && run.stage !== 'failed';
  // The newest of what each account reports and what the last sync from this console finished.
  const lastSynced = [...accounts.map((account) => account.sync.lastSyncedAt), lastScanAt]
    .filter(Boolean)
    .sort((a, b) => new Date(a) - new Date(b))
    .at(-1);

  return (
    <div className="grid gap-3 @min-[34rem]:grid-cols-2 @min-[64rem]:grid-cols-4">
      <Tile
        icon={ShieldCheck}
        label="Connections"
        value={`${formatNumber(connected.length)}/${formatNumber(accounts.length)}`}
        unit="healthy"
        tone={attention.length ? 'medium' : 'low'}
        caption={
          attention.length
            ? `${formatNumber(attention.length)} ${attention.length === 1 ? 'account needs' : 'accounts need'} attention - open it below to see the fix.`
            : 'Every account passed its last connection check.'
        }
      />
      <Tile
        icon={Globe2}
        label="Coverage"
        value={formatNumber(regions.length)}
        unit={regions.length === 1 ? 'region' : 'regions'}
        tone="info"
        title={regions.map((code) => regionNames.get(code) ?? code).join('\n')}
        caption={regions.length ? `${regions.slice(0, 3).join(', ')}${regions.length > 3 ? ` +${regions.length - 3}` : ''}, plus IAM, which is global.` : 'No region is read yet.'}
      />
      <Tile
        icon={Layers}
        label="Discovered"
        value={identities === null ? '-' : formatNumber(identities)}
        unit="identities"
        tone="info"
        caption={credentials === null ? 'Counts are not available right now.' : `${formatNumber(credentials)} credentials across ${formatNumber(accounts.length)} ${accounts.length === 1 ? 'account' : 'accounts'}.`}
      />
      <Tile
        icon={Clock3}
        label="Data freshness"
        value={running ? 'Syncing now' : lastSynced ? formatRelative(lastSynced) : pending.length ? 'Queued' : '-'}
        tone={failed.length ? 'medium' : 'neutral'}
        title={lastSynced ? `Last sync finished ${formatDateTime(lastSynced)}` : undefined}
        caption={
          running
            ? 'Reading your accounts and scoring what is found. Open the progress from Sync now.'
            : failed.length
            ? `The last sync failed for ${failed.map((account) => account.accountId).join(', ')}.`
            : pending.length
              ? `${syncWords(pending)} This page updates on its own.`
              : lastSynced
                ? 'When the most recent sync finished.'
                : 'No sync has completed yet.'
        }
      />
    </div>
  );
}

const CODE_SOURCES = [
  { key: 'github', name: 'GitHub', Mark: GitHubMark, what: 'Repositories a read-only token can see. The token stays in your own Secrets Manager.' },
  { key: 'codecommit', name: 'AWS CodeCommit', Mark: CodeCommitMark, what: 'Repositories in each account, read through the role you already created.' },
];

/** AWS plus the two code sources, each with its state and its next action. */
export function SourceCards({ accounts, exposureEnabled, canManage, canSeeFindings, onConnect, onExposure }) {
  const eligible = accounts.filter((account) => account.connectedAt);
  const connected = accounts.filter((account) => account.status === 'connected').length;
  const attention = accounts.some((account) => account.status === 'action_needed');

  return (
    <section aria-label="Data sources" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-[14.5px] font-semibold text-ink">Data sources</h2>
        <p className="text-[12px] text-ink-3">Every connection is read-only. Nothing is written to your accounts or repositories.</p>
      </div>
      <div className="grid gap-3 @min-[56rem]:grid-cols-3">
        <SourceCard
          mark={<AwsMark size="sm" />}
          name="Amazon Web Services"
          kind="Cloud accounts"
          state={attention ? { tone: 'medium', label: 'Needs attention' } : { tone: 'low', label: 'Connected' }}
          detail={`${formatNumber(connected)} of ${formatNumber(accounts.length)} ${accounts.length === 1 ? 'account' : 'accounts'} connected through a read-only IAM role and an EventBridge rule.`}
          action={
            canManage ? (
              <Button variant="secondary" size="sm" icon={Plus} onClick={onConnect}>
                Connect account
              </Button>
            ) : null
          }
        />
        {CODE_SOURCES.map((source) => {
          const on = eligible.filter((account) => exposureState(account.exposure[source.key]) === 'on');
          const failing = eligible.filter((account) => exposureState(account.exposure[source.key]) === 'failing');
          const state = failing.length
            ? { tone: 'medium', label: 'Check failed' }
            : on.length
              ? { tone: 'low', label: 'Scanning' }
              : { tone: 'neutral', label: 'Off' };
          const detail = failing.length
            ? `Its check failed in ${failing.map((account) => account.accountId).join(', ')}, so nothing is scanned there.`
            : on.length
              ? `Scanning in ${on.length === eligible.length ? (eligible.length === 1 ? 'your account' : `all ${formatNumber(eligible.length)} accounts`) : `${formatNumber(on.length)} of ${formatNumber(eligible.length)} accounts`}. ${source.what}`
              : `Looks for keys, tokens and passwords committed to code. ${source.what}`;
          return (
            <SourceCard
              key={source.key}
              mark={<source.Mark size="sm" />}
              name={source.name}
              kind="Code repositories"
              state={state}
              detail={detail}
              action={
                canManage && eligible.length ? (
                  <Button
                    variant={on.length || failing.length ? 'secondary' : 'primary'}
                    size="sm"
                    icon={on.length || failing.length ? Settings2 : ShieldCheck}
                    onClick={() => onExposure(failing[0]?.accountId ?? on[0]?.accountId ?? null)}
                  >
                    {failing.length ? 'See what failed' : on.length ? 'Manage' : 'Set up scanning'}
                  </Button>
                ) : null
              }
            />
          );
        })}
      </div>
      {exposureEnabled && canSeeFindings && (
        <p className="text-[12px] text-ink-3">
          Findings from code scanning are listed under{' '}
          <Link to="/exposure" className="inline-flex items-center gap-0.5 font-medium text-brand hover:underline">
            Exposed credentials
            <ArrowRight aria-hidden="true" className="size-3" />
          </Link>
        </p>
      )}
    </section>
  );
}

const STATE_STYLE = {
  low: { className: 'border-low/30 bg-low-soft text-low', icon: CheckCircle2 },
  medium: { className: 'border-medium/35 bg-medium-soft text-medium', icon: AlertTriangle },
  neutral: { className: 'border-line bg-surface-2 text-ink-3', icon: null },
};

function SourceCard({ mark, name, kind, state, detail, action }) {
  const style = STATE_STYLE[state.tone] ?? STATE_STYLE.neutral;
  const Icon = style.icon;
  return (
    <div className="flex flex-col rounded-[var(--radius-panel)] border border-line bg-surface p-4 shadow-[var(--t-shadow-sm)]">
      <div className="flex items-start gap-3">
        {mark}
        <div className="min-w-0 flex-1">
          <p className="text-[13.5px] font-semibold text-ink">{name}</p>
          <p className="text-[11.5px] text-ink-3">{kind}</p>
        </div>
        <span className={cn('inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium', style.className)}>
          {Icon && <Icon aria-hidden="true" className="size-3" />}
          {state.label}
        </span>
      </div>
      <p className="mt-3 flex-1 text-[12.5px] leading-relaxed text-ink-2">{detail}</p>
      {action && <div className="mt-3">{action}</div>}
    </div>
  );
}

/** "1 sync is running and 1 is queued." - the words for the syncs in flight. */
function syncWords(pending) {
  const running = pending.filter((account) => account.sync.status === 'running').length;
  const queued = pending.length - running;
  const parts = [];
  if (running) parts.push(`${formatNumber(running)} ${running === 1 ? 'sync is' : 'syncs are'} running`);
  if (queued) parts.push(`${formatNumber(queued)} ${running ? (queued === 1 ? 'is' : 'are') : queued === 1 ? 'sync is' : 'syncs are'} queued for the discovery service`);
  return `${parts.join(' and ')}.`;
}
