import { useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { useFocusTrap, useScrollLock } from '../lib/hooks';
import { IconButton } from './Button';
import { cn } from './cn';

function useEscape(active, onClose) {
  useEffect(() => {
    if (!active) return undefined;
    const onKeyDown = (event) => {
      if (event.key === 'Escape') onClose?.();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [active, onClose]);
}

/**
 * Right-hand drawer. Used for record detail so the operator keeps their place
 * in the list behind it - on small screens it becomes a full-height sheet.
 */
export function Drawer({ open, onClose, title, subtitle, eyebrow, width = 'lg', header, children, footer }) {
  const panelRef = useRef(null);
  useScrollLock(open);
  useFocusTrap(panelRef, open);
  useEscape(open, onClose);

  if (!open) return null;

  const widths = {
    md: 'sm:max-w-xl',
    lg: 'sm:max-w-2xl',
    xl: 'sm:max-w-4xl',
  };

  return createPortal(
    <div className="fixed inset-0 z-[70] flex justify-end">
      <button
        type="button"
        aria-label="Close panel"
        onClick={onClose}
        className="animate-fade absolute inset-0 bg-[var(--t-overlay)] backdrop-blur-[2px]"
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        className={cn(
          'animate-slide-left relative flex h-full w-full flex-col border-l border-line bg-surface shadow-lg',
          widths[width],
        )}
      >
        <header className="flex items-start gap-3 border-b border-line bg-surface-2 px-4 py-3.5 sm:px-5">
          <div className="min-w-0 flex-1">
            {eyebrow && (
              <p className="text-[10.5px] font-semibold tracking-[0.13em] text-ink-3 uppercase">
                {eyebrow}
              </p>
            )}
            <h2 className="mt-0.5 truncate text-[16px] font-semibold text-ink" title={title}>
              {title}
            </h2>
            {subtitle && <div className="mt-1 min-w-0 text-[12.5px] text-ink-3">{subtitle}</div>}
            {header}
          </div>
          <IconButton icon={X} label="Close panel" onClick={onClose} />
        </header>
        {/* A query container: a drawer is a fixed slab roughly 640px wide that
            has nothing to do with the window, so content inside it must size
            against the panel. Without this the `@min-*` rules in drawer bodies
            match nothing and every two-up layout silently stacks. */}
        <div className="@container min-h-0 flex-1 overflow-y-auto overscroll-contain">{children}</div>
        {footer && (
          <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-line bg-surface-2 px-4 py-3 sm:px-5">
            {footer}
          </footer>
        )}
      </div>
    </div>,
    document.body,
  );
}

/** Centre-stage modal, reserved for decisions that must be answered now. */
export function Modal({ open, onClose, title, description, icon: Icon, tone = 'brand', children, footer }) {
  const panelRef = useRef(null);
  useScrollLock(open);
  useFocusTrap(panelRef, open);
  useEscape(open, onClose);

  if (!open) return null;

  const tones = {
    brand: 'border-brand/25 bg-info-soft text-brand',
    critical: 'border-critical/25 bg-critical-soft text-critical',
    medium: 'border-medium/25 bg-medium-soft text-medium',
  };

  return createPortal(
    <div className="fixed inset-0 z-[75] grid place-items-end p-0 sm:place-items-center sm:p-6">
      <button
        type="button"
        aria-label="Close dialog"
        onClick={onClose}
        className="animate-fade absolute inset-0 bg-[var(--t-overlay)] backdrop-blur-[2px]"
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        /* Capped to the viewport, with the body scrolling between a fixed
           header and footer. Without the cap a long dialog - the list of what
           the access graph is built from, for one - ran off the bottom of the
           screen with no way to reach the rest of it or the Close button. */
        className="animate-pop relative flex max-h-[calc(100dvh-1rem)] w-full max-w-lg flex-col rounded-t-[18px] border border-line bg-surface shadow-lg sm:max-h-[calc(100dvh-3rem)] sm:rounded-[18px]"
      >
        <div className="flex shrink-0 items-start gap-3 p-5 pb-3">
          {Icon && (
            <span className={cn('grid size-9 shrink-0 place-items-center rounded-full border', tones[tone])}>
              <Icon aria-hidden="true" className="size-4.5" />
            </span>
          )}
          <div className="min-w-0 flex-1">
            <h2 className="text-[15.5px] font-semibold text-ink">{title}</h2>
            {description && <p className="mt-1 text-[12.5px] leading-relaxed text-ink-3">{description}</p>}
          </div>
          <IconButton icon={X} label="Close dialog" size="sm" onClick={onClose} />
        </div>
        {children && (
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-4">{children}</div>
        )}
        {footer && (
          <div className="flex shrink-0 flex-wrap items-center justify-end gap-2 border-t sm:rounded-b-[18px] border-line bg-surface-2 px-5 py-3.5">
            {footer}
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}
