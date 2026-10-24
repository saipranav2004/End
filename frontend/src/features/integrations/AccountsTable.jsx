import { useMemo, useState } from 'react';
import {
  AlertTriangle,
  CheckCircle2,
  Clock3,
  Globe2,
  KeyRound,
  Loader2,
  MapPin,
  PanelRightOpen,
  RefreshCw,
  SearchX,
  ShieldCheck,
  Trash2,
  XCircle,
} from 'lucide-react';
import { removeAwsAccount, updateAwsAccountRegions, verifyAwsConnection } from '../../lib/api/integrations';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Field, Input, SearchInput } from '../../ui/Field';
import { Menu } from '../../ui/Menu';
import { Modal } from '../../ui/Overlay';
import { Panel } from '../../ui/Panel';
import { EmptyState, InlineError } from '../../ui/States';
import { SegmentedControl } from '../../ui/Tabs';
import { useSync } from '../sync/SyncContext';
import { useToast } from '../../ui/Toast';
import { cn } from '../../ui/cn';
import { ACCOUNT_STATUS } from './awsSetup';
import { AwsMark } from './parts';
import { ExposureBadges } from './ExposureBadges';
import { RegionPicker } from './RegionPicker';

const GRID =
  '@min-[66rem]:grid-cols-[minmax(0,1.5fr)_minmax(0,0.95fr)_minmax(0,0.9fr)_minmax(0,0.85fr)_minmax(0,1fr)_minmax(0,1fr)_auto]';

export const SYNC = {
  idle: { label: 'Not synced yet', icon: Clock3, tone: 'text-ink-3' },
  queued: { label: 'Queued', icon: Clock3, tone: 'text-info' },
  running: { label: 'Syncing', icon: Loader2, tone: 'text-brand', spin: true },
  succeeded: { label: 'Synced', icon: CheckCircle2, tone: 'text-low' },
  failed: { label: 'Sync failed', icon: XCircle, tone: 'text-critical' },
};

/** The line under the sync state: when it was asked for, started, or last finished. */
export function syncDetail(account, { withActor = false } = {}) {
  const { sync } = account;
  if (sync.status === 'failed' && sync.lastError) return sync.lastError;
  if (sync.status === 'running' && sync.startedAt) return `Started ${formatRelative(sync.startedAt)}`;
  if (sync.status === 'queued' && sync.requestedAt)
    return `Requested ${formatRelative(sync.requestedAt)}${withActor && sync.requestedBy ? ` by ${sync.requestedBy}` : ''}`;
  if (sync.lastSyncedAt) return `Last finished ${formatRelative(sync.lastSyncedAt)}`;
  return account.status !== 'connected' ? 'After the checks pass' : 'Starts automatically';
}

/** Why "Scan now" is not available, or '' when it is. */
export function syncBlockedReason(account) {
  if (account.status !== 'connected') return 'Fix the connection before scanning';
  if (account.regions.length === 0) return 'Choose regions to read first';
  return '';
}

/** Scan now and Re-check connection for one account, with their toasts. */
export function useAccountActions(account, { onChanged, onEditRegions } = {}) {
  const { notify } = useToast();
  const scan = useSync();
  const [busy, setBusy] = useState('');

  /* One scan covers the whole workspace: this opens the same progress panel as the
     top bar's Scan now, and follows a scan that is already running. */
  const sync = async () => {
    setBusy('sync');
    try {
      await scan.start();
      onChanged?.();
    } finally {
      setBusy('');
    }
  };

  const recheck = async () => {
    if (account.regions.length === 0) {
      notify({ variant: 'info', title: 'Choose regions first', description: 'This account has no regions to read yet.' });
      onEditRegions?.();
      return;
    }
    setBusy('check');
    try {
      const outcome = await verifyAwsConnection({ roleArn: account.roleArn, regions: account.regions });
      const failing = outcome.checks.find((check) => check.state !== 'pass');
      notify(
        outcome.status === 'connected'
          ? { variant: 'success', title: `${account.accountId} is healthy`, description: 'All three connection checks passed.' }
          : {
              variant: 'error',
              title: `${account.accountId}: ${failing?.label ?? 'a check'} did not pass`,
              description: failing?.fix || failing?.message,
            },
      );
      if (outcome.saved) onChanged?.();
    } catch (error) {
      notify({ variant: 'error', title: 'Could not run the checks', description: error?.message });
    } finally {
      setBusy('');
    }
  };

  return { busy, sync, recheck };
}

