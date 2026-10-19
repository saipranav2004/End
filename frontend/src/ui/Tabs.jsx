import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { cn } from './cn';

/**
 * Underlined tab set with a sliding indicator. Keyboard support follows the
 * WAI-ARIA tabs pattern (arrow keys move, and the panel is labelled by its tab).
 */
export function Tabs({ tabs, value, onChange, className, size = 'md' }) {
  const baseId = useId();
  const listRef = useRef(null);
  const [indicator, setIndicator] = useState(null);

  /* One indicator that slides between tabs, rather than a border per tab -
     the movement is what tells you which sibling view you landed on. */
  const measure = useCallback(() => {
    const list = listRef.current;
    if (!list) return;
    const active = list.querySelector('[aria-selected="true"]');
    if (!active) {
      setIndicator(null);
      return;
    }
    setIndicator({ left: active.offsetLeft, width: active.offsetWidth });
  }, []);

  /* What the tabs actually render, not just how many there are. Counts arrive
     from a separate query after the first paint, and each one that lands widens
     its tab and shifts every tab after it. Keying the measurement on
     `tabs.length` alone missed that entirely, which left the indicator sitting
     under wherever the active tab used to be. */
  const signature = tabs
    .map((tab) => `${tab.value}:${tab.label}:${tab.count ?? ''}`)
    .join('|');

  useLayoutEffect(measure, [measure, value, signature]);

  useEffect(() => {
    const list = listRef.current;
    if (!list || typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver(measure);
    /* Every tab, not only the list. The list is stretched by its parent, so its
       own box does not change when a child's width does - observing it alone
       never fired. */
    observer.observe(list);
    for (const tab of list.querySelectorAll('[role="tab"]')) observer.observe(tab);
    return () => observer.disconnect();
  }, [measure, signature]);

  /* Web fonts land after the first paint and change every text width with them. */
  useEffect(() => {
    if (typeof document === 'undefined' || !document.fonts?.ready) return undefined;
    let live = true;
    document.fonts.ready.then(() => {
      if (live) measure();
    });
    return () => {
      live = false;
    };
  }, [measure]);

  const onKeyDown = (event) => {
    const currentIndex = tabs.findIndex((tab) => tab.value === value);
    if (currentIndex === -1) return;
    let nextIndex = null;
    if (event.key === 'ArrowRight') nextIndex = (currentIndex + 1) % tabs.length;
    if (event.key === 'ArrowLeft') nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
    if (event.key === 'Home') nextIndex = 0;
    if (event.key === 'End') nextIndex = tabs.length - 1;
    if (nextIndex === null) return;
    event.preventDefault();
    onChange(tabs[nextIndex].value);
    document.getElementById(`${baseId}-tab-${tabs[nextIndex].value}`)?.focus();
  };

  return (
    <div
      ref={listRef}
      role="tablist"
      onKeyDown={onKeyDown}
      className={cn('relative flex min-w-0 items-stretch gap-1 overflow-x-auto', className)}
    >
      {indicator && (
        <span
          aria-hidden="true"
          className="absolute bottom-0 h-[2px] rounded-full bg-brand transition-[left,width] duration-260 ease-[var(--ease-out-quint)]"
          style={{ left: indicator.left, width: indicator.width }}
        />
      )}
      {tabs.map((tab) => {
        const active = tab.value === value;
        return (
          <button
            key={tab.value}
            id={`${baseId}-tab-${tab.value}`}
            role="tab"
            type="button"
            aria-selected={active}
            tabIndex={active ? 0 : -1}
            onClick={() => onChange(tab.value)}
            className={cn(
              'relative inline-flex shrink-0 items-center gap-2 px-3 font-medium transition-colors duration-150',
              size === 'sm' ? 'h-9 text-[12.5px]' : 'h-11 text-[13.5px]',
              active ? 'text-ink' : 'text-ink-3 hover:text-ink-2',
            )}
          >
            {tab.icon && <tab.icon aria-hidden="true" className="size-4" />}
            {tab.label}
            {tab.count !== undefined && tab.count !== null && (
              <span
                data-numeric=""
                className={cn(
                  'rounded-full px-1.5 py-0.5 text-[11px] font-semibold',
                  active ? 'bg-info-soft text-brand' : 'bg-surface-3 text-ink-3',
                )}
              >
                {tab.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

export function TabPanel({ id, tabValue, value, children, className }) {
  if (tabValue !== value) return null;
  return (
    <div
      role="tabpanel"
      id={id}
      tabIndex={0}
      className={cn('animate-fade focus-visible:outline-none', className)}
    >
      {children}
    </div>
  );
}

/** Segmented control for mutually exclusive view modes. */
export function SegmentedControl({ options, value, onChange, label, className }) {
  return (
    <div
      role="group"
      aria-label={label}
      className={cn(
        'inline-flex items-center gap-0.5 rounded-[var(--radius-control)] border border-line bg-surface-2 p-0.5',
        className,
      )}
    >
      {options.map((option) => {
        const active = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={active}
            onClick={() => onChange(option.value)}
            title={option.title || option.label}
            className={cn(
              'inline-flex h-7.5 items-center gap-1.5 rounded-[7px] px-2.5 text-[12.5px] font-medium transition-colors duration-150',
              active ? 'bg-surface text-ink shadow-sm' : 'text-ink-3 hover:text-ink-2',
            )}
          >
            {option.icon && <option.icon aria-hidden="true" className="size-3.5" />}
            {option.label}
          </button>
        );
      })}
    </div>
  );
}
