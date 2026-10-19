import { useId, useMemo, useRef, useState } from 'react';
import { AlertCircle, ChevronDown, Search, X } from 'lucide-react';
import { cn } from '../../ui/cn';

const CHIP_LIMIT = 10;

/**
 * Choose the AWS regions we may read. Inline rather than a popover so it
 * works inside the setup's animated cards and inside a dialog without being
 * clipped. Native checkboxes keep it keyboard- and screen-reader-friendly.
 *
 *   catalogue  [{ code, name, group, optIn }] - from the API, in display order
 *   value      selected codes
 *   onChange   (codes) => void, always in catalogue order
 *
 * Opens on its own while nothing is chosen; once something is, it folds to
 * the chosen chips with "Change", so the setup stays short.
 */
export function RegionPicker({ catalogue, value, onChange, error, label = 'Regions to read', hint, defaultExpanded }) {
  const id = useId();
  const searchRef = useRef(null);
  const [query, setQuery] = useState('');
  const [expanded, setExpanded] = useState(defaultExpanded ?? value.length === 0);
  const [showAllChips, setShowAllChips] = useState(false);

  const selected = useMemo(() => new Set(value), [value]);
  const byCode = useMemo(() => new Map(catalogue.map((region) => [region.code, region])), [catalogue]);
  const order = useMemo(() => new Map(catalogue.map((region, index) => [region.code, index])), [catalogue]);
  const emit = (set) => onChange([...set].filter((code) => order.has(code)).sort((a, b) => order.get(a) - order.get(b)));

  const toggle = (code) => {
    const next = new Set(selected);
    if (next.has(code)) next.delete(code);
    else next.add(code);
    emit(next);
  };

  const term = query.trim().toLowerCase();
  const matches = useMemo(
    () =>
      catalogue.filter(
        (region) =>
          !term ||
          region.code.includes(term) ||
          region.name.toLowerCase().includes(term) ||
          region.group.toLowerCase().includes(term),
      ),
    [catalogue, term],
  );
  const groups = useMemo(() => {
    const map = new Map();
    for (const region of matches) {
      if (!map.has(region.group)) map.set(region.group, []);
      map.get(region.group).push(region);
    }
    return [...map.entries()];
  }, [matches]);

  const setMany = (codes, on) => {
    const next = new Set(selected);
    for (const code of codes) {
      if (on) next.add(code);
      else next.delete(code);
    }
    emit(next);
  };

  const chips = showAllChips ? value : value.slice(0, CHIP_LIMIT);
  const optInChosen = value.filter((code) => byCode.get(code)?.optIn);

  return (
    <fieldset className="min-w-0" aria-labelledby={`${id}-label`} aria-describedby={error ? `${id}-error` : `${id}-hint`}>
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <span id={`${id}-label`} className="text-[12.5px] font-medium tracking-[0.005em] text-ink-2">
          {label}
          <span className="ml-1 text-critical" aria-hidden="true">
            *
          </span>
        </span>
        <span className="text-[12px] text-ink-3" data-numeric="" aria-live="polite">
          {value.length === 0 ? 'None selected' : `${value.length} of ${catalogue.length} selected`}
        </span>
      </div>

      <div
        className={cn(
          'mt-1.5 rounded-[var(--radius-control)] border bg-surface transition-colors',
          error ? 'border-critical/70' : 'border-line-strong',
        )}
      >
        {/* What is chosen, always visible. */}
        <div className="flex flex-wrap items-center gap-1.5 px-2.5 py-2">
          {value.length === 0 ? (
            <span className="px-0.5 text-[12.5px] text-ink-3">Choose the regions your workloads run in.</span>
          ) : (
            chips.map((code) => (
              <span
                key={code}
                title={byCode.get(code)?.name ?? code}
                className="inline-flex items-center gap-1 rounded-md border border-line bg-surface-2 py-0.5 pr-0.5 pl-2 text-[12px] text-ink"
              >
                <span className="font-mono">{code}</span>
                <button
                  type="button"
                  onClick={() => toggle(code)}
                  aria-label={`Remove ${byCode.get(code)?.name ?? code}`}
                  className="grid size-5 place-items-center rounded text-ink-3 transition-colors hover:bg-surface-3 hover:text-ink"
                >
                  <X aria-hidden="true" className="size-3" />
                </button>
              </span>
            ))
          )}
          {value.length > CHIP_LIMIT && (
            <button
              type="button"
              onClick={() => setShowAllChips((v) => !v)}
              className="text-[12px] font-medium text-brand hover:underline"
            >
              {showAllChips ? 'Show fewer' : `+${value.length - CHIP_LIMIT} more`}
            </button>
          )}
          <button
            type="button"
            onClick={() => {
              setExpanded((v) => !v);
              if (!expanded) setTimeout(() => searchRef.current?.focus(), 0);
            }}
            aria-expanded={expanded}
            aria-controls={`${id}-list`}
            className="ml-auto inline-flex items-center gap-1 rounded-md px-2 py-1 text-[12.5px] font-medium text-brand transition-colors hover:bg-info-soft"
          >
            {expanded ? 'Done' : value.length ? 'Change' : 'Choose'}
            <ChevronDown aria-hidden="true" className={cn('size-3.5 transition-transform', expanded && 'rotate-180')} />
          </button>
        </div>

        {expanded && (
          <div id={`${id}-list`} className="border-t border-line">
            <div className="flex flex-wrap items-center gap-2 px-2.5 py-2">
              <div className="relative min-w-[12rem] flex-1">
                <Search aria-hidden="true" className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-ink-3" />
                <input
                  ref={searchRef}
                  type="search"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  onKeyDown={(event) => {
                    /* Enter picks the only match: "mumbai⏎" is the fastest way in. */
                    if (event.key === 'Enter') {
                      event.preventDefault();
                      if (matches.length === 1) {
                        toggle(matches[0].code);
                        setQuery('');
                      }
                    }
                  }}
                  placeholder="Search by name or code - e.g. Mumbai, eu-west"
                  aria-label="Search regions"
                  className="h-8 w-full rounded-md border border-line bg-surface-2 pr-2 pl-8 text-[12.5px] text-ink placeholder:text-ink-3/85 focus:border-brand focus:bg-surface focus:outline-none [&::-webkit-search-cancel-button]:hidden"
                />
              </div>
              <button
                type="button"
                onClick={() => setMany(matches.map((region) => region.code), true)}
                className="rounded-md px-2 py-1 text-[12px] font-medium text-ink-2 transition-colors hover:bg-surface-3 hover:text-ink"
              >
                {term ? 'Select matches' : 'Select all'}
              </button>
              <button
                type="button"
                onClick={() => (term ? setMany(matches.map((region) => region.code), false) : onChange([]))}
                disabled={value.length === 0}
                className="rounded-md px-2 py-1 text-[12px] font-medium text-ink-2 transition-colors hover:bg-surface-3 hover:text-ink disabled:opacity-40"
              >
                Clear
              </button>
            </div>

            <div className="max-h-64 overflow-y-auto overscroll-contain border-t border-line">
              {groups.length === 0 ? (
                <p className="px-3 py-6 text-center text-[12.5px] text-ink-3">No region matches &ldquo;{query.trim()}&rdquo;.</p>
              ) : (
                groups.map(([group, regions]) => {
                  const codes = regions.map((region) => region.code);
                  const chosen = codes.filter((code) => selected.has(code)).length;
                  const all = chosen === codes.length;
                  return (
                    <div key={group} role="group" aria-label={group}>
                      <div className="sticky top-0 z-[1] flex items-center justify-between gap-2 border-b border-line bg-surface-2/95 px-3 py-1.5 backdrop-blur-sm">
                        <span className="text-[11px] font-semibold tracking-[0.06em] text-ink-3 uppercase">{group}</span>
                        <button
                          type="button"
                          onClick={() => setMany(codes, !all)}
                          className="text-[11.5px] font-medium text-brand hover:underline"
                        >
                          {all ? 'Clear group' : 'Select group'}
                        </button>
                      </div>
                      <ul>
                        {regions.map((region) => {
                          const checked = selected.has(region.code);
                          return (
                            <li key={region.code}>
                              <label
                                className={cn(
                                  'flex cursor-pointer items-center gap-2.5 px-3 py-1.5 text-[12.5px] transition-colors hover:bg-surface-2',
                                  checked && 'bg-info-soft/60',
                                )}
                              >
                                <input
                                  type="checkbox"
                                  checked={checked}
                                  onChange={() => toggle(region.code)}
                                  className="size-3.5 shrink-0 accent-[var(--t-brand)]"
                                />
                                <span className="min-w-0 flex-1 truncate text-ink">{region.name}</span>
                                {region.optIn && (
                                  <span
                                    title="Disabled in an AWS account until its owner enables it"
                                    className="shrink-0 rounded border border-medium/30 bg-medium-soft px-1 text-[10.5px] font-medium text-medium"
                                  >
                                    opt-in
                                  </span>
                                )}
                                <span className="w-[7.5rem] shrink-0 text-right font-mono text-[11.5px] text-ink-3">{region.code}</span>
                              </label>
                            </li>
                          );
                        })}
                      </ul>
                    </div>
                  );
                })
              )}
            </div>
          </div>
        )}
      </div>

      {error ? (
        <p id={`${id}-error`} className="mt-1.5 flex items-start gap-1.5 text-[12px] font-medium text-critical" role="alert">
          <AlertCircle aria-hidden="true" className="mt-px size-3.5 shrink-0" />
          {error}
        </p>
      ) : (
        <p id={`${id}-hint`} className="mt-1.5 text-[12px] text-ink-3">
          {hint ?? 'We read regional resources only in these regions.'}
          {optInChosen.length > 0 && (
            <span className="text-medium"> {optInChosen.join(', ')} must be enabled in your AWS account.</span>
          )}
        </p>
      )}
    </fieldset>
  );
}
