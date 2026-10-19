import { Link } from 'react-router-dom';
import { Dna } from 'lucide-react';
import { fetchGenomeIdentity } from '../../lib/api/genome';
import { canonicalGenomeName, genomeRoute, genomeSourceType } from '../../lib/identityLinks';
import { useQuery } from '../../lib/hooks';
import { formatNumber } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Meter } from '../../ui/Meter';
import { Tag } from '../../ui/Tag';
import { baselineMeta, learningPercent } from '../genome/meta';

/**
 * Where this identity is in NHI Genome: still learning its normal behaviour
 * (with progress), or watched against an established baseline (with its open
 * anomalies). Identities still learning are not listed on the NHI Genome page,
 * so this is where their progress is shown. Hidden for identity types the
 * genome does not watch, and when the genome does not know the identity.
 */
export function BehaviourBaseline({ identity }) {
  const sourceType = genomeSourceType(identity.identity_type);
  const identityName = sourceType && identity.arn ? canonicalGenomeName(identity.identity_type, identity.arn) : null;
  const record = useQuery(
    (signal) => (identityName ? fetchGenomeIdentity(sourceType, identityName, {}, signal).catch(() => null) : Promise.resolve(null)),
    [sourceType, identityName],
  );
  const data = record.data;
  if (!identityName || !data) return null;

  const summary = data.baseline_summary ?? {};
  const meta = baselineMeta(summary.status);
  const route = genomeRoute(sourceType, identityName);
  const open = data.headline_metrics?.open_anomalies;

  if (meta.key === 'active') {
    return (
      <Strip>
        <Tag tone="low" size="sm" dot>
          Baseline established
        </Tag>
        <span className="min-w-0 flex-1 text-[12.5px] text-ink-2">
          NHI Genome knows this identity's normal behaviour and raises an anomaly when it departs from it.
          {open ? (
            <span className="font-medium text-high"> {formatNumber(open)} open {open === 1 ? 'anomaly' : 'anomalies'} to review.</span>
          ) : (
            ' Nothing is open.'
          )}
        </span>
        <Button variant="secondary" size="sm" as={Link} to={open ? `${route}?tab=anomalies` : route} icon={Dna}>
          {open ? 'Review anomalies' : 'Open in NHI Genome'}
        </Button>
      </Strip>
    );
  }

  const pct = learningPercent(summary.built_days, summary.required_days);
  const remaining = Number(summary.required_days) - Number(summary.built_days);
  const ready = Number.isFinite(remaining) && remaining > 0 ? remaining : null;
  return (
    <Strip>
      <Tag tone="medium" size="sm" dot>
        {meta.key === 'none' ? 'Not learning yet' : 'Learning its baseline'}
      </Tag>
      <span className="min-w-0 flex-1 text-[12.5px] text-ink-2">
        <span className="block">
          {meta.key === 'none'
            ? 'NHI Genome has not started learning this identity’s normal behaviour - it starts once the identity makes calls. No anomaly can be raised until then.'
            : `NHI Genome is learning this identity’s normal behaviour: ${formatNumber(summary.built_days)} of ${formatNumber(summary.required_days)} days.${ready ? ` Anomalies can be raised in about ${formatNumber(ready)} ${ready === 1 ? 'day' : 'days'}.` : ''}`}
        </span>
        {pct !== null && <Meter value={pct} tone="medium" height={4} className="mt-2 max-w-xs" label="Learning progress" />}
      </span>
      <Button variant="ghost" size="sm" as={Link} to={route} icon={Dna}>
        Details
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
