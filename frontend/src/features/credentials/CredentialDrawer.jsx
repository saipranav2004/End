import { useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle, Braces, Eye, Fingerprint, GitCompareArrows, History, KeyRound, Link2, ShieldCheck } from 'lucide-react';
import { fetchCredential } from '../../lib/api/inventory';
import { EntityChangesTab } from '../changes/EntityChangesTab';
import { useQuery } from '../../lib/hooks';
import { identityRoute } from '../../lib/identityLinks';
import { formatDate, formatDateTime, formatNumber, formatRelative, percentValue } from '../../lib/format';
import { Drawer } from '../../ui/Overlay';
import { DetailList, DetailRow, SectionLabel } from '../../ui/Panel';
import { SegmentedControl, Tabs } from '../../ui/Tabs';
import { Code, Tag } from '../../ui/Tag';
import { CopyableValue } from '../../ui/Copyable';
import { DataGrid } from '../../ui/DataGrid';
import { Pagination } from '../../ui/Pagination';
import { DetailSkeleton } from '../../ui/Skeleton';
import { ClearState, EmptyState, InlineError } from '../../ui/States';
import { JsonBlock, RawJson } from '../../ui/RawJson';
import { cn } from '../../ui/cn';
import { AttributeList, TagList } from '../identities/IdentityRecord';
import { SignalBreakdown } from '../identities/status';
import {
  EDGE_LABELS,
  bindingLabel,
  credentialTypeMeta,
  identityTypeLabel,
  lifecycleMeta,
  nodeTypeLabel,
  privilegeLabel,
} from '../identities/meta';

const TABS = [
  { value: 'overview', label: 'Overview', icon: KeyRound },
  { value: 'trust', label: 'Trust & permissions', icon: ShieldCheck },
  { value: 'activity', label: 'Activity', icon: Eye },
  { value: 'rotation', label: 'Rotation', icon: History },
  { value: 'relationships', label: 'Relationships', icon: Link2 },
  { value: 'changes', label: 'Changes', icon: GitCompareArrows },
  { value: 'raw', label: 'Raw', icon: Braces },
];

/**
 * One credential (`GET /api/v1/credentials/{id}?tab=`), opened over the
 * register. `row` is the list item, shown at once while the tabs load.
 */
export function CredentialDrawer({ credentialId, row, onClose, initialTab }) {
  const [tab, setTab] = useState(() => (TABS.some((entry) => entry.value === initialTab) ? initialTab : 'overview'));
  const open = Boolean(credentialId);

  const overviewQuery = useCredentialTab(credentialId, 'overview', open);
  const credential = overviewQuery.data?.credential ?? row;
  const status = lifecycleMeta(credential?.status);
  const meta = credentialTypeMeta(credential?.credential_type);
  /* The overview's flags when it has them, else the register row's. */
  const flag = (key) => overviewQuery.data?.credential?.[key] ?? row?.[key];

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="lg"
      eyebrow={meta.label}
      title={credential?.name || (overviewQuery.isLoading ? 'Loading credential…' : 'Credential')}
      subtitle={
        credential ? (
          <span className="flex flex-col gap-1.5">
            {credential.arn && <CopyableValue value={credential.arn} />}
            <span className="flex flex-wrap items-center gap-1.5">
              <Tag tone={status.tone} size="sm" dot>
                {status.label}
              </Tag>
              {flag('rotation_overdue') && (
                <Tag tone="high" size="sm">
                  Rotation overdue
                </Tag>
              )}
              {flag('is_orphaned') && (
                <Tag tone="high" size="sm">
                  Orphaned
                </Tag>
              )}
              {flag('has_external_trust') && (
                <Tag tone="medium" size="sm">
                  External trust
                </Tag>
              )}
            </span>
          </span>
        ) : null
      }
    >
      <div className="px-4 sm:px-5">
        <Tabs tabs={TABS} value={tab} onChange={setTab} size="sm" />
      </div>
      <div className="px-4 py-4 sm:px-5">
        {tab === 'overview' && <OverviewTab credentialId={credentialId} query={overviewQuery} />}
        {tab === 'trust' && <TrustTab credentialId={credentialId} />}
        {tab === 'rotation' && <RotationTab credentialId={credentialId} longLived={meta.longLived} />}
        {tab === 'activity' && <ActivityTab credentialId={credentialId} />}
        {tab === 'relationships' && <RelationshipsTab credentialId={credentialId} />}
        {tab === 'changes' && <EntityChangesTab kind="credential" id={credentialId} />}
        {tab === 'raw' && <RawTab credentialId={credentialId} name={credential?.name} />}
      </div>
    </Drawer>
  );
}

