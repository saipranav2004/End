import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Activity, ArrowLeftRight, ArrowUpRight, Info, KeyRound, ListChecks, ShieldAlert } from 'lucide-react';
import { fetchIdentity } from '../../lib/api/inventory';
import { useQuery } from '../../lib/hooks';
import { identityRoute } from '../../lib/identityLinks';
import { formatDateTime, formatNumber, formatRelative, percentValue, policyName, titleCaseEnum } from '../../lib/format';
import { SegmentedControl, TabPanel } from '../../ui/Tabs';
import { DetailList, DetailRow, SectionLabel } from '../../ui/Panel';
import { Code, Tag } from '../../ui/Tag';
import { CopyableValue } from '../../ui/Copyable';
import { DataGrid } from '../../ui/DataGrid';
import { Meter } from '../../ui/Meter';
import { DetailSkeleton, Skeleton } from '../../ui/Skeleton';
import { cn } from '../../ui/cn';
import { ClearState, EmptyState, InlineError } from '../../ui/States';
import { EventTimeline } from '../activity/EventTimeline';
import { useEventState } from '../activity/eventState';
import {
  EDGE_LABELS,
  bindingLabel,
  credentialTypeMeta,
  environmentLabel,
  functionalClassMeta,
  identityTypeLabel,
  lifecycleMeta,
  nodeTypeLabel,
  shareText,
} from './meta';
import { SignalBreakdown } from './status';

