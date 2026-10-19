import { useEffect, useId, useRef, useState } from 'react';
import { Check, ChevronDown, Copy } from 'lucide-react';
import { CopyButton } from '../../ui/Copyable';
import { cn } from '../../ui/cn';

/**
 * Small pieces the AWS setup screens share: copy chips that sit inside an
 * instruction, code blocks, numbered steps, disclosures and provider marks.
 */

/** Clipboard write with a selection fallback for non-secure origins. */
export async function copyText(value) {
  const text = String(value ?? '');
  try {
    if (navigator.clipboard?.writeText && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* fall through to the selection fallback */
  }
  try {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand('copy');
    area.remove();
    return ok;
  } catch {
    return false;
  }
}

/** A value inline in a sentence that copies on click - placed exactly where the console asks for it. */
export function CopyChip({ value, label, className }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef(null);
  useEffect(() => () => clearTimeout(timer.current), []);
  if (!value) return null;
  const copy = async (event) => {
    event.stopPropagation();
    const ok = await copyText(value);
    setCopied(ok);
    clearTimeout(timer.current);
    if (ok) timer.current = setTimeout(() => setCopied(false), 1600);
  };
  return (
    <button
      type="button"
      onClick={copy}
      title={copied ? 'Copied' : 'Click to copy'}
      aria-label={copied ? `Copied ${label ?? value}` : `Copy ${label ?? value}`}
      className={cn(
        'inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-md border px-1.5 py-0.5 align-middle font-mono text-[12px] transition-colors',
        copied ? 'border-low/40 bg-low-soft text-low' : 'border-line bg-inset text-ink hover:border-line-strong hover:bg-surface-3',
        className,
      )}
    >
      <span className="min-w-0 text-left break-all">{value}</span>
      {copied ? (
        <Check aria-hidden="true" className="size-3 shrink-0" />
      ) : (
        <Copy aria-hidden="true" className="size-3 shrink-0 text-ink-3" />
      )}
    </button>
  );
}

export function CodeBlock({ code, copyLabel, className, maxHeight = 'max-h-80' }) {
  return (
    <div className={cn('relative mt-2', className)}>
      <pre
        className={cn(
          'overflow-auto rounded-[var(--radius-control)] border border-line bg-inset p-3 pr-11 font-mono text-[11.5px] leading-relaxed text-ink-2',
          maxHeight,
        )}
      >
        {code}
      </pre>
      <CopyButton value={code} label={copyLabel} className="absolute top-2 right-2 border border-line bg-surface" />
    </div>
  );
}

/** Numbered instructions; each value sits where the console asks for it. */
export function Instructions({ items }) {
  return (
    <ol className="flex flex-col gap-2.5">
      {items.filter(Boolean).map((item, index) => (
        <li key={index} className="flex gap-3 text-[13px] leading-relaxed text-ink-2 [&_b]:font-semibold [&_b]:text-ink">
          <span
            aria-hidden="true"
            data-numeric=""
            className="mt-px grid size-5 shrink-0 place-items-center rounded-md bg-surface-3 text-[11px] font-semibold text-ink-3"
          >
            {index + 1}
          </span>
          <span className="min-w-0 flex-1">{item}</span>
        </li>
      ))}
    </ol>
  );
}

export function Disclosure({ label, children, defaultOpen = false, className }) {
  const [open, setOpen] = useState(defaultOpen);
  const id = useId();
  return (
    <div className={cn('mt-3.5', className)}>
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        aria-controls={id}
        className="inline-flex items-center gap-1 text-left text-[12.5px] font-medium text-brand hover:underline"
      >
        <ChevronDown aria-hidden="true" className={cn('size-3.5 shrink-0 transition-transform', open ? 'rotate-0' : '-rotate-90')} />
        {label}
      </button>
      {open && (
        <div id={id} className="animate-fade">
          {children}
        </div>
      )}
    </div>
  );
}

const MARK_BOX = {
  lg: 'size-14 rounded-[14px]',
  md: 'size-10 rounded-[10px]',
  sm: 'size-8 rounded-[8px]',
};

/**
 * The provider tile. AWS's own squid-ink and smile colours, so the tile reads
 * the same in light and dark themes and is recognised before it is read.
 */
export function AwsMark({ size = 'md', className }) {
  return (
    <span
      className={cn(
        'grid shrink-0 place-items-center bg-[#232f3e] shadow-[inset_0_0_0_1px_rgba(255,255,255,0.08),0_6px_16px_-8px_rgba(35,47,62,0.7)]',
        MARK_BOX[size] ?? MARK_BOX.md,
        className,
      )}
    >
      <svg viewBox="0 0 64 40" aria-hidden="true" className="w-[72%]">
        <text
          x="32"
          y="22"
          textAnchor="middle"
          fill="#ffffff"
          fontFamily="Arial, Helvetica, sans-serif"
          fontWeight="700"
          fontSize="21"
          letterSpacing="-0.5"
        >
          aws
        </text>
        <path d="M13 28.5c11 6.5 27 6.5 37 0" fill="none" stroke="#ff9900" strokeWidth="3" strokeLinecap="round" />
        <path d="M45.5 26.2l5.2 1.9-2.1 5" fill="none" stroke="#ff9900" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <span className="sr-only">Amazon Web Services</span>
    </span>
  );
}

/** GitHub's mark on its own dark tile. */
export function GitHubMark({ size = 'md', className }) {
  return (
    <span
      className={cn(
        'grid shrink-0 place-items-center bg-[#1f2328] text-white shadow-[inset_0_0_0_1px_rgba(255,255,255,0.08)]',
        MARK_BOX[size] ?? MARK_BOX.md,
        className,
      )}
    >
      <svg viewBox="0 0 16 16" aria-hidden="true" className="w-[56%]" fill="currentColor">
        <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
      </svg>
      <span className="sr-only">GitHub</span>
    </span>
  );
}

/** CodeCommit: AWS's developer-tools blue, with a branch glyph. */
export function CodeCommitMark({ size = 'md', className }) {
  return (
    <span
      className={cn(
        'grid shrink-0 place-items-center bg-[linear-gradient(135deg,#2e27ad_0%,#527fff_100%)] text-white shadow-[inset_0_0_0_1px_rgba(255,255,255,0.1)]',
        MARK_BOX[size] ?? MARK_BOX.md,
        className,
      )}
    >
      <svg viewBox="0 0 24 24" aria-hidden="true" className="w-[56%]" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="6" cy="5" r="2.2" />
        <circle cx="6" cy="19" r="2.2" />
        <circle cx="18" cy="8" r="2.2" />
        <path d="M6 7.2v9.6" />
        <path d="M18 10.2c0 4-6.5 3.2-11 6.6" />
      </svg>
      <span className="sr-only">AWS CodeCommit</span>
    </span>
  );
}
