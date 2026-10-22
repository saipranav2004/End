import { ChevronRight, Download } from 'lucide-react';
import { downloadText } from '../lib/csv';
import { Button } from './Button';
import { CopyButton } from './Copyable';
import { cn } from './cn';

const isEmpty = (value) =>
  value === null ||
  value === undefined ||
  value === '' ||
  (Array.isArray(value) && value.length === 0) ||
  (typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0);

function sizeOf(value) {
  if (Array.isArray(value)) return `${value.length} ${value.length === 1 ? 'item' : 'items'}`;
  if (value && typeof value === 'object') {
    const keys = Object.keys(value).length;
    return `${keys} ${keys === 1 ? 'field' : 'fields'}`;
  }
  return null;
}

/**
 * One JSON value behind a native disclosure, pretty-printed and wrapped so a
 * long ARN or policy string never widens the page. Copy sits outside the
 * summary: a control inside one is announced as part of its label.
 */
export function JsonBlock({ label, value, defaultOpen = false, className }) {
  const text = JSON.stringify(value, null, 2) ?? 'null';
  const size = sizeOf(value);
  return (
    <details open={defaultOpen} className={cn('group relative min-w-0 rounded-[var(--radius-control)] border border-line bg-surface-2', className)}>
      <summary className="flex cursor-pointer list-none items-center gap-2 py-2 pr-10 pl-3 text-[12.5px] font-medium text-ink [&::-webkit-details-marker]:hidden">
        <ChevronRight aria-hidden="true" className="size-3.5 shrink-0 text-ink-3 transition-transform group-open:rotate-90" />
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {size && <span className="shrink-0 text-[11px] font-normal text-ink-3">{size}</span>}
      </summary>
      <CopyButton value={text} label={`Copy ${label}`} size="sm" className="absolute top-1.5 right-1.5" />
      <pre className="max-h-[28rem] overflow-y-auto border-t border-line bg-inset px-3 py-2.5 font-mono text-[11.5px] leading-relaxed whitespace-pre-wrap text-ink-2 [overflow-wrap:anywhere]">
        {text}
      </pre>
    </details>
  );
}

/**
 * The Raw tab of a detail view: each block of the API payload as JSON, the
 * first one open, and the whole payload as a download. Blocks the API left
 * empty are named in one line rather than shown as `null`.
 */
export function RawJson({ blocks, payload, filename, intro }) {
  const shown = blocks.filter((block) => !isEmpty(block.value));
  const empty = blocks.filter((block) => isEmpty(block.value));
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        {intro && <p className="max-w-3xl min-w-[min(100%,20rem)] flex-1 text-[12.5px] leading-relaxed text-ink-3">{intro}</p>}
        <Button
          size="sm"
          icon={Download}
          className="ml-auto"
          onClick={() => downloadText(JSON.stringify(payload, null, 2), filename, 'application/json')}
        >
          Download JSON
        </Button>
      </div>
      {shown.map((block, index) => (
        <JsonBlock key={block.key} label={block.label} value={block.value} defaultOpen={index === 0} />
      ))}
      {empty.length > 0 && (
        <p className="text-[12px] text-ink-3">Empty or not recorded: {empty.map((block) => block.label).join(', ')}.</p>
      )}
    </div>
  );
}
