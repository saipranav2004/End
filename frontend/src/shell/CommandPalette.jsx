import { useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { CornerDownLeft, Fingerprint, KeyRound, Search } from 'lucide-react';
import { fetchCredentials, fetchIdentities } from '../lib/api/inventory';
import { useDebouncedValue, useQuery, useScrollLock } from '../lib/hooks';
import { useAccess } from '../app/useAccess';
import { useWorkspace } from '../app/WorkspaceContext';
import { navGroupsFor } from './navigation';
import { identityRoute } from '../lib/identityLinks';
import { credentialTypeMeta, identityTypeLabel } from '../features/identities/meta';
import { Skeleton } from '../ui/Skeleton';
import { cn } from '../ui/cn';

/**
 * Command palette: jump to a section, find an identity by name, ARN or tag,
 * or find a credential by name or ARN - an access key id, a secret, a role -
 * using the same server-side `search` the Identities and Credentials screens
 * use. Sections are the ones the navigation shows this user: filtered by
 * permission and by what the workspace has switched on.
 */
export function CommandPalette({ open, onClose }) {
  const navigate = useNavigate();
  const [term, setTerm] = useState('');
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef(null);
  const debounced = useDebouncedValue(term.trim(), 260);

  useScrollLock(open);

  useEffect(() => {
    if (open) {
      setTerm('');
      setActiveIndex(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  /* The API searches with ILIKE; one character would match nearly everything,
     so shorter terms only filter the section list. */
  const searchable = open && debounced.length >= 2;
  const identityQuery = useQuery(
    (signal) => fetchIdentities({ search: debounced, page: 1, pageSize: 6, sortBy: 'name', sortOrder: 'asc' }, signal),
    [debounced],
    { enabled: searchable },
  );

  const credentialQuery = useQuery(
    (signal) => fetchCredentials({ search: debounced, page: 1, pageSize: 6, sortBy: 'name', sortOrder: 'asc' }, signal),
    [debounced],
    { enabled: searchable },
  );

  const { can, role } = useAccess();
  /* The same items as the sidebar: by permission, and a feature's screens
     only once the workspace has switched it on. */
  const { exposureEnabled } = useWorkspace();
  const navMatches = useMemo(() => {
    const needle = term.trim().toLowerCase();
    const allowed = navGroupsFor(can, role, { exposure: exposureEnabled }).flatMap((group) =>
      group.items.map((item) => ({ ...item, group: group.label })),
    );
    if (!needle) return allowed;
    return allowed.filter(
      (item) => item.label.toLowerCase().includes(needle) || item.group.toLowerCase().includes(needle),
    );
  }, [term, can, role, exposureEnabled]);

  const identityMatches = useMemo(() => (searchable ? identityQuery.data?.rows ?? [] : []), [identityQuery.data, searchable]);
  const credentialMatches = useMemo(
    () => (searchable ? credentialQuery.data?.rows ?? [] : []),
    [credentialQuery.data, searchable],
  );

  const options = useMemo(
    () => [
      ...navMatches.map((item) => ({
        id: `nav:${item.to}`,
        kind: 'nav',
        label: item.label,
        hint: item.group,
        icon: item.icon,
        run: () => navigate(item.to),
      })),
      ...identityMatches.map((identity) => ({
        id: `identity:${identity.id}`,
        kind: 'identity',
        label: identity.name || identity.arn,
        hint: `Identity · ${identityTypeLabel(identity.identity_type)}`,
        sub: identity.arn,
        icon: Fingerprint,
        run: () => navigate(identityRoute(identity.id)),
      })),
      ...credentialMatches.map((credential) => ({
        id: `credential:${credential.id}`,
        kind: 'credential',
        label: credential.name || credential.arn,
        hint: `Credential · ${credentialTypeMeta(credential.credential_type).label}`,
        sub: credential.arn,
        icon: KeyRound,
        run: () =>
          navigate(
            `/credentials?search=${encodeURIComponent(credential.name || credential.arn)}&credential=${encodeURIComponent(credential.id)}`,
          ),
      })),
    ],
    [navMatches, identityMatches, credentialMatches, navigate],
  );

  useEffect(() => {
    setActiveIndex(0);
  }, [term, identityMatches.length, credentialMatches.length]);

  if (!open) return null;

  const commit = (option) => {
    if (!option) return;
    option.run();
    onClose();
  };

  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      onClose();
      return;
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setActiveIndex((index) => Math.min(options.length - 1, index + 1));
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault();
      setActiveIndex((index) => Math.max(0, index - 1));
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      commit(options[activeIndex]);
    }
  };

  const searching = searchable && (identityQuery.isLoading || credentialQuery.isLoading);

  return createPortal(
    <div className="fixed inset-0 z-[85] flex items-start justify-center px-4 pt-[12vh]">
      <button
        type="button"
        aria-label="Close command palette"
        onClick={onClose}
        className="animate-fade absolute inset-0 bg-[var(--t-overlay)] backdrop-blur-[3px]"
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="animate-pop relative w-full max-w-xl overflow-hidden rounded-[16px] border border-line bg-surface shadow-lg"
      >
        <div className="flex items-center gap-3 border-b border-line px-4">
          <Search aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
          <input
            ref={inputRef}
            value={term}
            onChange={(event) => setTerm(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Search sections, identities or credentials…"
            aria-label="Search sections, identities and credentials"
            aria-controls="command-results"
            className="h-13 min-w-0 flex-1 bg-transparent text-[14px] text-ink outline-none placeholder:text-ink-3"
          />
          <kbd className="hidden shrink-0 rounded border border-line bg-surface-2 px-1.5 py-0.5 font-mono text-[10.5px] text-ink-3 sm:block">
            Esc
          </kbd>
        </div>

        <div id="command-results" role="listbox" className="max-h-[52vh] overflow-y-auto p-1.5">
          {options.length === 0 && !searching && (
            <p className="px-3 py-8 text-center text-[12.5px] text-ink-3">
              Nothing matches “{term}”.{term.trim().length < 2 ? ' Type at least two characters to search identities and credentials.' : ''}
            </p>
          )}

          {options.map((option, index) => (
            <button
              key={option.id}
              type="button"
              role="option"
              aria-selected={index === activeIndex}
              onMouseEnter={() => setActiveIndex(index)}
              onClick={() => commit(option)}
              className={cn(
                'flex w-full items-center gap-3 rounded-md px-2.5 py-2 text-left transition-colors',
                index === activeIndex ? 'bg-surface-3' : 'hover:bg-surface-2',
              )}
            >
              <option.icon aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[13px] font-medium text-ink">{option.label}</span>
                {option.sub && (
                  <span className="block truncate font-mono text-[11px] text-ink-3">{option.sub}</span>
                )}
              </span>
              <span className="shrink-0 text-[11px] text-ink-3">{option.hint}</span>
              {index === activeIndex && (
                <CornerDownLeft aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
              )}
            </button>
          ))}

          {searching && (
            <div className="flex flex-col gap-2 px-2.5 py-2">
              {Array.from({ length: 3 }).map((_, index) => (
                <div key={index} className="flex items-center gap-3">
                  <Skeleton className="size-4 rounded" />
                  <Skeleton className="h-3 flex-1 rounded" />
                </div>
              ))}
            </div>
          )}

          {searchable && (identityQuery.isError || credentialQuery.isError) && (
            <p role="alert" className="px-3 py-3 text-[12px] text-critical">
              Search failed: {(identityQuery.error ?? credentialQuery.error)?.message}
            </p>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
