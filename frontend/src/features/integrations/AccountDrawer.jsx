import { useMemo, useState } from 'react';
import { CheckCircle2, Globe2, KeyRound, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { CopyableValue } from '../../ui/Copyable';
import { Drawer } from '../../ui/Overlay';
import { DetailList, DetailRow, SectionLabel } from '../../ui/Panel';
import { Tabs } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { cn } from '../../ui/cn';
import { SYNC, syncBlockedReason, syncDetail, useAccountActions } from './AccountsTable';
import { ACCOUNT_STATUS } from './awsSetup';
import { CheckList } from './CheckList';
import { exposureState } from './ExposureBadges';
import { AwsMark, CodeCommitMark, GitHubMark } from './parts';

const TABS = [
  { value: 'overview', label: 'Overview' },
  { value: 'health', label: 'Connection checks' },
  { value: 'sync', label: 'Data sync' },
  { value: 'code', label: 'Code scanning' },
  { value: 'activity', label: 'Activity' },
];

/**
 * One AWS account in full: how it is connected, whether the connection holds,
 * how fresh its data is, whether its code is scanned, and who did what to it.
 * Everything here comes from the integration record itself.
 */
export function AccountDrawer({ account, setup, catalogue, counts, canManage, onClose, onChanged, onEditRegions, onRemove, onExposure }) {
  const [tab, setTab] = useState('overview');
  const { busy, sync, recheck } = useAccountActions(account, { onChanged, onEditRegions });
  const names = useMemo(() => new Map(catalogue.map((region) => [region.code, region.name])), [catalogue]);
  const meta = ACCOUNT_STATUS[account.status] ?? ACCOUNT_STATUS.action_needed;
  const blocked = syncBlockedReason(account);

  return (
    <Drawer
      open
      onClose={onClose}
      width="lg"
      eyebrow="AWS account"
      title={account.accountId}
      subtitle={
        <span className="flex flex-wrap items-center gap-1.5">
          <Tag tone={meta.tone === 'info' ? 'info' : meta.tone} size="sm" dot>
            {meta.label}
          </Tag>
          <span>
            {account.regions.length} {account.regions.length === 1 ? 'region' : 'regions'} + global IAM
          </span>
          {counts && <span>· {formatNumber(counts.identities)} identities · {formatNumber(counts.credentials)} credentials</span>}
        </span>
      }
      footer={
        canManage ? (
          <>
            <Button variant="ghost" icon={Trash2} className="mr-auto !text-critical" onClick={onRemove}>
              Remove account
            </Button>
            <Button variant="secondary" icon={ShieldCheck} loading={busy === 'check'} disabled={Boolean(busy)} onClick={recheck}>
              Re-check connection
            </Button>
            <span title={blocked || undefined}>
              <Button variant="primary" icon={RefreshCw} loading={busy === 'sync'} disabled={Boolean(blocked) || Boolean(busy)} onClick={sync}>
                Sync all accounts
              </Button>
            </span>
          </>
        ) : null
      }
    >
      <div className="shrink-0 border-b border-line px-4 sm:px-5">
        <Tabs tabs={TABS} value={tab} onChange={setTab} size="sm" />
      </div>
      <div className="flex flex-col gap-5 px-4 py-4 sm:px-5">
        {tab === 'overview' && (
          <OverviewTab account={account} setup={setup} names={names} counts={counts} canManage={canManage} onEditRegions={onEditRegions} />
        )}
        {tab === 'health' && <HealthTab account={account} />}
        {tab === 'sync' && <SyncTab account={account} blocked={blocked} />}
        {tab === 'code' && <CodeTab account={account} canManage={canManage} onExposure={onExposure} />}
        {tab === 'activity' && <ActivityTab account={account} />}
      </div>
    </Drawer>
  );
}

function OverviewTab({ account, setup, names, counts, canManage, onEditRegions }) {
  const roleName = account.roleArn.split('/').pop();
  return (
    <>
      <div className="flex items-center gap-3 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3.5 py-3">
        <AwsMark size="sm" />
        <p className="min-w-0 flex-1 text-[12.5px] leading-relaxed text-ink-2">
          {account.status === 'connected'
            ? 'We read this account through a read-only IAM role, and receive its CloudTrail events through an EventBridge rule. Nothing is written to the account.'
            : 'This account is not fully connected. The Connection checks tab says what failed and how to fix it.'}
        </p>
      </div>

      <div>
        <SectionLabel>Connection</SectionLabel>
        <DetailList className="mt-1">
          <DetailRow label="Account ID" mono>
            <CopyableValue value={account.accountId} />
          </DetailRow>
          <DetailRow label="Role">
            <span className="flex min-w-0 flex-col">
              <span className="font-mono text-[12.5px] text-ink">{roleName}</span>
              <CopyableValue value={account.roleArn} className="max-w-full text-[11.5px] text-ink-3" />
            </span>
          </DetailRow>
          {setup?.externalId && (
            <DetailRow label="External ID">
              <span className="flex flex-col">
                <CopyableValue value={setup.externalId} className="font-mono text-[12px]" />
                <span className="text-[11.5px] text-ink-3">Your workspace's own value; the role trusts only requests that carry it.</span>
              </span>
            </DetailRow>
          )}
          <DetailRow label="Permissions">
            {(setup?.managedPolicies?.length ? setup.managedPolicies : ['SecurityAudit', 'ViewOnlyAccess']).join(' + ')}
            <span className="block text-[11.5px] text-ink-3">AWS-managed, read-only</span>
          </DetailRow>
          <DetailRow label="Connected">
            {account.connectedAt ? (
              <span title={formatDateTime(account.connectedAt)}>{formatRelative(account.connectedAt)}</span>
            ) : (
              <span className="text-ink-3">Not yet</span>
            )}
          </DetailRow>
        </DetailList>
      </div>

      <div>
        <div className="flex items-center justify-between gap-2">
          <SectionLabel>Regions read</SectionLabel>
          {canManage && (
            <Button variant="link" size="sm" icon={Globe2} onClick={onEditRegions}>
              Edit regions
            </Button>
          )}
        </div>
        {account.regions.length === 0 ? (
          <p className="mt-1.5 text-[12.5px] text-medium">No region is chosen, so nothing regional is read.</p>
        ) : (
          <ul className="mt-1.5 grid gap-1.5 @min-[30rem]:grid-cols-2">
            {account.regions.map((code) => (
              <li key={code} className="flex items-center gap-2 rounded-[var(--radius-control)] border border-line px-2.5 py-1.5 text-[12.5px]">
                <span className="font-mono text-[11.5px] text-ink-3">{code}</span>
                <span className="truncate text-ink-2">{names.get(code) ?? code}</span>
              </li>
            ))}
          </ul>
        )}
        <p className="mt-1.5 text-[11.5px] text-ink-3">IAM roles and users are global, so they are always read.</p>
      </div>

      <div>
        <SectionLabel>Discovered in this account</SectionLabel>
        {counts ? (
          <div className="mt-1.5 grid grid-cols-2 gap-2">
            <Count label="Identities" value={counts.identities} />
            <Count label="Credentials" value={counts.credentials} />
          </div>
        ) : (
          <p className="mt-1.5 text-[12.5px] text-ink-3">Nothing discovered yet, or this account is outside your dashboard scope.</p>
        )}
      </div>
    </>
  );
}

function Count({ label, value }) {
  return (
    <div className="rounded-[var(--radius-control)] border border-line px-3 py-2">
      <p className="text-[11px] text-ink-3">{label}</p>
      <p data-numeric="" className="text-[18px] font-bold text-ink">
        {formatNumber(value)}
      </p>
    </div>
  );
}

function HealthTab({ account }) {
  const failing = account.checks.filter((check) => check.state === 'fail' || check.state === 'error');
  return (
    <>
      <p className="text-[12.5px] leading-relaxed text-ink-2">
        Three checks run when the account is connected and whenever you re-check it: can we sign in to your role, can the role read the account,
        and are its events arriving.
      </p>
      {failing.length > 0 && (
        <div className="rounded-[var(--radius-control)] border border-medium/35 bg-medium-soft px-3.5 py-3 text-[12.5px] leading-relaxed text-ink-2">
          <p className="font-semibold text-ink">
            {failing.length === 1 ? `"${failing[0].label}" did not pass.` : `${failing.length} checks did not pass.`}
          </p>
          {failing[0].fix && <p className="mt-1">{failing[0].fix}</p>}
          <p className="mt-1 text-[12px] text-ink-3">Fix it in the AWS console, then use Re-check connection below.</p>
        </div>
      )}
      <div className="rounded-[var(--radius-control)] border border-line px-3 py-1">
        <CheckList checks={account.checks} />
      </div>
      {account.lastVerifiedAt && (
        <p className="text-[12px] text-ink-3" title={formatDateTime(account.lastVerifiedAt)}>
          Last checked {formatRelative(account.lastVerifiedAt)}
          {account.lastVerifiedBy ? ` by ${account.lastVerifiedBy}` : ''}.
        </p>
      )}
    </>
  );
}

function SyncTab({ account, blocked }) {
  const state = SYNC[account.sync.status] ?? SYNC.idle;
  const Icon = state.icon;
  const { sync } = account;
  return (
    <>
      <div className="flex items-start gap-3 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3.5 py-3">
        <Icon aria-hidden="true" className={cn('mt-0.5 size-4.5 shrink-0', state.tone, state.spin && 'animate-spin')} />
        <div className="min-w-0">
          <p className={cn('text-[13px] font-semibold', state.tone)}>{state.label}</p>
          <p className="text-[12.5px] text-ink-2">{syncDetail(account, { withActor: true })}</p>
        </div>
      </div>
      <p className="text-[12.5px] leading-relaxed text-ink-2">
        A sync re-reads every allowed region and global IAM for all connected accounts, then scores what it finds. It always covers every
        account, not just this one. Sync all accounts starts one and shows its progress; the dashboards change when it finishes.
      </p>
      {blocked && <p className="text-[12px] text-ink-3">Sync is not available: {blocked.toLowerCase()}.</p>}
      <DetailList>
        <DetailRow label="Last requested">
          {sync.requestedAt ? `${formatDateTime(sync.requestedAt)}${sync.requestedBy ? ` by ${sync.requestedBy}` : ''}` : <span className="text-ink-3">Never</span>}
        </DetailRow>
        <DetailRow label="Started">{sync.startedAt ? formatDateTime(sync.startedAt) : <span className="text-ink-3">-</span>}</DetailRow>
        <DetailRow label="Last finished">{sync.lastSyncedAt ? formatDateTime(sync.lastSyncedAt) : <span className="text-ink-3">Not yet</span>}</DetailRow>
        {sync.lastError && <DetailRow label="Last error">{sync.lastError}</DetailRow>}
      </DetailList>
    </>
  );
}

const PLATFORMS = [
  { key: 'github', name: 'GitHub', Mark: GitHubMark },
  { key: 'codecommit', name: 'AWS CodeCommit', Mark: CodeCommitMark },
];

function CodeTab({ account, canManage, onExposure }) {
  if (!account.connectedAt) {
    return <p className="text-[12.5px] text-ink-3">Code scanning can be set up once this account is connected.</p>;
  }
  return (
    <>
      <p className="text-[12.5px] leading-relaxed text-ink-2">
        Exposed-credential scanning looks for access keys, tokens and passwords committed to code, in the repositories this account can reach.
      </p>
      <ul className="flex flex-col gap-2">
        {PLATFORMS.map((platform) => {
          const record = account.exposure[platform.key];
          const state = exposureState(record);
          return (
            <li key={platform.key} className="rounded-[var(--radius-control)] border border-line px-3.5 py-3">
              <div className="flex items-center gap-3">
                <platform.Mark size="sm" />
                <p className="min-w-0 flex-1 text-[13px] font-semibold text-ink">{platform.name}</p>
                <Tag tone={state === 'on' ? 'low' : state === 'failing' ? 'medium' : 'neutral'} size="sm" dot={state !== 'off'}>
                  {state === 'on' ? 'Scanning' : state === 'failing' ? 'Check failed' : 'Off'}
                </Tag>
              </div>
              {record.checks.length > 0 && (
                <ul className="mt-2 flex flex-col gap-1 text-[12px]">
                  {record.checks.map((check) => (
                    <li key={check.key} className="flex items-start gap-1.5">
                      <CheckCircle2 aria-hidden="true" className={cn('mt-0.5 size-3.5 shrink-0', check.state === 'pass' ? 'text-low' : 'text-medium')} />
                      <span className="text-ink-2">
                        {check.label}
                        {check.message ? <span className="text-ink-3"> - {check.message}</span> : null}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
              {record.verifiedAt && <p className="mt-1.5 text-[11.5px] text-ink-3">Checked {formatRelative(record.verifiedAt)}</p>}
            </li>
          );
        })}
      </ul>
      {canManage && onExposure && (
        <Button variant="secondary" icon={KeyRound} onClick={onExposure} className="self-start">
          Manage code scanning
        </Button>
      )}
    </>
  );
}

/** Who did what to this account, from the timestamps the record keeps. */
function ActivityTab({ account }) {
  const events = [
    account.connectedAt && { at: account.connectedAt, text: 'Account connected - all three checks passed' },
    account.lastVerifiedAt && {
      at: account.lastVerifiedAt,
      text: `Connection checked${account.status === 'connected' ? ' - healthy' : ' - needs attention'}`,
      by: account.lastVerifiedBy,
    },
    account.sync.requestedAt && { at: account.sync.requestedAt, text: 'Sync requested', by: account.sync.requestedBy },
    account.sync.startedAt && { at: account.sync.startedAt, text: 'Sync started' },
    account.sync.lastSyncedAt && { at: account.sync.lastSyncedAt, text: account.sync.status === 'failed' ? 'Sync failed' : 'Sync finished' },
    account.exposure.github.verifiedAt && {
      at: account.exposure.github.verifiedAt,
      text: `GitHub scanning ${account.exposure.github.connected ? 'switched on' : 'checked - not on'}`,
      by: account.exposure.updatedBy,
    },
    account.exposure.codecommit.verifiedAt && {
      at: account.exposure.codecommit.verifiedAt,
      text: `CodeCommit scanning ${account.exposure.codecommit.connected ? 'switched on' : 'checked - not on'}`,
      by: account.exposure.updatedBy,
    },
  ]
    .filter(Boolean)
    .sort((a, b) => String(b.at).localeCompare(String(a.at)));

  if (events.length === 0) return <p className="text-[12.5px] text-ink-3">Nothing has happened to this account yet.</p>;
  return (
    <>
      <ol className="relative flex flex-col gap-3 border-l border-line pl-4">
        {events.map((event) => (
          <li key={`${event.at}-${event.text}`} className="relative">
            <span aria-hidden="true" className="absolute top-1.5 -left-[21px] size-2 rounded-full bg-brand ring-2 ring-surface" />
            <p className="text-[12.5px] text-ink">{event.text}</p>
            <p className="text-[11.5px] text-ink-3" title={formatDateTime(event.at)}>
              {formatRelative(event.at)}
              {event.by ? ` · ${event.by}` : ''}
            </p>
          </li>
        ))}
      </ol>
      <p className="text-[11.5px] text-ink-3">The latest of each kind of change is kept with the account.</p>
    </>
  );
}
