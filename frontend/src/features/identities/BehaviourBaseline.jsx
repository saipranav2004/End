import { Link } from 'react-router-dom';
import { Dna } from 'lucide-react';
import { fetchGenomeTimeline } from '../../lib/api/genome';
import { canonicalGenomeName, genomeRoute, genomeSourceType } from '../../lib/identityLinks';
import { useQuery } from '../../lib/hooks';
import { formatDate, formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Tag } from '../../ui/Tag';

/**
 * Where this identity is in NHI Genome, read from its genome timeline:
 * baseline established (with any open anomalies), learning since a date, or
 * not learning yet. Hidden for identity types the genome does not watch, and
 * when the genome does not know the identity in this scope.
 */
export function BehaviourBaseline({ identity }) {
  const sourceType = genomeSourceType(identity.identity_type);
  const identityName = sourceType && identity.arn ? canonicalGenomeName(identity.identity_type, identity.arn) : null;
  const timeline = useQuery(
    (signal) =>
      identityName
        ? fetchGenomeTimeline(sourceType, identityName, { limit: 50 }, signal).catch(() => null)
        : Promise.resolve(null),
    [sourceType, identityName],
  );
  const items = timeline.data?.items;
  if (!identityName || !items) return null;

  const route = genomeRoute(sourceType, identityName);
  const established = items.find((item) => item.type === 'baseline_established');
  const learning = items.find((item) => item.type === 'learning_started');
  const open = items
    .filter((item) => item.type === 'anomaly' && String(item.status).toLowerCase() === 'open')
    .reduce((sum, item) => sum + (Number(/^(\d+)/.exec(item.title ?? '')?.[1]) || 1), 0);

  if (established) {
    return (
      <Strip>
        <Tag tone="low" size="sm" dot>
          Baseline established
        </Tag>
        <span className="min-w-0 flex-1 text-[12.5px] text-ink-2">
          NHI Genome has known this identity’s normal behaviour since{' '}
          <span title={formatDateTime(established.occurred_at)}>{formatDate(established.occurred_at)}</span> and flags
          departures from it.
          {open ? (
            <span className="font-medium text-high"> {formatNumber(open)} open {open === 1 ? 'anomaly' : 'anomalies'}.</span>
          ) : (
            ' Nothing is open.'
          )}
        </span>
        <Button variant="secondary" size="sm" as={Link} to={open ? `${route}?tab=timeline` : route} icon={Dna}>
          {open ? 'See the anomalies' : 'Open in NHI Genome'}
        </Button>
      </Strip>
    );
  }

  return (
    <Strip>
      <Tag tone={learning ? 'medium' : 'neutral'} size="sm" dot>
        {learning ? 'Learning its baseline' : 'Not learning yet'}
      </Tag>
      <span className="min-w-0 flex-1 text-[12.5px] text-ink-2">
        {learning ? (
          <>
            NHI Genome started learning this identity’s normal behaviour{' '}
            <span title={formatDateTime(learning.occurred_at)}>{formatRelative(learning.occurred_at)}</span>. Nothing is
            flagged until the baseline is established.
          </>
        ) : (
          'NHI Genome reads this identity’s calls; its baseline starts once the behaviour pipeline has learned from them. You can already see its fingerprint and calls.'
        )}
      </span>
      <Button variant="ghost" size="sm" as={Link} to={route} icon={Dna}>
        Open in NHI Genome
      </Button>
    </Strip>
  );
}

function Strip({ children }) {
  return (
    <div className="animate-rise flex flex-wrap items-center gap-x-3 gap-y-2 rounded-[var(--radius-panel)] border border-line bg-surface px-4 py-3 shadow-[var(--t-shadow-sm)]">
      {children}
    </div>
  );
}
