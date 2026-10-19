import { useId, useState } from 'react';
import {
  ArrowLeftRight,
  ArrowRight,
  ChevronDown,
  CircleMinus,
  CirclePlus,
  Fingerprint,
  KeyRound,
  Link2,
  ListChecks,
  PencilLine,
  ScrollText,
} from 'lucide-react';
import { formatDateTime, formatRelative, humanizeToken } from '../../lib/format';
import { CopyableValue } from '../../ui/Copyable';
import { cn, TONE_BG } from '../../ui/cn';

/* ── Vocabulary ──────────────────────────────────────────────────────────── */

export const CHANGE_META = {
  CREATED: { label: 'Created', badge: 'New', tone: 'low', icon: CirclePlus, filter: 'Created' },
  DELETED: { label: 'Deleted', badge: 'Deleted', tone: 'critical', icon: CircleMinus, filter: 'Deleted' },
  MODIFIED: { label: 'Modified', badge: 'Changed', tone: 'medium', icon: PencilLine, filter: 'Modified' },
};

export const ENTITY_META = {
  identity: { label: 'Identity', plural: 'Identities', icon: Fingerprint },
  credential: { label: 'Credential', plural: 'Credentials', icon: KeyRound },
  permission: { label: 'Permission', plural: 'Permissions', icon: ListChecks },
  binding: { label: 'Binding', plural: 'Bindings', icon: Link2 },
  trust_edge: { label: 'Trust', plural: 'Trust relationships', icon: ArrowLeftRight },
};

export const CHANGE_TYPES = ['CREATED', 'DELETED', 'MODIFIED'];
export const ENTITY_TYPES = ['identity', 'credential', 'permission', 'binding', 'trust_edge'];

const TONE_CHIP = {
  low: 'border-low/30 bg-low-soft text-low',
  critical: 'border-critical/30 bg-critical-soft text-critical',
  medium: 'border-medium/35 bg-medium-soft text-medium',
};

/** CREATED / DELETED / MODIFIED as a coloured badge with its icon. */
export function ChangeBadge({ type, className, short = false }) {
  const meta = CHANGE_META[type] ?? CHANGE_META.MODIFIED;
  const Icon = meta.icon;
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-semibold tracking-wide uppercase',
        TONE_CHIP[meta.tone],
        className,
      )}
    >
      <Icon aria-hidden="true" className="size-3" />
      {short ? meta.badge : meta.label}
    </span>
  );
}

/** The NEW / CHANGED marker beside a name in the inventory lists. */
export function RecentChangeMarker({ change }) {
  if (change !== 'CREATED' && change !== 'MODIFIED') return null;
  const created = change === 'CREATED';
  return (
    <span
      title={created ? 'Discovered in the latest scan' : 'Changed in the latest scan'}
      className={cn(
        'inline-flex shrink-0 items-center rounded px-1 py-px text-[9.5px] font-bold tracking-[0.06em] uppercase',
        created ? 'bg-low-soft text-low' : 'bg-medium-soft text-medium',
      )}
    >
      {created ? 'New' : 'Changed'}
    </span>
  );
}

export function EntityLabel({ type, className }) {
  const meta = ENTITY_META[type] ?? { label: humanizeToken(type), icon: ScrollText };
  const Icon = meta.icon;
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-1.5 text-[12px] text-ink-2', className)}>
      <Icon aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
      <span className="truncate">{meta.label}</span>
    </span>
  );
}

/* ── Values ──────────────────────────────────────────────────────────────── */

const FIELD_LABELS = {
  type: 'Type',
  name: 'Name',
  account_id: 'Account',
  region: 'Region',
  owner: 'Owner',
  status: 'Status',
  runtime: 'Runtime',
  cluster: 'Cluster',
  attached_policies: 'Attached policies',
  last_used: 'Last used',
  age_days: 'Age (days)',
  is_publicly_accessible: 'Reachable from the internet',
  security_group_ids: 'Security groups',
  last_rotated_at: 'Last rotated',
  rotation_enabled: 'Automatic rotation',
  policy_name: 'Policy',
  policy_type: 'Policy type',
  edge_type: 'Relationship',
  binding: 'Bound through',
};

