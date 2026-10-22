import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Activity, ArrowLeftRight, ArrowUpRight, ChevronRight, Info, KeyRound, ListChecks, ShieldAlert, Waypoints } from 'lucide-react';
import { fetchIdentity } from '../../lib/api/inventory';
import { useQuery } from '../../lib/hooks';
import { accessDetailRoute, identityRoute } from '../../lib/identityLinks';
import { Button } from '../../ui/Button';
import { formatDateTime, formatNumber, formatRelative, percentValue, policyName, titleCaseEnum } from '../../lib/format';
import { SegmentedControl, TabPanel } from '../../ui/Tabs';
import { DetailList, DetailRow, SectionLabel } from '../../ui/Panel';
import { Code, Tag } from '../../ui/Tag';
import { CopyableValue } from '../../ui/Copyable';
import { DataGrid } from '../../ui/DataGrid';
import { Meter } from '../../ui/Meter';
import { DetailSkeleton, Skeleton } from '../../ui/Skeleton';
import { JsonBlock, RawJson } from '../../ui/RawJson';
import { cn } from '../../ui/cn';
import { EmptyState, InlineError } from '../../ui/States';
import { EventTimeline } from '../activity/EventTimeline';
import { useEventState } from '../activity/eventState';
import {
  bindingLabel,
  credentialTypeMeta,
  edgeWords,
  environmentLabel,
  functionalClassMeta,
  identityTypeLabel,
  lifecycleMeta,
  nodeTypeLabel,
  shareText,
} from './meta';
import { SignalBreakdown } from './status';
import { AccountHealth, HowItWasFound, ROOT_SAFETY_KEYS, hasAccountSummary } from './DiscoveryVisuals';

export const RECORD_TABS = [
  { value: 'overview', label: 'Overview', icon: Info },
  { value: 'permissions', label: 'Permissions', icon: ListChecks },
  { value: 'credentials', label: 'Credentials', icon: KeyRound },
  { value: 'relationships', label: 'Relationships', icon: ArrowLeftRight },
  { value: 'activity', label: 'Activity', icon: Activity },
];

/** The record tabs with counts, for whichever surface shows them. */
export function recordTabs(overview, { withPermissions = true } = {}) {
  const credentials = overview?.bound_credentials?.length;
  return RECORD_TABS.filter((entry) => withPermissions || entry.value !== 'permissions').map((entry) =>
    entry.value === 'credentials' && Number.isFinite(credentials) ? { ...entry, count: credentials } : entry,
  );
}

/** One tab of the identity record, fetched when first needed. */
export function useIdentityTab(id, tab, { enabled = true, extra } = {}) {
  return useQuery((signal) => fetchIdentity(id, tab, extra ?? {}, signal), [id, tab, JSON.stringify(extra ?? {})], {
    enabled: Boolean(id) && enabled,
  });
}

function TabIntro({ children }) {
  return <p className="mb-4 max-w-3xl text-[12.5px] leading-relaxed text-ink-3">{children}</p>;
}

function Fact({ label, children, wide = false }) {
  return (
    <div className={cn('min-w-0', wide && 'sm:col-span-2')}>
      <dt className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{label}</dt>
      <dd className="mt-1 min-w-0 text-[13px] leading-snug text-ink">{children}</dd>
    </div>
  );
}

function TabError({ query, label }) {
  if (query.error?.status === 404) {
    return <EmptyState compact title="Not in the inventory" description="A later discovery scan may have removed this identity. Go back to Identities to find it again." />;
  }
  return <InlineError error={query.error} onRetry={query.refetch} label={label} />;
}

/**
 * The inventory record of one identity, shared by the Identities drawer and
 * the identity page so both say the same thing.
 *
 * Laid out on what a reviewer has to answer before approving an identity:
 * what it is and where, who owns it, what it acts as and with which
 * permissions, what it authenticates with, who can take it on and when it was
 * last used. The first screen answers all of them in one block; the tabs are
 * the evidence behind each answer. Each tab fetches only when first shown.
 *
 * `overviewQuery` is the `tab=overview` query the caller already holds.
 */
export function IdentityRecordPanels({ identityId, tab, active = true, overviewQuery, compact = false }) {
  return (
    <>
      <TabPanel tabValue="overview" value={tab}>
        {tab === 'overview' && <OverviewPanel identityId={identityId} query={overviewQuery} active={active} compact={compact} />}
      </TabPanel>
      <TabPanel tabValue="permissions" value={tab}>
        {tab === 'permissions' && active && <PermissionsPanel identityId={identityId} />}
      </TabPanel>
      <TabPanel tabValue="credentials" value={tab}>
        {tab === 'credentials' && active && <CredentialsPanel identityId={identityId} />}
      </TabPanel>
      <TabPanel tabValue="relationships" value={tab}>
        {tab === 'relationships' && active && <RelationshipsPanel identityId={identityId} overview={overviewQuery.data} />}
      </TabPanel>
      <TabPanel tabValue="activity" value={tab}>
        {tab === 'activity' && active && <CompactActivity identityId={identityId} />}
      </TabPanel>
      <TabPanel tabValue="raw" value={tab}>
        {tab === 'raw' && active && <RawPanel identityId={identityId} name={overviewQuery.data?.identity?.name} />}
      </TabPanel>
    </>
  );
}

/* ── Overview ────────────────────────────────────────────────────────────── */

const INBOUND_TRUST = new Set(['assume_role', 'trusts', 'service_trust', 'cross_account', 'federated_oidc', 'federated_saml']);

