import { useEffect, useId, useRef, useState } from 'react';
import { MoreHorizontal } from 'lucide-react';
import { cn } from './cn';

/**
 * Small anchored action menu for table rows. Closes on outside click, Escape
 * and selection; items are plain buttons so keyboard users get them for free.
 */
export function Menu({ items, label = 'Row actions', icon: Icon = MoreHorizontal, align = 'end', outlined = false }) {
  const [open, setOpen] = useState(false);
  const wrapperRef = useRef(null);
  const id = useId();

  useEffect(() => {
    if (!open) return undefined;
    const onPointerDown = (event) => {
      if (!wrapperRef.current?.contains(event.target)) setOpen(false);
    };
    const onKeyDown = (event) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  const visible = items.filter(Boolean);
  if (visible.length === 0) return null;

  return (
    <div ref={wrapperRef} className="relative">
      <button
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={(event) => {
          event.stopPropagation();
          setOpen((value) => !value);
        }}
        className={cn(
          'grid size-8 place-items-center rounded-md text-ink-3 transition-colors',
          outlined && 'border border-line bg-surface shadow-xs',
          open ? 'bg-surface-3 text-ink' : 'hover:bg-surface-3 hover:text-ink-2',
        )}
      >
        <Icon aria-hidden="true" className="size-4" />
      </button>
      {open && (
        <div
          id={id}
          role="menu"
          className={cn(
            'animate-pop absolute z-40 mt-1 min-w-52 overflow-hidden rounded-[var(--radius-control)] border border-line bg-surface p-1 shadow-lg',
            align === 'end' ? 'right-0' : 'left-0',
          )}
        >
          {visible.map((item) => (
            <button
              key={item.key || item.label}
              role="menuitem"
              type="button"
              disabled={item.disabled}
              onClick={(event) => {
                event.stopPropagation();
                setOpen(false);
                item.onSelect?.();
              }}
              className={cn(
                'flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-[13px] transition-colors',
                item.tone === 'critical' ? 'text-critical' : 'text-ink-2',
                item.disabled
                  ? 'cursor-not-allowed opacity-50'
                  : 'hover:bg-surface-3 hover:text-ink',
              )}
            >
              {item.icon && <item.icon aria-hidden="true" className="size-4 shrink-0" />}
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
