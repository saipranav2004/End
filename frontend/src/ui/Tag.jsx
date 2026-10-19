import { cn, TONE_BG, TONE_CLASSES } from './cn';

export function Tag({ tone = 'neutral', size = 'md', dot = false, icon: Icon, className, children }) {
  return (
    <span
      className={cn(
        'inline-flex max-w-full items-center gap-1.5 rounded-full border font-medium',
        size === 'sm' ? 'px-2 py-0.5 text-[11px]' : 'px-2.5 py-1 text-[11.5px]',
        TONE_CLASSES[tone] || TONE_CLASSES.neutral,
        className,
      )}
    >
      {dot && (
        <span
          aria-hidden="true"
          className={cn('size-1.5 shrink-0 rounded-full', TONE_BG[tone] || TONE_BG.neutral)}
        />
      )}
      {Icon && <Icon aria-hidden="true" className="size-3.5 shrink-0" />}
      <span className="truncate">{children}</span>
    </span>
  );
}

/** Monospace chip for ARNs, commit hashes, file paths and redacted secrets. */
export function Code({ children, className, title }) {
  return (
    <code
      title={title}
      className={cn(
        'inline-block max-w-full truncate rounded-md border border-line bg-inset px-1.5 py-0.5 font-mono text-[12px] text-ink-2',
        className,
      )}
    >
      {children}
    </code>
  );
}