export function fieldLabel(field) {
  if (FIELD_LABELS[field]) return FIELD_LABELS[field];
  if (field.startsWith('tags.')) return `Tag "${field.slice(5)}"`;
  return humanizeToken(field.replaceAll('.', '_'));
}

/** A JSON value as words: never raw JSON on screen. */
function Value({ value, mono = false }) {
  if (value === null || value === undefined || value === '') return <span className="text-ink-3 italic">none</span>;
  if (typeof value === 'boolean') return <span>{value ? 'Yes' : 'No'}</span>;
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="text-ink-3 italic">none</span>;
    return <span className={cn(mono && 'font-mono text-[11.5px]')}>{value.map((item) => (typeof item === 'object' ? JSON.stringify(item) : String(item))).join(', ')}</span>;
  }
  if (typeof value === 'object') {
    return (
      <span>
        {Object.entries(value)
          .map(([key, inner]) => `${fieldLabel(key)}: ${typeof inner === 'object' ? JSON.stringify(inner) : String(inner)}`)
          .join(' · ')}
      </span>
    );
  }
  const text = String(value);
  const looksLikeId = /^arn:|^[A-Z0-9]{16,}$|^\d{12}$/.test(text);
  return <span className={cn((mono || looksLikeId) && 'font-mono text-[11.5px] break-all')}>{text}</span>;
}

function FieldDiff({ field, before, after }) {
  return (
    <li className="grid gap-1 py-2 @min-[34rem]:grid-cols-[minmax(0,11rem)_minmax(0,1fr)] @min-[34rem]:gap-4">
      <span className="text-[12px] font-medium text-ink-2">{fieldLabel(field)}</span>
      <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[12.5px]">
        <span className="rounded bg-critical-soft px-1.5 py-0.5 text-critical line-through decoration-critical/60" title="Before">
          <Value value={before} />
        </span>
        <ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
        <span className="rounded bg-low-soft px-1.5 py-0.5 font-medium text-low" title="After">
          <Value value={after} />
        </span>
      </span>
    </li>
  );
}

function ActionList({ title, actions, tone }) {
  if (!actions?.length) return null;
  return (
    <div className="min-w-0">
      <p className={cn('text-[11px] font-semibold tracking-[0.08em] uppercase', tone === 'low' ? 'text-low' : 'text-critical')}>
        {title} · {actions.length}
      </p>
      <ul className="mt-1.5 flex flex-wrap gap-1.5">
        {actions.map((action) => (
          <li
            key={action}
            className={cn(
              'rounded border px-1.5 py-0.5 font-mono text-[11.5px]',
              tone === 'low' ? 'border-low/30 bg-low-soft text-low' : 'border-critical/30 bg-critical-soft text-critical line-through decoration-critical/60',
            )}
          >
            {tone === 'low' ? '+ ' : '− '}
            {action}
          </li>
        ))}
      </ul>
    </div>
  );
}

const SHOWN_ELSEWHERE = new Set(['fields_changed', 'actions_added', 'actions_removed', 'source', 'target', 'external']);

/**
 * The change's details, readable: a before / after diff for a modification,
 * the granted and revoked actions for a permission change, the two ends of a
 * trust relationship, and the remaining facts as label / value pairs.
 */
