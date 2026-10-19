import { complianceLabel } from '../../lib/posture';
import { useMemo, useState } from 'react';
import {
  Activity,
  ArrowDownLeft,
  ArrowUpRight,
  Gauge,
  History,
  KeyRound,
  Lock,
  RotateCw,
  Users,
  Wrench,
} from 'lucide-react';
import { useAccess } from '../../app/useAccess';
import {
  DEFAULT_WINDOW,
  fetchIdentityActivity,
  fetchIdentityAlerts,
  fetchIdentityChecks,
  fetchIdentityCredentials,
  fetchIdentityHistory,
  fetchIdentityPosture,
  fetchIdentityRoleUse,
  POSTURE_WINDOWS,
  windowPhrase,
} from '../../lib/api/posture';
import { severityMeta } from '../../lib/domain';
import { formatDate, formatDateTime, formatNumber, formatRelative, titleCaseEnum } from '../../lib/format';
import { useDebouncedValue, useQuery } from '../../lib/hooks';
import { TrendChart } from '../../charts/TrendChart';
import { Button } from '../../ui/Button';
import { DataGrid } from '../../ui/DataGrid';
import { Field, Input, Select } from '../../ui/Field';
import { Menu } from '../../ui/Menu';
import { Panel, PanelHeader } from '../../ui/Panel';
import { DetailSkeleton, ListSkeleton, Skeleton } from '../../ui/Skeleton';
import { ClearState, EmptyState, ErrorState, InlineError } from '../../ui/States';
import { SegmentedControl } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { TONE_VAR } from '../../ui/cn';
import { CheckDrawer } from './CheckDrawer';
import { FindingDialog, findingActions, FindingStatusTag } from './findings';
import { BandTag, Delta, formatScore, identityTypeLabel, pillarLabel, PillarBars, resolveBand, ScoreRing } from './parts';
import { useCursorList } from './useCursorList';
import { usePostureIdentity } from './usePostureIdentity';

const VIEWS = [
  { value: 'overview', label: 'Overview' },
  { value: 'checks', label: 'Checks' },
  { value: 'history', label: 'History' },
  { value: 'credentials', label: 'Credentials' },
  { value: 'roles', label: 'Role use' },
  { value: 'activity', label: 'Activity' },
  { value: 'findings', label: 'Findings' },
];

/* Views whose numbers depend on the window. */
const WINDOWED = new Set(['overview', 'history', 'activity', 'findings']);

const USAGE_TONE = { 'In use': 'neutral', Unused: 'medium', 'Never used': 'medium' };

/**
 * One identity's posture, as the Posture tab of the identity page.
 *
 * `identity` is the discovery identity { id, arn, name }. The posture service
 * keys identities by its own id, so the identity is matched by exact ARN
 * first; one it has not scored gets an honest "not scored yet".
 *
 * Why the score is what it is, and what each fix is worth. Overview leads
 * with the failed checks, because that is what somebody opened it to do;
 * Checks lists every check that applies, passing ones included; History is
 * how it got here. Findings can be accepted, suppressed, reopened, assigned
 * and annotated by analysts and admins - the one write the service offers.
 */
export function PostureRecord({ identity }) {
  const resolved = usePostureIdentity(identity);

  if (!identity?.arn) {
    return (
      <Panel>
        <EmptyState icon={Gauge} title="No posture score" description="This identity has no ARN, so it cannot be matched to a posture score." />
      </Panel>
    );
  }
  if (resolved.isError && !resolved.data) {
    return (
      <Panel>
        <ErrorState error={resolved.error} onRetry={resolved.refetch} title="Posture failed to load" />
      </Panel>
    );
  }
  if (resolved.isLoading || resolved.data === undefined) return <RecordSkeleton />;
  if (!resolved.data) {
    return (
      <Panel>
        <EmptyState
          icon={Gauge}
          title="Not scored yet"
          description={`The posture service has no score for ${identity.name || 'this identity'}. Identities are scored after each discovery scan, so one discovered since the last scan appears after the next.`}
          action={
            <Button variant="secondary" size="sm" icon={RotateCw} onClick={resolved.refetch}>
              Check again
            </Button>
          }
        />
      </Panel>
    );
  }
  return <PostureBody key={resolved.data.identity_id} header={resolved.data} identity={identity} />;
}

function RecordSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy="true">
      <Panel prominence="lead">
        <div className="flex items-center gap-6">
          <Skeleton className="size-[104px] rounded-full" />
          <div className="flex-1">
            <Skeleton className="h-4 w-48 rounded" />
            <Skeleton className="mt-3 h-4 w-72 rounded" />
          </div>
        </div>
      </Panel>
      <Panel>
        <DetailSkeleton rows={5} />
      </Panel>
      <span role="status" className="sr-only">
        Loading posture
      </span>
    </div>
  );
}

