import { useState } from 'react';
import { Check, Copy } from 'lucide-react';
import { cn } from './cn';

/**
 * ARNs, commit hashes and file paths are copied far more often than they are
 * read, so every one of them gets a one-click copy affordance.
 */
export function CopyButton({ value, label = 'Copy', className, size = 'md' }) {
  const [copied, setCopied] = useState(false);

  const onCopy = async (event) => {
    event.stopPropagation();
    try {
      await navigator.clipboard.writeText(String(value ?? ''));
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      setCopied(false);
    }
  };

  if (!value) return null;

  return (
    <button
      type="button"
      onClick={onCopy}
      aria-label={copied ? 'Copied to clipboard' : label}
      title={copied ? 'Copied' : label}
      className={cn(
        'grid shrink-0 place-items-center rounded-md transition-colors',
        size === 'sm' ? 'size-6' : 'size-7',
        copied ? 'text-low' : 'text-ink-3 hover:bg-surface-3 hover:text-ink-2',
        className,
      )}
    >
      {copied ? (
        <Check aria-hidden="true" className="size-3.5" />
      ) : (
        <Copy aria-hidden="true" className="size-3.5" />
      )}
    </button>
  );
}

/** Monospace value plus copy control, aligned for use inside detail lists. */
export function CopyableValue({ value, mono = true, className, children }) {
  if (!value) return <span className="text-ink-3">-</span>;
  return (
    <span className={cn('inline-flex min-w-0 max-w-full items-center gap-1.5', className)}>
      <span className={cn('min-w-0 truncate', mono && 'font-mono text-[12.5px]')} title={value}>
        {children ?? value}
      </span>
      <CopyButton value={value} size="sm" />
    </span>
  );
}
