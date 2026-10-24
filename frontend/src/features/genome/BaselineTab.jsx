import { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { fetchGenomeBaseline, fetchGenomeDrift, fetchGenomePeers } from '../../lib/api/genome';
import { useQuery } from '../../lib/hooks';
import { formatDate, formatDateTime, formatNumber, formatRelative, titleCaseEnum } from '../../lib/format';
import { genomeRoute } from '../../lib/identityLinks';
import { Meter } from '../../ui/Meter';
import { Panel, PanelHeader, DetailList, DetailRow } from '../../ui/Panel';
import { EmptyState, InlineError } from '../../ui/States';
import { ListSkeleton } from '../../ui/Skeleton';
import { Tag } from '../../ui/Tag';
import { dimensionLabel } from './meta';

const known = (value) => value !== null && value !== undefined && Number.isFinite(Number(value));
const pct = (value) => (known(value) ? `${Math.round(Number(value))}%` : null);
const NOT_MEASURED = <span className="text-ink-3">Not measured yet</span>;

/**
 * How trustworthy this identity's normal behaviour is: how much of the learning
 * period is done, whether its behaviour is drifting, and how it compares with
 * identities like it (same kind of workload, account and region).
 */
export function BaselineTab({ sourceType, identityName }) {
  const baseline = useQuery((signal) => fetchGenomeBaseline(sourceType, identityName, signal), [sourceType, identityName]);
  const drift = useQuery((signal) => fetchGenomeDrift(sourceType, identityName, signal), [sourceType, identityName]);
  const peers = useQuery((signal) => fetchGenomePeers(sourceType, identityName, signal), [sourceType, identityName]);

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-4 @min-[60rem]:grid-cols-2">
        <LearningPanel query={baseline} />
        <DriftPanel query={drift} />
      </div>
      <PeersPanel query={peers} sourceType={sourceType} />
    </div>
  );
}

function LearningPanel({ query }) {
  const b = query.data;
  const progress = b && b.required_days > 0 ? Math.min(100, (Number(b.built_days) / Number(b.required_days)) * 100) : null;
  return (
    <Panel prominence="lead" className="animate-rise">
      <PanelHeader prominence="lead" title="Learning progress" subtitle="Normal behaviour is learned from the identity's own calls over a learning period." />
      {query.isError && !b ? (
        <InlineError error={query.error} onRetry={query.refetch} />
      ) : !b ? (
        <ListSkeleton rows={4} />
      ) : (
        <div className="mt-4 flex flex-col gap-4">
          <div>
            <div className="flex items-baseline justify-between gap-3">
              <p className="text-[13px] font-medium text-ink">
                {b.status === 'active' ? 'Baseline established' : b.status ? titleCaseEnum(b.status) : 'Learning'}
              </p>
              <p className="text-[12px] text-ink-3" data-numeric="">
                {formatNumber(b.built_days)} of {formatNumber(b.required_days)} days
              </p>
            </div>
            {progress !== null && <Meter className="mt-2" value={progress} tone={progress >= 100 ? 'low' : 'brand'} label="Share of the learning period completed" />}
            {b.days_remaining > 0 && <p className="mt-1.5 text-[12px] text-ink-3">{formatNumber(b.days_remaining)} more days of activity are needed.</p>}
          </div>
          <DetailList>
            <DetailRow label="First seen">{b.first_seen_date ? formatDate(b.first_seen_date) : NOT_MEASURED}</DetailRow>
            <DetailRow label="Baseline active since">{b.active_since ? <span title={formatDateTime(b.active_since)}>{formatDate(b.active_since)}</span> : NOT_MEASURED}</DetailRow>
            <DetailRow label="Last updated">{b.updated_at ? <span title={formatDateTime(b.updated_at)}>{formatRelative(b.updated_at)}</span> : NOT_MEASURED}</DetailRow>
            <DetailRow label="How sure the model is">{pct(b.model_confidence_percent) ?? NOT_MEASURED}</DetailRow>
            <DetailRow label="Versions published">{formatNumber((b.versions ?? []).length)}</DetailRow>
          </DetailList>
        </div>
      )}
    </Panel>
  );
}

