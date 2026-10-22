import { useCallback, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Download, ExternalLink, Network, ShieldCheck } from 'lucide-react';
import { getIdentityEvents, getIdentityTab } from '../../lib/api/access';
import { exportRowsToCsv, timestampedName } from '../../lib/csv';
import { arnResource, formatDateTime, formatNumber, formatRelative, humanizeToken, policyName } from '../../lib/format';
import { useQuery } from '../../lib/hooks/useQuery';
import { accessGraphRoute, identityRoute } from '../../lib/identityLinks';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { DataGrid } from '../../ui/DataGrid';
import { Meter, ProportionBar } from '../../ui/Meter';
import { Panel, PanelHeader, SectionLabel } from '../../ui/Panel';
import { DetailSkeleton, StatStripSkeleton } from '../../ui/Skeleton';
import { MetricTile } from '../../ui/Stat';
import { EmptyState, ErrorState, InlineError } from '../../ui/States';
import { Tabs } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';
import { NODE_KINDS, accountOf, edgeKindMeta, edgeLabel, subtypeLabel } from './graphModel';
import { CopyButton } from '../../ui/Copyable';

/**
 * One identity's access, in full.
 *
 * The graph answers "how is this connected". This screen answers what
 * follows for a single identity, as tables: what it is granted against what
 * it used, every route into and out of it, what crosses an account boundary,
 * and what it actually called. It is the page somebody opens with a ticket in
 * their hand.
 *
 *   Granted vs used   ?tab=permissions (grants; used = covered by a successful
 *                     CloudTrail call in the last 90 days)
 *   Routes            ?tab=relationships, depth 1 (relationship_table)
 *   Cross-account     /events?section=cross_account (30 days), the trust
 *                     principals of its credentials, and cross-account edges
 *   Resources called  /events?section=resources (30 days)
 *
 * There is no escalation tab: the service does not compute escalation paths,
 * and a list of them would have to be invented.
 */
const TABS = [
  { value: 'access', label: 'What it can do' },
  { value: 'routes', label: 'Relationships' },
  { value: 'cross', label: 'Other accounts and trust' },
  { value: 'resources', label: 'What it used' },
];

const settleQuery = { keepPrevious: false };