const FILTERS = [
  { value: 'all', label: 'All' },
  { value: 'healthy', label: 'Healthy' },
  { value: 'attention', label: 'Needs attention' },
];

/**
 * Every account the workspace has connected: its health, what it reads, what
 * it brought in, how fresh that is and whether its code is scanned - with the
 * actions an admin takes on one account in reach. A row opens the account's
 * detail panel.
 */
export function AccountsTable({ accounts, catalogue, inventory, canManage, onChanged, onRemoved, onOpen, onExposure, editing, setEditing, removing, setRemoving }) {
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState('all');
  const connectedCount = accounts.filter((account) => account.status === 'connected').length;
  const attentionCount = accounts.length - connectedCount;
  const names = useMemo(() => new Map(catalogue.map((region) => [region.code, region.name])), [catalogue]);

  const shown = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return accounts
      .filter((account) => (filter === 'healthy' ? account.status === 'connected' : filter === 'attention' ? account.status !== 'connected' : true))
      .filter((account) => !needle || account.accountId.includes(needle) || account.roleArn.toLowerCase().includes(needle))
      /* Problems first, then by account ID - the order someone fixing things reads in. */
      .sort((a, b) => Number(a.status === 'connected') - Number(b.status === 'connected') || a.accountId.localeCompare(b.accountId));
  }, [accounts, filter, query]);

  return (
    <Panel flush className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3.5 sm:px-5">
        <div>
          <h2 className="text-[14.5px] font-semibold text-ink">
            AWS accounts <span className="font-normal text-ink-3" data-numeric="">· {accounts.length}</span>
          </h2>
          <p className="mt-0.5 text-[12px] text-ink-3">Open an account for its connection details, checks, sync history and activity.</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {accounts.length > 1 && (
            <SegmentedControl
              label="Filter accounts"
              value={filter}
              onChange={setFilter}
              options={FILTERS.map((option) => ({
                ...option,
                label:
                  option.value === 'all'
                    ? `All (${accounts.length})`
                    : option.value === 'healthy'
                      ? `Healthy (${connectedCount})`
                      : `Needs attention (${attentionCount})`,
              }))}
            />
          )}
          {accounts.length > 3 && (
            <SearchInput value={query} onChange={setQuery} placeholder="Account ID or role" size="sm" className="w-48" aria-label="Search accounts" />
          )}
        </div>
      </div>
      <div
        aria-hidden="true"
        className={cn('hidden gap-4 border-b border-line bg-surface-2 px-5 py-2 text-[11px] font-medium text-ink-3 @min-[66rem]:grid', GRID)}
      >
        <span>Account</span>
        <span>Connection</span>
        <span>Regions</span>
        <span>Discovered</span>
        <span>Last sync</span>
        <span>Code scanning</span>
        <span className="w-[148px]" />
      </div>
      {shown.length === 0 ? (
        <EmptyState
          compact
          icon={SearchX}
          title="No account matches"
          description="Change the filter or the search."
          action={
            <Button variant="secondary" size="sm" onClick={() => { setFilter('all'); setQuery(''); }}>
              Show all accounts
            </Button>
          }
        />
      ) : (
        <ul className="divide-y divide-line">
          {shown.map((account) => (
            <AccountRow
              key={account.accountId}
              account={account}
              names={names}
              counts={inventory?.get(account.accountId) ?? null}
              inventoryKnown={Boolean(inventory)}
              canManage={canManage}
              onChanged={onChanged}
              onOpen={() => onOpen(account)}
              onEdit={() => setEditing(account)}
              onRemove={() => setRemoving(account)}
              onExposure={onExposure ? () => onExposure(account) : null}
            />
          ))}
        </ul>
      )}

      <EditRegionsModal
        account={editing}
        catalogue={catalogue}
        onClose={() => setEditing(null)}
        onSaved={() => {
          setEditing(null);
          onChanged?.();
        }}
      />
      <RemoveAccountModal
        account={removing}
        lastConnected={removing?.status === 'connected' && connectedCount === 1}
        onClose={() => setRemoving(null)}
        onRemoved={(accountId) => {
          setRemoving(null);
          onRemoved?.(accountId);
        }}
      />
    </Panel>
  );
}

