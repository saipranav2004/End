import { useEffect, useMemo, useState } from 'react';
import { Database, SlidersHorizontal } from 'lucide-react';
import { useWorkspace } from '../../app/WorkspaceContext';
import { INTEGRATIONS_PREVIEW } from '../../lib/api/integrationsPreview';
import { fetchScopeOptions, updateScopeSettings } from '../../lib/api/overview';
import { useMutation, useQuery } from '../../lib/hooks';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Modal } from '../../ui/Overlay';
import { SectionLabel } from '../../ui/Panel';
import { Skeleton } from '../../ui/Skeleton';
import { InlineError } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';
import { cn } from '../../ui/cn';

/**
 * What the figures below are "as of", and over what.
 *
 * Discovery answers from its latest scan, scoped by the accounts and regions
 * this user saved - so the strip states both: when discovery last synced, and
 * the scope in force, with the control to change it. A stale snapshot or a
 * narrowed scope is never mistaken for the whole estate.
 */
export function ScopeStrip({ scope, lastScanAt, onScopeSaved }) {
  const { integration } = useWorkspace();
  const [editing, setEditing] = useState(false);

  /* The most recent completed sync across connected accounts. The discovery
     API serves the latest scan; this is when that scan landed. */
  const sync = useMemo(() => {
    /* A preview build's accounts are samples; real figures never mix with them. */
    const accounts = INTEGRATIONS_PREVIEW ? [] : integration?.accounts ?? [];
    let last = null;
    let running = 0;
    let queued = 0;
    for (const account of accounts) {
      const at = account.sync?.lastSyncedAt;
      if (at && (!last || new Date(at) > new Date(last))) last = at;
      if (account.sync?.status === 'running') running += 1;
      if (account.sync?.status === 'queued') queued += 1;
    }
    const newest = lastScanAt || last;
    return { last: newest, ageDays: newest ? (Date.now() - new Date(newest).getTime()) / 86_400_000 : null, running, queued, known: accounts.length > 0 };
  }, [integration, lastScanAt]);

  if (scope.isLoading && !scope.data) {
    return (
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-[var(--radius-panel)] border border-line bg-surface px-4 py-3" aria-busy="true">
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="flex flex-col gap-1.5">
            <Skeleton className="h-2.5 w-16 rounded" />
            <Skeleton className="h-3.5 w-24 rounded" />
          </div>
        ))}
      </div>
    );
  }

  if (scope.isError && !scope.data) {
    return (
      <div className="flex items-center gap-2.5 rounded-[var(--radius-panel)] border border-line bg-surface px-4 py-3 text-[12.5px] text-ink-3">
        <Database aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1">
          Your data scope could not be loaded, so the figures below cannot be attributed to a set of
          accounts and regions.
        </span>
        <Button variant="ghost" size="sm" onClick={scope.refetch}>
          Retry
        </Button>
      </div>
    );
  }

  const settings = scope.data;
  const isDefault = settings.accountsDefault && settings.regionsDefault;

  const { ageDays } = sync;
  const dot = ageDays === null ? 'bg-ink-3' : ageDays <= 1 ? 'bg-low' : ageDays <= 7 ? 'bg-ink-3' : 'bg-medium';
  const syncText = sync.last
    ? `Synced ${formatRelative(sync.last)}`
    : sync.running
      ? 'Syncing now'
      : sync.queued
        ? 'Sync queued'
        : sync.known
          ? 'Not synced yet'
          : 'No sync yet';
  const count = (list, one) => `${formatNumber(list.length)} ${one}${list.length === 1 ? '' : 's'}`;

  /* One quiet line, not a card: when the figures are from, and what they cover. */
  return (
    <>
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-[12.5px] text-ink-2">
        <span className="inline-flex items-center gap-1.5 font-medium text-ink" title={sync.last ? formatDateTime(sync.last) : undefined}>
          <span aria-hidden="true" className={cn('size-2 rounded-full', dot)} />
          {syncText}
        </span>
        <span aria-hidden="true" className="text-ink-3">
          ·
        </span>
        <span title={settings.accounts.join('\n')} data-numeric="">
          {settings.accounts.length ? count(settings.accounts, 'account') : 'No accounts selected'}
        </span>
        <span aria-hidden="true" className="text-ink-3">
          ·
        </span>
        <span title={settings.regions.join('\n')} data-numeric="">
          {settings.regions.length ? count(settings.regions, 'region') : 'No regions selected'}
        </span>
        {isDefault && <span className="text-ink-3">(all)</span>}
        {(sync.running > 0 || sync.queued > 0) && (
          <Tag tone="info" dot size="sm">
            {sync.running > 0 ? 'Sync running' : 'Sync queued'}
          </Tag>
        )}
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="inline-flex items-center gap-1 rounded font-medium text-brand hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand"
        >
          <SlidersHorizontal aria-hidden="true" className="size-3.5" />
          Change scope
        </button>
      </div>

      <ScopeEditor
        open={editing}
        current={settings}
        onClose={() => setEditing(false)}
        onSaved={() => {
          setEditing(false);
          onScopeSaved?.();
        }}
      />
    </>
  );
}

/**
 * Choose the accounts and regions discovery figures cover. Saved per user by
 * the API; an empty list would hide everything, so at least one of each is
 * required.
 */
