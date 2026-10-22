import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Crosshair, ExternalLink, Fingerprint, Radius } from 'lucide-react';
import { findPosture, getCredentialTab, getIdentityTab } from '../../lib/api/access';
import { useQuery } from '../../lib/hooks/useQuery';
import { accessDetailRoute, genomeRouteFor, identityRoute } from '../../lib/identityLinks';
import { formatDateTime, formatNumber, formatRelative, humanizeToken } from '../../lib/format';
import { severityMeta } from '../../lib/domain';
import { Button } from '../../ui/Button';
import { CopyButton } from '../../ui/Copyable';
import { Panel, SectionLabel } from '../../ui/Panel';
import { Meter } from '../../ui/Meter';
import { DetailSkeleton } from '../../ui/Skeleton';
import { InlineError } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { cn } from '../../ui/cn';
import { KIND_LABEL, riskReason } from './graphTheme';
import { NODE_KINDS, edgeKindMeta, edgeLabel, edgesOf, subtypeLabel } from './graphModel';

/**
 * The selected node, in tabs.
 *
 * ── Why tabs and not one scrolling column ───────────────────────────────────
 * A 340px column holding a node's record, its connections, its activity, its
 * permissions and its credentials is a long scroll where the thing you want is
 * never on screen. Tabs make each question one click, and the tab row itself
 * is the list of questions this panel can answer.
 *
 * ── Where each tab comes from ───────────────────────────────────────────────
 *   Summary      identity or credential overview tab (+ posture score, when
 *                the posture service knows the ARN)
 *   Connections  the graph store: every edge read so far that touches it
 *   Activity     the activity tab's analytics, last 30 days (CloudTrail)
 *   Permissions  the identity permissions tab: granted against used
 *   Credentials  the identity credentials tab / the credential's bound
 *                identities
 *   Trust        the credential trust tab
 * A tab is shown only when its source answered with something to show, so the
 * row never offers a click that leads to "nothing here".
 */

const ALL_TABS = [
  { value: 'summary', label: 'Summary' },
  { value: 'connections', label: 'Relationships' },
  { value: 'activity', label: 'Activity' },
  { value: 'permissions', label: 'What it can do' },
  { value: 'credentials', label: 'Credentials it uses' },
  { value: 'bound', label: 'Used by' },
  { value: 'trust', label: 'Who can use it' },
];

export const PRIVILEGE_LEVELS = {
  4: { label: 'Administrator', tone: 'critical' },
  3: { label: 'High', tone: 'high' },
  2: { label: 'Medium', tone: 'medium' },
  1: { label: 'Low', tone: 'low' },
};

const BAND_TONE = { CRITICAL: 'critical', POOR: 'high', FAIR: 'medium', HEALTHY: 'low' };

const settle = (promise) =>
  promise.then(
    (value) => ({ value }),
    (error) => ({ error }),
  );

/** Everything the panel shows about one node, read in parallel. */
async function loadDetail(node, signal) {
  if (node.identityId) {
    const id = node.identityId;
    const [overview, credentials, activity, permissions] = await Promise.all([
      settle(getIdentityTab(id, 'overview', {}, signal)),
      settle(getIdentityTab(id, 'credentials', {}, signal)),
      settle(getIdentityTab(id, 'activity', { time_range: '30d', page_size: 1 }, signal)),
      settle(getIdentityTab(id, 'permissions', {}, signal)),
    ]);
    if (overview.error) throw overview.error;
    return { kind: 'identity', overview: overview.value, credentials, activity, permissions };
  }
  if (node.credentialId) {
    const id = node.credentialId;
    const [overview, trust, activity] = await Promise.all([
      settle(getCredentialTab(id, 'overview', {}, signal)),
      settle(getCredentialTab(id, 'trust', {}, signal)),
      settle(getCredentialTab(id, 'activity', { time_range: '30d', page_size: 1 }, signal)),
    ]);
    if (overview.error) throw overview.error;
    return { kind: 'credential', overview: overview.value, trust, activity };
  }
  return { kind: 'none' };
}

