import { useMemo, useState } from 'react';
import { ChevronDown, Crosshair, Search } from 'lucide-react';
import { usePopover } from '../../lib/hooks';
import { KIND_LABEL } from './graphTheme';
import { cn } from '../../ui/cn';

/**
 * Where the graph starts.
 *
 * Ranked, not alphabetical: identities trusted from another account first,
 * then publicly reachable ones, because those are the two things somebody
 * opens this screen to look at - and both are flags the inventory reports,
 * not a judgement this screen makes. Then every other identity, then the
 * credentials that no identity shares an ARN with (keys, secrets, roles
 * nothing runs as), which can be a starting point too.
 *
 * Typing filters on name and ARN, and that is all it needs to do.
 */
export function FocusPicker({ options, value, onChange, partial = false }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const { wrapperRef, triggerRef, placement, panelProps } = usePopover(open, () => setOpen(false));

  const current = options.find((option) => option.id === value);

  const groups = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const matches = needle
      ? options.filter(
          (option) =>
            option.name.toLowerCase().includes(needle) || String(option.arn ?? '').toLowerCase().includes(needle),
        )
      : options;
    const identities = matches.filter((o) => o.kind === 'identity');
    return [
      {
        key: 'external',
        label: 'Can be used from another account',
        rows: identities.filter((o) => o.externalTrust),
      },
      {
        key: 'public',
        label: 'Publicly accessible',
        rows: identities.filter((o) => o.publicPolicy && !o.externalTrust),
      },
      {
        key: 'rest',
        label: 'Identities',
        /* Capped: past sixty the list stops being a list and the filter is
           the faster route anyway. */
        rows: identities.filter((o) => !o.externalTrust && !o.publicPolicy).slice(0, 60),
      },
      {
        key: 'credentials',
        label: 'Credentials',
        rows: matches.filter((o) => o.kind === 'credential').slice(0, 40),
      },
    ].filter((group) => group.rows.length > 0);
  }, [options, query]);

  return (
    <div ref={wrapperRef} className="relative min-w-0">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((current2) => !current2)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className={cn(
          'flex h-8 min-w-0 max-w-[15rem] items-center gap-2 rounded-[var(--radius-control)] border px-2.5 text-left text-[12.5px] transition-colors duration-150',
          open
            ? 'border-brand/55 bg-info-soft'
            : 'border-line-strong bg-surface hover:border-ink-3/50 hover:bg-surface-2',
        )}
      >
        <Crosshair aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
        <span className="min-w-0 flex-1 truncate">
          <span className="font-medium text-ink">{current ? current.name : 'Starting point'}</span>
        </span>
        <ChevronDown
          aria-hidden="true"
          className={cn('size-3.5 shrink-0 text-ink-3 transition-transform duration-200', open && 'rotate-180')}
        />
      </button>

      {open && (
        <div
          {...panelProps}
          role="listbox"
          aria-label="Starting point"
          className={cn(
            'animate-pop absolute right-0 z-50 flex w-[19rem] flex-col overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface shadow-lg',
            placement === 'top' ? 'bottom-full mb-1.5' : 'top-full mt-1.5',
          )}
        >
          <div className="flex shrink-0 items-center gap-2 border-b border-line px-2.5 py-2">
            <Search aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
            <input
              autoFocus
              type="text"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Filter by name or AWS address (ARN)..."
              aria-label="Filter starting points"
              className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-3"
            />
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain py-1">
            {groups.length === 0 ? (
              <p className="px-3 py-4 text-center text-[12px] text-ink-3">Nothing matches that name. Try part of it, or the AWS address (ARN).</p>
            ) : (
              groups.map((group) => (
                <div key={group.key}>
                  <p className="px-2.5 pt-2 pb-1 text-[10px] font-semibold tracking-[0.1em] text-ink-3 uppercase">
                    {group.label}
                  </p>
                  {group.rows.map((row) => (
                    <button
                      key={row.id}
                      type="button"
                      role="option"
                      aria-selected={row.id === value}
                      onClick={() => {
                        onChange(row.id);
                        setOpen(false);
                        setQuery('');
                      }}
                      className={cn(
                        'flex w-full items-center gap-2 px-2.5 py-1.5 text-left hover:bg-surface-3',
                        row.id === value && 'bg-info-soft',
                      )}
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[12.5px] text-ink">{row.name}</span>
                        <span className="block truncate text-[10.5px] text-ink-3">
                          {row.typeLabel || KIND_LABEL[row.kind] || row.kind}
                          {row.accountName ? ` · ${row.accountName}` : ''}
                        </span>
                      </span>
                    </button>
                  ))}
                </div>
              ))
            )}
          </div>
          {partial && (
            <p className="shrink-0 border-t border-line px-2.5 py-1.5 text-[10.5px] text-ink-3">
              The first {options.length} records in scope are listed. Search the Identities page to find
              the rest.
            </p>
          )}
        </div>
      )}
    </div>
  );
}
