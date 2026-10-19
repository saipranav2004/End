import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import {
  Box,
  ChevronDown,
  ChevronRight,
  CircleHelp,
  Database,
  Globe,
  KeyRound,
  Layers,
  Loader2,
  UserCircle,
} from 'lucide-react';
import { KIND_LABEL, riskReason, toneFor } from './graphTheme';
import { cn } from '../../ui/cn';

/**
 * The node itself.
 *
 * Three jobs, in this order: say what it is, say whether it is a problem, and
 * say whether there is more behind it. Everything else belongs in the panel
 * beside the graph - a node that tries to be a record is a node nobody can
 * scan.
 *
 * Kind is carried by the icon and the small label under the name, never by
 * hue. See `graphTheme.js` for the measurements behind that: with seven node
 * kinds no colour assignment stays distinguishable when any two nodes can sit
 * side by side, so colour is spent on the one thing worth interrupting for.
 */

const KIND_ICON = {
  federated: Globe,
  service: Box,
  identity: UserCircle,
  credential: KeyRound,
  resource: Database,
  account: Layers,
  other: CircleHelp,
};

export const AccessNode = memo(function AccessNode({ data, selected }) {
  const tone = toneFor(data);
  const Icon = KIND_ICON[data.kind] ?? Box;
  const risk = riskReason(data);
  /* The focus is always open - its hidden neighbours sit behind the fold in
     the next column - so it gets no chevron. A control whose click changes
     nothing is worse than no control. */
  const canExpand = data.unseenCount > 0 && !data.isFocus;
  /* Its own neighbourhood is still being read, so how much is behind it is
     not known yet. Said quietly rather than with a chevron that may lie. */
  const counting = data.pending && !data.isFocus && !canExpand;
  const label = data.typeLabel || KIND_LABEL[data.kind] || data.kind;

  return (
    <div
      className={cn(
        'access-node group relative flex h-[56px] w-[244px] items-center gap-2.5 rounded-[10px] border px-2.5 text-left',
        tone === 'critical' ? 'access-node-critical' : 'access-node-neutral',
        selected && 'access-node-selected',
        data.isFocus && 'access-node-focus',
      )}
      /* The whole node is the target. React Flow handles the click, so this is
         a div rather than a button - but it still has to announce itself. */
      role="button"
      tabIndex={-1}
      aria-label={`${label}: ${data.name}${risk ? `. ${risk}` : ''}${canExpand ? `. ${data.unseenCount} more connected, ${data.expanded ? 'open' : 'closed'}` : ''}`}
      title={risk ? `${data.name} - ${risk}` : data.name}
    >
      <Handle type="target" position={Position.Left} className="access-handle" isConnectable={false} />

      <span className={cn('access-node-icon', tone === 'critical' && 'access-node-icon-critical')}>
        <Icon aria-hidden="true" className="size-3.5" />
      </span>

      <span className="min-w-0 flex-1">
        {/* Two lines, because an IAM name is often a path or an OIDC issuer
            and one line turns `token.actions.githubusercontent.com` into
            `token.actions.githubuse...`, which names nothing. */}
        <span className="block text-[12px] leading-[1.25] font-semibold text-ink line-clamp-2 break-words">
          {data.name}
        </span>
        <span className="mt-0.5 flex items-center gap-1 text-[10px] leading-none text-ink-3">
          <span className="truncate">{label}</span>
          {data.accountName && (
            <>
              <span aria-hidden="true">·</span>
              <span className="truncate">{data.accountName}</span>
            </>
          )}
        </span>
      </span>

      {/* The expansion affordance doubles as the count, so one glyph answers
          both "is there more" and "how much". `data-expand` is what tells the
          canvas this click opens the node rather than selecting it. */}
      {canExpand && (
        <span
          data-expand=""
          className={cn(
            'access-node-more nodrag nopan',
            data.expanded && 'access-node-more-open',
          )}
          aria-hidden="true"
        >
          {data.expanded ? (
            <ChevronDown className="size-3" />
          ) : (
            <>
              <ChevronRight className="size-3" />
              <span data-numeric="" className="text-[9.5px] font-bold">
                {data.unseenCount}
              </span>
            </>
          )}
        </span>
      )}

      {counting && (
        <Loader2 aria-hidden="true" className="size-3 shrink-0 animate-spin text-ink-3" />
      )}

      <Handle type="source" position={Position.Right} className="access-handle" isConnectable={false} />
    </div>
  );
});

/**
 * The fold-away.
 *
 * Stands for the neighbours the budget left out, and says how many and of what
 * kind - "+6 more" alone makes the reader open it to find out whether it was
 * worth opening. Clicking reveals two at a time, so the graph grows at a pace
 * the eye can follow rather than doubling in one step.
 */
export const MoreNode = memo(function MoreNode({ data }) {
  return (
    <div
      className="access-more group flex h-[32px] w-[244px] items-center gap-2 rounded-full border px-2.5"
      role="button"
      tabIndex={-1}
      aria-label={`Show more from ${data.parentName}: ${data.hiddenCount} hidden, ${data.summary}`}
      title={`${data.summary} - hidden to keep the graph readable`}
    >
      <Handle type="target" position={Position.Left} className="access-handle" isConnectable={false} />
      <span className="access-more-badge" data-numeric="">
        +{data.hiddenCount}
      </span>
      <span className="min-w-0 flex-1 truncate text-[10.5px] leading-none text-ink-2">
        {data.summary}
      </span>
      <ChevronRight aria-hidden="true" className="size-3 shrink-0 text-ink-3" />
    </div>
  );
});

export const nodeTypes = { access: AccessNode, more: MoreNode };