function AccountRow({ account, names, counts, inventoryKnown, canManage, onChanged, onOpen, onEdit, onRemove, onExposure }) {
  const { busy, sync: runSync, recheck } = useAccountActions(account, { onChanged, onEditRegions: onEdit });
  const meta = ACCOUNT_STATUS[account.status] ?? ACCOUNT_STATUS.action_needed;
  const sync = SYNC[account.sync.status] ?? SYNC.idle;
  const SyncIcon = sync.icon;
  const passed = account.checks.filter((check) => check.state === 'pass').length;
  const failing = account.checks.find((check) => check.state === 'fail' || check.state === 'error');
  const blocked = syncBlockedReason(account);

  return (
    <li
      className="group cursor-pointer px-4 py-3.5 transition-colors hover:bg-surface-2/60 sm:px-5"
      onClick={(event) => {
        if (event.target.closest('button, a, [role="menu"]')) return;
        onOpen();
      }}
    >
      <div className={cn('grid items-center gap-x-4 gap-y-3', GRID)}>
        {/* Account */}
        <div className="flex min-w-0 items-center gap-3">
          <AwsMark size="sm" />
          <div className="min-w-0">
            <button
              type="button"
              onClick={onOpen}
              className="font-mono text-[13.5px] font-semibold text-ink hover:text-brand hover:underline"
              aria-label={`Open account ${account.accountId}`}
            >
              {account.accountId}
            </button>
            <p className="truncate text-[11.5px] text-ink-3" title={account.connectedAt ? formatDateTime(account.connectedAt) : undefined}>
              {account.connectedAt
                ? `Connected ${formatRelative(account.connectedAt)}`
                : 'Not connected yet'}
            </p>
          </div>
        </div>

        {/* Connection */}
        <div className="min-w-0">
          <span className={cn('inline-flex items-center gap-1.5 text-[12.5px] font-medium', { low: 'text-low', info: 'text-info' }[meta.tone] ?? 'text-medium')}>
            {account.status === 'connected' ? <CheckCircle2 aria-hidden="true" className="size-3.5" /> : <AlertTriangle aria-hidden="true" className="size-3.5" />}
            {meta.label}
          </span>
          <span className="mt-0.5 block truncate text-[11.5px] text-ink-3" title={failing ? `${failing.label}: ${failing.message}` : undefined}>
            {failing ? `Failing: ${failing.label}` : account.checks.length ? (passed === 3 ? 'All 3 checks pass' : `${passed} of 3 checks pass`) : 'Not checked yet'}
          </span>
        </div>

        {/* Regions */}
        <div className="min-w-0">
          {account.regions.length === 0 ? (
            <span className="inline-flex items-center gap-1.5 text-[12px] font-medium text-medium">
              <AlertTriangle aria-hidden="true" className="size-3.5" />
              No regions chosen
            </span>
          ) : (
            <div className="min-w-0" title={account.regions.map((code) => `${code} - ${names.get(code) ?? code}`).join('\n')}>
              <span className="flex min-w-0 items-center gap-1.5 text-[12.5px] font-medium text-ink">
                <MapPin aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
                <span className="truncate">
                  {account.regions.length === 1 ? names.get(account.regions[0]) ?? '1 region' : `${account.regions.length} regions`}
                </span>
              </span>
              <span className="mt-0.5 block truncate font-mono text-[11px] text-ink-3">{account.regions.join(', ')}</span>
            </div>
          )}
        </div>

        {/* Discovered */}
        <div className="min-w-0 text-[12px]">
          {counts ? (
            <>
              <span data-numeric="" className="block font-semibold text-ink">
                {formatNumber(counts.identities)} identities
              </span>
              <span data-numeric="" className="block text-[11.5px] text-ink-3">
                {formatNumber(counts.credentials)} credentials
              </span>
            </>
          ) : (
            <span className="text-[11.5px] text-ink-3" title={inventoryKnown ? 'Nothing discovered in this account yet, or it is outside your dashboard scope.' : undefined}>
              {inventoryKnown ? 'Nothing yet' : '-'}
            </span>
          )}
        </div>

        {/* Last sync */}
        <div className="min-w-0 text-[12px]" aria-live="polite">
          <span className={cn('inline-flex items-center gap-1.5 font-medium', sync.tone)}>
            <SyncIcon aria-hidden="true" className={cn('size-3.5', sync.spin && 'animate-spin')} />
            {sync.label}
          </span>
          <span className="block truncate text-[11.5px] text-ink-3" title={account.sync.lastError || syncDetail(account, { withActor: true })}>
            {syncDetail(account)}
          </span>
        </div>

        {/* Code scanning */}
        <div className="min-w-0">
          <ExposureBadges account={account} onOpen={canManage && onExposure && account.connectedAt ? onExposure : null} />
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1.5 @min-[66rem]:w-[148px] @min-[66rem]:justify-end">
          {canManage && (
            <span title={blocked || undefined}>
              <Button
                variant="secondary"
                size="sm"
                icon={RefreshCw}
                loading={busy === 'sync'}
                disabled={Boolean(blocked) || Boolean(busy)}
                onClick={runSync}
                aria-label={`Sync ${account.accountId} now`}
              >
                Scan now
              </Button>
            </span>
          )}
          <Menu
            outlined
            label={`More actions for ${account.accountId}`}
            items={[
              { key: 'open', label: 'View details', icon: PanelRightOpen, onSelect: onOpen },
              canManage ? { key: 'regions', label: 'Edit regions', icon: Globe2, onSelect: onEdit, disabled: Boolean(busy) } : null,
              canManage && onExposure && account.connectedAt
                ? { key: 'exposure', label: 'Code scanning…', icon: KeyRound, onSelect: onExposure, disabled: Boolean(busy) }
                : null,
              canManage ? { key: 'check', label: busy === 'check' ? 'Checking…' : 'Re-check connection', icon: ShieldCheck, onSelect: recheck, disabled: Boolean(busy) } : null,
              canManage ? { key: 'remove', label: 'Remove account', icon: Trash2, onSelect: onRemove, tone: 'critical', disabled: Boolean(busy) } : null,
            ].filter(Boolean)}
          />
        </div>
      </div>
    </li>
  );
}