export function IdentityPanel({ node, isFocusNode, store, status, error, onRetry, onExpand, onFocus, expandedIds }) {
  const [tab, setTab] = useState('summary');

  const detail = useQuery((signal) => loadDetail(node, signal), [node?.id, node?.identityId, node?.credentialId], {
    enabled: Boolean(node),
    keepPrevious: false,
  });
  const posture = useQuery(() => findPosture(node.arn, node.name), [node?.arn], {
    enabled: Boolean(node?.identityId),
    keepPrevious: false,
  });

  const data = detail.data;
  const connections = useMemo(() => (node && store ? edgesOf(store, node.id) : []), [node, store]);

  const activity = data?.activity?.value;
  const activityTotal = activity?.events_pagination?.total_count ?? 0;
  const permissions = data?.permissions?.value;
  const heldCredentials = data?.credentials?.value?.credentials ?? [];
  const boundIdentities = data?.kind === 'credential' ? (data.overview?.bound_identities ?? []) : [];
  const trust = data?.trust?.value;

  /* Cheap enough to work out on every render. */
  const available = (() => {
    if (!node) return [];
    const has = {
      summary: true,
      connections: connections.length > 0,
      activity: Boolean(activity?.activity_data_available) && activityTotal > 0,
      permissions: (permissions?.summary?.granted_count ?? 0) > 0,
      credentials: heldCredentials.length > 0,
      bound: boundIdentities.length > 0,
      trust: Boolean(trust && ((trust.trust_policy?.principals ?? []).length > 0 || (trust.attached_policies ?? []).length > 0)),
    };
    const counts = {
      connections: connections.length,
      credentials: heldCredentials.length,
      bound: boundIdentities.length,
    };
    return ALL_TABS.filter((entry) => has[entry.value]).map((entry) =>
      counts[entry.value] ? { ...entry, count: counts[entry.value] } : entry,
    );
  })();

  const activeTab = available.some((entry) => entry.value === tab) ? tab : 'summary';

  if (!node) {
    return (
      <Panel prominence="quiet" className="animate-rise">
        <DetailSkeleton rows={5} />
      </Panel>
    );
  }

  const risk = riskReason(node);
  const isOpen = expandedIds.includes(node.id);
  const privilege = PRIVILEGE_LEVELS[node.privilegeLevel];
  const discoveryId = node.identityId ?? node.credentialId;

  return (
    /* The head does not scroll: the title and the tabs are pinned and only
       the pane below them moves, which is what makes a bounded panel usable. */
    <Panel
      prominence="lead"
      className="access-inspector animate-rise flex flex-col gap-3 self-start overflow-hidden"
      style={{ '--graph-max-h': '560px' }}
    >
      <div className="shrink-0">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">
              {isFocusNode ? 'Starting point' : 'Selected'} · {node.typeLabel || KIND_LABEL[node.kind] || node.kind}
            </p>
            <h2 className="mt-0.5 truncate text-[16px] font-semibold text-ink" title={node.name}>
              {node.name}
            </h2>
          </div>
          {node.unseenCount > 0 && !isFocusNode && (
            <Button variant="secondary" size="sm" onClick={() => onExpand(node)}>
              {isOpen ? 'Hide its connections' : `Show ${node.unseenCount} connected`}
            </Button>
          )}
        </div>

        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          {risk && (
            <Tag tone="critical" size="sm" dot>
              {risk}
            </Tag>
          )}
          {privilege && !node.isAdmin && (
            <Tag tone={privilege.tone} size="sm">
              {privilege.label} access
            </Tag>
          )}
          {node.externalTrust && (
            <Tag tone="high" size="sm" dot>
              Can be used from another account
            </Tag>
          )}
          {node.accountName && (
            <Tag tone="neutral" size="sm">
              Account {node.accountName}
            </Tag>
          )}
          {node.environment && (
            <Tag tone="neutral" size="sm">
              {node.environment}
            </Tag>
          )}
          {typeof node.depth === 'number' && node.depth > 0 && (
            <Tag tone="info" size="sm">
              {node.depth} step{node.depth === 1 ? '' : 's'} away
            </Tag>
          )}
        </div>
      </div>

      <div className="shrink-0">
        <PanelTabs tabs={available} value={activeTab} onChange={setTab} />
      </div>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain">
        {status === 'error' && (
          <InlineError error={error} onRetry={() => onRetry(node.id)} label="Its relationships could not be loaded" />
        )}

        {activeTab === 'summary' && (
          <SummaryPane
            node={node}
            isFocusNode={isFocusNode}
            detail={detail}
            posture={posture}
            discoveryId={discoveryId}
            onFocus={onFocus}
          />
        )}
        {activeTab === 'connections' && <ConnectionsPane node={node} connections={connections} />}
        {activeTab === 'activity' && <ActivityPane activity={activity} total={activityTotal} />}
        {activeTab === 'permissions' && <PermissionsPane permissions={permissions} identityId={node.identityId} />}
        {activeTab === 'credentials' && <CredentialList credentials={heldCredentials} />}
        {activeTab === 'bound' && <BoundList identities={boundIdentities} />}
        {activeTab === 'trust' && <TrustPane trust={trust} />}
      </div>
    </Panel>
  );
}

