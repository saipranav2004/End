import { Link } from 'react-router-dom';
import { getSettings } from '../../lib/api/access';
import { useQuery } from '../../lib/hooks/useQuery';
import { InlineError } from '../../ui/States';
import { Skeleton } from '../../ui/Skeleton';
import { Tag } from '../../ui/Tag';

/**
 * The "graph inputs" explainer: where every node and edge on the canvas comes
 * from, what is not evaluated, and the scope that cuts all of it.
 *
 * Written from what the discovery service actually does (see
 * internal/repository/v1/graph_scope.go and relationship_activity.go): trust
 * edges are read from Postgres hop by hop, CloudTrail edges from ClickHouse,
 * and an edge is kept only when both of its ends are inside the saved scope.
 */
export function GraphInputs({ activityAvailable }) {
  const settings = useQuery((signal) => getSettings(signal), []);
  const accounts = settings.data?.settings?.accounts?.selected ?? [];
  const regions = settings.data?.settings?.regions?.selected ?? [];

  const inputs = [
    {
      key: 'inventory',
      label: 'Identities and credentials',
      source: 'Discovery inventory (Postgres): identities, credentials',
      gives: 'The nodes. Workloads (functions, tasks, instances, pipelines, IAM users) and what they authenticate with (roles, keys, secrets), with their privilege level and status.',
      covered: true,
      links: [
        { to: '/identities', label: 'Identities' },
        { to: '/credentials', label: 'Credentials' },
      ],
    },
    {
      key: 'trust',
      label: 'Trust and binding edges',
      source: 'Postgres trust_edges, written by discovery',
      gives: 'Who can assume which role (assume_role, cross_account, trusts, federation), and which role each workload runs as (bound_to, service_trust). Drawn dashed.',
      covered: true,
    },
    {
      key: 'activity',
      label: 'Observed calls',
      source: 'CloudTrail events in ClickHouse, last 90 days',
      gives: 'accesses edges - what each identity successfully called, with a call count - and created_by, the principal that created it. Drawn solid.',
      covered: activityAvailable !== false,
      note:
        activityAvailable === false
          ? 'The event store could not be read just now, so the graph shows configuration only.'
          : null,
    },
  ];

  const notEvaluated = [
    {
      key: 'guardrails',
      label: 'SCPs and permission boundaries',
      note: 'An edge is what a trust policy allows. Organisation-level denies and boundaries are not applied to it.',
    },
    {
      key: 'escalation',
      label: 'Privilege escalation paths',
      note: 'Combinations such as iam:PassRole with a compute permission are not computed, so no edge is drawn as an escalation.',
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <ul className="flex flex-col gap-2">
        {inputs.map((input) => (
          <li
            key={input.key}
            className={`rounded-[var(--radius-control)] border p-2.5 ${
              input.covered ? 'border-line bg-surface-2' : 'border-medium/40 bg-medium-soft'
            }`}
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-[12.5px] font-semibold text-ink">{input.label}</span>
              {input.covered ? (
                <Tag tone="low" size="sm">
                  Collected
                </Tag>
              ) : (
                <Tag tone="medium" size="sm" dot>
                  Unavailable
                </Tag>
              )}
              {input.links?.map((link) => (
                <Link key={link.to} to={link.to} className="text-[11px] text-brand hover:underline">
                  {link.label}
                </Link>
              ))}
            </div>
            <p className="mt-1 font-mono text-[10.5px] break-words text-ink-3">{input.source}</p>
            <p className="mt-1 text-[11.5px] leading-relaxed text-ink-2">{input.gives}</p>
            {input.note && <p className="mt-1 text-[11.5px] leading-relaxed text-ink-3">{input.note}</p>}
          </li>
        ))}

        <li className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[12.5px] font-semibold text-ink">Scope</span>
            <Link to="/integrations" className="text-[11px] text-brand hover:underline">
              Integrations
            </Link>
          </div>
          <p className="mt-1 text-[11.5px] leading-relaxed text-ink-2">
            Your saved accounts and regions. An edge is kept only when both of its ends are in scope, so a
            path that leaves the selection stops at its edge, and AWS service principals (which have no
            account) are not drawn.
          </p>
          {settings.isError ? (
            <div className="mt-2">
              <InlineError error={settings.error} onRetry={settings.refetch} label="Scope not loaded" />
            </div>
          ) : !settings.data ? (
            <Skeleton className="mt-2 h-5 w-2/3" />
          ) : (
            <dl className="mt-2 flex flex-col gap-1 text-[11.5px]">
              <div className="flex flex-wrap items-baseline gap-1.5">
                <dt className="text-ink-3">Accounts</dt>
                <dd className="flex flex-wrap gap-1">
                  {accounts.length === 0 ? (
                    <span className="text-ink-3">none selected</span>
                  ) : (
                    accounts.map((account) => (
                      <Tag key={account} tone="neutral" size="sm">
                        {account}
                      </Tag>
                    ))
                  )}
                </dd>
              </div>
              <div className="flex flex-wrap items-baseline gap-1.5">
                <dt className="text-ink-3">Regions</dt>
                <dd className="flex flex-wrap gap-1">
                  {regions.length === 0 ? (
                    <span className="text-ink-3">all</span>
                  ) : (
                    regions.map((region) => (
                      <Tag key={region} tone="neutral" size="sm">
                        {region}
                      </Tag>
                    ))
                  )}
                </dd>
              </div>
            </dl>
          )}
        </li>
      </ul>

      <div>
        <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Not evaluated</p>
        <ul className="mt-1.5 flex flex-col gap-2">
          {notEvaluated.map((item) => (
            <li key={item.key} className="rounded-[var(--radius-control)] border border-medium/40 bg-medium-soft p-2.5">
              <span className="text-[12.5px] font-semibold text-ink">{item.label}</span>
              <p className="mt-1 text-[11.5px] leading-relaxed text-ink-2">{item.note}</p>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