export function ChangeDetails({ change }) {
  const details = change.details ?? {};
  const fields = Array.isArray(details.fields_changed) ? details.fields_changed : [];
  const added = Array.isArray(details.actions_added) ? details.actions_added : [];
  const removed = Array.isArray(details.actions_removed) ? details.actions_removed : [];
  const facts = Object.entries(details).filter(([key, value]) => !SHOWN_ELSEWHERE.has(key) && value !== undefined);
  const hasTrust = details.source || details.target;

  return (
    <div className="flex flex-col gap-4">
      {fields.length > 0 && (
        <div>
          <p className="text-[11px] font-semibold tracking-[0.08em] text-ink-3 uppercase">What changed</p>
          <ul className="mt-1 divide-y divide-line">
            {fields.map((entry, index) => (
              <FieldDiff key={`${entry.field}-${index}`} field={String(entry.field ?? '')} before={entry.old} after={entry.new} />
            ))}
          </ul>
        </div>
      )}

      {(added.length > 0 || removed.length > 0) && (
        <div className="grid gap-4 @min-[34rem]:grid-cols-2">
          <ActionList title="Added" actions={added} tone="low" />
          <ActionList title="Removed" actions={removed} tone="critical" />
        </div>
      )}

      {hasTrust && (
        <div>
          <p className="text-[11px] font-semibold tracking-[0.08em] text-ink-3 uppercase">Relationship</p>
          <p className="mt-1.5 flex min-w-0 flex-wrap items-center gap-2 text-[12.5px]">
            <span className="min-w-0 font-mono text-[11.5px] break-all text-ink-2">{details.source}</span>
            <ArrowRight aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />
            <span className="min-w-0 font-mono text-[11.5px] break-all text-ink-2">{details.target}</span>
            {details.external && (
              <span className="rounded-full border border-high/30 bg-high-soft px-2 py-0.5 text-[11px] font-medium text-high">
                Outside your accounts
              </span>
            )}
          </p>
        </div>
      )}

      {facts.length > 0 && (
        <dl className="grid gap-x-6 gap-y-2.5 @min-[34rem]:grid-cols-2">
          {facts.map(([key, value]) => (
            <div key={key} className="min-w-0">
              <dt className="text-[11px] text-ink-3">{fieldLabel(key)}</dt>
              <dd className="mt-0.5 text-[12.5px] text-ink">
                <Value value={key === 'type' || key === 'policy_type' || key === 'edge_type' || key === 'binding' ? humanizeToken(String(value)) : value} />
              </dd>
            </div>
          ))}
        </dl>
      )}

      {fields.length === 0 && added.length === 0 && removed.length === 0 && !hasTrust && facts.length === 0 && (
        <p className="text-[12.5px] text-ink-3">The scan recorded no further detail for this change.</p>
      )}
    </div>
  );
}

/** Who made the change, according to CloudTrail. */
export function EvidenceCard({ evidence, changeType }) {
  if (!evidence) return null;
  const verb = changeType === 'DELETED' ? 'Deleted by' : 'Created by';
  return (
    <div className="rounded-[var(--radius-control)] border border-info/25 bg-info-soft/50 px-3 py-2.5">
      <p className="flex items-center gap-1.5 text-[10.5px] font-semibold tracking-[0.1em] text-info uppercase">
        <ScrollText aria-hidden="true" className="size-3.5" />
        CloudTrail
      </p>
      <dl className="mt-1.5 grid gap-x-5 gap-y-1.5 text-[12px] @min-[34rem]:grid-cols-[auto_minmax(0,1fr)]">
        <dt className="text-ink-3">{verb}</dt>
        <dd className="min-w-0">
          <CopyableValue value={evidence.callerArn} className="text-ink" />
        </dd>
        <dt className="text-ink-3">Via</dt>
        <dd className="font-mono text-[11.5px] text-ink">{evidence.eventName || '-'}</dd>
        <dt className="text-ink-3">From IP</dt>
        <dd className="font-mono text-[11.5px] text-ink">{evidence.sourceIp || '-'}</dd>
        <dt className="text-ink-3">At</dt>
        <dd className="text-ink" title={formatDateTime(evidence.eventTime)}>
          {formatDateTime(evidence.eventTime)}
        </dd>
      </dl>
    </div>
  );
}

/* ── One change ──────────────────────────────────────────────────────────── */

