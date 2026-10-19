import { useEffect, useMemo, useState } from 'react';
import { Database, Globe2, SlidersHorizontal } from 'lucide-react';
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
export function ScopeStrip({ scope, kpis, onScopeSaved }) {
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
    return { last: kpis?.lastScanTime || last, running, queued, known: accounts.length > 0 };
  }, [integration, kpis]);

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

  const facts = [
    {
      label: 'Accounts',
      value: settings.accounts.length ? settings.accounts.join(', ') : 'None selected',
      mono: settings.accounts.length > 0,
    },
    {
      label: 'Regions',
      value: settings.regions.length ? settings.regions.join(', ') : 'None selected',
      mono: settings.regions.length > 0,
    },
    kpis && { label: 'Accounts with data', value: formatNumber(kpis.accountsScanned) },
    {
      label: 'Last discovery sync',
      value: sync.last
        ? formatRelative(sync.last)
        : sync.running
          ? 'Running now'
          : sync.queued
            ? 'Queued'
            : sync.known
            ? 'Not synced yet'
            : '-',
      title: sync.last ? formatDateTime(sync.last) : undefined,
    },
  ].filter(Boolean);

  return (
    <>
      <div className="flex flex-wrap items-center gap-x-7 gap-y-3 rounded-[var(--radius-panel)] border border-line bg-surface px-4 py-3">
        <div className="flex items-center gap-2.5">
          <span className="grid size-8 shrink-0 place-items-center rounded-md border border-line bg-surface-2 text-brand">
            <Globe2 aria-hidden="true" className="size-4" />
          </span>
          <div className="min-w-0">
            <p className="text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">Data scope</p>
            <p className="text-[12.5px] font-semibold text-ink">
              {isDefault ? 'All accounts and regions' : 'Your saved scope'}
              <span className="font-normal text-ink-3"> · applies to inventory figures</span>
            </p>
          </div>
        </div>

        <dl className="flex min-w-0 flex-wrap items-start gap-x-7 gap-y-3">
          {facts.map((fact) => (
            <div key={fact.label} className="min-w-0">
              <dt className="text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">{fact.label}</dt>
              <dd
                className={cn('mt-0.5 max-w-56 truncate text-[12.5px] font-medium text-ink', fact.mono && 'font-mono')}
                data-numeric=""
                title={fact.title ?? String(fact.value)}
              >
                {fact.value}
              </dd>
            </div>
          ))}
        </dl>

        <div className="ml-auto flex items-center gap-2">
          {(sync.running > 0 || sync.queued > 0) && (
            <Tag tone="info" dot size="sm">
              {sync.running > 0 ? 'Sync running' : 'Sync queued'}
            </Tag>
          )}
          <Button variant="secondary" size="sm" icon={SlidersHorizontal} onClick={() => setEditing(true)}>
            Change scope
          </Button>
        </div>
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

function ChoiceGroup({ label, options, selected, onToggle, onAll, mono }) {
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
        <div className="mt-1.5 grid gap-0.5 sm:grid-cols-2">
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
              <span className={cn('min-w-0 flex-1 truncate', mono && 'font-mono text-[12px]')}>{option.id}</span>
              <span data-numeric="" className="shrink-0 text-[11.5px] text-ink-3">
                {option.identities === null ? 'no data' : `${formatNumber(option.identities)} identities`}
              </span>
            </label>
          ))}
        </div>
      )}
    </fieldset>
  );
}
