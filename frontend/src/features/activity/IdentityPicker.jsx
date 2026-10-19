import { useEffect, useRef, useState } from 'react';
import { Check, ChevronDown, Search } from 'lucide-react';
import { fetchIdentities } from '../../lib/api/inventory';
import { useDebouncedValue, usePopover, useQuery } from '../../lib/hooks';
import { formatRelativeShort } from '../../lib/format';
import { Skeleton } from '../../ui/Skeleton';
import { cn } from '../../ui/cn';
import { identityTypeLabel } from '../identities/meta';

/**
 * Identity picker for the activity screen.
 *
 * CloudTrail events are served per identity (`/identities/{id}/events`), so
 * the screen starts by choosing one. The search runs on the server, over name,
 * ARN and tags of every identity in scope - not over a loaded page - and the
 * list leads with the most recently active, which is usually who the reader
 * is looking for.
 *
 * Placement comes from `usePopover`, so the list fits the space below the
 * trigger, or flips above it when that is roomier.
 */
export function IdentityPicker({ value, label, onChange }) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [active, setActive] = useState(0);
  const debounced = useDebouncedValue(search.trim(), 260);
  const searchRef = useRef(null);
  const { wrapperRef, triggerRef, panelProps } = usePopover(open, () => {
    setOpen(false);
    setSearch('');
  });

  const query = useQuery(
    (signal) => fetchIdentities({ search: debounced, page: 1, pageSize: 20, sortBy: 'last_activity_at', sortOrder: 'desc' }, signal),
    [debounced],
    { enabled: open },
  );
  const identities = query.data?.rows ?? [];

  useEffect(() => {
    if (open) searchRef.current?.focus();
  }, [open]);

  const close = () => {
    setOpen(false);
    setSearch('');
    setActive(0);
  };

  const choose = (identity) => {
    onChange(identity);
    close();
    triggerRef.current?.focus();
  };

  const onKeyDown = (event) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setActive((index) => Math.min(identities.length - 1, index + 1));
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      setActive((index) => Math.max(0, index - 1));
    } else if (event.key === 'Enter' && identities[active]) {
      event.preventDefault();
      choose(identities[active]);
    } else if (event.key === 'Escape') {
      close();
      triggerRef.current?.focus();
    }
  };

  return (
    <div ref={wrapperRef} className="relative min-w-0 basis-72">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => (open ? close() : setOpen(true))}
        aria-haspopup="listbox"
        aria-expanded={open}
        className={cn(
          'flex h-8 w-full min-w-0 items-center gap-2 rounded-[var(--radius-control)] border px-2.5 text-left text-[12.5px] transition-colors duration-150',
          open ? 'border-brand/55 bg-info-soft' : 'border-line-strong bg-surface hover:border-ink-3/50 hover:bg-surface-2',
        )}
      >
        <Search aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate">
          <span className="text-ink-3">Identity </span>
          <span className={cn('font-medium', value ? 'text-ink' : 'text-ink-3')}>{value ? label || 'Selected identity' : 'Choose one…'}</span>
        </span>
        <ChevronDown aria-hidden="true" className={cn('size-3.5 shrink-0 text-ink-3 transition-transform duration-200', open && 'rotate-180')} />
      </button>

      {open && (
        <div
          {...panelProps}
          className="animate-pop absolute left-0 z-40 flex w-[min(92vw,28rem)] flex-col rounded-[var(--radius-panel)] border border-line bg-surface shadow-lg"
        >
          <div className="shrink-0 border-b border-line bg-surface-2 p-2">
            <input
              ref={searchRef}
              type="text"
              value={search}
              onChange={(event) => {
                setSearch(event.target.value);
                setActive(0);
              }}
              onKeyDown={onKeyDown}
              placeholder="Search name, ARN or tag…"
              aria-label="Search identities"
              aria-controls="identity-picker-list"
              className="h-8 w-full rounded-[var(--radius-control)] border border-line-strong bg-surface px-2.5 text-[12.5px] text-ink placeholder:text-ink-3/85 focus:border-brand"
            />
          </div>

          <div id="identity-picker-list" role="listbox" aria-label="Identities" className="min-h-0 flex-1 overflow-y-auto p-1">
            {query.isLoading && !query.data && (
              <div className="flex flex-col gap-1 p-1.5" aria-hidden="true">
                {Array.from({ length: 6 }).map((_, index) => (
                  <Skeleton key={index} className="h-8 rounded-md" />
                ))}
              </div>
            )}

            {query.isError && !query.data && (
              <p className="px-2.5 py-3 text-[12px] text-critical">The identity list could not be loaded. {query.error?.message}</p>
            )}

            {query.data && identities.length === 0 && (
              <p className="px-2.5 py-3 text-[12px] text-ink-3">{debounced ? `No identity matches “${debounced}”.` : 'No identities in your scope.'}</p>
            )}

            {identities.map((identity, index) => {
              const selected = identity.id === value;
              return (
                <button
                  key={identity.id}
                  type="button"
                  role="option"
                  aria-selected={selected}
                  onMouseEnter={() => setActive(index)}
                  onClick={() => choose(identity)}
                  className={cn(
                    'flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors',
                    selected ? 'bg-info-soft' : index === active ? 'bg-surface-3' : 'hover:bg-surface-2',
                  )}
                >
                  <Check aria-hidden="true" className={cn('size-4 shrink-0', selected ? 'text-brand' : 'text-transparent')} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[13px] font-medium text-ink">{identity.name || identity.arn}</span>
                    <span className="block truncate text-[11px] text-ink-3" title={identity.arn}>
                      {identityTypeLabel(identity.identity_type)} · {identity.account_id}
                    </span>
                  </span>
                  <span className="shrink-0 text-[11px] text-ink-3">
                    {identity.last_activity_at ? formatRelativeShort(identity.last_activity_at) : 'no activity'}
                  </span>
                </button>
              );
            })}
          </div>

          {query.data && (
            <p className="shrink-0 border-t border-line bg-surface-2 px-2.5 py-2 text-[11px] leading-snug text-ink-3">
              {query.data.total > identities.length
                ? `Showing the ${identities.length} most recently active of ${query.data.total}. Type to narrow.`
                : 'Most recently active first.'}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