function DriftPanel({ query }) {
  const d = query.data;
  const dimensions = useMemo(() => (Array.isArray(d?.dimensions) ? d.dimensions : []), [d]);
  const measured = d && (d.status || known(d.drift));
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
      <PanelHeader prominence="quiet" title="Is its behaviour drifting?" subtitle="Drift is how far recent behaviour has moved from the learned normal." />
      {query.isError && !d ? (
        <InlineError error={query.error} onRetry={query.refetch} />
      ) : !d ? (
        <ListSkeleton rows={4} />
      ) : !measured ? (
        <EmptyState compact title="No drift measured yet" description="Drift is scored once the baseline is established and the pipeline has run its checks." />
      ) : (
        <div className="mt-3 flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            {d.status && <Tag tone={/stale|retrain|drift/i.test(d.status) ? 'high' : 'low'} size="sm" dot>{titleCaseEnum(d.status)}</Tag>}
            {d.retraining_status && <Tag tone="neutral" size="sm">Retraining: {titleCaseEnum(d.retraining_status)}</Tag>}
          </div>
          <DetailList>
            <DetailRow label="Drift score">{known(d.drift) ? Number(d.drift).toFixed(2) : NOT_MEASURED}</DetailRow>
            <DetailRow label="Model confidence">{pct(d.model_confidence_percent) ?? NOT_MEASURED}</DetailRow>
            <DetailRow label="Biggest change">{d.psi_max_dimension ? dimensionLabel(d.psi_max_dimension) : NOT_MEASURED}</DetailRow>
          </DetailList>
          {Array.isArray(d.reasons) && d.reasons.length > 0 && (
            <ul className="list-disc pl-5 text-[12.5px] text-ink-2">
              {d.reasons.map((reason) => (
                <li key={String(reason)}>{String(reason)}</li>
              ))}
            </ul>
          )}
          {dimensions.length > 0 && (
            <ul className="flex flex-col gap-1.5 border-t border-line pt-3">
              {dimensions.map((row, index) => (
                <li key={row.dimension ?? index} className="flex items-center justify-between gap-3 text-[12.5px]">
                  <span className="text-ink-2">{dimensionLabel(row.dimension)}</span>
                  <span className="text-ink-3" data-numeric="">{known(row.psi) ? `change ${Number(row.psi).toFixed(2)}` : ''}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </Panel>
  );
}

function PeersPanel({ query, sourceType }) {
  const peers = query.data;
  return (
    <Panel prominence="quiet" className="animate-rise" data-stagger="" style={{ '--stagger': 2 }}>
      <PanelHeader prominence="quiet" title="Compared with similar identities" subtitle="Identities of the same kind of workload in the same account and region." />
      {query.isError && !peers ? (
        <InlineError error={query.error} onRetry={query.refetch} />
      ) : !peers ? (
        <ListSkeleton rows={3} />
      ) : peers.length === 0 ? (
        <EmptyState compact title="No similar identities" description="This is the only identity of its kind here, so there is nothing to compare it with." />
      ) : (
        <ul className="mt-3 divide-y divide-line">
          {peers.map((peer) => {
            const name = peer.display_name || peer.identity_name;
            return (
              <li key={peer.identity_name} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2">
                <div className="min-w-0">
                  {sourceType && peer.identity_name ? (
                    <Link className="block min-w-0 truncate text-[13px] font-medium text-brand hover:underline" to={genomeRoute(sourceType, peer.identity_name)}>
                      {name}
                    </Link>
                  ) : (
                    <span className="text-[13px]">{name}</span>
                  )}
                  {peer.outlier_dimension && (
                    <span className="block text-[11.5px] text-high">Stands out on {dimensionLabel(peer.outlier_dimension).toLowerCase()}</span>
                  )}
                </div>
                <span className="text-[12px] text-ink-3" data-numeric="">
                  {known(peer.recent_call_count) ? `${formatNumber(peer.recent_call_count)} recent calls` : 'Calls not measured'}
                  {known(peer.open_anomaly_count) && peer.open_anomaly_count > 0 ? ` · ${formatNumber(peer.open_anomaly_count)} open` : ''}
                  {known(peer.risk_score) ? ` · risk ${peer.risk_score}` : ''}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </Panel>
  );
}