function ScopeEditor({ open, current, onClose, onSaved }) {
  const { notify } = useToast();
  const options = useQuery((signal) => fetchScopeOptions(signal), [open], { enabled: open });
  const save = useMutation((input) => updateScopeSettings(input));
  const [accounts, setAccounts] = useState(() => new Set());
  const [regions, setRegions] = useState(() => new Set());

  useEffect(() => {
    if (!open) return;
    setAccounts(new Set(current.accounts));
    setRegions(new Set(current.regions));
    save.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, current]);

  const toggle = (setter) => (value) =>
    setter((prev) => {
      const next = new Set(prev);
      if (next.has(value)) next.delete(value);
      else next.add(value);
      return next;
    });

  const accountOptions = useMemo(() => withSelected(options.data?.accounts, current.accounts), [options.data, current]);
  const regionOptions = useMemo(() => withSelected(options.data?.regions, current.regions), [options.data, current]);

  const invalid = accounts.size === 0 || regions.size === 0;

  const onSave = async () => {
    const result = await save.mutate({ accounts: [...accounts].sort(), regions: [...regions].sort() });
    if (!result.ok) return;
    notify({
      variant: 'success',
      title: 'Scope saved',
      description: `Inventory figures now cover ${accounts.size} account${accounts.size === 1 ? '' : 's'} in ${regions.size} region${regions.size === 1 ? '' : 's'}.`,
    });
    onSaved();
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      icon={SlidersHorizontal}
      title="Data scope"
      description="Which AWS accounts and regions your inventory, credential and dashboard figures cover. Saved for you only; posture and behaviour figures are not affected."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" onClick={onSave} loading={save.pending} disabled={invalid || options.isLoading}>
            Save scope
          </Button>
        </>
      }
    >
      {options.isError && !options.data ? (
        <InlineError error={options.error} onRetry={options.refetch} label="Accounts and regions unavailable" />
      ) : options.isLoading && !options.data ? (
        <div className="flex flex-col gap-2" aria-busy="true">
          {Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className="h-6 w-full rounded" />
          ))}
        </div>
      ) : (
        <div className="flex flex-col gap-5">
          <ChoiceGroup
            label="Accounts"
            options={accountOptions}
            selected={accounts}
            onToggle={toggle(setAccounts)}
            onAll={() => setAccounts(new Set(accountOptions.map((o) => o.id)))}
            mono
            wide
          />
          <ChoiceGroup
            label="Regions"
            options={regionOptions}
            selected={regions}
            onToggle={toggle(setRegions)}
            onAll={() => setRegions(new Set(regionOptions.map((o) => o.id)))}
            mono
          />
          {invalid && (
            <p className="text-[12px] text-critical" role="status">
              Choose at least one account and one region - an empty scope would hide every figure.
            </p>
          )}
          {save.error && <InlineError error={save.error} label="Could not save" />}
        </div>
      )}
    </Modal>
  );
}

/* Saved values the options list no longer carries stay visible, so they can be unticked. */
function withSelected(list = [], selected = []) {
  const out = [...list];
  for (const id of selected) {
    if (!out.some((option) => option.id === id)) out.push({ id, identities: null });
  }
  return out.sort((a, b) => String(a.id).localeCompare(String(b.id)));
}

/* `wide`: one option per line, for options that carry more than one count. */
function ChoiceGroup({ label, options, selected, onToggle, onAll, mono, wide }) {
  const allOn = options.length > 0 && options.every((option) => selected.has(option.id));
  return (
    <fieldset>
      <div className="flex items-center justify-between gap-3">
        <legend className="contents">
          <SectionLabel>{label}</SectionLabel>
        </legend>
        {!allOn && options.length > 1 && (
          <Button variant="ghost" size="sm" onClick={onAll}>
            Select all
          </Button>
        )}
      </div>
      {options.length === 0 ? (
        <p className="mt-1.5 text-[12px] text-ink-3">Nothing has been discovered yet.</p>
      ) : (
        <div className={cn('mt-1.5 grid gap-0.5', !wide && 'sm:grid-cols-2')}>
          {options.map((option) => (
            <label
              key={option.id}
              className={cn(
                'flex cursor-pointer items-center gap-2.5 rounded-md px-1.5 py-1.5 text-[12.5px] transition-colors hover:bg-surface-2',
                selected.has(option.id) ? 'text-ink' : 'text-ink-2',
              )}
            >
              <input
                type="checkbox"
                checked={selected.has(option.id)}
                onChange={() => onToggle(option.id)}
                className="size-3.5 shrink-0 accent-[var(--t-brand)]"
              />
              <span className="flex min-w-0 flex-1 items-center gap-2">
                <span className={cn('min-w-0 truncate', mono && 'font-mono text-[12px]')}>{option.id}</span>
                {option.isDefault && (
                  <span title="In the default scope, used until you save your own">
                    <Tag tone="neutral" size="sm">
                      Default
                    </Tag>
                  </span>
                )}
              </span>
              <span data-numeric="" className="shrink-0 text-right text-[11.5px] text-ink-3">
                {option.identities === null
                  ? 'no data'
                  : [
                      `${formatNumber(option.identities)} identit${option.identities === 1 ? 'y' : 'ies'}`,
                      option.credentials !== undefined &&
                        `${formatNumber(option.credentials)} credential${option.credentials === 1 ? '' : 's'}`,
                    ]
                      .filter(Boolean)
                      .join(' · ')}
              </span>
            </label>
          ))}
        </div>
      )}
    </fieldset>
  );
}
