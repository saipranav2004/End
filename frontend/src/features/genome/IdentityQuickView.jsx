import { Link, useNavigate } from 'react-router-dom';
import { ArrowRight, Boxes } from 'lucide-react';
import { fetchGenomeIdentity, fetchGenomeTimeline } from '../../lib/api/genome';
import { genomeRoute } from '../../lib/identityLinks';
import { useQuery } from '../../lib/hooks';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { CopyableValue } from '../../ui/Copyable';
import { Drawer } from '../../ui/Overlay';
import { Meter } from '../../ui/Meter';
import { SectionLabel } from '../../ui/Panel';
import { Skeleton } from '../../ui/Skeleton';
import { InlineError } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import {
  anomalyStatusMeta,
  baselineMeta,
  dimensionLabel,
  learningPercent,
  Measured,
  parseAnomaly,
  Row,
  sourceTypeLabel,
} from './meta';

const QUICK_TIMELINE = 25;

/**
 * Quick view of one monitored identity, opened from the fleet list. The row
 * already carries the list fields; this adds what only the identity endpoints
 * know (calls in the last 14 days, the newest anomalies) so the operator can
 * decide whether the full genome is worth opening.
 */
export function IdentityQuickView({ identity, onClose }) {
  const navigate = useNavigate();
  if (!identity) return null;

  const baseline = baselineMeta(identity.baseline?.status);
  const route = genomeRoute(identity.source_type, identity.identity_name);

  return (
    <Drawer
      open={Boolean(identity)}
      onClose={onClose}
      eyebrow={sourceTypeLabel(identity.source_type)}
      title={identity.display_name || identity.identity_name}
      subtitle={`${identity.account_id}${identity.region ? ` · ${identity.region}` : ''}${identity.status ? ` · ${identity.status}` : ''}`}
      header={
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <Tag tone={baseline.tone} size="sm" dot>
            {baseline.label}
          </Tag>
          {identity.risk?.score !== null && identity.risk?.score !== undefined && (
            <Tag tone="neutral" size="sm">
              Latest risk {identity.risk.score}/100
            </Tag>
          )}
          <span className="text-[11.5px] text-ink-3">
            {identity.last_seen ? `Last seen ${formatRelative(identity.last_seen)}` : 'No calls seen yet'}
          </span>
        </div>
      }
      footer={
        <>
          <Button
            variant="secondary"
            as={Link}
            to={`/identities?search=${encodeURIComponent(identity.identity_name)}`}
            icon={Boxes}
          >
            Open in inventory
          </Button>
          <Button
            variant="primary"
            iconRight={ArrowRight}
            onClick={() => {
              onClose();
              navigate(route);
            }}
          >
            Open genome
          </Button>
        </>
      }
    >
      <QuickBody key={identity.id} identity={identity} />
    </Drawer>
  );
}

function QuickBody({ identity }) {
  const st = identity.source_type;
  const name = identity.identity_name;
  const summary = useQuery((signal) => fetchGenomeIdentity(st, name, {}, signal), [st, name]);
  const timeline = useQuery((signal) => fetchGenomeTimeline(st, name, { limit: QUICK_TIMELINE }, signal), [st, name]);

  const pct = learningPercent(identity.baseline?.built_days, identity.baseline?.required_days);
  const anomalies = (timeline.data?.items ?? []).filter((item) => item.type === 'anomaly').slice(0, 5);
  const metrics = summary.data?.headline_metrics;

  return (
    <div className="flex flex-col gap-5 px-4 py-4 sm:px-5">
      <div>
        <SectionLabel>Last 14 days</SectionLabel>
        {summary.isError && !summary.data ? (
          <div className="mt-2">
            <InlineError error={summary.error} onRetry={summary.refetch} label="Headline metrics failed" />
          </div>
        ) : (
          <dl className="mt-2 grid grid-cols-3 gap-3">
            <Stat label="Calls" loading={!summary.data}>
              <Measured value={metrics?.calls_in_window} empty="-" />
            </Stat>
            <Stat label="Open anomalies" loading={!summary.data}>
              <Measured value={metrics?.open_anomalies} empty="-" />
            </Stat>
          </dl>
        )}
      </div>

      <div>
        <SectionLabel>Baseline</SectionLabel>
        {pct === null ? (
          <p className="mt-2 text-[12.5px] text-ink-3">
            Learning has not started for this identity, so it cannot raise an anomaly yet.
          </p>
        ) : (
          <div className="mt-2 flex items-center gap-3">
            <Meter
              value={pct}
              tone={identity.baseline?.status === 'active' ? 'low' : 'medium'}
              height={4}
              className="min-w-0 flex-1"
              label="Learning progress"
            />
            <span data-numeric="" className="shrink-0 text-[12.5px] font-semibold text-ink">
              {formatNumber(identity.baseline.built_days)} of {formatNumber(identity.baseline.required_days)} days
            </span>
          </div>
        )}
      </div>

      <div>
        <SectionLabel>Newest anomalies</SectionLabel>
        {timeline.isError && !timeline.data ? (
          <div className="mt-2">
            <InlineError error={timeline.error} onRetry={timeline.refetch} label="Anomalies failed" />
          </div>
        ) : !timeline.data ? (
          <div className="mt-2 flex flex-col gap-2">
            <Skeleton className="h-10" />
            <Skeleton className="h-10" />
          </div>
        ) : anomalies.length === 0 ? (
          <p className="mt-2 text-[12.5px] text-ink-3">No anomaly is on record for this identity.</p>
        ) : (
          <ul className="mt-2 flex flex-col gap-2">
            {anomalies.map((item) => {
              const { dimension, reason } = parseAnomaly(item);
              const status = anomalyStatusMeta(item.status);
              return (
                <li
                  key={`${item.occurred_at}-${item.description}`}
                  className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2"
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <Tag tone={status.tone} size="sm">
                      {status.label}
                    </Tag>
                    {dimension && (
                      <Tag tone="neutral" size="sm">
                        {dimensionLabel(dimension)}
                      </Tag>
                    )}
                    <span className="ml-auto text-[11px] text-ink-3" title={formatDateTime(item.occurred_at)}>
                      {formatRelative(item.occurred_at)}
                    </span>
                  </div>
                  <p className="mt-1 text-[12.5px] text-ink-2">{reason || item.title}</p>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <div>
        <SectionLabel>Detail</SectionLabel>
        <dl className="mt-2 flex flex-col">
          <Row label="ARN">
            <CopyableValue value={identity.identity_name} />
          </Row>
          {identity.role_arn && (
            <Row label="Role">
              <CopyableValue value={identity.role_arn} />
            </Row>
          )}
          {identity.service_name && <Row label="Service">{identity.service_name}</Row>}
          {identity.role_type && <Row label="Role type">{identity.role_type}</Row>}
          <Row label="Workload key">
            <span className="font-mono text-[11.5px] break-all">{identity.workload_key}</span>
          </Row>
          <Row label="Last seen">{identity.last_seen ? formatDateTime(identity.last_seen) : 'No calls seen yet'}</Row>
        </dl>
      </div>
    </div>
  );
}

function Stat({ label, loading, children }) {
  return (
    <div className="min-w-0 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2">
      <dt className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{label}</dt>
      <dd data-numeric="" className="mt-1 truncate text-[15px] font-semibold text-ink">
        {loading ? <Skeleton className="h-4 w-10" /> : children}
      </dd>
    </div>
  );
}