function useCredentialTab(id, tab, enabled = true, extra = {}) {
  return useQuery((signal) => fetchCredential(id, tab, extra, signal), [id, tab, JSON.stringify(extra)], {
    enabled: Boolean(id) && enabled,
    keepPrevious: tab === 'activity',
  });
}

function TabState({ query, label, children }) {
  if (query.isError && !query.data) {
    if (query.error?.status === 404) {
      return <EmptyState compact title="Not in the inventory" description="A later discovery run may have removed this credential." />;
    }
    return <InlineError error={query.error} onRetry={query.refetch} label={label} />;
  }
  if (!query.data) return <DetailSkeleton rows={6} />;
  return children;
}

/* ── Overview ────────────────────────────────────────────────────────────── */

function OverviewTab({ credentialId, query }) {
  const rawQuery = useCredentialTab(credentialId, 'raw');
  const privilege = privilegeLabel(rawQuery.data?.credential_record?.privilege_level);

  return (
    <TabState query={query} label="Credential unavailable">
      {query.data && (
        <OverviewBody data={query.data} privilege={privilege} record={rawQuery.data?.credential_record} recordLoading={rawQuery.isLoading && !rawQuery.data} />
      )}
    </TabState>
  );
}

function OverviewBody({ data, privilege, record, recordLoading }) {
  const credential = data.credential ?? {};
  const meta = credentialTypeMeta(credential.credential_type);
  const bound = data.bound_identities ?? [];
  const raised = (data.risk_signals ?? []).filter((signal) => signal.status).length;

  return (
    <div className="flex flex-col gap-5">
      <div>
        <SectionLabel>Credential</SectionLabel>
        <DetailList className="mt-1">
          <DetailRow label="Type">
            {meta.label}
            {credential.category && !meta.label.toLowerCase().includes(String(credential.category).toLowerCase()) && (
              <span className="ml-1.5 text-ink-3">{credential.category}</span>
            )}
          </DetailRow>
          <DetailRow label="Where">
            <span className="font-mono text-[12.5px]">{credential.account_id}</span>
            <span className="ml-1.5 text-ink-3">{credential.region}</span>
          </DetailRow>
          <DetailRow label="Privilege level">{recordLoading ? '…' : privilege || 'Not recorded'}</DetailRow>
          <DetailRow label="Created">
            {formatDateTime(credential.created_at_aws)}
            {Number.isFinite(credential.age_days) && <span className="ml-1.5 text-ink-3">({formatNumber(credential.age_days)} days old)</span>}
          </DetailRow>
          <DetailRow label="Last used">
            {credential.last_used_at ? formatRelative(credential.last_used_at) : 'No use recorded'}
            {Number.isFinite(credential.idle_days) && credential.idle_days > 0 && (
              <span className="ml-1.5 text-ink-3">(idle {formatNumber(credential.idle_days)} days)</span>
            )}
          </DetailRow>
          <DetailRow label="Rotation">
            {credential.rotation_status || (credential.rotation_enabled ? 'Enabled' : 'Disabled')}
            {credential.rotation_enabled && credential.rotation_interval_days && (
              <span className="ml-1.5 text-ink-3">every {formatNumber(credential.rotation_interval_days)} days</span>
            )}
            {credential.last_rotated_at && <span className="ml-1.5 text-ink-3">· last {formatRelative(credential.last_rotated_at)}</span>}
          </DetailRow>
          <DetailRow label="Expires">
            {credential.expires_at ? (
              <span className={cn(credential.days_until_expiry < 0 ? 'font-semibold text-critical' : credential.days_until_expiry <= 30 ? 'font-semibold text-high' : '')}>
                {formatDateTime(credential.expires_at)}
                <span className="ml-1.5 font-normal text-ink-3">
                  ({credential.days_until_expiry < 0 ? `expired ${formatNumber(-credential.days_until_expiry)} days ago` : `in ${formatNumber(credential.days_until_expiry)} days`})
                </span>
              </span>
            ) : meta.longLived ? (
              'Never - this kind does not expire on its own'
            ) : (
              'Not applicable - credentials are issued per session'
            )}
          </DetailRow>
          {credential.max_session_duration && (
            <DetailRow label="Session limit">{formatNumber(Math.round(credential.max_session_duration / 60))} minutes</DetailRow>
          )}
          <DetailRow label="Permission boundary" mono>
            {credential.permission_boundary_arn ? <CopyableValue value={credential.permission_boundary_arn} /> : <span className="font-sans text-ink-3">None</span>}
          </DetailRow>
          {credential.owner_principal_arn && (
            <DetailRow label="Owner principal" mono>
              <CopyableValue value={credential.owner_principal_arn} />
            </DetailRow>
          )}
        </DetailList>
      </div>

      <div>
        <SectionLabel>Bound to</SectionLabel>
        {bound.length === 0 ? (
          <p className="mt-2 flex items-center gap-1.5 text-[12.5px] font-medium text-high">
            <AlertTriangle aria-hidden="true" className="size-3.5" />
            No identity is bound to this credential - it is orphaned.
          </p>
        ) : (
          <ul className="mt-2 flex flex-col gap-2">
            {bound.map((identity) => (
              <li key={identity.identity_id} className="flex items-center gap-3 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3.5 py-2.5">
                <Fingerprint aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
                <span className="min-w-0 flex-1">
                  <Link to={identityRoute(identity.identity_id)} className="block truncate text-[13px] font-medium text-ink hover:text-brand hover:underline">
                    {identity.name}
                  </Link>
                  <span className="block truncate text-[11.5px] text-ink-3">
                    {identityTypeLabel(identity.identity_type)} · {bindingLabel(identity.binding_type)} · {identity.account_id}
                  </span>
                </span>
                <span className="flex shrink-0 flex-col items-end gap-1 text-right text-[11.5px] text-ink-3">
                  {identity.status && (
                    <Tag tone={lifecycleMeta(identity.status).tone} size="sm" dot>
                      {lifecycleMeta(identity.status).label}
                    </Tag>
                  )}
                  {identity.last_activity_at ? `Active ${formatRelative(identity.last_activity_at)}` : 'No activity recorded'}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div>
        <SectionLabel>Risk signals{raised > 0 ? ` · ${raised} raised` : ''}</SectionLabel>
        <div className="mt-1">
          <SignalBreakdown signals={data.risk_signals} />
        </div>
      </div>

      <div>
        <SectionLabel>{meta.label} attributes</SectionLabel>
        <AttributeList attributes={data.type_attributes} />
      </div>

      <div>
        <SectionLabel>Tags</SectionLabel>
        {recordLoading ? <p className="mt-2 text-[12.5px] text-ink-3">…</p> : <TagList tags={record?.tags} empty="No tags on this credential." />}
      </div>

      <div>
        <SectionLabel>What this kind is</SectionLabel>
        <p className="mt-2 rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5 text-[12.5px] leading-relaxed text-ink-2">{meta.what}</p>
      </div>
    </div>
  );
}

/* ── Trust ───────────────────────────────────────────────────────────────── */

function TrustTab({ credentialId }) {
  const query = useCredentialTab(credentialId, 'trust');
  const data = query.data;
  const principals = data?.trust_policy?.principals ?? [];
  const warnings = data?.trust_policy?.warnings ?? [];
  const attached = Array.isArray(data?.attached_policies) ? data.attached_policies : [];
  const inline = Array.isArray(data?.inline_policies) ? data.inline_policies : [];
  const effective = data?.effective_permissions_summary ?? [];
  const assumable = data?.assumable_roles ?? [];
  const boundary = data?.permission_boundary ?? {};

  return (
    <TabState query={query} label="Trust unavailable">
      <div className="flex flex-col gap-5">
        <div>
          <SectionLabel>Who can use it (trust policy)</SectionLabel>
          {warnings.length > 0 && (
            <ul className="mt-2 flex flex-col gap-1.5">
              {warnings.map((warning) => (
                <li key={warning} className="flex items-start gap-1.5 text-[12.5px] text-high">
                  <AlertTriangle aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
                  {warning}
                </li>
              ))}
            </ul>
          )}
          {principals.length === 0 && !data?.trust_policy?.raw ? (
            <p className="mt-2 text-[12.5px] text-ink-3">No trust policy - this kind of credential is used by whoever holds it.</p>
          ) : (
            <ul className="mt-2 divide-y divide-line">
              {principals.map((principal) => (
                <li key={`${principal.type}-${principal.principal}`} className="flex flex-wrap items-center gap-2 py-2">
                  <span className="min-w-0 flex-1 truncate font-mono text-[12.5px] text-ink" title={principal.principal}>
                    {principal.principal}
                  </span>
                  <Tag tone="neutral" size="sm">
                    {principal.type}
                  </Tag>
                  {principal.is_external && (
                    <Tag tone="medium" size="sm">
                      External
                    </Tag>
                  )}
                  <span className={cn('text-[11.5px]', principal.condition ? 'text-ink-3' : 'text-medium')}>
                    {principal.condition ? `Condition: ${principal.condition}` : 'No condition'}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {data?.trust_policy?.raw && <JsonBlock label="View JSON" value={data.trust_policy.raw} className="mt-2" />}
        </div>

        <div>
          <SectionLabel>Policies</SectionLabel>
          {attached.length === 0 && inline.length === 0 ? (
            <p className="mt-2 text-[12.5px] text-ink-3">No policy is attached.</p>
          ) : (
            <ul className="mt-2 flex flex-wrap gap-1.5">
              {/* The API names these {name, arn}; older payloads used policy_name / policy_arn. */}
              {attached.map((policy, index) => (
                <li key={policy.arn || policy.policy_arn || index}>
                  <Code title={policy.arn || policy.policy_arn}>{policy.name || policy.policy_name || policy.arn || policy.policy_arn}</Code>
                </li>
              ))}
              {inline.map((policy, index) => (
                <li key={`inline-${index}`}>
                  <Code>{policy.name || policy.policy_name || 'Inline policy'} (inline)</Code>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div>
          <SectionLabel>Effective permissions by service</SectionLabel>
          <DataGrid
            className="mt-2"
            caption="Effective permissions by service"
            rows={effective}
            rowKey={(row) => row.service}
            density="compact"
            columns={[
              { key: 'service', header: 'Service', primary: true, cell: (row) => <span className="font-mono text-[12.5px] text-ink">{row.service}</span> },
              { key: 'allowed', header: 'Allowed', align: 'right', cell: (row) => <span data-numeric="" className="text-[12.5px] font-semibold text-ink">{formatNumber(row.allowed_actions)}</span> },
              { key: 'denied', header: 'Denied', align: 'right', cell: (row) => <span data-numeric="" className="text-[12.5px] text-ink-2">{formatNumber(row.denied_actions)}</span> },
              {
                key: 'scope',
                header: 'Resource scope',
                cell: (row) => (
                  <span className={cn('block truncate font-mono text-[12px]', row.resource_scope === '*' ? 'text-high' : 'text-ink-2')} title={row.resource_scope}>
                    {row.resource_scope === '*' ? '* (any resource)' : row.resource_scope}
                  </span>
                ),
              },
            ]}
            emptyState={<p className="py-2 text-[12.5px] text-ink-3">No effective permissions were recorded.</p>}
          />
        </div>

        <div>
          <SectionLabel>Roles it can assume</SectionLabel>
          {assumable.length === 0 ? (
            <p className="mt-2 text-[12.5px] text-ink-3">None recorded.</p>
          ) : (
            <ul className="mt-2 divide-y divide-line">
              {assumable.map((role) => (
                <li key={role.target_role_arn} className="py-2">
                  <CopyableValue value={role.target_role_arn} />
                  <span className="block text-[11.5px] text-ink-3">
                    Account {role.target_account}
                    {role.last_assumed ? ` · last assumed ${formatRelative(role.last_assumed)}` : ' · not seen assumed'}
                    {role.conditions ? ` · ${role.conditions}` : ''}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div>
          <SectionLabel>Permission boundary</SectionLabel>
          {boundary.boundary_arn ? (
            <DetailList className="mt-1">
              <DetailRow label="Boundary" mono>
                <CopyableValue value={boundary.boundary_arn} />
              </DetailRow>
              {boundary.effective_restriction && <DetailRow label="Restricts">{boundary.effective_restriction}</DetailRow>}
              {(boundary.actions_blocked ?? []).length > 0 && (
                <DetailRow label="Blocks" mono>
                  {boundary.actions_blocked.join(', ')}
                </DetailRow>
              )}
            </DetailList>
          ) : (
            <p className="mt-2 text-[12.5px] text-ink-3">None - nothing caps what the attached policies grant.</p>
          )}
        </div>
      </div>
    </TabState>
  );
}

/* ── Rotation ────────────────────────────────────────────────────────────── */

function RotationTab({ credentialId, longLived }) {
  const query = useCredentialTab(credentialId, 'rotation');
  const data = query.data;
  const config = data?.configuration ?? {};
  const life = data?.lifecycle ?? {};
  const timeline = [...(data?.timeline ?? [])].sort((a, b) => Date.parse(b.date) - Date.parse(a.date));

  return (
    <TabState query={query} label="Rotation unavailable">
      <div className="flex flex-col gap-5">
        {!longLived && (
          <p className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5 text-[12.5px] leading-relaxed text-ink-2">
            This kind issues short-lived credentials per session, so there is nothing to rotate. Its age is how long it has
            existed, not how overdue it is.
          </p>
        )}
        <div>
          <SectionLabel>Configuration</SectionLabel>
          <DetailList className="mt-1">
            <DetailRow label="Status">{config.status || '-'}</DetailRow>
            <DetailRow label="Automatic rotation">{config.auto_rotation ? 'On' : 'Off'}</DetailRow>
            <DetailRow label="Interval">{config.rotation_interval_days ? `${formatNumber(config.rotation_interval_days)} days` : '-'}</DetailRow>
            <DetailRow label="Last rotation">{config.last_rotation ? formatDateTime(config.last_rotation) : 'Never rotated'}</DetailRow>
            <DetailRow label="Next expected">{config.next_expected ? formatDateTime(config.next_expected) : '-'}</DetailRow>
          </DetailList>
        </div>
        <div>
          <SectionLabel>Lifecycle</SectionLabel>
          <DetailList className="mt-1">
            <DetailRow label="Created">{formatDateTime(life.created_at_aws)}</DetailRow>
            <DetailRow label="Age">{Number.isFinite(life.age_days) ? `${formatNumber(life.age_days)} days` : '-'}</DetailRow>
            <DetailRow label="Since rotation">{Number.isFinite(life.days_since_rotation) ? `${formatNumber(life.days_since_rotation)} days` : '-'}</DetailRow>
            <DetailRow label="Expires">
              {life.expires_at ? `${formatDateTime(life.expires_at)} (${life.days_until_expiry < 0 ? `${formatNumber(-life.days_until_expiry)} days ago` : `in ${formatNumber(life.days_until_expiry)} days`})` : 'No expiry'}
            </DetailRow>
          </DetailList>
        </div>
        <div>
          <SectionLabel>History</SectionLabel>
          {timeline.length === 0 ? (
            <p className="mt-2 text-[12.5px] text-ink-3">No lifecycle events recorded.</p>
          ) : (
            <ol className="mt-2 flex flex-col">
              {timeline.map((entry, index) => (
                <li key={`${entry.date}-${index}`} className="relative flex gap-3 pb-3 last:pb-0">
                  <span className="flex shrink-0 flex-col items-center">
                    <span className="mt-1 size-2.5 rounded-full bg-brand" />
                    {index < timeline.length - 1 && <span aria-hidden="true" className="mt-1 w-px flex-1 bg-line" />}
                  </span>
                  <span className="min-w-0">
                    <span className="block text-[13px] font-medium text-ink">{entry.event}</span>
                    <span className="block text-[12px] text-ink-3">
                      {formatDate(entry.date)}
                      {entry.details ? ` · ${entry.details}` : ''}
                    </span>
                  </span>
                </li>
              ))}
            </ol>
          )}
        </div>
      </div>
    </TabState>
  );
}

/* ── Activity ────────────────────────────────────────────────────────────── */

const RANGES = [
  { value: '7d', label: '7d', title: 'Last 7 days' },
  { value: '30d', label: '30d', title: 'Last 30 days' },
  { value: '90d', label: '90d', title: 'Last 90 days' },
];

function ActivityTab({ credentialId }) {
  const [range, setRange] = useState('30d');
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(25);
  const query = useCredentialTab(credentialId, 'activity', true, { time_range: range, page, page_size: size });
  const data = query.data;
  const unavailable = data && data.activity_data_available === false;
  const volume = data?.volume_chart ?? [];
  const peak = volume.reduce((max, point) => Math.max(max, Number(point.count) || 0), 0);
  const analytics = data?.analytics ?? {};
  const users = callersOf(data?.identities_using_this_credential ?? [], data?.recent_events ?? []);
  const failures = data?.error_events ?? [];
  const rate = (value, label) => (value === null || value === undefined ? null : `${value}% ${label}`);
  const usage = [
    analytics.usage_pattern && `${analytics.usage_pattern} use`,
    rate(analytics.error_rate_pct, 'errors'),
    rate(analytics.access_denied_rate_pct, 'access denied'),
  ].filter(Boolean);

  return (
    <div className="flex flex-col gap-5">
      <SegmentedControl
        label="Time range"
        options={RANGES}
        value={range}
        onChange={(value) => {
          setRange(value);
          setPage(1);
        }}
        className="self-start"
      />
      <TabState query={query} label="Activity unavailable">
        {unavailable ? (
          <EmptyState compact title="Event store unavailable" description="CloudTrail events cannot be read right now, so use of this credential is unknown - not zero." />
        ) : (
          <div className={cn('flex flex-col gap-5', query.isRefreshing && 'opacity-60')}>
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
              <div>
                <SectionLabel>Calls per day</SectionLabel>
                {volume.length === 0 ? (
                  <p className="mt-2 text-[12.5px] text-ink-3">No use recorded in this window.</p>
                ) : (
                  <div className="mt-2 flex h-16 items-end gap-[2px]" role="img" aria-label={`Daily calls, peak ${formatNumber(peak)}`}>
                    {volume.map((point) => (
                      <span
                        key={point.date}
                        title={`${formatDate(point.date)}: ${formatNumber(point.count)} calls`}
                        className="min-w-[2px] flex-1 rounded-t-[2px] bg-[var(--t-ramp-4)]"
                        style={{ height: `${Math.max(4, percentValue(point.count, peak))}%` }}
                      />
                    ))}
                  </div>
                )}
                {usage.length > 0 && <p className="mt-1 text-[11.5px] text-ink-3">{usage.join(' · ')}</p>}
              </div>
              <CountList title="Top services" rows={analytics.top_services} name={(entry) => entry.service} />
            </div>

            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
              <CountList title="Top actions" rows={analytics.top_actions} name={(entry) => entry.action} />
              <CountList title="Active regions" rows={analytics.active_regions} name={(entry) => entry.region} />
            </div>

            <div>
              <SectionLabel>Identities using it</SectionLabel>
              {users.length === 0 ? (
                <p className="mt-2 text-[12.5px] text-ink-3">No call was recorded in this window.</p>
              ) : (
                <ul className="mt-2 divide-y divide-line">
                  {users.map((user) => (
                    <li key={user.key} className="flex items-center gap-3 py-2">
                      <span className="min-w-0 flex-1">
                        {user.identity_id ? (
                          <Link to={identityRoute(user.identity_id)} className="block truncate text-[13px] font-medium text-ink hover:text-brand hover:underline">
                            {user.name}
                          </Link>
                        ) : (
                          <span className="block truncate font-mono text-[12.5px] text-ink">{user.caller_arn}</span>
                        )}
                        <span className="block truncate text-[11.5px] text-ink-3">
                          {user.identity_type ? identityTypeLabel(user.identity_type) : 'Not in the inventory'}
                          {user.terminated ? ' · deleted since discovery' : ''}
                          {user.source_ip ? ` · ${user.source_ip}` : ''}
                        </span>
                      </span>
                      <span className="max-w-[45%] shrink-0 text-right text-[11.5px] text-ink-3">
                        <span data-numeric="" className="block font-semibold text-ink-2">
                          {formatNumber(user.calls)} {user.calls === 1 ? 'call' : 'calls'}
                        </span>
                        <span className="block truncate" title={user.last_action || undefined}>
                          {user.last_action ? <span className="font-mono">{user.last_action}</span> : null}
                          {user.last_action && user.last_action_time ? ' · ' : ''}
                          {user.last_action_time ? formatRelative(user.last_action_time) : ''}
                        </span>
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            <div>
              <SectionLabel>Failed calls{failures.length > 0 ? ` · ${formatNumber(failures.length)}` : ''}</SectionLabel>
              {failures.length === 0 ? (
                <p className="mt-2 text-[12.5px] text-ink-3">No call failed in this window.</p>
              ) : (
                <ul className="mt-2 divide-y divide-line">
                  {failures.map((event, index) => (
                    <li key={`${event.event_time}-${event.event_name}-${index}`} className="flex items-start gap-3 py-2">
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[12.5px] font-medium text-critical" title={event.error_message || undefined}>
                          {event.event_name}
                          {event.error_code && <span className="ml-1.5 font-mono text-[11.5px] font-normal">{event.error_code}</span>}
                        </span>
                        <span className="block truncate text-[11px] text-ink-3">
                          {[event.service, event.source_ip, event.identity_name].filter(Boolean).join(' · ')}
                        </span>
                      </span>
                      <span className="shrink-0 text-[12px] whitespace-nowrap text-ink-2" title={formatDateTime(event.event_time)}>
                        {formatRelative(event.event_time)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            <div>
              <SectionLabel>Recent calls</SectionLabel>
              <DataGrid
                className="mt-2"
                caption="Recent calls made with this credential"
                rows={data?.recent_events ?? []}
                rowKey={(row, index) => row.request_id || `${row.event_time}-${index}`}
                density="compact"
                columns={[
                  {
                    key: 'event',
                    header: 'Call',
                    primary: true,
                    width: '28%',
                    cell: (row) => (
                      <span className="block min-w-0">
                        <span className={cn('block truncate text-[12.5px] font-medium', row.error_code ? 'text-critical' : 'text-ink')}>{row.event_name}</span>
                        <span className="block truncate text-[11px] text-ink-3">
                          {row.service}
                          {row.error_code ? ` · ${row.error_code}` : ''}
                        </span>
                      </span>
                    ),
                  },
                  {
                    key: 'who',
                    header: 'By',
                    width: '22%',
                    cell: (row) => {
                      const label = row.identity_name || row.caller_arn?.split('/').pop() || '-';
                      return (
                        <span className="block min-w-0 text-[12px]" title={row.caller_arn}>
                          {row.identity_id ? (
                            <Link to={identityRoute(row.identity_id)} className="block truncate font-medium text-ink-2 hover:text-brand hover:underline">
                              {label}
                            </Link>
                          ) : (
                            <span className="block truncate text-ink-2">{label}</span>
                          )}
                          {row.terminated && <span className="block text-[11px] text-ink-3">deleted</span>}
                        </span>
                      );
                    },
                  },
                  {
                    key: 'resource',
                    header: 'Resource',
                    width: '22%',
                    priority: 'wide',
                    cell: (row) => (
                      <span className="block truncate font-mono text-[12px] text-ink-2" title={row.resource_arn || undefined}>
                        {row.resource_arn ? row.resource_arn.split(/[:/]/).pop() || row.resource_arn : '-'}
                      </span>
                    ),
                  },
                  { key: 'ip', header: 'Source', width: '14%', priority: 'wide', cell: (row) => <span className="block truncate font-mono text-[12px] text-ink-2">{row.source_ip || '-'}</span> },
                  {
                    key: 'when',
                    header: 'When',
                    width: '14%',
                    cell: (row) => (
                      <span className="text-[12px] whitespace-nowrap text-ink-2" title={formatDateTime(row.event_time)}>
                        {formatRelative(row.event_time)}
                      </span>
                    ),
                  },
                ]}
                emptyState={<p className="py-2 text-[12.5px] text-ink-3">No call was made with this credential in the window.</p>}
              />
              {(data?.events_pagination?.total_count ?? 0) > size && (
                <Pagination
                  page={page}
                  pageSize={size}
                  total={data.events_pagination.total_count}
                  unit="calls"
                  onPageChange={setPage}
                  onPageSizeChange={(value) => {
                    setSize(value);
                    setPage(1);
                  }}
                />
              )}
            </div>
          </div>
        )}
      </TabState>
    </div>
  );
}

/* ── Relationships ───────────────────────────────────────────────────────── */

function RelationshipsTab({ credentialId }) {
  const query = useCredentialTab(credentialId, 'relationships');
  const data = query.data;
  const nodes = new Map((data?.graph?.nodes ?? []).map((node) => [node.arn, node]));
  const rows = data?.relationship_table ?? [];
  const blind = data?.activity_data_available === false;

  return (
    <TabState query={query} label="Relationships unavailable">
      {blind && (
        <p className="mb-3 flex items-start gap-1.5 text-[12.5px] text-medium">
          <AlertTriangle aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
          CloudTrail could not be read just now, so what this credential reached and who created it are unknown, not none.
          Only links from the inventory are shown.
        </p>
      )}
      {rows.length === 0 ? (
        blind ? (
          <EmptyState compact title="No inventory links recorded" description="Calls made with this credential could not be checked." />
        ) : (
          <ClearState compact title="No relationships recorded" description="Nothing in the inventory is linked to this credential." />
        )
      ) : (
        <DataGrid
          caption="Relationships"
          rows={rows}
          rowKey={(row, index) => `${row.direction}-${row.target_arn}-${row.relationship}-${index}`}
          density="compact"
          columns={[
            {
              key: 'other',
              header: 'Linked to',
              primary: true,
              width: '40%',
              cell: (row) => {
                const node = nodes.get(row.target_arn);
                return (
                  <span className="block min-w-0" title={row.target_arn}>
                    <span className="block truncate font-mono text-[12.5px] text-ink">{row.target_name || row.target_arn}</span>
                    <span className="block truncate text-[11px] text-ink-3">{nodeTypeLabel(node?.type ?? row.target_type, node?.subtype)}</span>
                  </span>
                );
              },
            },
            {
              key: 'rel',
              header: 'Relationship',
              width: '28%',
              cell: (row) => (
                <span className="block min-w-0">
                  <span className="block truncate text-[12.5px] text-ink-2">{EDGE_LABELS[row.relationship] ?? row.relationship}</span>
                  <span className="block text-[11px] text-ink-3">{row.direction === 'inbound' ? 'Into this credential' : 'From this credential'}</span>
                </span>
              ),
            },
            {
              key: 'calls',
              header: 'Calls',
              align: 'right',
              width: '12%',
              cell: (row) => <span data-numeric="" className="text-[12.5px] text-ink-2">{row.call_count === null || row.call_count === undefined ? '-' : formatNumber(row.call_count)}</span>,
            },
            {
              key: 'last',
              header: 'Last active',
              width: '20%',
              cell: (row) => <span className="text-[12px] text-ink-2">{row.last_active ? formatRelative(row.last_active) : '-'}</span>,
            },
          ]}
          emptyState={null}
        />
      )}
    </TabState>
  );
}

/* ── Raw ─────────────────────────────────────────────────────────────────── */

function RawTab({ credentialId, name }) {
  const query = useCredentialTab(credentialId, 'raw');
  const data = query.data;
  return (
    <TabState query={query} label="Raw record unavailable">
      {data && (
        <RawJson
          intro="The inventory record exactly as discovery stored it, with its policies and the cloud API's response when it was kept. For audits and support tickets."
          filename={`credential-${name || credentialId}.json`}
          payload={data}
          blocks={[
            { key: 'credential_record', label: 'Credential record', value: data.credential_record },
            { key: 'trust_policy', label: 'Trust policy', value: data.trust_policy },
            { key: 'attached_policies', label: 'Attached policies', value: data.attached_policies },
            { key: 'inline_policies', label: 'Inline policies', value: data.inline_policies },
            { key: 'effective_permissions', label: 'Effective permissions', value: data.effective_permissions },
            { key: 'type_attributes', label: 'Type attributes', value: data.type_attributes },
            { key: 'raw_api_response', label: 'Raw API response', value: data.raw_api_response },
          ]}
        />
      )}
    </TabState>
  );
}

/** Name / count rows for an analytics breakdown, top five. */
function CountList({ title, rows, name }) {
  const list = Array.isArray(rows) ? rows.slice(0, 5) : [];
  return (
    <div className="min-w-0">
      <SectionLabel>{title}</SectionLabel>
      <ul className="mt-2 flex flex-col gap-1">
        {list.map((entry) => (
          <li key={name(entry)} className="flex items-center justify-between gap-2 text-[12px]">
            <span className="truncate font-mono text-ink-2" title={name(entry)}>
              {name(entry)}
            </span>
            <span data-numeric="" className="font-semibold text-ink">
              {formatNumber(entry.count)}
            </span>
          </li>
        ))}
        {list.length === 0 && <li className="text-[12px] text-ink-3">None</li>}
      </ul>
    </div>
  );
}

/**
 * One row per caller, however many calls it made: the API lists a caller per
 * call. When it lists none but calls were recorded, the callers are read off
 * those calls, so the two sections never contradict each other.
 */
function callersOf(listed, events) {
  const byKey = new Map();
  const add = (key, base, at) => {
    const current = byKey.get(key);
    if (!current) {
      byKey.set(key, { ...base, key, calls: 1, last_action_time: at ?? base.last_action_time ?? null });
      return;
    }
    current.calls += 1;
    const time = at ?? base.last_action_time;
    if (time && (!current.last_action_time || String(time) > String(current.last_action_time))) {
      current.last_action_time = time;
      if (base.last_action) current.last_action = base.last_action;
    }
  };
  for (const user of listed) add(user.identity_id || user.caller_arn || user.name, user, user.last_action_time);
  if (byKey.size === 0) {
    for (const event of events) {
      const key = event.identity_id || event.caller_arn || event.identity_name;
      if (!key) continue;
      add(
        key,
        {
          identity_id: event.identity_id,
          name: event.identity_name,
          caller_arn: event.caller_arn,
          identity_type: event.identity_type,
          source_ip: event.source_ip,
          last_action: event.event_name,
          terminated: event.terminated,
        },
        event.event_time,
      );
    }
  }
  return [...byKey.values()].sort((a, b) => b.calls - a.calls);
}
