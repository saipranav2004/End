import { Link } from 'react-router-dom';
import { GitCompareArrows } from 'lucide-react';
import { fetchEntityChanges, scanChangesRoute } from '../../lib/api/changes';
import { formatNumber, formatRelative } from '../../lib/format';
import { useQuery } from '../../lib/hooks';
import { DetailSkeleton } from '../../ui/Skeleton';
import { EmptyState, InlineError } from '../../ui/States';
import { ChangeRow } from './parts';
import { scanTimeLabel } from './RecentChangesCard';

/**
 * The Changes tab of an identity (kind 'identity') or credential
 * ('credential'): every change discovery recorded for it, newest scan first,
 * grouped by scan. Loaded only when the tab is opened.
 */
export function EntityChangesTab({ kind, id }) {
  const query = useQuery((signal) => fetchEntityChanges(kind, id, signal), [kind, id], { enabled: Boolean(id) });
  const noun = kind === 'credential' ? 'credential' : 'identity';

  if (query.isError && !query.data) {
    if (query.error?.status === 404) {
      return <EmptyState compact title={`${noun === 'identity' ? 'Identity' : 'Credential'} not found`} description="A later discovery run may have removed it." />;
    }
    return <InlineError error={query.error} onRetry={query.refetch} label="Changes unavailable" />;
  }
  if (!query.data) return <DetailSkeleton rows={5} />;

  const { changes, truncated, available } = query.data;
  if (changes.length === 0) {
    return (
      <EmptyState
        compact
        icon={GitCompareArrows}
        title={`No changes recorded for this ${noun}`}
        description={
          available
            ? `Each discovery scan compares the ${noun} with the scan before it. Anything that changes will be listed here.`
            : 'Change tracking is not set up for this workspace yet.'
        }
      />
    );
  }

  const groups = [];
  for (const change of changes) {
    const last = groups[groups.length - 1];
    if (last && last.scanId === change.scanId) last.items.push(change);
    else groups.push({ scanId: change.scanId, scanAt: change.scanAt, items: [change] });
  }

  return (
    <div className="@container flex flex-col gap-6">
      <p className="max-w-3xl text-[12.5px] leading-relaxed text-ink-3">
        What each discovery scan found had changed on this {noun}, newest first. Creations and deletions show the CloudTrail call
        behind them when it was recorded.
      </p>
      {groups.map((group) => (
        <section key={group.scanId} aria-label={`Scan of ${scanTimeLabel(group.scanAt)}`}>
          <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2 border-b border-line pb-2">
            <h3 className="text-[12.5px] font-semibold text-ink">
              Scan · {scanTimeLabel(group.scanAt)}
              <span className="ml-2 font-normal text-ink-3">{formatRelative(group.scanAt)}</span>
            </h3>
            <Link to={scanChangesRoute(group.scanId)} className="text-[12px] font-medium text-brand hover:underline">
              Everything this scan changed
            </Link>
          </div>
          <ol className="relative flex flex-col gap-5 before:absolute before:top-2 before:bottom-2 before:left-[4px] before:w-px before:bg-line">
            {group.items.map((change) => (
              <ChangeRow key={change.id} change={change} layout="timeline" />
            ))}
          </ol>
        </section>
      ))}
      {truncated && (
        <p className="text-[12px] text-ink-3">
          Showing the latest {formatNumber(changes.length)} changes. Older scans are on the scan changes pages.
        </p>
      )}
    </div>
  );
}