/* ── Panes ───────────────────────────────────────────────────────────────── */

function SummaryPane({ node, isFocusNode, detail, posture, discoveryId, onFocus }) {
  const record =
    detail.data?.kind === 'identity'
      ? detail.data.overview?.identity
      : detail.data?.kind === 'credential'
        ? detail.data.overview?.credential
        : null;
  const signals = (detail.data?.overview?.risk_signals ?? []).filter((signal) => signal.status);
  const genome = node.identityId ? genomeRouteFor({ identity_type: node.identityType, arn: node.arn }) : null;
  const score = posture.data;

  return (
    <>
      {detail.isError && (
        <InlineError error={detail.error} onRetry={detail.refetch} label="Its details could not be loaded" />
      )}

      {signals.length > 0 && (
        <div>
          <SectionLabel>Risks</SectionLabel>
          <ul className="mt-1.5 flex flex-col gap-1">
            {signals.map((signal) => {
              const meta = severityMeta(signal.severity);
              return (
                <li key={signal.signal} className="flex items-start gap-1.5">
                  <Tag tone={meta.tone} size="sm" dot>
                    {meta.label}
                  </Tag>
                  <span className="min-w-0 flex-1 text-[11.5px] leading-snug text-ink-2">{signal.signal}</span>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      <div>
        <SectionLabel>At a glance</SectionLabel>
        <dl className="mt-1.5 flex flex-col gap-1 text-[11.5px]">
          {!node.identityId && !node.credentialId && (
            <p className="text-[11.5px] leading-relaxed text-ink-2">{NODE_KINDS[node.kind]?.description}</p>
          )}
          {detail.isLoading && (node.identityId || node.credentialId) ? (
            <DetailSkeleton rows={3} />
          ) : (
            record && (
              <>
                <Row label="Status" value={humanizeToken(record.status)} warn={record.status && record.status !== 'active'} />
                {record.region && <Row label="Region" value={record.region} />}
                {'owner' in record && <Row label="Owner" value={record.owner} fallback="No owner recorded" warn={!record.owner} />}
                {record.created_by && <Row label="Created by" value={record.created_by.split('/').pop()} title={record.created_by} />}
                {record.last_activity_at !== undefined && (
                  <Row
                    label="Last activity"
                    value={record.last_activity_at ? formatRelative(record.last_activity_at) : null}
                    title={record.last_activity_at ? formatDateTime(record.last_activity_at) : undefined}
                    fallback="Never used"
                  />
                )}
                {record.last_used_at !== undefined && (
                  <Row
                    label="Last used"
                    value={record.last_used_at ? formatRelative(record.last_used_at) : null}
                    title={record.last_used_at ? formatDateTime(record.last_used_at) : undefined}
                    fallback="Never used"
                  />
                )}
                {typeof record.age_days === 'number' && <Row label="Age" value={`${formatNumber(record.age_days)} days`} />}
              </>
            )
          )}
        </dl>
      </div>

      {node.identityId && (
        <div>
          <SectionLabel>Security score (0-100)</SectionLabel>
          {posture.isLoading ? (
            <DetailSkeleton rows={1} />
          ) : score && typeof score.score === 'number' ? (
            <div className="mt-1.5 flex items-center justify-between gap-2">
              <p className="flex items-center gap-2 text-[12px] text-ink-2">
                <span>
                  <span data-numeric="" className="text-[18px] font-semibold text-ink">
                    {formatNumber(score.score)}
                  </span>{' '}
                  / 100
                </span>
                {score.band && (
                  <Tag tone={BAND_TONE[String(score.band).toUpperCase()] ?? 'neutral'} size="sm" dot>
                    {humanizeToken(String(score.band).toLowerCase())}
                  </Tag>
                )}
              </p>
              <Link to={identityRoute(node.identityId, 'posture')} className="text-[11.5px] text-brand hover:underline">
                How it is scored
              </Link>
            </div>
          ) : (
            <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-3">Not scored yet.</p>
          )}
        </div>
      )}

      {(discoveryId || genome) && (
        <div className="flex flex-wrap gap-1.5">
          {!isFocusNode && discoveryId && (
            <Button variant="secondary" size="sm" icon={Crosshair} onClick={() => onFocus(discoveryId)}>
              Start the graph here
            </Button>
          )}
          {node.identityId && (
            <Button variant="ghost" size="sm" as={Link} to={accessDetailRoute(node.identityId)} iconRight={Radius}>
              Full access detail
            </Button>
          )}
          {node.identityId && (
            <Button variant="ghost" size="sm" as={Link} to={identityRoute(node.identityId)} iconRight={ExternalLink}>
              Open identity
            </Button>
          )}
          {!node.identityId && node.credentialId && (
            <Button
              variant="ghost"
              size="sm"
              as={Link}
              to={`/credentials?search=${encodeURIComponent(node.name)}`}
              iconRight={ExternalLink}
            >
              Open credential
            </Button>
          )}
          {genome && (
            <Button variant="ghost" size="sm" as={Link} to={genome} iconRight={Fingerprint}>
              Normal behaviour
            </Button>
          )}
        </div>
      )}

      {/* The address only software needs, last and closed. */}
      <details className="rounded-[var(--radius-control)] border border-line bg-surface-2">
        <summary className="cursor-pointer px-2.5 py-1.5 text-[11.5px] font-medium text-ink-2">Technical details</summary>
        <dl className="flex flex-col gap-1 border-t border-line px-2.5 py-2 text-[11.5px]">
          <dt className="text-ink-3">AWS address (ARN)</dt>
          <dd className="flex items-start gap-1">
            <span className="min-w-0 flex-1 font-mono text-[10.5px] leading-snug break-all text-ink-2">{node.arn}</span>
            <CopyButton value={node.arn} label="Copy ARN" size="sm" />
          </dd>
        </dl>
      </details>
    </>
  );
}

/** Every relationship read so far that touches this node, strongest first. */
function ConnectionsPane({ node, connections }) {
  const rows = [...connections].sort(
    (a, b) =>
      Number(b.crossAccount) - Number(a.crossAccount) ||
      edgeKindMeta(b.kind).weight - edgeKindMeta(a.kind).weight ||
      (b.callCount ?? 0) - (a.callCount ?? 0),
  );
  const shown = (node.neighbourCount ?? 0) - (node.unseenCount ?? 0);
  return (
    <>
      <dl className="grid grid-cols-2 gap-1.5">
        <Figure label="Connected to" value={formatNumber(node.neighbourCount ?? 0)} />
        <Figure label="Shown on the graph" value={formatNumber(shown)} />
      </dl>
      <ul className="flex flex-col">
        {rows.map((edge) => (
          <li key={edge.id} className="border-b border-line py-1.5 last:border-0">
            <div className="flex flex-wrap items-center gap-1.5">
              <Tag tone={edgeKindMeta(edge.kind).tone} size="sm">
                {relationLabel(edge)}
              </Tag>
              {edge.crossAccount && (
                <Tag tone="critical" size="sm" dot>
                  Another AWS account
                </Tag>
              )}
            </div>
            <p className="mt-1 truncate text-[12px] text-ink" title={edge.otherArn}>
              {edge.other?.name ?? edge.otherArn}
              {edge.other && (
                <span className="text-[10.5px] text-ink-3">
                  {' '}
                  · {subtypeLabel(edge.other.kind, edge.other.subtype) || KIND_LABEL[edge.other.kind]}
                </span>
              )}
            </p>
            <p className="text-[10.5px] text-ink-3">
              {[
                edge.callCount ? `${formatNumber(edge.callCount)} calls` : null,
                edge.lastObservedAt
                  ? `${edge.observed ? 'last call' : 'recorded'} ${formatRelative(edge.lastObservedAt)}`
                  : null,
                edge.kinds.length > 1
                  ? `also: ${edge.kinds
                      .filter((kind) => kind !== edge.kind)
                      .map((kind) => edgeLabel(kind, edge.direction).toLowerCase())
                      .join(', ')}`
                  : null,
              ]
                .filter(Boolean)
                .join(' · ') || (edge.observed ? 'Seen in the activity log' : 'Set up in AWS, not a recorded call')}
            </p>
          </li>
        ))}
      </ul>
    </>
  );
}

/* "Used by nhi-activity-ec2-role": read from this node's side. */
function relationLabel(edge) {
  return edgeLabel(edge.kind, edge.direction);
}

/** What it did in the last 30 days, from CloudTrail. */
function ActivityPane({ activity, total }) {
  const analytics = activity?.analytics ?? {};
  const services = analytics.top_services ?? [];
  const actions = analytics.top_actions ?? [];
  const regions = analytics.active_regions ?? [];
  const busiest = services[0]?.count || 1;
  return (
    <>
      <dl className="grid grid-cols-3 gap-1.5">
        <Figure label="Recorded actions" value={formatNumber(total)} />
        <Figure
          label="Failed (% of actions)"
          value={typeof analytics.error_rate_pct === 'number' ? `${analytics.error_rate_pct}%` : 'Not recorded'}
          tone={analytics.error_rate_pct > 5 ? 'medium' : 'neutral'}
        />
        <Figure label="Regions" value={formatNumber((analytics.active_regions ?? []).length)} />
        <Figure label="Network addresses (IPs)" value={formatNumber((analytics.source_ips ?? []).length)} />
        <Figure
          label="Refused (% of actions)"
          value={typeof analytics.access_denied_rate_pct === 'number' ? `${analytics.access_denied_rate_pct}%` : 'Not recorded'}
          tone={analytics.access_denied_rate_pct > 0 ? 'critical' : 'neutral'}
        />
      </dl>

      {regions.length > 0 && (
        <div>
          <SectionLabel>Regions</SectionLabel>
          <ul className="mt-1.5 flex flex-col gap-1">
            {regions.map((entry) => (
              <li key={entry.region} className="flex items-baseline justify-between gap-2">
                <span className="min-w-0 truncate font-mono text-[11px] text-ink-2">{entry.region}</span>
                <span data-numeric="" className="shrink-0 text-[11px] text-ink-3">
                  {formatNumber(entry.count)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {actions.length > 0 && (
        <div>
          <SectionLabel>What it did most</SectionLabel>
          <ul className="mt-1.5 flex flex-col gap-1">
            {actions.slice(0, 6).map((action) => (
              <li key={action.action} className="flex items-baseline justify-between gap-2">
                <span className="min-w-0 truncate font-mono text-[11px] text-ink-2">{action.action}</span>
                <span data-numeric="" className="shrink-0 text-[11px] text-ink-3">
                  {formatNumber(action.count)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {services.length > 0 && (
        <div>
          <SectionLabel>AWS services it used most</SectionLabel>
          <ul className="mt-1.5 flex flex-col gap-1.5">
            {services.slice(0, 5).map((service) => (
              <li key={service.service}>
                <div className="flex items-baseline justify-between gap-2">
                  <span className="min-w-0 truncate text-[11.5px] text-ink-2">{service.service}</span>
                  <span data-numeric="" className="shrink-0 text-[11px] text-ink-3">
                    {formatNumber(service.count)}
                  </span>
                </div>
                <Meter
                  className="mt-1"
                  value={(service.count / busiest) * 100}
                  tone="info"
                  label={`${service.service}: ${service.pct}% of calls`}
                />
              </li>
            ))}
          </ul>
        </div>
      )}

      <p className="text-[11px] leading-relaxed text-ink-3">
        Counted from the AWS activity log (CloudTrail) over the last 30 days
        {analytics.usage_pattern ? `; usage pattern: ${humanizeToken(analytics.usage_pattern).toLowerCase()}` : ''}.
      </p>
    </>
  );
}

/** Granted against used, from the permissions tab. */
function PermissionsPane({ permissions, identityId }) {
  const summary = permissions?.summary ?? {};
  const rows = (permissions?.permissions ?? []).filter((row) => row.action);
  const unused = rows.filter((row) => !row.used);
  return (
    <>
      <dl className="grid grid-cols-3 gap-1.5">
        <Figure label="Allowed" value={formatNumber(summary.granted_count)} />
        <Figure label="Used (90 days)" value={formatNumber(summary.used_count)} />
        <Figure
          label="Never used"
          value={formatNumber(summary.unused_count)}
          tone={summary.unused_count > 0 ? 'medium' : 'neutral'}
        />
      </dl>
      <div>
        <div className="flex items-baseline justify-between text-[11.5px]">
          <span className="text-ink-3">Share of its permissions it actually used</span>
          <span data-numeric="" className="font-medium text-ink-2">
            {formatNumber(summary.utilization_pct)}%
          </span>
        </div>
        <Meter className="mt-1" value={summary.utilization_pct} tone="info" label="Share of its permissions it actually used" />
      </div>
      {unused.length > 0 && (
        <div>
          <SectionLabel>Allowed but never used</SectionLabel>
          <ul className="mt-1.5 flex flex-col gap-1">
            {unused.slice(0, 8).map((row) => (
              <li key={`${row.action}-${row.resource_arn}`} className="flex items-baseline justify-between gap-2">
                <span className="min-w-0 truncate font-mono text-[11px] text-ink-2" title={row.resource_arn}>
                  {row.action}
                </span>
                <span className="shrink-0 truncate text-[10.5px] text-ink-3">{row.resource_arn}</span>
              </li>
            ))}
          </ul>
          {unused.length > 8 && <p className="mt-1 text-[11px] text-ink-3">And {unused.length - 8} more in full access detail.</p>}
        </div>
      )}
      {identityId && (
        <Button variant="ghost" size="sm" as={Link} to={accessDetailRoute(identityId)} iconRight={Radius}>
          Full access detail
        </Button>
      )}
    </>
  );
}

/** The credentials this identity authenticates with. */
function CredentialList({ credentials }) {
  return (
    <ul className="flex flex-col gap-2">
      {credentials.map((credential) => {
        const life = credential.lifecycle ?? {};
        const external = (credential.trust_principals ?? []).filter((principal) => principal.is_external);
        const policies = (Array.isArray(credential.attached_policies) ? credential.attached_policies : [])
          .map((policy) => policy?.policy_name)
          .filter(Boolean);
        return (
          <li key={credential.credential_id} className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-2.5">
            <div className="flex items-start justify-between gap-2">
              <span className="min-w-0">
                <span className="block truncate font-mono text-[11px] text-ink" title={credential.arn}>
                  {credential.name}
                </span>
                <span className="mt-0.5 block text-[10.5px] text-ink-3">
                  {subtypeLabel('credential', credential.credential_type)} · {humanizeToken(credential.binding_type)}
                </span>
              </span>
              {policies.includes('AdministratorAccess') && (
                <Tag tone="critical" size="sm" dot>
                  Full administrator access
                </Tag>
              )}
            </div>
            <dl className="mt-2 flex flex-col gap-1 text-[11px]">
              {typeof life.age_days === 'number' && (
                <Row label="Age" value={`${formatNumber(life.age_days)} days`} warn={life.age_days > 365} />
              )}
              <Row
                label="Last used"
                value={life.last_used_at ? formatRelative(life.last_used_at) : null}
                fallback="Never used"
              />
              <Row
                label="Key change (rotation)"
                value={life.rotation_enabled ? (life.rotation_overdue ? 'Overdue' : 'On schedule') : 'Not set up'}
                warn={!life.rotation_enabled || life.rotation_overdue}
              />
              {policies.length > 0 && <Row label="Permission policies" value={policies.join(', ')} title={policies.join(', ')} />}
              {external.length > 0 && (
                <Row label="Usable from another account by" value={external.map((principal) => principal.principal).join(', ')} warn />
              )}
            </dl>
          </li>
        );
      })}
    </ul>
  );
}

/** The identities that run as this credential. */
function BoundList({ identities }) {
  return (
    <ul className="flex flex-col">
      {identities.map((identity) => (
        <li key={identity.identity_id} className="border-b border-line py-1.5 last:border-0">
          <Link
            to={identityRoute(identity.identity_id)}
            className="block truncate text-[12px] font-medium text-ink hover:text-brand hover:underline"
          >
            {identity.name}
          </Link>
          <p className="text-[10.5px] text-ink-3">
            {subtypeLabel('identity', identity.identity_type)} · {humanizeToken(identity.binding_type)}
            {identity.last_activity_at ? ` · active ${formatRelative(identity.last_activity_at)}` : ''}
          </p>
        </li>
      ))}
    </ul>
  );
}

/** Who may assume this credential, and what it is granted. */
function TrustPane({ trust }) {
  const principals = trust?.trust_policy?.principals ?? [];
  const warnings = trust?.trust_policy?.warnings ?? [];
  const policies = (Array.isArray(trust?.attached_policies) ? trust.attached_policies : [])
    .map((policy) => policy?.policy_name)
    .filter(Boolean);
  const boundary = trust?.permission_boundary?.boundary_arn;
  return (
    <>
      {principals.length > 0 && (
        <div>
          <SectionLabel>Who may use (assume) it</SectionLabel>
          <ul className="mt-1.5 flex flex-col gap-1.5">
            {principals.map((principal) => (
              <li
                key={`${principal.type}-${principal.principal}`}
                className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-2.5 py-2"
              >
                <div className="flex items-start justify-between gap-2">
                  <span className="min-w-0 font-mono text-[11px] break-all text-ink">{principal.principal}</span>
                  {principal.is_external && (
                    <Tag tone="high" size="sm" dot>
                      Another account
                    </Tag>
                  )}
                </div>
                <p className="mt-1 text-[10.5px] text-ink-3">
                  {humanizeToken(principal.type)}
                  {principal.condition ? ` · only when ${principal.condition}` : ' · no extra conditions'}
                </p>
              </li>
            ))}
          </ul>
        </div>
      )}
      {warnings.length > 0 && (
        <ul className="flex flex-col gap-1">
          {warnings.map((warning) => (
            <li key={warning} className="text-[11.5px] leading-snug text-high">
              {warning}
            </li>
          ))}
        </ul>
      )}
      {policies.length > 0 && (
        <div>
          <SectionLabel>Permission policies</SectionLabel>
          <ul className="mt-1.5 flex flex-col gap-1">
            {policies.map((policy) => (
              <li key={policy} className="flex items-center gap-1.5">
                <span
                  aria-hidden="true"
                  className={cn(
                    'size-1.5 shrink-0 rounded-full',
                    policy === 'AdministratorAccess' ? 'bg-critical' : 'bg-line-strong',
                  )}
                />
                <span className="min-w-0 truncate font-mono text-[11px] text-ink-2" title={policy}>
                  {policy}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <Row label="Permission limit (boundary)" value={boundary} fallback="No limit set" warn={!boundary} />
    </>
  );
}

/* ── Small parts ─────────────────────────────────────────────────────────── */

/**
 * The tab row, wrapping.
 *
 * Not the app's `Tabs`: that one is a single scrolling line that cannot wrap,
 * so seven labels in a 340px column would hide most of them. A wrapping row of
 * pills fits and states the full set of questions the panel can answer. Still
 * a real tablist: `aria-selected`, roving tabindex, arrow-key movement.
 */
function PanelTabs({ tabs, value, onChange }) {
  const onKeyDown = (event) => {
    const focused = document.activeElement?.id?.replace('panel-tab-', '');
    const fromFocus = tabs.findIndex((tab) => tab.value === focused);
    const index = fromFocus === -1 ? tabs.findIndex((tab) => tab.value === value) : fromFocus;
    if (index === -1) return;
    let next = null;
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = (index + 1) % tabs.length;
    if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = (index - 1 + tabs.length) % tabs.length;
    if (event.key === 'Home') next = 0;
    if (event.key === 'End') next = tabs.length - 1;
    if (next === null) return;
    event.preventDefault();
    onChange(tabs[next].value);
    document.getElementById(`panel-tab-${tabs[next].value}`)?.focus();
  };

  return (
    <div role="tablist" aria-label="Node details" onKeyDown={onKeyDown} className="flex flex-wrap items-center gap-1">
      {tabs.map((tab) => {
        const active = tab.value === value;
        return (
          <button
            key={tab.value}
            id={`panel-tab-${tab.value}`}
            role="tab"
            type="button"
            aria-selected={active}
            tabIndex={active ? 0 : -1}
            onClick={() => onChange(tab.value)}
            className={cn(
              'flex items-center gap-1 rounded-full border px-2 py-[3px] text-[11.5px] transition-colors duration-150',
              active
                ? 'border-brand bg-info-soft font-semibold text-brand'
                : 'border-line-strong bg-surface text-ink-2 hover:border-ink-3/50 hover:bg-surface-3',
            )}
          >
            {tab.label}
            {tab.count ? (
              <span data-numeric="" className={cn('text-[10.5px]', active ? 'text-brand' : 'text-ink-3')}>
                {tab.count}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}

/** One figure in a metric grid; the label sits under the number. */
function Figure({ label, value, tone = 'neutral' }) {
  return (
    <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-2 py-1.5">
      <dd
        data-numeric=""
        className={cn(
          'truncate text-[14px] leading-none font-semibold',
          tone === 'critical' ? 'text-critical' : tone === 'medium' ? 'text-medium' : 'text-ink',
        )}
      >
        {value}
      </dd>
      <dt className="mt-1 text-[10px] leading-tight text-ink-3">{label}</dt>
    </div>
  );
}

/** A label and a value on one line, with the missing case called out. */
function Row({ label, value, fallback = '-', warn = false, title }) {
  return (
    <div className="flex items-baseline justify-between gap-2">
      <dt className="shrink-0 text-ink-3">{label}</dt>
      <dd
        className={cn('min-w-0 truncate text-right', warn ? 'font-medium text-high' : 'text-ink-2')}
        title={title ?? (typeof value === 'string' ? value : undefined)}
      >
        {value || fallback}
      </dd>
    </div>
  );
}