export default function AccessDetailPage() {
  const { id: rawId } = useParams();
  const id = decodeURIComponent(rawId ?? '');
  const { notify } = useToast();
  const [tab, setTab] = useState('access');

  const overview = useQuery((signal) => getIdentityTab(id, 'overview', {}, signal), [id], settleQuery);
  const permissions = useQuery((signal) => getIdentityTab(id, 'permissions', {}, signal), [id], settleQuery);
  const relationships = useQuery(
    (signal) => getIdentityTab(id, 'relationships', { depth: 1 }, signal),
    [id],
    settleQuery,
  );
  const credentials = useQuery((signal) => getIdentityTab(id, 'credentials', {}, signal), [id], settleQuery);
  const resources = useQuery(
    (signal) => getIdentityEvents(id, 'resources', { time_range: '30d' }, signal),
    [id],
    settleQuery,
  );
  const crossAccount = useQuery(
    (signal) => getIdentityEvents(id, 'cross_account', { time_range: '30d' }, signal),
    [id],
    settleQuery,
  );

  const identity = overview.data?.identity;
  const summary = permissions.data?.summary;
  const grants = useMemo(
    () =>
      (permissions.data?.permissions ?? [])
        .filter((row) => row.action)
        .map((row, index) => ({ ...row, key: `${row.action}|${row.resource_arn}|${row.policy_source}|${index}` }))
        .sort((a, b) => Number(a.used) - Number(b.used) || a.action.localeCompare(b.action)),
    [permissions.data],
  );

  const byService = useMemo(() => {
    const map = new Map();
    for (const row of grants) {
      const key = row.service || row.action.split(':')[0];
      const entry = map.get(key) ?? { service: key, granted: 0, used: 0, wildcard: false };
      entry.granted += 1;
      if (row.used) entry.used += 1;
      if (row.action.endsWith(':*') || row.action === '*') entry.wildcard = true;
      map.set(key, entry);
    }
    return [...map.values()].sort((a, b) => b.granted - a.granted || a.service.localeCompare(b.service));
  }, [grants]);

  const identityArn = identity?.arn;
  const routes = useMemo(() => {
    const rows = (relationships.data?.relationship_table ?? []).map((row, index) => ({
      ...row,
      key: `${row.direction}|${row.relationship}|${row.target_arn}|${index}`,
      crossAccount: Boolean(identityArn && accountOf(row.target_arn) && accountOf(row.target_arn) !== accountOf(identityArn)),
    }));
    return {
      inbound: rows.filter((row) => row.direction === 'inbound'),
      outbound: rows.filter((row) => row.direction === 'outbound'),
      all: rows,
    };
  }, [relationships.data, identityArn]);

  const crossEdges = useMemo(
    () =>
      (relationships.data?.graph?.edges ?? []).filter((edge) => {
        const a = accountOf(edge.source);
        const b = accountOf(edge.target);
        // Same rule as the graph: account ids decide; the edge type only when one is missing.
        return a && b ? a !== b : edge.edge_type === 'cross_account';
      }),
    [relationships.data],
  );

  const trustPrincipals = useMemo(
    () =>
      (credentials.data?.credentials ?? []).flatMap((credential) =>
        (credential.trust_principals ?? []).map((principal) => ({
          ...principal,
          credential: credential.name,
          key: `${credential.credential_id}|${principal.type}|${principal.principal}`,
        })),
      ),
    [credentials.data],
  );

  const isAdmin = (credentials.data?.credentials ?? []).some((credential) =>
    (Array.isArray(credential.attached_policies) ? credential.attached_policies : []).some(
      (policy) => policy?.policy_name === 'AdministratorAccess',
    ),
  );

  const resourceRows = useMemo(() => resources.data?.resources_accessed ?? [], [resources.data]);
  const crossRows = useMemo(() => crossAccount.data?.cross_account_activity ?? [], [crossAccount.data]);

  /* What the export writes is what the open tab shows. */
  const exportSpec = useMemo(() => {
    const name = identity?.name ?? 'identity';
    if (tab === 'access') {
      return {
        name: `access-${name}-grants`,
        rows: grants,
        columns: [
          { header: 'Action', value: (row) => row.action },
          { header: 'Service', value: (row) => row.service },
          { header: 'Resource', value: (row) => row.resource_arn },
          { header: 'Effect', value: (row) => row.effect },
          { header: 'Policy', value: (row) => row.policy_source },
          { header: 'Policy type', value: (row) => row.policy_type },
          { header: 'Used (90 days)', value: (row) => (row.used ? 'yes' : 'no') },
          { header: 'Last used', value: (row) => row.last_used_at ?? '' },
        ],
      };
    }
    if (tab === 'routes') {
      return {
        name: `access-${name}-routes`,
        rows: routes.all,
        columns: [
          { header: 'Direction', value: (row) => row.direction },
          { header: 'Relationship', value: (row) => row.relationship },
          { header: 'Name', value: (row) => row.target_name },
          { header: 'ARN', value: (row) => row.target_arn },
          { header: 'Type', value: (row) => row.target_type },
          { header: 'Crosses accounts', value: (row) => (row.crossAccount ? 'yes' : 'no') },
          { header: 'Calls', value: (row) => row.call_count ?? '' },
          { header: 'Last active', value: (row) => row.last_active ?? '' },
        ],
      };
    }
    if (tab === 'cross') {
      return {
        name: `access-${name}-cross-account`,
        rows: [
          ...crossRows.map((row) => ({
            kind: 'Calls into another account (30 days)',
            account: row.target_account_id,
            principal: row.target_resource_arn,
            detail: `${(row.actions ?? []).join(' ')} x${row.event_count}`,
            when: row.last_seen,
          })),
          ...crossEdges.map((edge) => ({
            kind: `Relationship: ${edgeLabel(edge.edge_type, edge.source === identity?.arn ? 'out' : 'in')}`,
            account: accountOf(edge.source === identity?.arn ? edge.target : edge.source),
            principal: edge.source === identity?.arn ? edge.target : edge.source,
            detail: edge.source === identity?.arn ? 'outbound' : 'inbound',
            when: edge.last_observed_at ?? '',
          })),
          ...trustPrincipals.map((principal) => ({
            kind: `Trust principal of ${principal.credential}`,
            account: accountOf(principal.principal),
            principal: principal.principal,
            detail: `${principal.type}${principal.is_external ? ', external' : ''}${principal.condition ? `, condition ${principal.condition}` : ', no condition'}`,
            when: '',
          })),
        ],
        columns: [
          { header: 'Kind', value: (row) => row.kind },
          { header: 'Account', value: (row) => row.account },
          { header: 'Principal or resource', value: (row) => row.principal },
          { header: 'Detail', value: (row) => row.detail },
          { header: 'Last seen', value: (row) => row.when },
        ],
      };
    }
    return {
      name: `access-${name}-resources`,
      rows: resourceRows,
      columns: [
        { header: 'Resource', value: (row) => row.resource_arn },
        { header: 'Type', value: (row) => row.resource_type },
        { header: 'Account', value: (row) => row.account_id },
        { header: 'Actions', value: (row) => (row.actions ?? []).join(' ') },
        { header: 'Reads', value: (row) => row.read_count },
        { header: 'Writes', value: (row) => row.write_count },
        { header: 'Errors', value: (row) => row.error_count },
        { header: 'First access', value: (row) => row.first_access },
        { header: 'Last access', value: (row) => row.last_access },
        { header: 'Cross-account', value: (row) => (row.is_cross_account ? 'yes' : 'no') },
      ],
    };
  }, [tab, identity, grants, routes, crossRows, crossEdges, trustPrincipals, resourceRows]);

  const onExport = useCallback(() => {
    exportRowsToCsv({ filename: timestampedName(exportSpec.name), columns: exportSpec.columns, rows: exportSpec.rows });
    notify({
      title: 'Exported',
      description: `${formatNumber(exportSpec.rows.length)} rows from "${TABS.find((entry) => entry.value === tab)?.label}" written.`,
      variant: 'success',
    });
  }, [exportSpec, notify, tab]);

  if (overview.isError) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Identity access"
          actions={
            <Button variant="secondary" as={Link} to="/access-graph" icon={ArrowLeft}>
              Back to the access graph
            </Button>
          }
        />
        <Panel prominence="quiet">
          <ErrorState
            error={overview.error}
            title={overview.error?.status === 404 ? 'This identity is not in the accounts and regions you selected' : undefined}
            onRetry={overview.error?.status === 404 ? undefined : overview.refetch}
          />
        </Panel>
      </div>
    );
  }

  if (!identity) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Identity access" />
        <StatStripSkeleton count={4} />
        <Panel prominence="lead">
          <DetailSkeleton rows={8} />
        </Panel>
      </div>
    );
  }

  const crossCount = crossEdges.length + crossRows.length;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={identity.name}
        lede="Everything this identity is allowed to do, what it actually used, and who or what it is connected to."
        actions={
          <>
            <Button variant="secondary" as={Link} to={accessGraphRoute(identity.id)} icon={Network}>
              Open access graph
            </Button>
            <Button variant="ghost" as={Link} to={identityRoute(identity.id)} iconRight={ExternalLink}>
              Open identity
            </Button>
            <Button
              variant="secondary"
              icon={Download}
              onClick={onExport}
              disabled={exportSpec.rows.length === 0}
            >
              Export this tab
            </Button>
          </>
        }
        meta={
          <div className="flex flex-wrap items-center gap-2">
            <Tag tone="neutral" size="sm">
              {subtypeLabel('identity', identity.identity_type)}
            </Tag>
            <Tag tone="neutral" size="sm">
              Account {identity.account_id}
            </Tag>
            {identity.region && (
              <Tag tone="neutral" size="sm">
                {identity.region}
              </Tag>
            )}
            {identity.environment && (
              <Tag tone="neutral" size="sm">
                {identity.environment}
              </Tag>
            )}
            {isAdmin && (
              <Tag tone="critical" size="sm" dot>
                Full administrator access
              </Tag>
            )}
            {identity.is_publicly_accessible && (
              <Tag tone="critical" size="sm" dot>
                Publicly accessible
              </Tag>
            )}
            {identity.last_activity_at && (
              <span className="text-[11.5px] text-ink-3" title={formatDateTime(identity.last_activity_at)}>
                Last active {formatRelative(identity.last_activity_at)}
              </span>
            )}
            {/* The AWS address, last: only needed to paste into another tool. */}
            <span className="flex min-w-0 max-w-full items-center gap-1 text-[11px] text-ink-3">
              <span className="min-w-0 truncate font-mono" title={identity.arn}>
                {identity.arn}
              </span>
              <CopyButton value={identity.arn} label="Copy ARN" size="sm" />
            </span>
          </div>
        }
      />

      <div className="grid gap-3 @min-[30rem]:grid-cols-2 @min-[54rem]:grid-cols-4">
        <MetricTile
          label="Permissions allowed"
          value={permissions.isError ? undefined : summary?.granted_count}
          tone="brand"
          caption={
            permissions.isError
              ? 'Could not be loaded. Try again later.'
              : summary
                ? `${formatNumber(summary.used_count)} used in the last 90 days`
                : 'Loading'
          }
          className="animate-rise"
        />
        <MetricTile
          label="Allowed but never used"
          value={permissions.isError ? undefined : summary?.unused_count}
          tone={summary?.unused_count > 0 ? 'medium' : 'low'}
          caption={summary ? `It used ${formatNumber(summary.utilization_pct)}% of its permissions` : undefined}
          meter={summary ? summary.utilization_pct : undefined}
          meterLabel="Share of its permissions it actually used"
          className="animate-rise"
          data-stagger=""
          style={{ '--stagger': 1 }}
        />
        <MetricTile
          label="Resources it used"
          value={resources.isError ? undefined : resources.data?.total_resources}
          tone="info"
          caption={
            resources.data && resources.data.data_available === false
              ? 'Activity log could not be read just now'
              : 'Last 30 days, from the AWS activity log'
          }
          className="animate-rise"
          data-stagger=""
          style={{ '--stagger': 2 }}
        />
        <MetricTile
          label="Relationships"
          value={relationships.isError || !relationships.data ? undefined : routes.all.length}
          tone={crossCount > 0 ? 'high' : 'info'}
          caption={
            relationships.data
              ? `${formatNumber(routes.outbound.length)} from it, ${formatNumber(routes.inbound.length)} to it${crossCount > 0 ? `, ${formatNumber(crossEdges.length)} reach another account` : ''}`
              : undefined
          }
          className="animate-rise"
          data-stagger=""
          style={{ '--stagger': 3 }}
        />
      </div>

      {/* Granted against used, per service: the least-privilege argument in
          one panel. */}
      <Panel prominence="lead" className="animate-rise">
        <PanelHeader
          prominence="lead"
          title="Allowed against used, by AWS service"
          subtitle="Each permission its credentials allow, and whether the AWS activity log recorded it being used in the last 90 days. Permissions never used can usually be removed."
        />
        {permissions.isError ? (
          <div className="mt-3">
            <InlineError error={permissions.error} onRetry={permissions.refetch} label="Permissions could not be loaded" />
          </div>
        ) : !permissions.data ? (
          <DetailSkeleton rows={3} />
        ) : byService.length === 0 ? (
          <EmptyState
            compact
            icon={ShieldCheck}
            title="No permissions recorded"
            description="Discovery found no permission on the credentials this identity uses, so it cannot do anything in AWS."
          />
        ) : (
          <div className="mt-3 grid gap-4 @min-[44rem]:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
            <ul className="flex flex-col gap-2.5">
              {byService.map((entry) => (
                <li key={entry.service}>
                  <div className="flex items-baseline justify-between gap-2 text-[12.5px]">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <span className="truncate font-medium text-ink">{entry.service}</span>
                      {entry.wildcard && (
                        <Tag tone="high" size="sm" title="A wildcard (*) allows every action of this service">
                          Allows everything
                        </Tag>
                      )}
                    </span>
                    <span data-numeric="" className="shrink-0 text-ink-3">
                      {formatNumber(entry.used)} of {formatNumber(entry.granted)} used
                    </span>
                  </div>
                  <Meter
                    className="mt-1"
                    value={(entry.used / Math.max(1, entry.granted)) * 100}
                    tone={entry.used === 0 ? 'medium' : 'info'}
                    label={`${entry.service}: ${entry.used} of ${entry.granted} grants used`}
                  />
                </li>
              ))}
            </ul>
            <div>
              <SectionLabel>All permissions</SectionLabel>
              <ProportionBar
                className="mt-2"
                height={10}
                total={Math.max(1, summary?.granted_count ?? 0)}
                ariaLabel={`${summary?.used_count ?? 0} used, ${summary?.unused_count ?? 0} unused`}
                segments={[
                  { key: 'used', label: 'Used', value: summary?.used_count ?? 0, color: 'var(--t-info)' },
                  { key: 'unused', label: 'Never used', value: summary?.unused_count ?? 0, color: 'var(--t-medium)' },
                ]}
              />
              <p className="mt-2 text-[12.5px] text-ink-2">
                {summary?.unused_count > 0
                  ? `${formatNumber(summary.unused_count)} permission${summary.unused_count === 1 ? ' has' : 's have'} not been used in 90 days. Each could be removed without breaking anything this identity has done.`
                  : 'Every permission has been used in the last 90 days.'}
              </p>
              {(permissions.data.policy_breakdown ?? []).length > 0 && (
                <>
                  <SectionLabel className="mt-3">By permission policy (used / allowed)</SectionLabel>
                  <ul className="mt-1.5 flex flex-col gap-1 text-[12px]">
                    {permissions.data.policy_breakdown.map((policy) => (
                      <li key={`${policy.policy_name}-${policy.policy_type}`} className="flex justify-between gap-2">
                        <span className="min-w-0 truncate text-ink-2">
                          {policyName(policy.policy_name)} <span className="text-ink-3">· {humanizeToken(policy.policy_type)}</span>
                        </span>
                        <span data-numeric="" className="shrink-0 text-ink-3">
                          {formatNumber(policy.used_permissions)} / {formatNumber(policy.total_permissions)}
                        </span>
                      </li>
                    ))}
                  </ul>
                </>
              )}
            </div>
          </div>
        )}
      </Panel>

      <Panel prominence="default" flush className="animate-rise overflow-hidden" data-stagger="" style={{ '--stagger': 1 }}>
        <div className="border-b border-line px-4 py-3">
          <Tabs size="sm" tabs={TABS} value={tab} onChange={setTab} />
        </div>

        {tab === 'access' &&
          (permissions.isError ? (
            <div className="p-4">
              <InlineError error={permissions.error} onRetry={permissions.refetch} label="Permissions could not be loaded" />
            </div>
          ) : (
            <DataGrid
              caption="Permissions it is allowed"
              columns={grantColumns}
              rows={grants}
              loading={permissions.isLoading}
              rowKey={(row) => row.key}
              density="comfortable"
              emptyState={
                <EmptyState
                  icon={ShieldCheck}
                  title="No permissions recorded"
                  description="Discovery found no permission on the credentials this identity uses, so it cannot do anything in AWS."
                />
              }
            />
          ))}

        {tab === 'routes' &&
          (relationships.isError ? (
            <div className="p-4">
              <InlineError error={relationships.error} onRetry={relationships.refetch} label="Relationships could not be loaded" />
            </div>
          ) : !relationships.data ? (
            <div className="p-4">
              <DetailSkeleton rows={4} />
            </div>
          ) : (
            <div className="grid gap-4 p-4 @min-[52rem]:grid-cols-2">
              <RouteList
                title="What it uses and reaches"
                subtitle="The roles it uses, the roles it can switch to, what it called, and who created it."
                rows={routes.outbound}
                empty="It uses and reaches nothing in the accounts and regions you selected."
              />
              <RouteList
                title="Who or what uses it"
                subtitle="Identities and services that use it, may switch to it, or called it."
                rows={routes.inbound}
                empty="Nothing in the accounts and regions you selected uses or reaches it."
              />
            </div>
          ))}

        {tab === 'cross' && (
          <div className="grid gap-5 p-4 @min-[52rem]:grid-cols-2">
            <div>
              <SectionLabel>What it used in other AWS accounts (last 30 days)</SectionLabel>
              {crossAccount.isError ? (
                <div className="mt-2">
                  <InlineError error={crossAccount.error} onRetry={crossAccount.refetch} label="Activity could not be loaded" />
                </div>
              ) : !crossAccount.data ? (
                <DetailSkeleton rows={2} />
              ) : crossRows.length === 0 ? (
                <p className="mt-2 text-[12px] text-ink-3">
                  {crossAccount.data.data_available === false
                    ? 'The AWS activity log could not be read just now, so this is unknown. Try again later.'
                    : 'Nothing it did reached another AWS account.'}
                </p>
              ) : (
                <ul className="mt-2 flex flex-col">
                  {crossRows.map((row) => (
                    <li key={`${row.target_account_id}-${row.target_resource_arn}`} className="border-b border-line py-2 last:border-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <Tag tone="high" size="sm" dot>
                          Account {row.target_account_id}
                        </Tag>
                        <span className="min-w-0 truncate text-[12.5px] text-ink" title={row.target_resource_arn}>
                          {row.target_resource_name || row.target_resource_arn}
                        </span>
                      </div>
                      <p className="mt-1 text-[11.5px] text-ink-3">
                        {(row.actions ?? []).join(', ')} · {formatNumber(row.event_count)} calls · last{' '}
                        <span title={formatDateTime(row.last_seen)}>{formatRelative(row.last_seen)}</span>
                      </p>
                    </li>
                  ))}
                </ul>
              )}

              <SectionLabel className="mt-4">Relationships that reach another account</SectionLabel>
              {crossEdges.length === 0 ? (
                <p className="mt-2 text-[12px] text-ink-3">
                  {relationships.data ? 'None of its relationships reach another AWS account.' : 'Loading.'}
                </p>
              ) : (
                <ul className="mt-2 flex flex-col">
                  {crossEdges.map((edge) => {
                    const out = edge.source === identity.arn;
                    const other = out ? edge.target : edge.source;
                    return (
                      <li key={`${edge.source}-${edge.target}-${edge.edge_type}`} className="border-b border-line py-2 last:border-0">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <Tag tone={edgeKindMeta(edge.edge_type).tone} size="sm">
                            {out ? edgeLabel(edge.edge_type, 'out') : edgeLabel(edge.edge_type, 'in')}
                          </Tag>
                          <span className="min-w-0 truncate font-mono text-[11.5px] text-ink" title={other}>
                            {other}
                          </span>
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>

            <div>
              <SectionLabel>Who may use (assume) its credentials</SectionLabel>
              {credentials.isError ? (
                <div className="mt-2">
                  <InlineError error={credentials.error} onRetry={credentials.refetch} label="Credentials could not be loaded" />
                </div>
              ) : !credentials.data ? (
                <DetailSkeleton rows={2} />
              ) : trustPrincipals.length === 0 ? (
                <p className="mt-2 text-[12px] text-ink-3">
                  No trust policy on its credentials names anyone, so nothing else can use them.
                </p>
              ) : (
                <ul className="mt-2 flex flex-col">
                  {trustPrincipals.map((principal) => (
                    <li key={principal.key} className="border-b border-line py-2 last:border-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="min-w-0 font-mono text-[11.5px] break-all text-ink">{principal.principal}</span>
                        {principal.is_external && (
                          <Tag tone="high" size="sm" dot>
                            Another account
                          </Tag>
                        )}
                        {!principal.condition && (
                          <Tag tone="medium" size="sm">
                            No extra conditions
                          </Tag>
                        )}
                      </div>
                      <p className="mt-1 text-[11.5px] text-ink-3">
                        {humanizeToken(principal.type)} · may use {principal.credential}
                        {principal.condition ? ` · only when ${principal.condition}` : ''}
                      </p>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        )}

        {tab === 'resources' &&
          (resources.isError ? (
            <div className="p-4">
              <InlineError error={resources.error} onRetry={resources.refetch} label="Resources could not be loaded" />
            </div>
          ) : (
            <DataGrid
              caption="Resources it used"
              columns={resourceColumns}
              rows={resourceRows}
              loading={resources.isLoading}
              rowKey={(row) => row.resource_arn}
              density="comfortable"
              emptyState={
                <EmptyState
                  icon={ShieldCheck}
                  title={resources.data?.data_available === false ? 'Activity log could not be read' : 'Nothing used in the last 30 days'}
                  description={
                    resources.data?.data_available === false
                      ? 'The AWS activity log (CloudTrail) could not be read just now, so what this identity used is unknown. Try again later.'
                      : 'The AWS activity log recorded no successful call from this identity against a resource in the last 30 days.'
                  }
                />
              }
            />
          ))}
      </Panel>
    </div>
  );
}

const grantColumns = [
  {
    key: 'action',
    header: 'Permission',
    primary: true,
    width: '28%',
    cell: (row) => (
      <span className="block min-w-0">
        <span className="block truncate font-mono text-[12.5px] text-ink">{row.action}</span>
        <span className="mt-0.5 block truncate text-[11.5px] text-ink-3">{row.service}</span>
      </span>
    ),
  },
  {
    key: 'resource',
    header: 'Resource',
    width: '24%',
    cell: (row) => (
      <span className="block truncate font-mono text-[12px] text-ink-2" title={row.resource_arn}>
        {row.resource_arn === '*' ? 'Every resource (*)' : row.resource_arn}
      </span>
    ),
  },
  {
    key: 'policy',
    header: 'Allowed by',
    width: '18%',
    priority: 'wide',
    cell: (row) => (
      <span className="block min-w-0">
        <span className="block truncate text-[12.5px] text-ink-2">{row.policy_source}</span>
        <span className="block truncate text-[11px] text-ink-3">
          {humanizeToken(row.policy_type)} · {humanizeToken(row.effect)}
        </span>
      </span>
    ),
  },
  {
    key: 'used',
    header: 'Used',
    width: '14%',
    cell: (row) =>
      row.used ? (
        <Tag tone="info" size="sm">
          Used
        </Tag>
      ) : (
        <Tag tone="medium" size="sm" dot>
          Never used
        </Tag>
      ),
  },
  {
    key: 'last',
    header: 'Last used',
    width: '16%',
    cell: (row) => (
      <span className="text-[12px] text-ink-3" title={row.last_used_at ? formatDateTime(row.last_used_at) : undefined}>
        {row.last_used_at ? formatRelative(row.last_used_at) : 'Not in 90 days'}
      </span>
    ),
  },
];

const resourceColumns = [
  {
    key: 'resource',
    header: 'Resource',
    primary: true,
    width: '34%',
    cell: (row) => (
      <span className="block min-w-0">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-[13px] font-medium text-ink" title={row.resource_arn}>
            {row.resource_name || row.resource_arn}
          </span>
          {row.is_cross_account && (
            <Tag tone="high" size="sm">
              Another account
            </Tag>
          )}
        </span>
        <span className="mt-0.5 block truncate text-[11.5px] text-ink-3">
          {(row.resource_type || '').replace(/^AWS::/, '').replace(/::/g, ' ')} · account {row.account_id}
        </span>
      </span>
    ),
  },
  {
    key: 'actions',
    header: 'What it did',
    width: '26%',
    cell: (row) => (
      <span className="block truncate font-mono text-[12px] text-ink-2" title={(row.actions ?? []).join(', ')}>
        {(row.actions ?? []).join(', ')}
      </span>
    ),
  },
  {
    key: 'counts',
    header: 'Reads / writes',
    width: '14%',
    cell: (row) => (
      <span data-numeric="" className="text-[12.5px] text-ink-2">
        {formatNumber(row.read_count)} / {formatNumber(row.write_count)}
        {row.error_count > 0 && <span className="text-high"> · {formatNumber(row.error_count)} failed</span>}
      </span>
    ),
  },
  {
    key: 'last',
    header: 'Last used',
    width: '16%',
    priority: 'wide',
    cell: (row) => (
      <span className="text-[12px] text-ink-3" title={formatDateTime(row.last_access)}>
        {formatRelative(row.last_access)}
      </span>
    ),
  },
];

/* The service's relationship labels are plain already; this covers the one an
   older build still sends. */
const PLAIN_RELATIONSHIP = { 'Bound to': 'Uses' };

function RouteList({ title, subtitle, rows, empty }) {
  return (
    <div>
      <SectionLabel>{title}</SectionLabel>
      <p className="mt-1 text-[11.5px] leading-relaxed text-ink-3">{subtitle}</p>
      {rows.length === 0 ? (
        <p className="mt-2 text-[12px] text-ink-3">{empty}</p>
      ) : (
        <ul className="mt-2 flex flex-col">
          {rows.map((row) => (
            <li key={row.key} className="border-b border-line py-2 last:border-0">
              <div className="flex flex-wrap items-center gap-1.5">
                <Tag tone="neutral" size="sm">
                  {PLAIN_RELATIONSHIP[row.relationship] ?? row.relationship}
                </Tag>
                <span className="min-w-0 truncate text-[12.5px] text-ink" title={row.target_arn}>
                  {row.target_type === 'resource' || row.target_type === 'unknown'
                    ? arnResource(row.target_arn)
                    : row.target_name || row.target_arn}
                </span>
                <span className="text-[11px] text-ink-3">{NODE_KINDS[row.target_type]?.label ?? humanizeToken(row.target_type)}</span>
                {row.crossAccount && (
                  <Tag tone="high" size="sm">
                    Another account
                  </Tag>
                )}
              </div>
              <p className="mt-1 text-[11.5px] text-ink-3">
                {/* `last_active` is the identity's last activity for an identity,
                    the last call for a CloudTrail edge, and only the time
                    discovery recorded a configured edge otherwise. */}
                {[
                  row.call_count ? `${formatNumber(row.call_count)} calls` : null,
                  row.last_active
                    ? `${row.target_type === 'identity' ? 'last active' : row.call_count ? 'last call' : 'recorded'} ${formatRelative(row.last_active)}`
                    : null,
                ]
                  .filter(Boolean)
                  .join(' · ') || 'Set up in AWS, not a recorded call'}
              </p>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export { AccessDetailPage };