function OverviewPanel({ identityId, query, active, compact }) {
  const relationshipsQuery = useIdentityTab(identityId, 'relationships', { enabled: active, extra: { depth: 2 } });
  const data = query.data;

  if (query.isError && !data) return <TabError query={query} label="Record unavailable" />;
  if (!data) return <DetailSkeleton rows={8} />;

  const identity = data.identity ?? {};
  const credentials = data.bound_credentials ?? [];
  const primary = credentials.find((entry) => entry.is_primary) ?? credentials[0];
  const granted = credentials.reduce((sum, entry) => sum + (Number(entry.granted_actions_count) || 0), 0);
  const used = credentials.reduce((sum, entry) => sum + (Number(entry.used_actions_count) || 0), 0);
  const kinds = [...new Set(credentials.map((entry) => credentialTypeMeta(entry.credential_type).label))];
  const takers = whoCanTakeItOn(relationshipsQuery.data, identity.arn);
  const raised = (data.risk_signals ?? []).filter((signal) => signal.status).length;
  const classLabel = functionalClassMeta(identity.functional_class).label;
  const discovery = data.discovery ?? {};

  return (
    <div className="flex flex-col gap-6">
      <section aria-label="At a glance" className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-4">
        <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2 xl:grid-cols-3">
          <Fact label="What it is">
            {identityTypeLabel(identity.identity_type)}
            <span className="block text-[12px] text-ink-3">
              {classLabel} workload
              {identity.category &&
              ![String(identity.identity_type), classLabel].some((value) => value.toLowerCase() === identity.category.toLowerCase())
                ? ` · ${identity.category}`
                : ''}
            </span>
          </Fact>
          <Fact label="Where it runs">
            AWS account <span className="font-mono text-[12.5px]">{identity.account_id}</span>
            <span className="block text-[12px] text-ink-3">
              {identity.region} · {environmentLabel(identity.environment)}
            </span>
          </Fact>
          <Fact label="Who owns it">
            {identity.owner || <span className="font-semibold text-high">No owner recorded</span>}
            <span className="block truncate text-[12px] text-ink-3" title={identity.created_by || undefined}>
              {identity.created_by ? `Created by ${identity.created_by.split('/').pop()}` : 'No creator recorded in the AWS activity log'}
            </span>
          </Fact>
          <Fact label="Attached credential" wide>
            {primary ? (
              <>
                <span className="font-mono text-[12.5px]">{primary.name}</span>
                <span className="block text-[11.5px] text-ink-3">
                  {credentialTypeMeta(primary.credential_type).label} · {bindingLabel(primary.binding_type)}
                  {primary.trust_principal ? ` · may be used by ${primary.trust_principal}` : ''}
                </span>
              </>
            ) : (
              <span className="text-ink-3">No credential attached</span>
            )}
          </Fact>
          <Fact label="What it can do">
            {granted > 0 ? (
              <>
                {formatNumber(granted)} actions allowed
                <span className="block text-[12px] text-ink-3">
                  {formatNumber(used)} of them used in 90 days ({shareText(used, granted)})
                </span>
              </>
            ) : (
              'No allowed actions recorded'
            )}
          </Fact>
          <Fact label="Signs in with">
            {credentials.length === 0 ? 'Nothing recorded' : kinds.join(', ')}
            <span className="block text-[12px] text-ink-3">
              {formatNumber(credentials.length)} credential{credentials.length === 1 ? '' : 's'}
            </span>
          </Fact>
          <Fact label="Who can use it">
            {relationshipsQuery.isLoading && !relationshipsQuery.data ? (
              <Skeleton className="h-4 w-32 rounded" />
            ) : relationshipsQuery.isError ? (
              <span className="text-ink-3">Not available just now</span>
            ) : takers.length === 0 ? (
              primary?.trust_principal ? (
                <>
                  <span className="font-mono text-[12.5px]">{primary.trust_principal}</span>
                  <span className="block text-[12px] text-ink-3">Allowed by its role&apos;s trust policy</span>
                </>
              ) : (
                'Only itself'
              )
            ) : (
              <>
                {takers[0].name}
                <span className="block text-[12px] text-ink-3">
                  {edgeWords(takers[0].edgeType, 'in')}
                  {takers.length > 1 ? ` · and ${takers.length - 1} more` : ''}
                </span>
              </>
            )}
            {identityId && (
              <Link to={identityRoute(identityId, 'relationships')} className="mt-0.5 block text-[12px] font-medium text-brand hover:underline">
                See all relationships
              </Link>
            )}
          </Fact>
          <Fact label="Last activity">
            {identity.last_activity_at ? (
              <span title={formatDateTime(identity.last_activity_at)}>{formatRelative(identity.last_activity_at)}</span>
            ) : (
              'Never used'
            )}
            <span className="block text-[12px] text-ink-3">
              {identity.last_activity_at ? formatDateTime(identity.last_activity_at) : 'No call in the AWS activity log'}
            </span>
            {Number.isFinite(identity.idle_days) && (
              <span className={cn('block text-[12px]', identity.idle_days >= 90 ? 'font-medium text-high' : 'text-ink-3')}>
                {identity.idle_days >= 90 ? 'Unused for 90+ days · ' : ''}Idle {formatNumber(identity.idle_days)} {identity.idle_days === 1 ? 'day' : 'days'}
              </span>
            )}
          </Fact>
        </dl>
      </section>

      <div>
        <SectionLabel>Risks{raised > 0 ? ` · ${raised} found` : ' · none found'}</SectionLabel>
        <div className="mt-1">
          <SignalBreakdown signals={data.risk_signals} />
        </div>
        <p className="mt-2 text-[11.5px] leading-relaxed text-ink-3">
          Checked by the discovery scan on this identity and its credentials.
          {identityId && (
            <>
              {' '}
              Its security score and how to fix each risk are on the{' '}
              <Link to={identityRoute(identityId, 'posture')} className="font-medium text-brand hover:underline">
                Posture tab
              </Link>
              .
            </>
          )}
        </p>
      </div>

      <div>
        <SectionLabel>What it signs in with</SectionLabel>
        {credentials.length === 0 ? (
          <p className="mt-2 text-[12.5px] text-ink-3">No credential is linked to this identity.</p>
        ) : (
          <ul className="mt-2 flex flex-col gap-2">
            {credentials.map((credential) => (
              <li
                key={credential.credential_id}
                className="flex flex-wrap items-center gap-x-4 gap-y-1.5 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3.5 py-2.5"
              >
                <span className="min-w-0 flex-1">
                  <Link
                    to={`/credentials?search=${encodeURIComponent(credential.name)}&credential=${encodeURIComponent(credential.credential_id)}`}
                    className="block truncate font-mono text-[12.5px] font-medium text-ink hover:text-brand hover:underline"
                  >
                    {credential.name}
                  </Link>
                  <span className="block text-[11.5px] text-ink-3">
                    {credentialTypeMeta(credential.credential_type).label} · {bindingLabel(credential.binding_type)}
                    {credential.is_primary ? ' · main' : ''}
                  </span>
                </span>
                <span className="w-44 shrink-0">
                  <span className="flex justify-between text-[11.5px] text-ink-3">
                    <span>
                      {Number(credential.granted_actions_count) > 0
                        ? `${formatNumber(credential.used_actions_count)} of ${formatNumber(credential.granted_actions_count)} allowed actions used`
                        : 'Uses the permissions of the identity it belongs to'}
                    </span>
                  </span>
                  <Meter
                    className="mt-1"
                    height={4}
                    value={credential.utilization_pct ?? 0}
                    label={`${credential.utilization_pct ?? 0}% of allowed actions used`}
                  />
                </span>
                <span className="text-[11.5px] text-ink-3">
                  {formatNumber(credential.attached_policies_count)} attached {Number(credential.attached_policies_count) === 1 ? 'policy' : 'policies'} ·{' '}
                  {formatNumber(credential.inline_policies_count)} inline
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className={cn('grid gap-6', !compact && 'xl:grid-cols-2')}>
        <div>
          <SectionLabel>Network and status</SectionLabel>
          <DetailList className="mt-1">
            <DetailRow label="Status">{lifecycleMeta(identity.status).label}</DetailRow>
            <DetailRow label="Reachable from the internet">
              {identity.is_publicly_accessible ? <span className="font-semibold text-critical">Yes, publicly accessible</span> : 'No'}
            </DetailRow>
            <DetailRow label="Private network (VPC)" mono>
              {identity.vpc_id || <span className="font-sans text-ink-3">Not in a VPC</span>}
            </DetailRow>
            {identity.subnet_id && (
              <DetailRow label="Subnet" mono>
                {identity.subnet_id}
              </DetailRow>
            )}
            {Array.isArray(identity.security_group_ids) && identity.security_group_ids.length > 0 && (
              <DetailRow label="Security groups" mono>
                {identity.security_group_ids.join(', ')}
              </DetailRow>
            )}
            {identity.parent_arn && (
              <DetailRow label="Part of" mono>
                <CopyableValue value={identity.parent_arn} />
              </DetailRow>
            )}
          </DetailList>
        </div>

        <div>
          <SectionLabel>Tags</SectionLabel>
          <TagList tags={data.tags} />
          {(data.missing_tags ?? []).length > 0 && (
            <p className="mt-2 text-[12px] text-ink-3">
              Recommended tags it lacks: <span className="text-ink-2">{data.missing_tags.join(', ')}</span>
            </p>
          )}
        </div>
      </div>

      <HowItWasFound identity={identity} discovery={discovery} compact={compact} />
      {hasAccountSummary(data.type_attributes) && <AccountHealth attributes={data.type_attributes} />}

      <Disclosure label="Technical details" hint="Exact dates, ARN, inventory id and AWS attributes">
        <div className={cn('grid gap-6', !compact && 'xl:grid-cols-2')}>
          <div>
            <SectionLabel>Exact dates and IDs</SectionLabel>
            <DetailList className="mt-1">
              <DetailRow label="Created in AWS">
                {identity.created_at_aws ? formatDateTime(identity.created_at_aws) : 'Not recorded'}
                {Number.isFinite(identity.age_days) && <span className="ml-1.5 text-ink-3">({formatNumber(identity.age_days)} days ago)</span>}
              </DetailRow>
              <DetailRow label="First found">{formatDateTime(identity.first_seen_at)}</DetailRow>
              <DetailRow label="Last found">{formatDateTime(identity.last_seen_at)}</DetailRow>
              <DetailRow label="ARN" mono>
                <CopyableValue value={identity.arn} />
              </DetailRow>
              <DetailRow label="Inventory id" mono>
                <CopyableValue value={identity.id} />
              </DetailRow>
            </DetailList>
          </div>

          <div>
            <SectionLabel>{identityTypeLabel(identity.identity_type)} attributes from AWS</SectionLabel>
            <AttributeList attributes={data.type_attributes} hide={hasAccountSummary(data.type_attributes) ? ROOT_SAFETY_KEYS : []} />
          </div>
        </div>
      </Disclosure>
    </div>
  );
}

/** A collapsed group, for technical details a first read can skip. */
function Disclosure({ label, hint, children }) {
  return (
    <details className="group min-w-0 rounded-[var(--radius-control)] border border-line">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-3.5 py-2.5 text-[12.5px] font-medium text-ink [&::-webkit-details-marker]:hidden">
        <ChevronRight aria-hidden="true" className="size-3.5 shrink-0 text-ink-3 transition-transform group-open:rotate-90" />
        <span>{label}</span>
        {hint && <span className="min-w-0 truncate text-[11.5px] font-normal text-ink-3">{hint}</span>}
      </summary>
      <div className="border-t border-line px-3.5 py-3">{children}</div>
    </details>
  );
}

/** Inbound trust edges onto the identity or anything bound to it. */
function whoCanTakeItOn(relationships, arn) {
  if (!relationships || !arn) return [];
  const nodes = new Map((relationships.graph?.nodes ?? []).map((node) => [node.arn, node]));
  const edges = relationships.graph?.edges ?? [];
  const subjects = new Set([arn]);
  for (const edge of edges) if (edge.source === arn && edge.edge_type === 'bound_to') subjects.add(edge.target);
  const seen = new Set();
  const out = [];
  for (const edge of edges) {
    if (!subjects.has(edge.target) || subjects.has(edge.source) || !INBOUND_TRUST.has(edge.edge_type)) continue;
    const key = `${edge.source}|${edge.edge_type}`;
    if (seen.has(key)) continue;
    seen.add(key);
    const node = nodes.get(edge.source);
    out.push({ arn: edge.source, name: node?.name || edge.source, edgeType: edge.edge_type });
  }
  return out;
}

/* ── Permissions ─────────────────────────────────────────────────────────── */

const USE_FILTERS = [
  { value: 'all', label: 'All' },
  { value: 'used', label: 'Used' },
  { value: 'unused', label: 'Unused' },
];

function PermissionsPanel({ identityId }) {
  const query = useIdentityTab(identityId, 'permissions');
  const [use, setUse] = useState('all');
  const data = query.data;
  const rows = useMemo(() => {
    const list = data?.permissions ?? [];
    if (use === 'used') return list.filter((row) => row.used);
    if (use === 'unused') return list.filter((row) => !row.used);
    return list;
  }, [data, use]);

  if (query.isError && !data) return <TabError query={query} label="Permissions unavailable" />;
  if (!data) return <DetailSkeleton rows={6} />;

  const summary = data.summary ?? {};
  const breakdown = data.policy_breakdown ?? [];

  return (
    <div className="flex flex-col gap-6">
      <TabIntro>
        What the policies on this identity&apos;s credentials allow, and which of those actions it was seen using in the AWS
        activity log (CloudTrail) over the last 90 days. An action allowed but never used is access an attacker would get for
        free, so it is the first thing to remove.
      </TabIntro>

      <section aria-label="Utilisation" className="grid gap-4 rounded-[var(--radius-control)] border border-line bg-surface-2 p-4 sm:grid-cols-4">
        <Figure label="Actions allowed" value={summary.granted_count} />
        <Figure label="Used (90 days)" value={summary.used_count} tone="text-low" />
        <Figure label="Never used" value={summary.unused_count} tone={summary.unused_count > 0 ? 'text-high' : undefined} />
        <div>
          <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Share used</p>
          <p data-numeric="" className="mt-1 text-[22px] font-bold text-ink">
            {formatNumber(summary.utilization_pct)}%
          </p>
          <Meter className="mt-1.5" height={5} value={summary.utilization_pct} label="Share of allowed actions used" />
        </div>
      </section>

      <div>
        <SectionLabel>Where the permissions come from</SectionLabel>
        {breakdown.length === 0 ? (
          <p className="mt-2 text-[12.5px] text-ink-3">No permission policy is recorded on this identity&apos;s credentials.</p>
        ) : (
          <ul className="mt-2 flex flex-col gap-2.5">
            {breakdown.map((policy) => (
              <li key={`${policy.policy_name}-${policy.policy_type}`} className="grid items-center gap-x-4 gap-y-1 sm:grid-cols-[minmax(0,1fr)_14rem]">
                <span className="min-w-0">
                  <span className="block truncate font-mono text-[12.5px] text-ink">{policyName(policy.policy_name)}</span>
                  <span className="text-[11.5px] text-ink-3">{titleCaseEnum(policy.policy_type)} policy</span>
                </span>
                <span>
                  <span className="flex justify-between text-[11.5px] text-ink-3">
                    <span>
                      {formatNumber(policy.used_permissions)} of {formatNumber(policy.total_permissions)} actions used
                    </span>
                    <span data-numeric="">{shareText(policy.used_permissions, policy.total_permissions)}</span>
                  </span>
                  <Meter className="mt-1" height={4} value={percentValue(policy.used_permissions, policy.total_permissions)} />
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <SectionLabel>Allowed actions</SectionLabel>
          <SegmentedControl label="Show" options={USE_FILTERS} value={use} onChange={setUse} />
        </div>
        <DataGrid
          className="mt-2"
          caption="Allowed actions"
          rows={rows}
          rowKey={(row, index) => `${row.policy_source}-${row.action}-${row.resource_arn}-${index}`}
          density="compact"
          columns={[
            {
              key: 'action',
              header: 'Action',
              primary: true,
              width: '30%',
              cell: (row) => (
                <span className="block min-w-0">
                  <span className="block truncate font-mono text-[12.5px] text-ink">{row.action || 'No action recorded'}</span>
                  <span className="block truncate text-[11px] text-ink-3">{row.service || 'Service not recorded'}</span>
                </span>
              ),
            },
            {
              key: 'resource',
              header: 'Resource',
              width: '24%',
              cell: (row) => (
                <span className={cn('block truncate font-mono text-[12px]', row.resource_arn === '*' ? 'text-high' : 'text-ink-2')} title={row.resource_arn}>
                  {row.resource_arn === '*' ? '* (every resource)' : row.resource_arn || 'Not recorded'}
                </span>
              ),
            },
            {
              key: 'policy',
              header: 'Allowed by',
              width: '20%',
              priority: 'wide',
              cell: (row) => (
                <span className="block min-w-0">
                  <span className="block truncate text-[12.5px] text-ink-2">{policyName(row.policy_source)}</span>
                  <span className="block truncate text-[11px] text-ink-3">
                    {titleCaseEnum(row.policy_type)} policy · {row.effect === 'Deny' ? 'denies' : 'allows'}
                  </span>
                </span>
              ),
            },
            {
              key: 'used',
      none: 'resources it used',
              header: 'Used (90 days)',
              width: '16%',
              cell: (row) =>
                row.used ? (
                  <span className="block min-w-0">
                    <Tag tone="low" size="sm" dot>
                      Used
                    </Tag>
                    <span className="mt-0.5 block text-[11px] text-ink-3">{row.last_used_at ? formatRelative(row.last_used_at) : ''}</span>
                  </span>
                ) : (
                  <Tag tone="neutral" size="sm">
                    Never used
                  </Tag>
                ),
            },
          ]}
          emptyState={
            (data.permissions ?? []).length === 0 ? (
              <EmptyState compact icon={ListChecks} title="No allowed actions recorded" description="The discovery scan found no permissions on this identity's credentials." />
            ) : (
              <EmptyState compact icon={ListChecks} title={use === 'used' ? 'None of its allowed actions were used' : 'Every allowed action was used'} description="Choose All to see the rest." />
            )
          }
        />
      </div>
    </div>
  );
}

function Figure({ label, value, tone }) {
  return (
    <div>
      <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{label}</p>
      <p data-numeric="" className={cn('mt-1 text-[22px] font-bold', tone || 'text-ink')}>
        {formatNumber(value)}
      </p>
    </div>
  );
}

/* ── Credentials ─────────────────────────────────────────────────────────── */

function CredentialsPanel({ identityId }) {
  const query = useIdentityTab(identityId, 'credentials');
  const credentials = query.data?.credentials;

  if (query.isError && !query.data) return <TabError query={query} label="Credentials unavailable" />;
  if (!credentials) return <DetailSkeleton rows={6} />;

  return (
    <>
      <TabIntro>
        What this identity signs in with: the role it runs as, its keys and its secrets. A role hands out short-lived sign-ins
        that expire on their own; a key or secret lasts until someone replaces it (key change, or rotation).
      </TabIntro>
      {credentials.length === 0 ? (
        <EmptyState compact icon={KeyRound} title="No credentials on this identity" description="Nothing in the inventory is linked to it for signing in." />
      ) : (
        <ul className="flex flex-col gap-3">
          {credentials.map((credential) => {
            const meta = credentialTypeMeta(credential.credential_type);
            const life = credential.lifecycle ?? {};
            const policies = Array.isArray(credential.attached_policies) ? credential.attached_policies : [];
            const inline = Array.isArray(credential.inline_policies) ? credential.inline_policies : [];
            return (
              <li key={credential.credential_id} className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <p className="text-[13px] font-semibold text-ink">
                    {meta.label}
                    <span className="ml-2 text-[12px] font-normal text-ink-3">
                      {bindingLabel(credential.binding_type)}
                      {credential.is_primary ? ' · main' : ''}
                    </span>
                    {credential.binding_source && (
                      <span className="mt-0.5 block font-mono text-[11px] font-normal break-all text-ink-3" title="Where the discovery scan found the link">
                        Found through {credential.binding_source}
                      </span>
                    )}
                  </p>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {life.rotation_overdue && (
                      <Tag tone="high" size="sm" dot>
                        Key change overdue
                      </Tag>
                    )}
                    {meta.longLived && !life.rotation_enabled && (
                      <Tag tone="medium" size="sm">
                        No automatic key change
                      </Tag>
                    )}
                    {(credential.trust_principals ?? []).some((principal) => principal.is_external) && (
                      <Tag tone="medium" size="sm">
                        Usable from outside your accounts
                      </Tag>
                    )}
                    <Link
                      to={`/credentials?search=${encodeURIComponent(credential.name)}&credential=${encodeURIComponent(credential.credential_id)}`}
                      className="inline-flex items-center gap-1 text-[12px] font-medium text-brand hover:underline"
                    >
                      Open credential
                      <ArrowUpRight aria-hidden="true" className="size-3.5" />
                    </Link>
                  </div>
                </div>

                <div className="mt-2">
                  <CopyableValue value={credential.arn || credential.name} />
                </div>

                <dl className="mt-2.5 grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-4">
                  <MiniFact label="Created">{life.created_at_aws ? formatDateTime(life.created_at_aws) : 'Not recorded'}</MiniFact>
                  {Number.isFinite(life.age_days) && <MiniFact label="Age">{formatNumber(life.age_days)} days</MiniFact>}
                  <MiniFact label="Last used">{life.last_used_at ? formatRelative(life.last_used_at) : 'No use recorded'}</MiniFact>
                  <MiniFact label="Key change (rotation)">
                    {life.rotation_enabled
                      ? life.rotation_interval_days
                        ? `Every ${formatNumber(life.rotation_interval_days)} days`
                        : 'Automatic'
                      : meta.longLived
                        ? life.last_rotated_at
                          ? 'Not scheduled'
                          : 'Never changed'
                        : 'Not needed (short-lived)'}
                  </MiniFact>
                  {life.last_rotated_at && (
                    <MiniFact label="Last changed">
                      <span title={formatDateTime(life.last_rotated_at)}>{formatRelative(life.last_rotated_at)}</span>
                    </MiniFact>
                  )}
                  {/* A role's credentials are issued per session: its "days since rotation" is only its age. */}
                  {meta.longLived && Number.isFinite(life.days_since_rotation) && (
                    <MiniFact label="Since last change">{formatNumber(life.days_since_rotation)} days</MiniFact>
                  )}
                  <MiniFact label={life.expires_at ? 'Expires' : life.max_session_duration ? 'Session limit' : 'Expires'}>
                    {life.expires_at
                      ? formatDateTime(life.expires_at)
                      : life.max_session_duration
                        ? `${formatNumber(Math.round(life.max_session_duration / 60))} min`
                        : 'Never expires'}
                  </MiniFact>
                </dl>

                {(credential.trust_principals ?? []).length > 0 && (
                  <div className="mt-3">
                    <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Who may use it (trust policy)</p>
                    <ul className="mt-1 flex flex-wrap gap-1.5">
                      {credential.trust_principals.map((principal) => (
                        <li key={`${principal.type}-${principal.principal}`}>
                          <Tag tone={principal.is_external ? 'medium' : 'neutral'} size="sm">
                            <span className="truncate">
                              <span className="font-mono">{principal.principal}</span>
                              {` · ${principalKind(principal.type)}${principal.is_external ? ' · outside your accounts' : ''}${principal.condition ? ' · with conditions' : ' · no conditions'}`}
                            </span>
                          </Tag>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {(policies.length > 0 || inline.length > 0) && (
                  <div className="mt-3">
                    <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Permission policies</p>
                    <ul className="mt-1 flex flex-wrap gap-1.5">
                      {/* The API names these {name, arn}; older payloads used policy_name / policy_arn. */}
                      {policies.map((policy, index) => (
                        <li key={policy.arn || policy.policy_arn || policy.name || index}>
                          <Code title={policy.arn || policy.policy_arn}>{policy.name || policy.policy_name || policy.arn || policy.policy_arn || 'Policy'}</Code>
                        </li>
                      ))}
                      {inline.map((policy, index) => (
                        <li key={`inline-${policy.name || policy.policy_name || index}`}>
                          <Code>{policy.name || policy.policy_name || 'Inline policy'} (inline)</Code>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {life.permission_boundary_arn && (
                  <p className="mt-2 text-[12px] text-ink-3">
                    Permission limit (boundary): <span className="font-mono text-ink-2">{life.permission_boundary_arn}</span>
                  </p>
                )}

                {(credential.assumable_roles ?? []).length > 0 && (
                  <div className="mt-3">
                    <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Can switch to (assume)</p>
                    <ul className="mt-1 flex flex-col gap-1">
                      {credential.assumable_roles.map((role) => (
                        <li key={role.target_role_arn} className="min-w-0 text-[12px] text-ink-3">
                          <span className="font-mono text-ink-2" title={role.target_role_arn}>
                            {role.target_role_arn.split('/').pop()}
                          </span>
                          {role.target_account && <span> · account <span className="font-mono">{role.target_account}</span></span>}
                          {role.last_assumed ? ` · last switched ${formatRelative(role.last_assumed)}` : ' · never seen switching'}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {credential.trust_policy && (
                  <JsonBlock label="Trust policy (JSON)" value={credential.trust_policy} className="mt-3 bg-surface" />
                )}
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}

function MiniFact({ label, children }) {
  return (
    <div className="min-w-0">
      <dt className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{label}</dt>
      <dd className="mt-0.5 truncate text-[12px] text-ink-2">{children}</dd>
    </div>
  );
}

/* ── Relationships ───────────────────────────────────────────────────────── */

const SWITCH_TO = new Set(['assume_role', 'cross_account', 'federated_oidc', 'federated_saml', 'pass_role']);
const FROM_OUTSIDE = new Set(['service_trust', 'cross_account', 'federated_oidc', 'federated_saml']);
const CAN_USE = new Set(['assume_role', 'trusts', 'manages', 'uses', 'pass_role']);

/**
 * Every edge of `tab=relationships` (depth 2, so an instance's role and what
 * that role trusts or reaches are included), sorted into plain questions.
 * "Subjects" are the identity and the roles/keys it runs as: an edge onto its
 * role is an edge onto it, shown "via" that role. An edge that fits no
 * question lands in "Other connections", so nothing the API returns is lost.
 */
function relationshipModel(data, arn) {
  const graph = data?.graph;
  if (!graph) return null;
  const current = arn || graph.nodes?.find((node) => node.is_current)?.arn;
  const nodes = new Map((graph.nodes ?? []).map((node) => [node.arn, node]));
  const edges = graph.edges ?? [];
  const subjects = new Set([current]);
  for (const edge of edges) if (edge.source === current && edge.edge_type === 'bound_to') subjects.add(edge.target);
  const nameOf = (value) => nodes.get(value)?.name || String(value).split(/[/:]/).pop() || value;
  const row = (edge, other, subject, direction) => {
    const node = nodes.get(other);
    return {
      key: `${edge.source}|${edge.target}|${edge.edge_type}`,
      arn: other,
      name: nameOf(other),
      nodeType: node?.type,
      type: nodeTypeLabel(node?.type, node?.subtype),
      edgeTypes: [edge.edge_type],
      direction,
      via: subject && subject !== current ? nameOf(subject) : null,
      lastObserved: edge.last_observed_at,
      calls: edge.call_count,
    };
  };
  const out = { runsAs: [], sharedWith: [], canUse: [], outside: [], switchTo: [], used: [], createdBy: [], other: [] };
  for (const edge of edges) {
    const type = edge.edge_type;
    const inbound = subjects.has(edge.target) && !subjects.has(edge.source);
    const outbound = subjects.has(edge.source) && !subjects.has(edge.target);
    if (edge.source === current && type === 'bound_to') out.runsAs.push(row(edge, edge.target, null, 'out'));
    else if (inbound) {
      const entry = row(edge, edge.source, edge.target, 'in');
      if (type === 'bound_to') out.sharedWith.push(entry);
      else if (FROM_OUTSIDE.has(type) || entry.nodeType === 'service') out.outside.push(entry);
      else if (CAN_USE.has(type)) out.canUse.push(entry);
      else if (type === 'created_by') out.createdBy.push(entry);
      else out.other.push(entry);
    } else if (outbound) {
      const entry = row(edge, edge.target, edge.source, 'out');
      if (SWITCH_TO.has(type)) out.switchTo.push(entry);
      else if (type === 'accesses') out.used.push(entry);
      else if (type === 'created_by') out.createdBy.push(entry);
      else out.other.push(entry);
    } else {
      /* Between its own roles, or further away (depth 3): named from its source. */
      out.other.push({ ...row(edge, edge.target, null, 'out'), via: edge.source === current ? null : nameOf(edge.source) });
    }
  }
  out.used.sort((a, b) => (b.calls ?? 0) - (a.calls ?? 0));
  /* One row per principal: a role trusted both directly and cross-account is
     one relationship described two ways. */
  const merge = (list) => {
    const byKey = new Map();
    for (const entry of list) {
      const key = `${entry.arn}|${entry.via ?? ''}|${entry.direction}`;
      const existing = byKey.get(key);
      if (!existing) byKey.set(key, { ...entry });
      else {
        if (!existing.edgeTypes.includes(entry.edgeTypes[0])) existing.edgeTypes.push(entry.edgeTypes[0]);
        if (Date.parse(entry.lastObserved || 0) > Date.parse(existing.lastObserved || 0)) existing.lastObserved = entry.lastObserved;
      }
    }
    return [...byKey.values()];
  };
  return Object.fromEntries(Object.entries(out).map(([key, list]) => [key, key === 'used' ? list : merge(list)]));
}

function RelationshipsPanel({ identityId, overview }) {
  const query = useIdentityTab(identityId, 'relationships', { extra: { depth: 2 } });
  const credentialsQuery = useIdentityTab(identityId, 'credentials');
  const identity = overview?.identity;
  const model = useMemo(() => relationshipModel(query.data, identity?.arn), [query.data, identity?.arn]);

  /* Credential deep links need the inventory id, which the graph does not carry. */
  const credentialIds = useMemo(
    () => new Map((credentialsQuery.data?.credentials ?? []).map((credential) => [credential.arn, credential])),
    [credentialsQuery.data],
  );
  const principals = useMemo(
    () =>
      (credentialsQuery.data?.credentials ?? []).flatMap((credential) =>
        (credential.trust_principals ?? []).map((principal) => ({ ...principal, credential: credential.name })),
      ),
    [credentialsQuery.data],
  );

  if (query.isError && !query.data) return <TabError query={query} label="Relationships unavailable" />;
  if (!model) return <DetailSkeleton rows={6} />;

  const linkFor = (row) => {
    if (row.nodeType === 'identity') return `/identities?search=${encodeURIComponent(row.arn)}`;
    if (row.nodeType !== 'credential') return null;
    const credential = credentialIds.get(row.arn);
    return credential
      ? `/credentials?search=${encodeURIComponent(credential.name)}&credential=${encodeURIComponent(credential.credential_id)}`
      : `/credentials?search=${encodeURIComponent(row.name)}`;
  };
  const noActivity = query.data?.activity_data_available === false;
  const creator = identity?.created_by;

  const sections = [
    {
      key: 'runsAs',
      none: 'what it runs as',
      title: 'What it runs as',
      help: 'The roles and keys this identity signs in with. Whoever controls one of these can act as it.',
      /* Its binding says how it uses each one (instance profile, access key...). */
      rows: model.runsAs.map((row) => {
        const binding = credentialIds.get(row.arn)?.binding_type;
        return binding ? { ...row, how: `Uses it as its ${bindingLabel(binding).toLowerCase()}` } : row;
      }),
      extra: model.sharedWith.length > 0 && (
        <p className="mt-2 flex items-start gap-1.5 text-[12px] text-medium">
          <ShieldAlert aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
          <span>
            Also used by{' '}
            {model.sharedWith.map((entry, index) => (
              <span key={entry.key}>
                {index > 0 && ', '}
                <RelName row={entry} to={linkFor(entry)} inline />
                {entry.via && ` (via ${entry.via})`}
              </span>
            ))}
            . If either is taken over, both are.
          </span>
        </p>
      ),
    },
    {
      key: 'canUse',
      none: 'who can use it',
      title: 'Who can use it',
      help: 'Other identities, roles and users that can switch to this identity or the roles it runs as.',
      rows: model.canUse,
    },
    {
      key: 'outside',
      none: 'AWS services or other accounts that can use it',
      title: 'AWS services and other accounts that can use it',
      help: 'Trust relationships: AWS services, other AWS accounts and sign-in providers allowed to use its roles.',
      rows: model.outside,
      extra: principals.length > 0 && (
        <div className="mt-2">
          <p className="text-[12px] text-ink-3">Its trust policy allows:</p>
          <ul className="mt-1 flex flex-wrap gap-1.5" aria-label="Principals the trust policy allows">
            {principals.map((principal) => (
              <li key={`${principal.credential}-${principal.principal}`} className="max-w-full">
                <Tag tone={principal.is_external ? 'medium' : 'neutral'} size="sm">
                  <span className="truncate" title={`On ${principal.credential}`}>
                    <span className="font-mono">{principal.principal}</span>
                    {` · ${principalKind(principal.type)}${principal.is_external ? ' · outside your accounts' : ''}${principal.condition ? ' · with conditions' : ' · no conditions'}`}
                  </span>
                </Tag>
              </li>
            ))}
          </ul>
        </div>
      ),
      count: model.outside.length + principals.length,
    },
    {
      key: 'switchTo',
      none: 'roles it can switch to',
      title: 'What it can switch to',
      help: 'Roles it can take on (assume) or hand to a service, including roles in other AWS accounts.',
      rows: model.switchTo,
    },
    {
      key: 'used',
      none: 'resources it used',
      title: 'What it used',
      help: 'Resources it called, read from the AWS activity log (CloudTrail), busiest first.',
      rows: model.used,
      showCalls: true,
      count: noActivity ? 1 : model.used.length,
      extra: noActivity && (
        <p className="mt-2 flex items-start gap-1.5 text-[12.5px] text-medium">
          <ShieldAlert aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
          The AWS activity log (CloudTrail) could not be read just now, so what it used is unknown, not none.
          {model.used.length > 0 ? ' The list may be incomplete.' : ' Try again later.'}
        </p>
      ),
    },
    {
      key: 'createdBy',
      none: 'who created it',
      title: 'Who created it',
      help: 'The person or automation recorded as creating it.',
      rows: model.createdBy,
      when: 'Created',
      count: model.createdBy.length + (creator ? 1 : 0),
      extra: creator && model.createdBy.length === 0 && (
        <p className="mt-2 text-[12.5px] text-ink-2">
          <span className="font-mono" title={creator}>
            {creator.split('/').pop()}
          </span>
          <span className="block text-[11.5px] text-ink-3">From the AWS activity log</span>
        </p>
      ),
    },
    {
      key: 'other',
      none: 'other connections',
      title: 'Other connections',
      help: 'Further links in the inventory, such as what contains or manages it.',
      rows: model.other,
    },
  ];
  const shown = sections.filter((section) => (section.count ?? section.rows.length) > 0);
  const empty = sections.filter((section) => (section.count ?? section.rows.length) === 0 && section.key !== 'other');

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <p className="max-w-3xl min-w-[min(100%,20rem)] flex-1 text-[12.5px] leading-relaxed text-ink-3">
          Everything this identity is connected to: what it runs as, who and what can use it, what it can switch to and what
          it used. Select a name to open it.
        </p>
        <Button variant="secondary" size="sm" as={Link} to={accessDetailRoute(identityId)} icon={Waypoints}>
          Open in access graph
        </Button>
      </div>

      {shown.map((section) => (
        <section key={section.key} aria-labelledby={`rel-${section.key}`}>
          <h3 id={`rel-${section.key}`} className="text-[13px] font-semibold text-ink">
            {section.title}
            {section.rows.length > 0 && <span className="ml-1.5 font-normal text-ink-3">{formatNumber(section.rows.length)}</span>}
          </h3>
          <p className="mt-0.5 text-[12px] text-ink-3">{section.help}</p>
          {section.rows.length > 0 && <RelList rows={section.rows} linkFor={linkFor} showCalls={section.showCalls} when={section.when} />}
          {section.extra}
        </section>
      ))}

      {empty.length > 0 && (
        <p className="text-[12.5px] text-ink-3">
          Nothing recorded for {empty.map((section) => section.none).filter(Boolean).join(', ')}.
        </p>
      )}

      {credentialsQuery.isError && <InlineError error={credentialsQuery.error} onRetry={credentialsQuery.refetch} label="Trust policy unavailable" />}

      <JsonBlock label="Technical details: relationship data as returned" value={query.data} />
    </div>
  );
}

const PRINCIPAL_KINDS = { Service: 'AWS service', AWS: 'AWS account or role', Federated: 'sign-in provider', CanonicalUser: 'AWS account' };
const principalKind = (type) => PRINCIPAL_KINDS[type] ?? type ?? 'principal';

function RelName({ row, to, inline = false }) {
  const className = cn('font-mono text-[12.5px] font-medium', inline ? 'text-ink-2' : 'block truncate text-ink');
  return to ? (
    <Link to={to} title={row.arn} className={cn(className, 'hover:text-brand hover:underline')}>
      {row.name}
    </Link>
  ) : (
    <span title={row.arn} className={className}>
      {row.name}
    </span>
  );
}

/** Related items as rows: name and plain type first, then how, then when. */
function RelList({ rows, linkFor, showCalls = false, when = 'Last seen' }) {
  return (
    <ul className="mt-2 divide-y divide-line rounded-[var(--radius-control)] border border-line bg-surface-2">
      {rows.map((row) => (
        <li key={row.key} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3.5 py-2.5">
          <span className="min-w-0 flex-[1_1_14rem]">
            <RelName row={row} to={linkFor(row)} />
            <span className="block truncate text-[11.5px] text-ink-3">
              {row.type}
              {row.via ? ` · via ${row.via}` : ''}
            </span>
          </span>
          <span className="min-w-0 flex-[1_1_10rem] text-[12.5px] text-ink-2">
            {row.how ?? row.edgeTypes.map((type) => edgeWords(type, row.direction)).join(' · ')}
          </span>
          <span className="shrink-0 text-right text-[12px] text-ink-3 sm:w-36">
            {showCalls && Number.isFinite(row.calls) && (
              <span data-numeric="" className="block font-semibold text-ink">
                {formatNumber(row.calls)} {row.calls === 1 ? 'call' : 'calls'}
              </span>
            )}
            <span className="block" title={row.lastObserved ? formatDateTime(row.lastObserved) : undefined}>
              {row.lastObserved ? `${when} ${formatRelative(row.lastObserved)}` : when === 'Created' ? 'Date not recorded' : 'No use recorded'}
            </span>
          </span>
        </li>
      ))}
    </ul>
  );
}

/* ── Raw ─────────────────────────────────────────────────────────────────── */

function RawPanel({ identityId, name }) {
  const query = useIdentityTab(identityId, 'raw');
  const data = query.data;

  if (query.isError && !data) return <TabError query={query} label="Raw record unavailable" />;
  if (!data) return <DetailSkeleton rows={6} />;

  return (
    <RawJson
      intro="Everything the discovery scan stored for this identity, exactly as received: the inventory record, the AWS response when it was kept, and the credential it was matched to. Useful for audits and support tickets."
      filename={`identity-${name || identityId}.json`}
      payload={data}
      blocks={[
        { key: 'identity_record', label: 'Identity record', value: data.identity_record },
        { key: 'resolved_credential', label: 'Resolved credential', value: data.resolved_credential },
        { key: 'type_attributes', label: 'Type attributes', value: data.type_attributes },
        { key: 'raw_api_response', label: 'Raw API response', value: data.raw_api_response },
      ]}
    />
  );
}

/* ── Activity (drawer) ───────────────────────────────────────────────────── */

const DRAWER_EVENTS = { range: '7d', size: 10 };

function CompactActivity({ identityId }) {
  const { state, patch, clearFilters } = useEventState({ urlBacked: false, defaults: DRAWER_EVENTS });
  return (
    <div className="-mx-4 -my-4 sm:-mx-5">
      <EventTimeline identityId={identityId} state={state} patch={patch} clearFilters={clearFilters} compact />
      <p className="border-t border-line px-4 py-3 text-[12px] text-ink-3 sm:px-5">
        Filters, busiest hours, where calls came from, sessions and failed calls are on the{' '}
        <Link to={identityRoute(identityId, 'activity')} className="font-medium text-brand hover:underline">
          identity&apos;s Activity tab
        </Link>
        .
      </p>
    </div>
  );
}

/** Resource tags as key=value chips; shared with the credential drawer. */
export function TagList({ tags, empty = 'No tags on this resource.' }) {
  const entries = tags && typeof tags === 'object' ? Object.entries(tags) : [];
  if (entries.length === 0) return <p className="mt-2 text-[12.5px] text-ink-3">{empty}</p>;
  return (
    <ul className="mt-2 flex flex-wrap gap-1.5">
      {entries.map(([key, value]) => (
        <li key={key} className="max-w-full">
          <Code title={`${key}=${value}`}>
            {key}={String(value)}
          </Code>
        </li>
      ))}
    </ul>
  );
}

/** Type-specific attributes as label/value rows; shared with the credential drawer. */
export function AttributeList({ attributes, hide = [] }) {
  const all = attributes && typeof attributes === 'object' ? Object.entries(attributes) : [];
  const entries = all.filter(([key]) => !hide.includes(key) && key !== 'field_status');
  const status = attributes?.field_status && typeof attributes.field_status === 'object' ? Object.entries(attributes.field_status) : [];
  // Fields that do not apply to this kind of identity read as one line, not ten rows.
  const notApplicable = status.filter(([, value]) => value === 'not_applicable').map(([key]) => titleCaseEnum(key));
  const otherStatus = status.filter(([, value]) => value !== 'not_applicable');
  if (entries.length === 0 && status.length === 0) return <p className="mt-2 text-[12.5px] text-ink-3">No type-specific attributes recorded.</p>;
  return (
    <>
      <DetailList className="mt-1">
        {entries.map(([key, value]) => (
          <DetailRow key={key} label={titleCaseEnum(key)} mono>
            <AttributeValue value={value} />
          </DetailRow>
        ))}
        {otherStatus.length > 0 && (
          <DetailRow label="Field status" mono>
            <AttributeValue value={Object.fromEntries(otherStatus)} />
          </DetailRow>
        )}
      </DetailList>
      {notApplicable.length > 0 && (
        <p className="mt-3 text-[12px] text-ink-3">
          Does not apply to this kind of identity: <span className="text-ink-2">{notApplicable.join(', ')}</span>
        </p>
      )}
    </>
  );
}

/**
 * A type attribute as discovery recorded it. Lists read one item per line and
 * objects as their own label/value pairs; anything nested deeper stays JSON.
 */
function AttributeValue({ value, depth = 0 }) {
  if (value === null || value === undefined || value === '') return <span className="font-sans text-ink-3">Not recorded</span>;
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="text-ink-3">None</span>;
    if (value.length === 1 && typeof value[0] !== 'object') return String(value[0]);
    return (
      <ul className="flex flex-col gap-1.5">
        {value.map((item, index) => (
          <li key={index} className={typeof item === 'object' && item !== null ? 'rounded-[var(--radius-control)] border border-line bg-surface-2 px-2.5 py-1.5' : undefined}>
            <AttributeValue value={item} depth={depth + 1} />
          </li>
        ))}
      </ul>
    );
  }
  if (typeof value === 'object') {
    if (depth > 1) return JSON.stringify(value);
    return (
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5">
        {Object.entries(value).map(([key, item]) => (
          <div key={key} className="contents">
            <dt className="font-sans text-[11.5px] text-ink-3">{titleCaseEnum(key)}</dt>
            <dd className="min-w-0 break-words">
              <AttributeValue value={item} depth={depth + 1} />
            </dd>
          </div>
        ))}
      </dl>
    );
  }
  return String(value);
}