export const RECORD_TABS = [
  { value: 'overview', label: 'Overview', icon: Info },
  { value: 'permissions', label: 'Permissions', icon: ListChecks },
  { value: 'credentials', label: 'Credentials', icon: KeyRound },
  { value: 'roles', label: 'Role use', icon: ArrowLeftRight },
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
    return <EmptyState compact title="Not in the inventory" description="A later discovery run may have removed this identity." />;
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
      <TabPanel tabValue="roles" value={tab}>
        {tab === 'roles' && active && <RoleUsePanel identityId={identityId} overview={overviewQuery.data} />}
      </TabPanel>
      <TabPanel tabValue="activity" value={tab}>
        {tab === 'activity' && active && <CompactActivity identityId={identityId} />}
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
  const tags = data.tags && typeof data.tags === 'object' ? Object.entries(data.tags) : [];
  const attributes = data.type_attributes && typeof data.type_attributes === 'object' ? Object.entries(data.type_attributes) : [];
  const raised = (data.risk_signals ?? []).filter((signal) => signal.status).length;

  return (
    <div className="flex flex-col gap-6">
      <section aria-label="At a glance" className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-4">
        <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2 xl:grid-cols-3">
          <Fact label="What it is">
            {identityTypeLabel(identity.identity_type)}
            <span className="block text-[12px] text-ink-3">
              {functionalClassMeta(identity.functional_class).label}
              {identity.category && identity.category.toLowerCase() !== String(identity.identity_type).toLowerCase()
                ? ` · ${identity.category}`
                : ''}
            </span>
          </Fact>
          <Fact label="Where">
            <span className="font-mono text-[12.5px]">{identity.account_id}</span>
            <span className="block text-[12px] text-ink-3">
              {identity.region} · {environmentLabel(identity.environment)}
            </span>
          </Fact>
          <Fact label="Owner">
            {identity.owner || <span className="font-semibold text-high">No owner</span>}
            <span className="block truncate text-[12px] text-ink-3" title={identity.created_by || undefined}>
              {identity.created_by ? `Created by ${identity.created_by.split('/').pop()}` : 'Creator not seen in CloudTrail'}
            </span>
          </Fact>
          <Fact label={primary ? `Acts as (${credentialTypeMeta(primary.credential_type).label})` : 'Acts as'} wide>
            {primary ? (
              <>
                <span className="font-mono text-[12.5px]">{primary.name}</span>
                <span className="block text-[11.5px] text-ink-3">
                  {bindingLabel(primary.binding_type)}
                  {primary.trust_principal ? ` · trusted principal ${primary.trust_principal}` : ''}
                </span>
              </>
            ) : (
              'No credential bound'
            )}
          </Fact>
          <Fact label="Permissions">
            {granted > 0 ? (
              <>
                {formatNumber(granted)} actions granted
                <span className="block text-[12px] text-ink-3">
                  {formatNumber(used)} used in 90 days · {shareText(used, granted)}
                </span>
              </>
            ) : (
              'No granted actions recorded'
            )}
          </Fact>
          <Fact label="Authenticates with">
            {credentials.length === 0 ? 'Nothing on record' : kinds.join(', ')}
            <span className="block text-[12px] text-ink-3">
              {formatNumber(credentials.length)} bound credential{credentials.length === 1 ? '' : 's'}
            </span>
          </Fact>
          <Fact label="Can be taken on by">
            {relationshipsQuery.isLoading && !relationshipsQuery.data ? (
              <Skeleton className="h-4 w-32 rounded" />
            ) : relationshipsQuery.isError ? (
              <span className="text-ink-3">Not available just now</span>
            ) : takers.length === 0 ? (
              primary?.trust_principal ? (
                <>
                  <span className="font-mono text-[12.5px]">{primary.trust_principal}</span>
                  <span className="block text-[12px] text-ink-3">Through its role&apos;s trust policy</span>
                </>
              ) : (
                'Nothing recorded besides its own binding'
              )
            ) : (
              <>
                {takers[0].name}
                <span className="block text-[12px] text-ink-3">
                  {EDGE_LABELS[takers[0].edgeType] ?? titleCaseEnum(takers[0].edgeType)}
                  {takers.length > 1 ? ` and ${takers.length - 1} more` : ''}
                </span>
              </>
            )}
          </Fact>
          <Fact label="Last activity">
            {identity.last_activity_at ? formatRelative(identity.last_activity_at) : 'No activity recorded'}
            <span className="block text-[12px] text-ink-3">
              {identity.last_activity_at ? formatDateTime(identity.last_activity_at) : 'Not seen in CloudTrail'}
            </span>
          </Fact>
        </dl>
      </section>

      <div>
        <SectionLabel>Risk signals{raised > 0 ? ` · ${raised} raised` : ''}</SectionLabel>
        <div className="mt-1">
          <SignalBreakdown signals={data.risk_signals} />
        </div>
        <p className="mt-2 text-[11.5px] leading-relaxed text-ink-3">
          Signals are evaluated by discovery on this record and its bound credentials.
          {identityId && (
            <>
              {' '}
              The scored evaluation, with fixes, is on the{' '}
              <Link to={identityRoute(identityId, 'posture')} className="font-medium text-brand hover:underline">
                Posture tab
              </Link>
              .
            </>
          )}
        </p>
      </div>

      <div>
        <SectionLabel>Bound credentials</SectionLabel>
        {credentials.length === 0 ? (
          <p className="mt-2 text-[12.5px] text-ink-3">No credential is bound to this identity.</p>
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
                    {credential.is_primary ? ' · primary' : ''}
                  </span>
                </span>
                <span className="w-44 shrink-0">
                  <span className="flex justify-between text-[11.5px] text-ink-3">
                    <span>
                      {Number(credential.granted_actions_count) > 0
                        ? `${formatNumber(credential.used_actions_count)} of ${formatNumber(credential.granted_actions_count)} actions used`
                        : 'Uses the permissions of the identity it belongs to'}
                    </span>
                  </span>
                  <Meter
                    className="mt-1"
                    height={4}
                    value={credential.utilization_pct ?? 0}
                    label={`${credential.utilization_pct ?? 0}% of granted actions used`}
                  />
                </span>
                <span className="text-[11.5px] text-ink-3">
                  {formatNumber(credential.attached_policies_count)} attached · {formatNumber(credential.inline_policies_count)} inline
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className={cn('grid gap-6', !compact && 'xl:grid-cols-2')}>
        <div>
          <SectionLabel>Placement</SectionLabel>
          <DetailList className="mt-1">
            <DetailRow label="Lifecycle status">{lifecycleMeta(identity.status).label}</DetailRow>
            <DetailRow label="Internet exposure">
              {identity.is_publicly_accessible ? <span className="font-semibold text-critical">Publicly accessible</span> : 'Not publicly accessible'}
            </DetailRow>
            <DetailRow label="VPC" mono>
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
              <DetailRow label="Parent" mono>
                <CopyableValue value={identity.parent_arn} />
              </DetailRow>
            )}
          </DetailList>
        </div>

        <div>
          <SectionLabel>How it was found</SectionLabel>
          <DetailList className="mt-1">
            <DetailRow label="Discovered by">{data.discovery?.discovery_source ? titleCaseEnum(data.discovery.discovery_source) : '-'}</DetailRow>
            <DetailRow label="Credential resolved by">{data.discovery?.credential_resolution || '-'}</DetailRow>
            <DetailRow label="Data sources">{data.discovery?.data_sources || '-'}</DetailRow>
            <DetailRow label="Created in AWS">
              {formatDateTime(identity.created_at_aws)}
              {Number.isFinite(identity.age_days) && <span className="ml-1.5 text-ink-3">({formatNumber(identity.age_days)} days ago)</span>}
            </DetailRow>
            <DetailRow label="First discovered">{formatDateTime(identity.first_seen_at)}</DetailRow>
            <DetailRow label="Last discovered">{formatDateTime(identity.last_seen_at)}</DetailRow>
          </DetailList>
        </div>
      </div>

      <div className={cn('grid gap-6', !compact && 'xl:grid-cols-2')}>
        <div>
          <SectionLabel>Tags</SectionLabel>
          {tags.length === 0 ? (
            <p className="mt-2 text-[12.5px] text-ink-3">No tags on this resource.</p>
          ) : (
            <ul className="mt-2 flex flex-wrap gap-1.5">
              {tags.map(([key, value]) => (
                <li key={key}>
                  <Code title={`${key}=${value}`}>
                    {key}={String(value)}
                  </Code>
                </li>
              ))}
            </ul>
          )}
          {(data.missing_tags ?? []).length > 0 && (
            <p className="mt-2 text-[12px] text-ink-3">
              Missing recommended tags: <span className="text-ink-2">{data.missing_tags.join(', ')}</span>
            </p>
          )}
        </div>

        <div>
          <SectionLabel>{identityTypeLabel(identity.identity_type)} attributes</SectionLabel>
          {attributes.length === 0 ? (
            <p className="mt-2 text-[12.5px] text-ink-3">No type-specific attributes recorded.</p>
          ) : (
            <DetailList className="mt-1">
              {attributes.map(([key, value]) => (
                <DetailRow key={key} label={titleCaseEnum(key)} mono>
                  {typeof value === 'object' && value !== null ? JSON.stringify(value) : String(value)}
                </DetailRow>
              ))}
            </DetailList>
          )}
        </div>
      </div>

      {!compact && (
        <p className="text-[11.5px] text-ink-3">
          ARN <CopyableValue value={identity.arn} /> · inventory id <span className="font-mono">{identity.id}</span>
        </p>
      )}
    </div>
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
        What the policies on this identity&apos;s credentials grant, and which of those actions CloudTrail saw it use in the
        last 90 days. An action granted and never used is access an attacker gets for free.
      </TabIntro>

      <section aria-label="Utilisation" className="grid gap-4 rounded-[var(--radius-control)] border border-line bg-surface-2 p-4 sm:grid-cols-4">
        <Figure label="Actions granted" value={summary.granted_count} />
        <Figure label="Used (90 days)" value={summary.used_count} tone="text-low" />
        <Figure label="Never used" value={summary.unused_count} tone={summary.unused_count > 0 ? 'text-high' : undefined} />
        <div>
          <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Utilisation</p>
          <p data-numeric="" className="mt-1 text-[22px] font-bold text-ink">
            {formatNumber(summary.utilization_pct)}%
          </p>
          <Meter className="mt-1.5" height={5} value={summary.utilization_pct} label="Share of granted actions used" />
        </div>
      </section>

      <div>
        <SectionLabel>By policy</SectionLabel>
        {breakdown.length === 0 ? (
          <p className="mt-2 text-[12.5px] text-ink-3">No policy is recorded on this identity&apos;s credentials.</p>
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
                      {formatNumber(policy.used_permissions)} of {formatNumber(policy.total_permissions)} used
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
          <SectionLabel>Granted permissions</SectionLabel>
          <SegmentedControl label="Show" options={USE_FILTERS} value={use} onChange={setUse} />
        </div>
        <DataGrid
          className="mt-2"
          caption="Granted permissions"
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
                  <span className="block truncate font-mono text-[12.5px] text-ink">{row.action || '(no action recorded)'}</span>
                  <span className="block truncate text-[11px] text-ink-3">{row.service || '-'}</span>
                </span>
              ),
            },
            {
              key: 'resource',
              header: 'Resource',
              width: '24%',
              cell: (row) => (
                <span className={cn('block truncate font-mono text-[12px]', row.resource_arn === '*' ? 'text-high' : 'text-ink-2')} title={row.resource_arn}>
                  {row.resource_arn === '*' ? '* (any resource)' : row.resource_arn}
                </span>
              ),
            },
            {
              key: 'policy',
              header: 'Granted by',
              width: '20%',
              priority: 'wide',
              cell: (row) => (
                <span className="block min-w-0">
                  <span className="block truncate text-[12.5px] text-ink-2">{policyName(row.policy_source)}</span>
                  <span className="block truncate text-[11px] text-ink-3">
                    {titleCaseEnum(row.policy_type)} · {row.effect}
                  </span>
                </span>
              ),
            },
            {
              key: 'used',
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
              <EmptyState compact icon={ListChecks} title="No granted permissions recorded" description="Discovery recorded no effective permissions for this identity's credentials." />
            ) : (
              <EmptyState compact icon={ListChecks} title={use === 'used' ? 'Nothing used' : 'Everything granted was used'} description="Switch the filter to see the rest." />
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
        What this identity authenticates with: the role it acts as, its keys, and the secrets bound to it. A role issues
        short-lived credentials; a key or secret lasts until someone rotates it.
      </TabIntro>
      {credentials.length === 0 ? (
        <EmptyState compact icon={KeyRound} title="No credentials on this identity" description="Nothing is bound to it in the inventory." />
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
                      {credential.is_primary ? ' · primary' : ''}
                    </span>
                  </p>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {life.rotation_overdue && (
                      <Tag tone="high" size="sm" dot>
                        Rotation overdue
                      </Tag>
                    )}
                    {meta.longLived && !life.rotation_enabled && (
                      <Tag tone="medium" size="sm">
                        No rotation schedule
                      </Tag>
                    )}
                    {(credential.trust_principals ?? []).some((principal) => principal.is_external) && (
                      <Tag tone="medium" size="sm">
                        External trust
                      </Tag>
                    )}
                    <Link
                      to={`/credentials?search=${encodeURIComponent(credential.name)}&credential=${encodeURIComponent(credential.credential_id)}`}
                      className="inline-flex items-center gap-1 text-[12px] font-medium text-brand hover:underline"
                    >
                      Open in Credentials
                      <ArrowUpRight aria-hidden="true" className="size-3.5" />
                    </Link>
                  </div>
                </div>

                <div className="mt-2">
                  <CopyableValue value={credential.arn || credential.name} />
                </div>

                <dl className="mt-2.5 grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-4">
                  <MiniFact label="Created">{formatDateTime(life.created_at_aws)}</MiniFact>
                  <MiniFact label="Last used">{life.last_used_at ? formatRelative(life.last_used_at) : 'No use recorded'}</MiniFact>
                  <MiniFact label="Rotation">
                    {life.rotation_enabled
                      ? `Every ${formatNumber(life.rotation_interval_days)} days`
                      : life.last_rotated_at
                        ? `Last ${formatRelative(life.last_rotated_at)}`
                        : meta.longLived
                          ? 'Never rotated'
                          : 'Not applicable'}
                  </MiniFact>
                  <MiniFact label={life.expires_at ? 'Expires' : life.max_session_duration ? 'Session limit' : 'Expires'}>
                    {life.expires_at
                      ? formatDateTime(life.expires_at)
                      : life.max_session_duration
                        ? `${formatNumber(Math.round(life.max_session_duration / 60))} min`
                        : 'Never'}
                  </MiniFact>
                </dl>

                {(credential.trust_principals ?? []).length > 0 && (
                  <div className="mt-3">
                    <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Trusted principals</p>
                    <ul className="mt-1 flex flex-wrap gap-1.5">
                      {credential.trust_principals.map((principal) => (
                        <li key={`${principal.type}-${principal.principal}`}>
                          <Tag tone={principal.is_external ? 'medium' : 'neutral'} size="sm">
                            <span className="truncate">
                              <span className="font-mono">{principal.principal}</span>
                              {` · ${principal.type}${principal.is_external ? ' · external' : ''}${principal.condition ? ' · conditioned' : ''}`}
                            </span>
                          </Tag>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {(policies.length > 0 || inline.length > 0) && (
                  <div className="mt-3">
                    <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Policies</p>
                    <ul className="mt-1 flex flex-wrap gap-1.5">
                      {policies.map((policy, index) => (
                        <li key={policy.policy_arn || policy.policy_name || index}>
                          <Code title={policy.policy_arn}>{policy.policy_name || policy.policy_arn || 'Policy'}</Code>
                        </li>
                      ))}
                      {inline.map((policy, index) => (
                        <li key={`inline-${policy.policy_name || index}`}>
                          <Code>{policy.policy_name || 'Inline policy'} (inline)</Code>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {life.permission_boundary_arn && (
                  <p className="mt-2 text-[12px] text-ink-3">
                    Capped by permission boundary <span className="font-mono text-ink-2">{life.permission_boundary_arn}</span>
                  </p>
                )}

                {(credential.assumable_roles ?? []).length > 0 && (
                  <p className="mt-2 text-[12px] text-ink-3">
                    Can assume{' '}
                    <span className="font-mono text-ink-2">
                      {credential.assumable_roles.map((role) => role.target_role_arn.split('/').pop()).join(', ')}
                    </span>
                  </p>
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

/* ── Role use ────────────────────────────────────────────────────────────── */

const OUTBOUND_ASSUME = new Set(['assume_role', 'cross_account', 'federated_oidc', 'federated_saml']);

function RoleUsePanel({ identityId, overview }) {
  const query = useIdentityTab(identityId, 'relationships', { extra: { depth: 2 } });
  const credentialsQuery = useIdentityTab(identityId, 'credentials');
  const arn = overview?.identity?.arn;

  const model = useMemo(() => {
    const graph = query.data?.graph;
    if (!graph) return null;
    const current = arn || graph.nodes?.find((node) => node.is_current)?.arn;
    const nodes = new Map((graph.nodes ?? []).map((node) => [node.arn, node]));
    const subjects = new Set([current]);
    for (const edge of graph.edges ?? []) if (edge.source === current && edge.edge_type === 'bound_to') subjects.add(edge.target);
    const via = (subject) => (subject === current ? null : nodes.get(subject)?.name || subject);
    const row = (edge, other, subject) => {
      const node = nodes.get(other);
      return {
        key: `${edge.source}|${edge.target}|${edge.edge_type}`,
        arn: other,
        name: node?.name || other.split(/[/:]/).pop() || other,
        type: nodeTypeLabel(node?.type, node?.subtype),
        identityLink: node?.type === 'identity',
        edgeType: edge.edge_type,
        via: via(subject),
        lastObserved: edge.last_observed_at,
        calls: edge.call_count,
      };
    };
    const inbound = [];
    const sharedWith = [];
    const assumes = [];
    const reaches = [];
    for (const edge of graph.edges ?? []) {
      if (subjects.has(edge.target) && !subjects.has(edge.source)) {
        if (INBOUND_TRUST.has(edge.edge_type)) inbound.push(row(edge, edge.source, edge.target));
        else if (edge.edge_type === 'bound_to') sharedWith.push(row(edge, edge.source, edge.target));
      }
      if (subjects.has(edge.source) && !subjects.has(edge.target)) {
        if (OUTBOUND_ASSUME.has(edge.edge_type)) assumes.push(row(edge, edge.target, edge.source));
        else if (edge.edge_type === 'accesses') reaches.push(row(edge, edge.target, edge.source));
      }
    }
    reaches.sort((a, b) => (b.calls ?? 0) - (a.calls ?? 0));
    /* One row per principal: a role trusted both directly and cross-account
       is one relationship described two ways. */
    const merge = (list) => {
      const byKey = new Map();
      for (const entry of list) {
        const key = `${entry.arn}|${entry.via ?? ''}`;
        const existing = byKey.get(key);
        if (!existing) byKey.set(key, { ...entry, edgeTypes: [entry.edgeType] });
        else {
          if (!existing.edgeTypes.includes(entry.edgeType)) existing.edgeTypes.push(entry.edgeType);
          if (Date.parse(entry.lastObserved || 0) > Date.parse(existing.lastObserved || 0)) existing.lastObserved = entry.lastObserved;
        }
      }
      return [...byKey.values()];
    };
    return { inbound: merge(inbound), sharedWith: merge(sharedWith), assumes: merge(assumes), reaches };
  }, [query.data, arn]);

  const principals = useMemo(
    () =>
      (credentialsQuery.data?.credentials ?? []).flatMap((credential) =>
        (credential.trust_principals ?? []).map((principal) => ({ ...principal, credential: credential.name })),
      ),
    [credentialsQuery.data],
  );

  if (query.isError && !query.data) return <TabError query={query} label="Role use unavailable" />;
  if (!model) return <DetailSkeleton rows={6} />;

  const relColumns = (whoHeader, { showCalls = false } = {}) => [
    {
      key: 'who',
      header: whoHeader,
      primary: true,
      width: '36%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate font-mono text-[12.5px] text-ink" title={row.arn}>
            {row.name}
          </span>
          <span className="block truncate text-[11px] text-ink-3">{row.type}</span>
        </span>
      ),
    },
    {
      key: 'how',
      header: 'How',
      width: '22%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate text-[12.5px] text-ink-2">
            {(row.edgeTypes ?? [row.edgeType]).map((type) => EDGE_LABELS[type] ?? titleCaseEnum(type)).join(' · ')}
          </span>
          {row.via && <span className="block truncate text-[11px] text-ink-3">via {row.via}</span>}
        </span>
      ),
    },
    ...(showCalls
      ? [
          {
            key: 'calls',
            header: 'Calls',
            align: 'right',
            width: '12%',
            cell: (row) => (
              <span data-numeric="" className="text-[13px] font-semibold text-ink">
                {row.calls === null || row.calls === undefined ? '-' : formatNumber(row.calls)}
              </span>
            ),
          },
        ]
      : []),
    {
      key: 'last',
      header: 'Last observed',
      width: '18%',
      cell: (row) => (
        <span className="block text-[12px] text-ink-2" title={formatDateTime(row.lastObserved)}>
          {row.lastObserved ? formatRelative(row.lastObserved) : '-'}
        </span>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-6">
      <TabIntro>
        Who can take on this identity&apos;s role, which roles it can take on in turn, and what it was seen reaching. Trust comes
        from the role&apos;s trust policy and the inventory&apos;s trust edges; calls are counted from CloudTrail.
      </TabIntro>

      <section>
        <SectionLabel>Can be taken on by</SectionLabel>
        {principals.length > 0 && (
          <ul className="mt-2 flex flex-wrap gap-1.5" aria-label="Principals the trust policy allows">
            {principals.map((principal) => (
              <li key={`${principal.credential}-${principal.principal}`}>
                <Tag tone={principal.is_external ? 'medium' : 'neutral'} size="sm">
                  <span className="truncate">
                    <span className="font-mono">{principal.principal}</span>
                    {` · ${principal.type}${principal.is_external ? ' · external' : ''}${principal.condition ? ' · conditioned' : ' · no condition'}`}
                  </span>
                </Tag>
              </li>
            ))}
          </ul>
        )}
        {model.inbound.length === 0 ? (
          <p className="mt-2 text-[12.5px] text-ink-3">
            {principals.length > 0
              ? 'No other identity in the inventory is recorded assuming or trusting it beyond the trust policy above.'
              : 'Nothing in the inventory can assume this identity or its role.'}
          </p>
        ) : (
          <DataGrid className="mt-2" caption="Who can take on this identity" rows={model.inbound} rowKey={(row) => row.key} columns={relColumns('Principal')} emptyState={null} density="compact" />
        )}
        {model.sharedWith.length > 0 && (
          <p className="mt-2 flex items-start gap-1.5 text-[12px] text-medium">
            <ShieldAlert aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
            Its role is also bound to {model.sharedWith.map((entry) => entry.name).join(', ')} - a compromise of either is a
            compromise of both.
          </p>
        )}
      </section>

      <section>
        <SectionLabel>Roles it can assume</SectionLabel>
        {model.assumes.length === 0 ? (
          <ClearState compact title="No role chaining" description="It is not recorded assuming any other role." />
        ) : (
          <DataGrid className="mt-2" caption="Roles this identity can assume" rows={model.assumes} rowKey={(row) => row.key} columns={relColumns('Role')} emptyState={null} density="compact" />
        )}
      </section>

      <section>
        <SectionLabel>What it reached (CloudTrail)</SectionLabel>
        {model.reaches.length === 0 ? (
          <p className="mt-2 text-[12.5px] text-ink-3">No resource access was recorded for it in the activity window.</p>
        ) : (
          <DataGrid className="mt-2" caption="Resources this identity reached" rows={model.reaches} rowKey={(row) => row.key} columns={relColumns('Resource', { showCalls: true })} emptyState={null} density="compact" />
        )}
      </section>

      {credentialsQuery.isError && <InlineError error={credentialsQuery.error} onRetry={credentialsQuery.refetch} label="Trust policy unavailable" />}
    </div>
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
        Filters, heatmap, source IPs, sessions and errors are on the{' '}
        <Link to={identityRoute(identityId, 'activity')} className="font-medium text-brand hover:underline">
          full activity record
        </Link>
        .
      </p>
    </div>
  );
}