/** Row grid for the scan changes list (header and rows share it). */
export const CHANGE_GRID = '@min-[56rem]:grid-cols-[7.25rem_8.5rem_minmax(0,1fr)_minmax(0,15rem)_7.5rem_2rem]';

/**
 * One change as a row: type, entity, the sentence, the ARN and when, with
 * the details and CloudTrail evidence behind a disclosure.
 */
export function ChangeRow({ change, layout = 'table' }) {
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const evidence = change.evidence;

  if (layout === 'timeline') {
    return (
      <li className="relative pl-6">
        <span aria-hidden="true" className={cn('absolute top-1.5 left-0 size-2.5 rounded-full border-2 border-surface', TONE_BG[CHANGE_META[change.changeType]?.tone ?? 'medium'])} />
        <div className="flex flex-wrap items-center gap-2">
          <ChangeBadge type={change.changeType} />
          <EntityLabel type={change.entityType} />
        </div>
        <p className="mt-1.5 text-[13px] font-medium text-ink">{change.summary}</p>
        {change.arn && (
          <p className="mt-0.5 min-w-0 text-[11.5px] text-ink-3">
            <CopyableValue value={change.arn} className="max-w-full" />
          </p>
        )}
        {evidence && (
          <div className="mt-2.5 max-w-2xl">
            <EvidenceCard evidence={evidence} changeType={change.changeType} />
          </div>
        )}
        <button
          type="button"
          aria-expanded={open}
          aria-controls={panelId}
          onClick={() => setOpen((value) => !value)}
          className="mt-2 inline-flex items-center gap-1 text-[12px] font-medium text-brand hover:underline"
        >
          {open ? 'Hide details' : 'Show details'}
          <ChevronDown aria-hidden="true" className={cn('size-3.5 transition-transform', open && 'rotate-180')} />
        </button>
        {open && (
          <div id={panelId} className="@container mt-2.5 max-w-3xl rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
            <ChangeDetails change={change} />
          </div>
        )}
      </li>
    );
  }

  return (
    <li className="border-b border-line last:border-b-0">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((value) => !value)}
        className={cn(
          'grid w-full items-center gap-x-4 gap-y-2 px-4 py-3 text-left transition-colors hover:bg-surface-2/70 sm:px-5',
          CHANGE_GRID,
          open && 'bg-surface-2/70',
        )}
      >
        <span className="flex items-center gap-2">
          <ChangeBadge type={change.changeType} />
        </span>
        <EntityLabel type={change.entityType} />
        <span className="min-w-0">
          <span className="block text-[13px] font-medium text-ink">{change.summary}</span>
          <span className="mt-0.5 block truncate text-[11.5px] text-ink-3">
            {[change.name, change.accountId, change.region].filter(Boolean).join(' · ')}
          </span>
        </span>
        <span className="hidden min-w-0 truncate font-mono text-[11.5px] text-ink-3 @min-[56rem]:block" title={change.arn}>
          {change.arn}
        </span>
        <span className="text-[12px] whitespace-nowrap text-ink-3" title={formatDateTime(change.detectedAt)}>
          {formatRelative(change.detectedAt)}
        </span>
        <ChevronDown aria-hidden="true" className={cn('hidden size-4 justify-self-end text-ink-3 transition-transform @min-[56rem]:block', open && 'rotate-180')} />
      </button>
      {open && (
        <div id={panelId} className="@container border-t border-line bg-surface-2/40 px-4 py-4 sm:px-5">
          <div className="grid gap-5 @min-[60rem]:grid-cols-[minmax(0,1fr)_minmax(0,20rem)]">
            <div className="min-w-0">
              <p className="mb-3 min-w-0 text-[12px] text-ink-3">
                <CopyableValue value={change.arn} />
              </p>
              <ChangeDetails change={change} />
            </div>
            {evidence && <EvidenceCard evidence={evidence} changeType={change.changeType} />}
          </div>
        </div>
      )}
    </li>
  );
}
