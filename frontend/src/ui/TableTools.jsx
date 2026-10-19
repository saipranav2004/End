import { useId, useState } from 'react';
import {
  Check,
  Download,
  MoreHorizontal,
  RefreshCw,
  Rows2,
  Rows3,
  Settings2,
} from 'lucide-react';
import { usePopover } from '../lib/hooks';
import { cn } from './cn';

/**
 * Record-surface tools: refresh, table settings, and an overflow menu.
 *
 * These used to be three full-width text buttons and a naked segmented
 * control sitting in the open - which is what made the surface read like a
 * prototype rather than a product. The controls themselves are standard
 * (Cloudscape names the two density modes "comfortable" and "compact" and
 * requires a mechanism to choose and persist one; every console of this kind
 * exports); what was wrong was the presentation.
 *
 * Both Carbon and PatternFly give the same rule, and it is the rule applied
 * here: expose at most two actions as buttons, put secondary actions in icon
 * buttons, and collapse the rest into an overflow menu. So:
 *
 *   - Refresh is an icon button. It is frequent, and it needs no label to be
 *     understood.
 *   - Density lives behind a settings control, as a preference, which is where
 *     Cloudscape's collection-preferences pattern puts it.
 *   - Export lives in the overflow menu. It is real and it is used, but it is
 *     not what an operator came to this screen to do.
 *
 * Everything keeps its accessible name, its tooltip and its keyboard path, and
 * both popovers are placed by `usePopover` so neither runs off a short window.
 */

const TOOL_BUTTON =
  'grid size-8 shrink-0 place-items-center rounded-[var(--radius-control)] border border-line-strong ' +
  'bg-surface text-ink-2 transition-colors duration-150 hover:border-ink-3/50 hover:bg-surface-2 ' +
  'hover:text-ink disabled:pointer-events-none disabled:opacity-55';

const DENSITIES = [
  { value: 'comfortable', label: 'Comfortable', hint: 'Standard row height', icon: Rows2 },
  { value: 'compact', label: 'Compact', hint: 'More rows per screen', icon: Rows3 },
];

/** Icon-only refresh. Spins only while a refetch is actually in flight. */
export function RefreshButton({ onRefresh, refreshing = false, label = 'Refresh records' }) {
  return (
    <button
      type="button"
      onClick={onRefresh}
      disabled={refreshing}
      aria-label={label}
      title={label}
      className={TOOL_BUTTON}
    >
      <RefreshCw aria-hidden="true" className={cn('size-3.5', refreshing && 'animate-spin')} />
    </button>
  );
}

/** Table settings: display preferences for this surface. */
export function TableSettings({ density, onDensityChange }) {
  const [open, setOpen] = useState(false);
  const { wrapperRef, triggerRef, panelProps } = usePopover(open, () => setOpen(false));
  const labelId = useId();

  return (
    <div ref={wrapperRef} className="relative shrink-0">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label="Table settings"
        title="Table settings"
        className={cn(TOOL_BUTTON, open && 'border-brand/55 bg-info-soft text-brand')}
      >
        <Settings2 aria-hidden="true" className="size-3.5" />
      </button>

      {open && (
        <div
          role="dialog"
          aria-labelledby={labelId}
          {...panelProps}
          className="animate-pop absolute right-0 z-40 w-64 rounded-[var(--radius-panel)] border border-line bg-surface shadow-lg"
        >
          <p
            id={labelId}
            className="shrink-0 border-b border-line bg-surface-2 px-3 py-2 text-[10.5px] font-semibold tracking-[0.12em] text-ink-3 uppercase"
          >
            Table settings
          </p>
          <div className="min-h-0 flex-1 overflow-y-auto p-2.5">
            <p className="text-[11.5px] font-semibold text-ink-2">Row density</p>
            <div role="radiogroup" aria-label="Row density" className="mt-1.5 flex flex-col gap-0.5">
              {DENSITIES.map((option) => {
                const active = density === option.value;
                return (
                  <button
                    key={option.value}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    onClick={() => onDensityChange(option.value)}
                    className={cn(
                      'flex items-center gap-2.5 rounded-md px-2 py-1.5 text-left transition-colors',
                      active ? 'bg-info-soft' : 'hover:bg-surface-2',
                    )}
                  >
                    <option.icon
                      aria-hidden="true"
                      className={cn('size-4 shrink-0', active ? 'text-brand' : 'text-ink-3')}
                    />
                    <span className="min-w-0 flex-1">
                      <span
                        className={cn(
                          'block text-[12.5px] font-medium',
                          active ? 'text-brand' : 'text-ink',
                        )}
                      >
                        {option.label}
                      </span>
                      <span className="block text-[11px] text-ink-3">{option.hint}</span>
                    </span>
                    <Check
                      aria-hidden="true"
                      className={cn('size-3.5 shrink-0', active ? 'text-brand' : 'text-transparent')}
                    />
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * Overflow menu. `items` entries: { key, label, hint?, icon?, onSelect,
 * disabled?, disabledHint? }. A disabled entry stays visible and says why, so
 * "export" never simply vanishes when a filter returns nothing.
 */
export function OverflowMenu({ items, label = 'More actions' }) {
  const [open, setOpen] = useState(false);
  const { wrapperRef, triggerRef, panelProps } = usePopover(open, () => setOpen(false));
  const visible = (items || []).filter(Boolean);
  if (visible.length === 0) return null;

  return (
    <div ref={wrapperRef} className="relative shrink-0">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        title={label}
        className={cn(TOOL_BUTTON, open && 'border-brand/55 bg-info-soft text-brand')}
      >
        <MoreHorizontal aria-hidden="true" className="size-4" />
      </button>

      {open && (
        <div
          role="menu"
          {...panelProps}
          className="animate-pop absolute right-0 z-40 w-60 rounded-[var(--radius-panel)] border border-line bg-surface shadow-lg"
        >
          <div className="min-h-0 flex-1 overflow-y-auto p-1">
            {visible.map((item) => {
              const Icon = item.icon || Download;
              return (
                <button
                  key={item.key}
                  role="menuitem"
                  type="button"
                  disabled={item.disabled}
                  title={item.disabled ? item.disabledHint : undefined}
                  onClick={() => {
                    item.onSelect();
                    setOpen(false);
                  }}
                  className="flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors hover:bg-surface-2 disabled:pointer-events-none disabled:opacity-50"
                >
                  <Icon aria-hidden="true" className="mt-px size-4 shrink-0 text-ink-3" />
                  <span className="min-w-0">
                    <span className="block text-[12.5px] font-medium text-ink">{item.label}</span>
                    {(item.disabled ? item.disabledHint : item.hint) && (
                      <span className="mt-0.5 block text-[11px] leading-snug text-ink-3">
                        {item.disabled ? item.disabledHint : item.hint}
                      </span>
                    )}
                  </span>
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

/** Groups the tools with a consistent gap. */
export function TableToolbar({ children, className }) {
  return <div className={cn('flex shrink-0 items-center gap-1.5', className)}>{children}</div>;
}