/* ── Edit regions ──────────────────────────────────────────────────────── */

export function EditRegionsModal({ account, catalogue, onClose, onSaved }) {
  return (
    <Modal
      open={Boolean(account)}
      onClose={onClose}
      icon={Globe2}
      title={account ? `Regions for ${account.accountId}` : ''}
      description="We read regional resources only in the regions you allow."
    >
      {/* Keyed by account so every opening starts from that account's regions. */}
      {account && <EditRegionsBody key={account.accountId} account={account} catalogue={catalogue} onClose={onClose} onSaved={onSaved} />}
    </Modal>
  );
}

function EditRegionsBody({ account, catalogue, onClose, onSaved }) {
  const { notify } = useToast();
  const [regions, setRegions] = useState(account.regions);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  const before = new Set(account.regions);
  const after = new Set(regions);
  const added = regions.filter((code) => !before.has(code));
  const removed = account.regions.filter((code) => !after.has(code));
  const changed = added.length > 0 || removed.length > 0;

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      await updateAwsAccountRegions({ accountId: account.accountId, regions });
      notify({
        variant: 'success',
        title: 'Regions updated',
        description: added.length ? 'Scan now to read the new regions.' : 'The next sync reads only these regions.',
      });
      onSaved();
    } catch (failure) {
      setError(failure);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col gap-4 px-5 pb-5">
      <RegionPicker
        catalogue={catalogue}
        value={regions}
        onChange={setRegions}
        defaultExpanded
        error={regions.length === 0 ? 'Choose at least one region.' : undefined}
      />
      {changed && (
        <div className="rounded-[var(--radius-control)] bg-surface-2 px-3 py-2 text-[12px] leading-relaxed text-ink-2">
          {added.length > 0 && (
            <p>
              <span className="font-semibold text-low">Adding</span> <span className="font-mono">{added.join(', ')}</span> - create the
              EventBridge rule in {added.length === 1 ? 'that region' : 'those regions'} too.
            </p>
          )}
          {removed.length > 0 && (
            <p>
              <span className="font-semibold text-critical">Removing</span> <span className="font-mono">{removed.join(', ')}</span> - its
              records leave your dashboards.
            </p>
          )}
        </div>
      )}
      {error && <InlineError error={error} label="Not saved" />}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button variant="primary" onClick={save} loading={saving} disabled={!changed || regions.length === 0}>
          Save regions
        </Button>
      </div>
    </div>
  );
}

