import { useState } from 'react';
import { ChevronRight, Copy } from 'lucide-react';
import { formatNumber, formatRelative, humanizeToken, shortCommit } from '../../lib/format';
import { platformMeta, severityMeta } from '../../lib/domain';
import { EmptyState } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { cn } from '../../ui/cn';
import { findingKey, REPEAT_BUCKETS } from './scannerState';

/**
 * The Repeated list. A value copied into many files of one commit is one
 * problem, not many, so each value is one card that says how many times it
 * repeats; its places open underneath. The chips above split the values by how
 * often they repeat, so the worst copy-paste jobs are one tap away.
 */
export function RepeatedView({ groups, allGroups, bucket, onBucket, onSelect }) {
  const counts = REPEAT_BUCKETS.map((entry) => ({ ...entry, groups: allGroups.filter((group) => group.bucket === entry.key).length }));
  const total = allGroups.reduce((sum, group) => sum + group.count, 0);
  const widest = Math.max(1, ...allGroups.map((group) => group.count));

  return (
    <div>
      <div className="border-b border-line px-4 py-3.5">
        <p className="text-[12.5px] text-ink-2">
          <span className="font-semibold text-ink">{formatNumber(allGroups.length)}</span> repeated {allGroups.length === 1 ? 'value' : 'values'} making up{' '}
          <span className="font-semibold text-ink">{formatNumber(total)}</span> findings. Pick how often a value repeats:
        </p>
        <div className="mt-2.5 flex flex-wrap gap-2" role="group" aria-label="How many times a value repeats">
          <BucketChip active={bucket === ''} onClick={() => onBucket('')} label="Any number of times" value={allGroups.length} />
          {counts.map((entry) => (
            <BucketChip key={entry.key} active={bucket === entry.key} disabled={entry.groups === 0} onClick={() => onBucket(bucket === entry.key ? '' : entry.key)} label={entry.label} value={entry.groups} />
          ))}
        </div>
      </div>

      {groups.length === 0 ? (
        <EmptyState compact title="No repeated values match" description="Nothing matches the platform, filters and repeat count you chose." />
      ) : (
        <ul className="divide-y divide-line">
          {groups.map((group) => (
            <RepeatedGroup key={group.key} group={group} widest={widest} onSelect={onSelect} />
          ))}
        </ul>
      )}
    </div>
  );
}

function BucketChip({ active, disabled, onClick, label, value }) {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-2 rounded-full border px-3 py-1 text-[12px] transition-colors disabled:cursor-not-allowed disabled:opacity-45',
        active ? 'border-brand bg-info-soft font-semibold text-brand' : 'border-line bg-surface text-ink-2 hover:border-line-strong hover:text-ink',
      )}
    >
      {label}
      <span data-numeric="" className="rounded-full bg-surface-3 px-1.5 text-[11px] font-semibold text-ink-2">
        {formatNumber(value)}
      </span>
    </button>
  );
}

/** The copies as a row of dots, one per place, so "7 times" can be felt as well as read. */
function CopyDots({ count, tone }) {
  const shown = Math.min(count, 14);
  return (
    <span className="flex items-center gap-0.5" aria-hidden="true">
      {Array.from({ length: shown }, (_, index) => (
        <span key={index} className="size-2 rounded-full" style={{ background: `var(--t-${tone})`, opacity: 0.35 + 0.65 * ((index + 1) / shown) }} />
      ))}
      {count > shown && <span className="ml-1 text-[11px] text-ink-3">+{count - shown}</span>}
    </span>
  );
}

const PREVIEW = 4;

function RepeatedGroup({ group, onSelect }) {
  const [open, setOpen] = useState(false);
  const worst = group.findings.reduce((acc, finding) => {
    const meta = severityMeta(finding.risk_tier);
    return meta.weight > acc.weight ? meta : acc;
  }, severityMeta(null));
  const platform = platformMeta(group.platform);
  const shown = open ? group.findings : group.findings.slice(0, PREVIEW);
  const hidden = group.findings.length - shown.length;

  return (
    <li className="px-4 py-3.5">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="min-w-0">
          <p className="flex flex-wrap items-baseline gap-x-2 text-[13.5px] font-semibold text-ink">
            <Copy aria-hidden="true" className="size-4 shrink-0 self-center text-ink-3" />
            Same value found {formatNumber(group.count)} times
            <span className="text-[12px] font-normal text-ink-3">in {formatNumber(group.files)} {group.files === 1 ? 'file' : 'files'}</span>
          </p>
          <p className="mt-1 text-[12.5px] text-ink-2">
            {humanizeToken(group.detector)} · <span className="font-mono text-[12px]">{group.redacted || 'masked'}</span>
          </p>
          <p className="mt-0.5 truncate text-[11.5px] text-ink-3" title={group.commitMessage || undefined}>
            {group.repository || '-'} · commit <span className="font-mono">{shortCommit(group.commitId)}</span>
            {group.commitMessage ? ` · "${group.commitMessage}"` : ''} · found {formatRelative(group.createdAt)}
          </p>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-2">
          <div className="flex items-center gap-2">
            <Tag tone={platform.tone} size="sm">{platform.label}</Tag>
            <Tag tone={worst.tone} size="sm" dot>{worst.label}</Tag>
          </div>
          <CopyDots count={group.count} tone={worst.tone} />
        </div>
      </div>

      <ul className="mt-2.5 flex flex-col gap-1">
        {shown.map((finding) => (
          <li key={findingKey(finding)}>
            <button type="button" onClick={() => onSelect(finding)} className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-surface-2">
              <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full" style={{ background: `var(--t-${severityMeta(finding.risk_tier).tone})` }} />
              <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink-2">
                {finding.file_path}:{finding.line_number ?? '?'}
              </span>
              <ChevronRight aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
            </button>
          </li>
        ))}
      </ul>
      {(hidden > 0 || open) && group.findings.length > PREVIEW && (
        <button type="button" onClick={() => setOpen((value) => !value)} className="mt-1 px-2 text-[11.5px] font-medium text-brand hover:underline">
          {open ? 'Show fewer places' : `Show all ${formatNumber(group.findings.length)} places`}
        </button>
      )}
    </li>
  );
}
