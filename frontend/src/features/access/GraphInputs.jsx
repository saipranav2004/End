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
      label: 'The boxes: identities and credentials',
      source: 'Discovery inventory (Postgres): identities, credentials',
      gives: 'Identities (functions, tasks, instances, pipelines, IAM users) and the credentials they sign in with (roles, keys, secrets), with how much access each has and its status.',
      covered: true,
      links: [
        { to: '/identities', label: 'Identities' },
        { to: '/credentials', label: 'Credentials' },
      ],
    },
    {
      key: 'trust',
      label: 'The lines: who uses and who may use each role',
      source: 'Postgres trust_edges, written by discovery',
      gives: 'Which role each identity uses (heavy solid line), and who may switch to (assume) a role: another role, another AWS account, an AWS service, or people signing in through company single sign-on (dashed line).',
      covered: true,
    },
    {
      key: 'activity',
      label: 'The grey lines: what was actually used',
      source: 'AWS activity log (CloudTrail) events in ClickHouse, last 90 days',
      gives: 'What each identity successfully called, with how many calls, and who created it. Drawn as a solid grey line.',
      covered: activityAvailable !== false,
      note:
        activityAvailable === false
          ? 'The activity log could not be read just now, so the graph shows set-up only, not what was used.'
          : null,
    },
  ];

  const notEvaluated = [
    {
      key: 'guardrails',
      label: 'Organisation rules (SCPs) and permission limits',
      note: 'A line shows what a trust policy allows. Organisation-wide denies and permission limits (boundaries) that could still block it are not applied.',
    },
    {
      key: 'escalation',
      label: 'Ways to gain more access',
      note: 'Combinations that let an identity give itself more access (for example handing a role to a service it can then run) are not worked out, so no line is drawn as one.',
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
                  Included
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
            <span className="text-[12.5px] font-semibold text-ink">Which accounts and regions</span>
            <Link to="/integrations" className="text-[11px] text-brand hover:underline">
              Integrations
            </Link>
          </div>
          <p className="mt-1 text-[11.5px] leading-relaxed text-ink-2">
            The accounts and regions you selected. A line is drawn only when both of its ends are in them, so
            a path that leaves your selection stops at its edge.
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
                    <span className="text-ink-3">None selected</span>
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
                    <span className="text-ink-3">All regions</span>
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
        <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Not checked</p>
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