/** The posture views of one scored identity; also the body of `/posture/:id`. */
export function PostureBody({ header, identity }) {
  const { can } = useAccess();
  const [view, setView] = useState('overview');
  const [window, setWindow] = useState(DEFAULT_WINDOW);
  const [drawer, setDrawer] = useState(null);
  const [dialog, setDialog] = useState(null);
  const [revision, setRevision] = useState(0);
  const id = header.identity_id;
  const editable = can('posture.findings');

  const postureQuery = useQuery((signal) => fetchIdentityPosture(id, { window }, signal), [id, window, revision]);
  const failedQuery = useQuery((signal) => fetchIdentityChecks(id, { status: 'failed' }, signal), [id, revision]);
  /* Finding states for the checks: every open finding, plus those closed in
     the longest window. */
  const indexQuery = useQuery(
    (signal) => fetchIdentityAlerts(id, { window: '90d', status: 'all', limit: 100 }, signal),
    [id, revision],
  );

  const posture = postureQuery.data?.posture;
  const findingsById = useMemo(
    () => new Map((indexQuery.data?.alerts ?? []).map((alert) => [alert.finding_id, alert])),
    [indexQuery.data],
  );
  const pillarNames = useMemo(
    () => Object.fromEntries((posture?.pillars ?? []).map((pillar) => [pillar.pillar, pillar.name])),
    [posture],
  );

  const findingFor = (check) => {
    if (!check?.finding_id) return null;
    const alert = findingsById.get(check.finding_id);
    return { finding_id: check.finding_id, title: check.title, status: alert?.status ?? null, row: alert, updated_at: check.finding_updated_at ?? null };
  };

  const reload = () => setRevision((value) => value + 1);
  const score = posture?.score ?? header.score;
  const band = posture?.band || header.band;
  const grade = posture?.grade || header.grade;
  const drawerCheck = drawer?.check;
  const drawerFinding = drawerCheck ? findingFor(drawerCheck) : null;

  const ctx = {
    id,
    header,
    identity,
    posture,
    window,
    editable,
    pillarNames,
    revision,
    findingFor,
    openCheck: (check) => setDrawer({ check }),
    act: (finding, mode) => setDialog({ finding, mode }),
    setView,
  };

  return (
    <div className="flex flex-col gap-4">
      {postureQuery.isError && !posture ? (
        <Panel>
          <ErrorState error={postureQuery.error} onRetry={postureQuery.refetch} title="The posture score failed to load" compact />
        </Panel>
      ) : (
        <ScoreSummary header={header} posture={posture} score={score} band={band} grade={grade} window={window} onAlerts={() => setView('findings')} />
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="max-w-full overflow-x-auto">
          <SegmentedControl label="Posture view" options={VIEWS} value={view} onChange={setView} />
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {!editable && (
            <p className="flex items-center gap-1.5 text-[12px] text-ink-3">
              <Lock aria-hidden="true" className="size-3.5" />
              Read-only. Working findings needs the Analyst role or higher.
            </p>
          )}
          {WINDOWED.has(view) && (
            <SegmentedControl
              label="Window"
              options={POSTURE_WINDOWS.map((value) => ({ value, label: value, title: `Last ${windowPhrase(value)}` }))}
              value={window}
              onChange={setWindow}
            />
          )}
        </div>
      </div>

      {view === 'overview' && <OverviewView ctx={ctx} postureQuery={postureQuery} failedQuery={failedQuery} score={score} band={band} />}
      {view === 'checks' && <ChecksView ctx={ctx} />}
      {view === 'history' && <HistoryView ctx={ctx} band={band} />}
      {view === 'credentials' && <CredentialsView ctx={ctx} />}
      {view === 'roles' && <RoleUseView ctx={ctx} />}
      {view === 'activity' && <ActivityView ctx={ctx} />}
      {view === 'findings' && <FindingsView ctx={ctx} />}

      {drawerCheck && (
        <CheckDrawer
          ruleId={drawerCheck.rule_id}
          check={drawerCheck}
          identity={{ name: header.name, score, band }}
          finding={drawerFinding}
          pillarNames={pillarNames}
          onFindingAction={(mode) => setDialog({ finding: drawerFinding, mode })}
          onClose={() => setDrawer(null)}
        />
      )}

      {dialog && (
        <FindingDialog
          finding={dialog.finding}
          mode={dialog.mode}
          onClose={() => setDialog(null)}
          onDone={reload}
          onConflict={reload}
        />
      )}
    </div>
  );
}

/* ── Summary ─────────────────────────────────────────────────────────────── */

function ScoreSummary({ header, posture, score, band, grade, window, onAlerts }) {
  const peer = posture?.peer_group;
  const after = posture?.after_every_fix;
  const badges = header.badges ?? {};
  const failed = posture?.checks_failed ?? 0;
  return (
    <Panel prominence="lead" className="animate-rise">
      <div className="flex flex-wrap items-center gap-x-8 gap-y-5">
        <div className="flex items-center gap-4">
          <ScoreRing score={score} band={band} size={104} grade={grade} />
          <div>
            <p className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">Posture score</p>
            {posture ? (
              <>
                <p className="mt-1 text-[13px] text-ink-2">
                  {failed === 0
                    ? 'Every applicable check passes.'
                    : `${formatNumber(failed)} failed ${failed === 1 ? 'check costs' : 'checks cost'} ${formatScore(posture.points_lost)} points.`}
                </p>
                <p className="mt-0.5 flex flex-wrap items-center gap-1.5 text-[12px] text-ink-3">
                  {formatNumber(posture.checks_evaluated)} checks evaluated
                  {posture.change?.value !== null && posture.change?.value !== undefined && (
                    <>
                      <span aria-hidden="true">·</span>
                      <Delta value={posture.change.value} />
                      <span>over {windowPhrase(window)}</span>
                    </>
                  )}
                </p>
              </>
            ) : (
              <Skeleton className="mt-2 h-4 w-48 rounded" />
            )}
          </div>
        </div>
        {posture && (
          <dl className="grid flex-1 grid-cols-2 gap-x-6 gap-y-3 @min-[40rem]:grid-cols-4">
            <Fact label="After every fix" value={failed && after ? `${formatScore(after.score)} (+${formatScore(after.delta)})` : '-'} />
            <Fact
              label={peer ? `${titleCaseEnum(peer.functional_class)} average` : 'Peer average'}
              value={peer ? `${formatScore(peer.average)} across ${formatNumber(peer.size)} ${peer.size === 1 ? 'identity' : 'identities'}` : 'No peer group'}
            />
            <Fact label="Peer standing" value={peer?.standing ?? '-'} />
            <Fact label="Open findings" value={formatNumber(posture.open_alerts)} onClick={posture.open_alerts ? onAlerts : null} />
          </dl>
        )}
      </div>
      <div className="mt-4 flex flex-wrap items-center gap-1.5 border-t border-line pt-3">
        <BandTag score={score} band={band} />
        {grade && (
          <Tag tone="neutral" size="sm">
            Grade {grade}
          </Tag>
        )}
        {badges.mfa_enabled !== null && badges.mfa_enabled !== undefined && (
          <Tag tone={badges.mfa_enabled ? 'low' : 'critical'} size="sm">
            {badges.mfa_enabled ? 'MFA enabled' : 'MFA not enabled'}
          </Tag>
        )}
        {badges.console_access !== null && badges.console_access !== undefined && (
          <Tag tone={badges.console_access ? 'medium' : 'neutral'} size="sm">
            {badges.console_access ? 'Console access' : 'No console access'}
          </Tag>
        )}
        <span className="ml-1 min-w-0 truncate text-[12px] text-ink-3">
          {[
            identityTypeLabel(header.type),
            header.functional_class ? titleCaseEnum(header.functional_class) : null,
            header.account?.name || header.account?.id,
            header.region,
            `${formatNumber(header.counts?.credentials)} ${header.counts?.credentials === 1 ? 'credential' : 'credentials'}`,
          ]
            .filter(Boolean)
            .join(' · ')}
        </span>
      </div>
    </Panel>
  );
}

function Fact({ label, value, onClick }) {
  return (
    <div className="min-w-0">
      <dt className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{label}</dt>
      <dd data-numeric="" className="mt-0.5 truncate text-[14px] font-semibold text-ink">
        {onClick ? (
          <button type="button" onClick={onClick} className="text-brand hover:underline">
            {value}
          </button>
        ) : (
          value
        )}
      </dd>
    </div>
  );
}

/* ── Overview ───────────────────────────────────────────────────────────── */

function OverviewView({ ctx, postureQuery, failedQuery, score, band }) {
  const { posture, header, editable } = ctx;
  if (postureQuery.isError && !posture) return null;
  if (!posture) {
    return (
      <Panel>
        <ListSkeleton rows={6} />
      </Panel>
    );
  }
  const failing = failedQuery.data?.checks ?? [];
  const impact = posture.score_impact ?? [];
  const after = posture.after_every_fix;
  const credentials = posture.bound_credentials ?? [];
  const peer = posture.peer_group;

  return (
    <div className="grid gap-4 @min-[64rem]:grid-cols-[minmax(0,1.45fr)_minmax(0,1fr)]">
      <div className="flex min-w-0 flex-col gap-4">
        <Panel className="animate-rise">
          <PanelHeader title="Security areas" subtitle="Score in each area. Serious issues: a critical or high check fails there. Minor issues: only medium or low checks fail." />
          <div className="mt-4">
            {(posture.pillars ?? []).length === 0 ? (
              <p className="text-[12.5px] text-ink-3">No security area applies to this identity.</p>
            ) : (
              <PillarBars
                pillars={posture.pillars}
                detail={(pillar) =>
                  !pillar.evaluated
                    ? 'No check here applies to it'
                    : pillar.failing
                      ? `${pillar.failing} of ${pillar.evaluated} ${pillar.evaluated === 1 ? 'check' : 'checks'} failing`
                      : `${pillar.evaluated} ${pillar.evaluated === 1 ? 'check' : 'checks'} passing`
                }
              />
            )}
          </div>
        </Panel>

        <Panel className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
          <PanelHeader title="Why this score" />
          {posture.checks_failed === 0 ? (
            <ClearState compact title="Nothing is costing points" description={`Every check that applies to ${header.name} passes.`} />
          ) : (
            <p className="mt-2 text-[13px] leading-relaxed text-ink-2">{whyThisScore(header.name, score, posture, failing)}</p>
          )}
        </Panel>

        <Panel flush className="animate-rise overflow-hidden" data-stagger="" style={{ '--stagger': 2 }}>
          <div className="px-4 pt-4 sm:px-5">
            <PanelHeader title="Failed checks" subtitle="Worst first. Each shows what fixing it earns and how to fix it." />
          </div>
          {failedQuery.isError && !failedQuery.data ? (
            <div className="px-4 py-4 sm:px-5">
              <InlineError error={failedQuery.error} onRetry={failedQuery.refetch} label="Checks unavailable" />
            </div>
          ) : !failedQuery.data ? (
            <div className="px-4 py-4 sm:px-5">
              <ListSkeleton rows={3} />
            </div>
          ) : failing.length === 0 ? (
            <div className="px-4 pb-4 sm:px-5">
              <ClearState compact title="No failed checks" description="There is nothing to remediate on this identity." />
            </div>
          ) : (
            <ul className="mt-3 divide-y divide-line border-t border-line">
              {failing.map((check) => (
                <FailedCheck key={check.rule_id} check={check} ctx={ctx} editable={editable} />
              ))}
            </ul>
          )}
        </Panel>
      </div>

      <div className="flex min-w-0 flex-col gap-4">
        <Panel className="animate-rise">
          <PanelHeader title="Score impact" subtitle="What each fix adds, largest first." />
          {impact.length === 0 ? (
            <p className="mt-3 text-[12.5px] text-ink-3">At {formatScore(score)}, nothing left costs points.</p>
          ) : (
            <>
              <ul className="mt-3 flex flex-col gap-2">
                {impact.map((item) => (
                  <li key={item.rule_id} className="flex items-center gap-3">
                    <span className="min-w-0 flex-1 truncate text-[12.5px] text-ink-2" title={item.fix}>
                      {item.fix || item.rule_id}
                    </span>
                    <span data-numeric="" className="shrink-0 text-[13px] font-bold text-low">
                      +{formatScore(item.points)}
                    </span>
                  </li>
                ))}
              </ul>
              {after && (
                <div className="mt-4 flex items-center gap-3 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2.5">
                  <ScoreRing score={after.score} size={40} />
                  <p className="text-[12.5px] leading-snug text-ink-2">
                    Projected score after every fix: <span className="font-semibold text-ink">{formatScore(after.score)}</span>
                    {resolveBand({ score: after.score }) ? `, ${resolveBand({ score: after.score }).label}` : ''}.
                  </p>
                </div>
              )}
            </>
          )}
        </Panel>

        <Panel className="animate-rise" data-stagger="" style={{ '--stagger': 1 }}>
          <PanelHeader icon={KeyRound} title="Bound credentials" subtitle="What this identity holds." />
          {credentials.length === 0 ? (
            <p className="mt-3 text-[12.5px] text-ink-3">No credentials are bound to it.</p>
          ) : (
            <ul className="mt-3 divide-y divide-line">
              {credentials.map((cred) => (
                <li key={`${cred.type}-${cred.id}`} className="flex items-center gap-3 py-2">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-mono text-[12px] text-ink" title={cred.id}>
                      {cred.id}
                    </span>
                    <span className="block truncate text-[11px] text-ink-3">
                      {[
                        titleCaseEnum(cred.type),
                        Number.isFinite(cred.age_days) ? `${formatNumber(cred.age_days)} days old` : null,
                        Number.isFinite(cred.last_used_days)
                          ? cred.last_used_days === 0
                            ? 'used today'
                            : `used ${formatNumber(cred.last_used_days)} ${cred.last_used_days === 1 ? 'day' : 'days'} ago`
                          : null,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  </span>
                  {cred.status && (
                    <Tag tone={USAGE_TONE[cred.status] ?? 'neutral'} size="sm">
                      {cred.status}
                    </Tag>
                  )}
                </li>
              ))}
            </ul>
          )}
          <button type="button" onClick={() => ctx.setView('credentials')} className="mt-3 text-[12px] font-medium text-brand hover:underline">
            Credential posture of this identity
          </button>
        </Panel>

        <Panel className="animate-rise" data-stagger="" style={{ '--stagger': 2 }}>
          <PanelHeader
            icon={Users}
            title="Peer group"
            subtitle={peer ? `The ${formatNumber(peer.size)} identities classified ${titleCaseEnum(peer.functional_class)}.` : undefined}
          />
          {peer ? (
            <PeerBar peer={peer} score={score} band={band} />
          ) : (
            <p className="mt-3 text-[12.5px] text-ink-3">It has no functional class, so it has no peer group to compare with.</p>
          )}
        </Panel>
      </div>
    </div>
  );
}

function FailedCheck({ check, ctx, editable }) {
  const severity = severityMeta(check.severity);
  const finding = ctx.findingFor(check);
  const actions = finding && editable ? findingActions(finding.status, (mode) => ctx.act(finding, mode)) : [];
  const points = Number(check.points_if_fixed) || 0;
  return (
    <li className="flex flex-wrap items-start gap-x-4 gap-y-2 px-4 py-3.5 sm:px-5">
      <span aria-hidden="true" className="mt-1.5 size-2 shrink-0 rounded-full" style={{ background: TONE_VAR[severity.tone] }} />
      <div className="min-w-0 flex-1 basis-64">
        <p className="flex flex-wrap items-center gap-2">
          <span className="text-[13px] font-semibold text-ink">{check.title}</span>
          <Tag tone={severity.tone} size="sm">
            {severity.label}
          </Tag>
          <Tag tone="neutral" size="sm">
            {pillarLabel(check.pillar, ctx.pillarNames)}
          </Tag>
          {finding?.status && finding.status !== 'OPEN' && <FindingStatusTag status={finding.status} />}
        </p>
        {check.detail && <p className="mt-1 text-[12.5px] leading-relaxed text-ink-2">{check.detail}</p>}
        <p className="mt-1 text-[11.5px] text-ink-3">
          {check.failing_since ? `Failing since ${formatDate(check.failing_since)}` : 'Failing'}
          {check.compliance?.length ? ` · ${check.compliance.map(complianceLabel).join(' · ')}` : ''}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-2 self-center">
        <span data-numeric="" className="text-[12.5px] font-semibold text-low" title={points ? 'Points fixing it earns' : 'Costs no points now'}>
          +{formatScore(points)}
        </span>
        <Button variant="primary" size="sm" icon={Wrench} onClick={() => ctx.openCheck(check)}>
          How to fix
        </Button>
        {actions.length > 0 && <Menu items={actions} label={`Finding actions for ${check.title}`} />}
      </div>
    </li>
  );
}

function PeerBar({ peer, score, band }) {
  const marks = [
    { key: 'you', label: 'This identity', value: score, strong: true },
    { key: 'average', label: 'Group average', value: peer.average },
    { key: 'best', label: 'Best in group', value: peer.best },
  ].filter((mark) => Number.isFinite(Number(mark.value)));
  const tone = resolveBand({ band, score })?.tone ?? 'neutral';
  return (
    <div className="mt-4">
      <div className="relative h-2 rounded-full bg-track">
        {marks.map((mark) => (
          <span
            key={mark.key}
            aria-hidden="true"
            className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-surface"
            style={{
              left: `${Math.max(0, Math.min(100, Number(mark.value)))}%`,
              width: mark.strong ? 14 : 10,
              height: mark.strong ? 14 : 10,
              background: mark.strong ? TONE_VAR[tone] : 'var(--t-ink-3)',
            }}
          />
        ))}
      </div>
      <dl className="mt-4 grid grid-cols-3 gap-3">
        {marks.map((mark) => (
          <div key={mark.key}>
            <dt className="text-[11px] text-ink-3">{mark.label}</dt>
            <dd data-numeric="" className="text-[14px] font-semibold text-ink">
              {formatScore(mark.value)}
            </dd>
          </div>
        ))}
      </dl>
      <p className="mt-3 text-[11.5px] leading-relaxed text-ink-3">
        {peer.standing}
        {peer.size > 1 ? ` - ranked ${formatNumber(peer.rank)} of ${formatNumber(peer.size)}, highest score first.` : '.'}
      </p>
    </div>
  );
}

/* ── Checks ─────────────────────────────────────────────────────────────── */

function ChecksView({ ctx }) {
  const [status, setStatus] = useState('all');
  const [pillar, setPillar] = useState('');
  const query = useQuery((signal) => fetchIdentityChecks(ctx.id, { status, pillar }, signal), [ctx.id, status, pillar, ctx.revision]);
  const checks = query.data?.checks ?? [];
  const pillars = ctx.posture?.pillars ?? [];

  const columns = [
    {
      key: 'check',
      header: 'Check',
      primary: true,
      width: '28%',
      cell: (check) => (
        <span className="block min-w-0">
          <span className="block text-[12.5px] font-medium text-ink">{check.title}</span>
          {check.compliance?.length > 0 && <span className="block truncate text-[11px] text-ink-3">{check.compliance.map(complianceLabel).join(' · ')}</span>}
        </span>
      ),
    },
    {
      key: 'pillar',
      header: 'Area',
      width: '13%',
      cell: (check) => <span className="text-[12.5px] text-ink-2">{pillarLabel(check.pillar, ctx.pillarNames)}</span>,
    },
    {
      key: 'severity',
      header: 'Severity',
      width: '10%',
      cell: (check) =>
        check.severity ? (
          <Tag tone={severityMeta(check.severity).tone} size="sm">
            {severityMeta(check.severity).label}
          </Tag>
        ) : (
          <span className="text-[12.5px] text-ink-3">-</span>
        ),
    },
    {
      key: 'result',
      header: 'Result',
      width: '13%',
      cell: (check) => {
        const fail = String(check.status).toUpperCase() === 'FAIL';
        const finding = ctx.findingFor(check);
        return (
          <span className="flex flex-wrap items-center gap-1">
            <Tag tone={fail ? 'critical' : 'low'} size="sm">
              {fail ? 'Fail' : 'Pass'}
            </Tag>
            {finding?.status && finding.status !== 'OPEN' && <FindingStatusTag status={finding.status} />}
          </span>
        );
      },
    },
    {
      key: 'detail',
      header: 'Detail',
      width: '24%',
      priority: 'wide',
      cell: (check) => (
        <span className="block text-[12px] leading-snug text-ink-2">
          {check.detail || (String(check.status).toUpperCase() === 'FAIL' ? '-' : 'Passing.')}
          {check.failing_since && <span className="block text-[11px] text-ink-3">Since {formatDate(check.failing_since)}</span>}
        </span>
      ),
    },
    {
      key: 'points',
      header: 'Fix',
      width: '12%',
      cell: (check) =>
        String(check.status).toUpperCase() === 'FAIL' ? (
          <Button variant="secondary" size="sm" icon={Wrench} onClick={() => ctx.openCheck(check)} title="How to fix it">
            +{formatScore(check.points_if_fixed)}
          </Button>
        ) : (
          <Button variant="ghost" size="sm" onClick={() => ctx.openCheck(check)}>
            Details
          </Button>
        ),
    },
  ];

  return (
    <Panel flush className="animate-rise overflow-hidden">
      <div className="flex flex-wrap items-end justify-between gap-3 px-4 pt-4 pb-3 sm:px-5">
        <PanelHeader title="Every check that applies" subtitle="Checks that cannot apply to this identity are left out rather than counted as passes." />
        <div className="flex flex-wrap items-center gap-2">
          <SegmentedControl
            label="Result"
            options={[
              { value: 'all', label: 'All' },
              { value: 'failed', label: 'Failed' },
              { value: 'passed', label: 'Passed' },
            ]}
            value={status}
            onChange={setStatus}
          />
          <Select
            size="sm"
            aria-label="Security area"
            value={pillar}
            onChange={(event) => setPillar(event.target.value)}
            options={[{ value: '', label: 'Every pillar' }, ...pillars.map((entry) => ({ value: entry.pillar, label: entry.name || pillarLabel(entry.pillar) }))]}
            className="w-44"
          />
        </div>
      </div>
      {query.isError && !query.data ? (
        <ErrorState error={query.error} onRetry={query.refetch} compact />
      ) : (
        <DataGrid
          caption="Posture checks"
          columns={columns}
          rows={checks}
          rowKey={(check) => check.rule_id}
          density="compact"
          loading={query.isLoading && !query.data}
          refreshing={query.isRefreshing || (query.isLoading && Boolean(query.data))}
          rowActions={
            ctx.editable
              ? (check) => {
                  const finding = ctx.findingFor(check);
                  const items = finding ? findingActions(finding.status, (mode) => ctx.act(finding, mode)) : [];
                  return items.length ? <Menu items={items} label={`Finding actions for ${check.title}`} /> : <span className="block size-8" />;
                }
              : undefined
          }
          emptyState={
            <EmptyState
              compact
              title="No checks match"
              description={status === 'failed' ? 'Nothing fails with this filter.' : 'No check with this result applies in this area.'}
            />
          }
        />
      )}
    </Panel>
  );
}

/* ── History ────────────────────────────────────────────────────────────── */

function HistoryView({ ctx, band }) {
  const query = useQuery((signal) => fetchIdentityHistory(ctx.id, { window: ctx.window }, signal), [ctx.id, ctx.window, ctx.revision]);
  const perScan = ['24h', '3d', '7d'].includes(ctx.window);
  const points = query.data?.points ?? [];
  const series = points
    .filter((point) => point.score !== null && point.score !== undefined)
    .map((point) => ({
      ...point,
      label: perScan ? formatDateTime(point.at) : formatDate(point.at),
      subtitle: `${formatNumber(point.checks_failed)} failed checks`,
    }));
  const lowest = series.length ? Math.min(...series.map((point) => point.score)) : 0;

  /* The change log is read off consecutive points: only what the series itself says. */
  const changes = [];
  for (let index = 1; index < points.length; index += 1) {
    const before = points[index - 1];
    const after = points[index];
    const delta = Number(after.score) - Number(before.score);
    const checksDelta = (after.checks_failed ?? 0) - (before.checks_failed ?? 0);
    if ((Number.isFinite(delta) && Math.abs(delta) >= 0.05) || checksDelta !== 0) {
      changes.push({ at: after.at, delta: Number.isFinite(delta) ? delta : null, before, after, checksDelta });
    }
  }
  changes.reverse();

  if (query.isError && !query.data) {
    return (
      <Panel>
        <ErrorState error={query.error} onRetry={query.refetch} compact />
      </Panel>
    );
  }

  return (
    <div className="grid gap-4 @min-[64rem]:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
      <Panel className="animate-rise">
        <PanelHeader
          icon={History}
          title={`Score over ${windowPhrase(ctx.window)}`}
          subtitle={perScan ? 'One point per scan.' : 'One point per day.'}
        />
        {!query.data ? (
          <Skeleton className="mt-4 h-[200px] w-full rounded" />
        ) : series.length < 2 ? (
          <p className="mt-4 text-[12.5px] text-ink-3">
            {series.length === 0 ? 'No score recorded' : 'Only one score recorded'} in the last {windowPhrase(ctx.window)}, so there is no
            history to draw yet. A longer window may have more.
          </p>
        ) : (
          <TrendChart
            className="mt-4"
            data={series}
            dataKey="score"
            label="Posture score"
            height={200}
            valueFormatter={formatScore}
            color={TONE_VAR[resolveBand({ band, score: series[series.length - 1].score })?.tone ?? 'brand']}
            yDomain={[Math.max(0, Math.floor((lowest - 10) / 10) * 10), 100]}
          />
        )}
      </Panel>
      <Panel flush className="animate-rise overflow-hidden" data-stagger="" style={{ '--stagger': 1 }}>
        <div className="px-4 pt-4 sm:px-5">
          <PanelHeader title="Changes" subtitle="Newest first, read from the scores above." />
        </div>
        {!query.data ? (
          <div className="px-4 py-4 sm:px-5">
            <ListSkeleton rows={4} />
          </div>
        ) : changes.length === 0 ? (
          <div className="px-4 pb-4 sm:px-5">
            <ClearState compact title="No changes" description={`The score and the failed checks held steady over ${windowPhrase(ctx.window)}.`} />
          </div>
        ) : (
          <ol className="mt-3 max-h-[28rem] divide-y divide-line overflow-y-auto border-t border-line">
            {changes.map((change) => (
              <li key={change.at} className="flex items-start gap-3 px-4 py-3 sm:px-5">
                <span className="w-12 shrink-0 text-right">
                  <Delta value={change.delta} emptyLabel="-" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block text-[12.5px] font-medium text-ink">
                    {formatScore(change.before.score)} to {formatScore(change.after.score)}
                    {change.after.band && change.after.band !== change.before.band ? `, now ${resolveBand({ band: change.after.band })?.label}` : ''}
                  </span>
                  <span className="block text-[11.5px] leading-snug text-ink-3">
                    {change.checksDelta === 0
                      ? `${formatNumber(change.after.checks_failed)} failed checks, unchanged`
                      : `Failed checks ${formatNumber(change.before.checks_failed)} to ${formatNumber(change.after.checks_failed)}`}
                  </span>
                </span>
                <span className="shrink-0 text-[11px] text-ink-3" title={formatDateTime(change.at)}>
                  {perScan ? formatDateTime(change.at) : formatDate(change.at)}
                </span>
              </li>
            ))}
          </ol>
        )}
      </Panel>
    </div>
  );
}

/* ── Credentials ────────────────────────────────────────────────────────── */

function CredentialsView({ ctx }) {
  const query = useQuery((signal) => fetchIdentityCredentials(ctx.id, signal), [ctx.id, ctx.revision]);
  const rows = query.data?.credentials ?? [];
  const yesNo = (value) => (value === null || value === undefined ? '-' : value ? 'Yes' : 'No');
  const columns = [
    {
      key: 'credential',
      header: 'Credential',
      primary: true,
      width: '30%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate font-mono text-[12px] text-ink" title={row.credential_arn}>
            {row.credential_arn.split(/[/:]/).pop() || row.credential_arn}
          </span>
          <span className="block truncate text-[11px] text-ink-3">
            {titleCaseEnum(row.type)}
            {row.binding ? ` · ${titleCaseEnum(row.binding)}` : ''}
          </span>
        </span>
      ),
    },
    {
      key: 'usage',
      header: 'Usage',
      width: '14%',
      cell: (row) =>
        row.usage_state || row.status ? (
          <span className="flex flex-wrap gap-1">
            {row.status && (
              <Tag tone={/expired|inactive|revoked/i.test(row.status) ? 'critical' : 'neutral'} size="sm">
                {titleCaseEnum(row.status)}
              </Tag>
            )}
            {row.usage_state && (
              <Tag tone={USAGE_TONE[row.usage_state] ?? (/unused|never|stale/i.test(row.usage_state) ? 'medium' : 'neutral')} size="sm">
                {titleCaseEnum(row.usage_state)}
              </Tag>
            )}
          </span>
        ) : (
          '-'
        ),
    },
    {
      key: 'age',
      header: 'Age',
      width: '10%',
      cell: (row) => (Number.isFinite(row.age_days) ? `${formatNumber(row.age_days)} days` : '-'),
    },
    {
      key: 'used',
      header: 'Last used',
      width: '11%',
      cell: (row) =>
        Number.isFinite(row.last_used_days) ? (row.last_used_days === 0 ? 'Today' : `${formatNumber(row.last_used_days)} days ago`) : 'Never seen',
    },
    { key: 'privileged', header: 'Privileged', width: '9%', priority: 'wide', cell: (row) => yesNo(row.privileged) },
    { key: 'boundary', header: 'Boundary', width: '9%', priority: 'wide', cell: (row) => yesNo(row.has_boundary) },
    {
      key: 'session',
      header: 'Max session',
      width: '9%',
      priority: 'wide',
      cell: (row) => (Number.isFinite(row.max_session_duration) ? `${formatNumber(Math.round(row.max_session_duration / 60))} min` : '-'),
    },
    {
      key: 'findings',
      header: 'Open findings',
      width: '8%',
      align: 'right',
      cell: (row) => (
        <span data-numeric="" className={row.open_findings ? 'font-semibold text-critical' : 'text-ink-3'}>
          {formatNumber(row.open_findings)}
        </span>
      ),
    },
  ];
  return (
    <Panel flush className="animate-rise overflow-hidden">
      <div className="px-4 pt-4 pb-3 sm:px-5">
        <PanelHeader icon={KeyRound} title="Credentials" subtitle="Every credential this identity holds or can use, as the posture scan saw it." />
      </div>
      {query.isError && !query.data ? (
        <ErrorState error={query.error} onRetry={query.refetch} compact />
      ) : (
        <DataGrid
          caption="Credential posture"
          columns={columns}
          rows={rows}
          rowKey={(row) => row.credential_arn}
          density="compact"
          loading={query.isLoading && !query.data}
          emptyState={<EmptyState compact icon={KeyRound} title="No credentials" description="The posture scan found no credential for this identity." />}
        />
      )}
    </Panel>
  );
}

/* ── Role use ───────────────────────────────────────────────────────────── */

function RoleUseView({ ctx }) {
  const query = useQuery((signal) => fetchIdentityRoleUse(ctx.id, signal), [ctx.id, ctx.revision]);
  if (query.isError && !query.data) {
    return (
      <Panel>
        <ErrorState error={query.error} onRetry={query.refetch} compact />
      </Panel>
    );
  }
  if (!query.data) {
    return (
      <Panel>
        <ListSkeleton rows={4} />
      </Panel>
    );
  }
  const { roleUse } = query.data;
  return (
    <div className="grid gap-4 @min-[64rem]:grid-cols-2">
      <RoleList
        icon={ArrowDownLeft}
        title="Assumed by"
        subtitle={roleUse.assumable ? 'Who assumed this role, most sessions first.' : 'This identity is not a role that others can assume.'}
        items={roleUse.assumedBy}
        empty={roleUse.assumable ? 'Nobody assumed it in the recorded period.' : 'Not assumable.'}
      />
      <RoleList icon={ArrowUpRight} title="Assumes" subtitle="Roles this identity assumed." items={roleUse.assumes} empty="It assumed no role in the recorded period." />
    </div>
  );
}

function RoleList({ icon, title, subtitle, items, empty }) {
  return (
    <Panel flush className="animate-rise overflow-hidden">
      <div className="px-4 pt-4 sm:px-5">
        <PanelHeader icon={icon} title={title} subtitle={subtitle} />
      </div>
      {items.length === 0 ? (
        <p className="px-4 py-4 text-[12.5px] text-ink-3 sm:px-5">{empty}</p>
      ) : (
        <ul className="mt-3 divide-y divide-line border-t border-line">
          {items.map((item) => (
            <li key={item.role_arn} className="flex items-center gap-3 px-4 py-2.5 sm:px-5">
              <span className="min-w-0 flex-1">
                <span className="block truncate font-mono text-[12.5px] font-semibold text-ink" title={item.role_arn}>
                  {item.role_name || item.role_arn}
                </span>
                <span className="block truncate text-[11px] text-ink-3">
                  {[
                    item.functional_class ? titleCaseEnum(item.functional_class) : null,
                    item.first_assumed_at ? `first ${formatDate(item.first_assumed_at)}` : null,
                    item.last_assumed_at ? `last ${formatRelative(item.last_assumed_at)}` : null,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              </span>
              <span className="shrink-0 text-right">
                <span data-numeric="" className="block text-[13px] font-semibold text-ink">
                  {formatNumber(item.sessions)}
                </span>
                <span className="block text-[10.5px] text-ink-3">{item.sessions === 1 ? 'session' : 'sessions'}</span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

/* ── Activity ───────────────────────────────────────────────────────────── */

const SERVICE_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/;

function ActivityView({ ctx }) {
  const [mutating, setMutating] = useState('all');
  const [serviceText, setServiceText] = useState('');
  const debounced = useDebouncedValue(serviceText.trim(), 400);
  const serviceInvalid = Boolean(serviceText.trim()) && !SERVICE_PATTERN.test(serviceText.trim());
  const service = debounced && SERVICE_PATTERN.test(debounced) ? debounced : undefined;
  const list = useCursorList(
    (cursor, signal) =>
      fetchIdentityActivity(ctx.id, { window: ctx.window, mutating: mutating === 'all' ? undefined : mutating === 'write', service, limit: 50, cursor }, signal),
    [ctx.id, ctx.window, mutating, service, ctx.revision],
  );
  const events = list.pages.flatMap((page) => page.events);
  const available = list.pages[0]?.meta?.data_available;

  const columns = [
    {
      key: 'action',
      header: 'Action',
      primary: true,
      width: '28%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate font-mono text-[12.5px] font-semibold text-ink">{row.action}</span>
          <span className="block truncate text-[11px] text-ink-3">{row.service}</span>
        </span>
      ),
    },
    {
      key: 'kind',
      header: 'Kind',
      width: '12%',
      cell: (row) => (
        <Tag tone={row.mutating ? 'medium' : 'neutral'} size="sm">
          {row.mutating ? 'Write' : 'Read'}
        </Tag>
      ),
    },
    {
      key: 'result',
      header: 'Result',
      width: '16%',
      cell: (row) =>
        row.error_code ? (
          <Tag tone="critical" size="sm">
            {row.error_code}
          </Tag>
        ) : (
          <span className="text-[12.5px] text-ink-3">Succeeded</span>
        ),
    },
    { key: 'ip', header: 'Source IP', width: '16%', priority: 'wide', cell: (row) => <span className="font-mono text-[12px]">{row.source_ip || '-'}</span> },
    { key: 'region', header: 'Region', width: '12%', priority: 'wide', cell: (row) => row.region || '-' },
    {
      key: 'time',
      header: 'When',
      width: '16%',
      cell: (row) => (
        <span className="text-[12px] text-ink-2" title={formatDateTime(row.event_time)}>
          {formatRelative(row.event_time)}
        </span>
      ),
    },
  ];

  return (
    <Panel flush className="animate-rise overflow-hidden">
      <div className="flex flex-wrap items-end justify-between gap-3 px-4 pt-4 pb-3 sm:px-5">
        <PanelHeader icon={Activity} title="Recent activity" subtitle={`API calls made as this identity in the last ${windowPhrase(ctx.window)}, newest first.`} />
        <div className="flex flex-wrap items-start gap-2">
          <SegmentedControl
            label="Kind of call"
            options={[
              { value: 'all', label: 'All' },
              { value: 'write', label: 'Writes' },
              { value: 'read', label: 'Reads' },
            ]}
            value={mutating}
            onChange={setMutating}
          />
          <Field error={serviceInvalid ? 'Use a service name such as s3 or iam.' : undefined} className="w-44">
            <Input size="sm" aria-label="Service" placeholder="Service, e.g. s3" value={serviceText} invalid={serviceInvalid} onChange={(event) => setServiceText(event.target.value)} />
          </Field>
        </div>
      </div>
      {list.query.isError && events.length === 0 ? (
        <ErrorState error={list.query.error} onRetry={list.reload} compact />
      ) : available === false ? (
        <EmptyState
          compact
          icon={Activity}
          title="Activity is not available"
          description="The event store that records API calls is not connected to the posture service right now, so recent activity cannot be shown."
          action={
            <Button variant="secondary" size="sm" icon={RotateCw} onClick={list.reload}>
              Check again
            </Button>
          }
        />
      ) : (
        <>
          <DataGrid
            caption="Recent activity"
            columns={columns}
            rows={events}
            rowKey={(row, index) => `${row.event_time}-${row.action}-${index}`}
            density="compact"
            loading={list.loading}
            emptyState={<EmptyState compact icon={Activity} title="No activity" description={`No API call matches in the last ${windowPhrase(ctx.window)}.`} />}
          />
          <LoadMore list={list} />
        </>
      )}
    </Panel>
  );
}

function LoadMore({ list }) {
  if (list.query.isError && list.pages.length > 0) {
    return (
      <div className="border-t border-line px-4 py-3">
        <InlineError error={list.query.error} onRetry={list.query.refetch} label="The next page failed to load" />
      </div>
    );
  }
  if (!list.next && !list.loadingMore) return null;
  return (
    <div className="flex justify-center border-t border-line px-4 py-3">
      <Button variant="secondary" size="sm" loading={list.loadingMore} onClick={list.loadMore}>
        Load more
      </Button>
    </div>
  );
}

/* ── Findings ───────────────────────────────────────────────────────────── */

function FindingsView({ ctx }) {
  const [status, setStatus] = useState('open');
  const list = useCursorList(
    (cursor, signal) => fetchIdentityAlerts(ctx.id, { window: ctx.window, status, limit: 50, cursor }, signal),
    [ctx.id, ctx.window, status, ctx.revision],
  );
  const alerts = list.pages.flatMap((page) => page.alerts);

  const columns = [
    {
      key: 'title',
      header: 'Finding',
      primary: true,
      width: '32%',
      cell: (row) => (
        <span className="block min-w-0">
          <span className="block truncate text-[12.5px] font-medium text-ink" title={row.title}>
            {row.title || row.rule_id}
          </span>
          <span className="block truncate text-[11px] text-ink-3">
            {row.source === 'credential' ? 'On a credential it uses' : 'On this identity'}
          </span>
        </span>
      ),
    },
    {
      key: 'severity',
      header: 'Severity',
      width: '11%',
      cell: (row) => (
        <Tag tone={severityMeta(row.severity).tone} size="sm">
          {severityMeta(row.severity).label}
        </Tag>
      ),
    },
    {
      key: 'risk',
      header: 'Risk',
      width: '8%',
      align: 'right',
      cell: (row) => <span data-numeric="">{formatScore(row.risk_score)}</span>,
    },
    { key: 'status', header: 'Status', width: '12%', cell: (row) => <FindingStatusTag status={row.status} /> },
    {
      key: 'raised',
      header: 'Raised',
      width: '13%',
      cell: (row) => (
        <span className="text-[12px]" title={formatDateTime(row.raised_at)}>
          {formatDate(row.raised_at)}
        </span>
      ),
    },
    {
      key: 'resolved',
      header: 'Resolved',
      width: '13%',
      priority: 'wide',
      cell: (row) => (row.resolved_at ? <span className="text-[12px]">{formatDate(row.resolved_at)}</span> : <span className="text-ink-3">-</span>),
    },
  ];

  return (
    <Panel flush className="animate-rise overflow-hidden">
      <div className="flex flex-wrap items-end justify-between gap-3 px-4 pt-4 pb-3 sm:px-5">
        <PanelHeader
          title="Findings"
          subtitle={`On this identity and on the credentials it uses. Open findings are always listed; closed ones from the last ${windowPhrase(ctx.window)}. Accepting or suppressing one takes its points off at the next scan.`}
        />
        <SegmentedControl
          label="Finding status"
          options={[
            { value: 'open', label: 'Open' },
            { value: 'closed', label: 'Closed' },
            { value: 'all', label: 'All' },
          ]}
          value={status}
          onChange={setStatus}
        />
      </div>
      {list.query.isError && alerts.length === 0 ? (
        <ErrorState error={list.query.error} onRetry={list.reload} compact />
      ) : (
        <>
          <DataGrid
            caption="Findings"
            columns={columns}
            rows={alerts}
            rowKey={(row) => row.finding_id}
            density="compact"
            loading={list.loading}
            rowActions={
              ctx.editable
                ? (row) => {
                    const finding = { finding_id: row.finding_id, title: row.title || row.rule_id, status: row.status, row };
                    const items = findingActions(row.status, (mode) => ctx.act(finding, mode));
                    return items.length ? <Menu items={items} label={`Actions for ${finding.title}`} /> : <span className="block size-8" />;
                  }
                : undefined
            }
            emptyState={
              status === 'open' ? (
                <ClearState compact title="No open findings" description="Nothing is open against this identity or the credentials it uses." />
              ) : (
                <EmptyState compact title="No findings" description={status === 'closed' ? `None closed in the last ${windowPhrase(ctx.window)}.` : `Nothing open, and none closed in the last ${windowPhrase(ctx.window)}.`} />
              )
            }
          />
          <LoadMore list={list} />
        </>
      )}
    </Panel>
  );
}

/**
 * "Why this score" in one plain sentence: what the failed checks cost in
 * total, and the biggest of them. Built from the checks themselves so the
 * numbers always add up to the score shown.
 */
function whyThisScore(name, score, posture, failing) {
  const n = Number(posture.checks_failed) || failing.length;
  const lost = Number.isFinite(Number(score)) ? Math.max(0, 100 - Number(score)) : null;
  const top = [...failing]
    .filter((check) => Number(check.points_if_fixed) > 0)
    .sort((a, b) => Number(b.points_if_fixed) - Number(a.points_if_fixed))
    .slice(0, 3)
    .map((check) => `${check.title} (−${formatScore(check.points_if_fixed)})`);
  if (!n) return posture.why_this_score || '';
  const head = `${name} scores ${formatScore(score)} out of 100 because ${n} failed ${n === 1 ? 'check costs' : 'checks cost'}${lost !== null ? ` ${formatScore(lost)} points` : ' points'}.`;
  if (top.length === 0) return `${head} ${posture.why_this_score || ''}`.trim();
  const biggest = top.length === 1 ? `The biggest is ${top[0]}.` : `The biggest are ${top.slice(0, -1).join(', ')} and ${top.at(-1)}.`;
  return `${head} ${biggest}${n > top.length ? ` All ${n} are listed under Failed checks.` : ''}`;
}