/* ── Remove ────────────────────────────────────────────────────────────── */

export function RemoveAccountModal({ account, lastConnected, onClose, onRemoved }) {
  return (
    <Modal
      open={Boolean(account)}
      onClose={onClose}
      icon={Trash2}
      tone="critical"
      title={account ? `Remove AWS account ${account.accountId}?` : ''}
      description="You can connect it again at any time."
    >
      {account && (
        <RemoveAccountBody key={account.accountId} account={account} lastConnected={lastConnected} onClose={onClose} onRemoved={onRemoved} />
      )}
    </Modal>
  );
}

function RemoveAccountBody({ account, lastConnected, onClose, onRemoved }) {
  const { notify } = useToast();
  const [typed, setTyped] = useState('');
  const [removing, setRemoving] = useState(false);
  const [error, setError] = useState(null);
  const confirmed = typed.trim() === account.accountId;

  const remove = async () => {
    setRemoving(true);
    setError(null);
    try {
      await removeAwsAccount({ accountId: account.accountId });
      notify({ variant: 'success', title: `${account.accountId} removed`, description: 'Delete the role and rule in that AWS account to finish.' });
      onRemoved(account.accountId);
    } catch (failure) {
      setError(failure);
      setRemoving(false);
    }
  };

  return (
    <div className="flex flex-col gap-4 px-5 pb-5">
      <ul className="flex flex-col gap-1.5 text-[12.5px] leading-relaxed text-ink-2">
        <li className="flex gap-2">
          <span aria-hidden="true" className="mt-[7px] size-1.5 shrink-0 rounded-full bg-ink-3" />
          We stop reading this account and refuse the events it forwards.
        </li>
        <li className="flex gap-2">
          <span aria-hidden="true" className="mt-[7px] size-1.5 shrink-0 rounded-full bg-ink-3" />
          Its identities and credentials leave your dashboards and lists.
        </li>
        <li className="flex gap-2">
          <span aria-hidden="true" className="mt-[7px] size-1.5 shrink-0 rounded-full bg-ink-3" />
          Nothing changes in the AWS account itself - delete the role and the EventBridge rule there.
        </li>
      </ul>
      {lastConnected && (
        <p className="flex items-start gap-2 rounded-[var(--radius-control)] border border-medium/30 bg-medium-soft px-3 py-2 text-[12.5px] text-ink-2">
          <AlertTriangle aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-medium" />
          This is your only connected account. Until you connect another, the console shows only the setup.
        </p>
      )}
      <Field label={`Type ${account.accountId} to confirm`} htmlFor="remove-account-confirm">
        <Input
          id="remove-account-confirm"
          value={typed}
          onChange={(event) => setTyped(event.target.value)}
          inputMode="numeric"
          autoComplete="off"
          spellCheck={false}
          className="font-mono"
          placeholder={account.accountId}
        />
      </Field>
      {error && <InlineError error={error} label="Not removed" />}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={onClose} disabled={removing}>
          Cancel
        </Button>
        <Button variant="danger" icon={Trash2} onClick={remove} loading={removing} disabled={!confirmed}>
          Remove account
        </Button>
      </div>
    </div>
  );
}
