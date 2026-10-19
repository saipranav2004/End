import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react';
import { AlertTriangle, CheckCircle2, Info, X } from 'lucide-react';
import { cn } from './cn';

const ToastContext = createContext(null);

const ICONS = {
  success: CheckCircle2,
  error: AlertTriangle,
  info: Info,
};

const TONES = {
  success: 'border-low/30 text-low',
  error: 'border-critical/30 text-critical',
  info: 'border-info/30 text-info',
};

/**
 * Deliberately sparse: confirmations live inline wherever possible, and this
 * channel is reserved for outcomes the operator would otherwise miss after
 * the surrounding UI has moved on. Never more than three at a time.
 */
export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([]);
  const timers = useRef(new Map());

  const dismiss = useCallback((id) => {
    setToasts((current) => current.filter((toast) => toast.id !== id));
    const timer = timers.current.get(id);
    if (timer) {
      clearTimeout(timer);
      timers.current.delete(id);
    }
  }, []);

  const notify = useCallback(
    ({ title, description, variant = 'info', duration = 5000, action = null }) => {
      const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
      setToasts((current) => [...current.slice(-2), { id, title, description, variant, action }]);
      if (duration > 0) {
        timers.current.set(
          id,
          setTimeout(() => dismiss(id), duration),
        );
      }
      return id;
    },
    [dismiss],
  );

  const value = useMemo(() => ({ notify, dismiss }), [notify, dismiss]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        aria-live="polite"
        aria-atomic="false"
        className="pointer-events-none fixed inset-x-0 bottom-0 z-[80] flex flex-col items-center gap-2 p-4 sm:right-4 sm:left-auto sm:items-end"
      >
        {toasts.map((toast) => {
          const Icon = ICONS[toast.variant] || Info;
          return (
            <div
              key={toast.id}
              role="status"
              className={cn(
                'animate-pop pointer-events-auto flex w-full max-w-sm items-start gap-3 rounded-[var(--radius-control)] border bg-surface p-3 shadow-lg',
                TONES[toast.variant] || TONES.info,
              )}
            >
              <Icon aria-hidden="true" className="mt-px size-4 shrink-0" />
              <div className="min-w-0 flex-1">
                <p className="text-[13px] font-semibold text-ink">{toast.title}</p>
                {toast.description && (
                  <p className="mt-0.5 text-[12.5px] leading-relaxed text-ink-2">{toast.description}</p>
                )}
                {toast.action && (
                  <button
                    type="button"
                    onClick={() => {
                      toast.action.onSelect();
                      dismiss(toast.id);
                    }}
                    className="mt-1.5 text-[12.5px] font-semibold text-brand hover:underline"
                  >
                    {toast.action.label}
                  </button>
                )}
              </div>
              <button
                type="button"
                onClick={() => dismiss(toast.id)}
                aria-label="Dismiss notification"
                className="grid size-6 shrink-0 place-items-center rounded-md text-ink-3 transition-colors hover:bg-surface-3 hover:text-ink-2"
              >
                <X aria-hidden="true" className="size-3.5" />
              </button>
            </div>
          );
        })}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) throw new Error('useToast must be used inside a ToastProvider');
  return context;
}
