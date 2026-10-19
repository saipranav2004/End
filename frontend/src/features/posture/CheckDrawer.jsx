import { complianceLabel } from '../../lib/posture';
import { useMemo } from 'react';
import { ArrowRight, Filter, Info, Loader2, Wrench } from 'lucide-react';
import { useAccess } from '../../app/useAccess';
import { fetchCheckDetail, fetchQuickWinIdentities } from '../../lib/api/posture';
import { severityMeta } from '../../lib/domain';
import { formatDate, formatNumber } from '../../lib/format';
import { useQuery } from '../../lib/hooks';
import { Button } from '../../ui/Button';
import { CopyButton } from '../../ui/Copyable';
import { Drawer } from '../../ui/Overlay';
import { SectionLabel } from '../../ui/Panel';
import { ListSkeleton, Skeleton } from '../../ui/Skeleton';
import { InlineError } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { FindingStatusTag, findingActions } from './findings';
import { BandTag, formatScore, pillarLabel, ScoreRing } from './parts';
import { useCursorList } from './useCursorList';
import { useOpenPostureIdentity } from './useOpenIdentity';

/**
 * Copyable artefacts found in the check's own remediation text: fenced
 * blocks, AWS CLI lines and a JSON policy. Nothing is generated - when the
 * text carries none, only the text itself is offered to copy.
 */
function artefactsFrom(text) {
  const source = String(text ?? '');
  const artefacts = [];
  let prose = source;

  const fence = /```([\w-]*)\n?([\s\S]*?)```/g;
  for (const match of source.matchAll(fence)) {
    const content = match[2].trim();
    if (!content) continue;
    const language = match[1] || (content.startsWith('{') ? 'json' : 'shell');
    artefacts.push({ label: language === 'json' ? 'Policy' : 'Commands', language, content });
  }
  prose = prose.replace(fence, '').trim();

  if (artefacts.length === 0) {
    const lines = prose.split('\n');
    const cli = [];
    const rest = [];
    let continuing = false;
    for (const line of lines) {
      const trimmed = line.trim();
      if (/^(\$\s*)?aws\s+\S+/.test(trimmed) || continuing) {
        cli.push(trimmed.replace(/^\$\s*/, ''));
        continuing = trimmed.endsWith('\\');
      } else {
        rest.push(line);
      }
    }
    if (cli.length) {
      artefacts.push({ label: 'AWS CLI', language: 'shell', content: cli.join('\n') });
      prose = rest.join('\n').trim();
    }
    const start = prose.indexOf('{');
    const end = prose.lastIndexOf('}');
    if (start >= 0 && end > start) {
      try {
        const parsed = JSON.parse(prose.slice(start, end + 1));
        artefacts.push({ label: 'Policy', language: 'json', content: JSON.stringify(parsed, null, 2) });
        prose = `${prose.slice(0, start)}${prose.slice(end + 1)}`.trim();
      } catch {
        /* not JSON - it stays in the prose */
      }
    }
  }
  return { prose, artefacts };
}

/**
 * One check, explained: what fails, what the fix is, how to apply it, which
 * frameworks it maps to and how many identities fail it.
 *
 * Opened from an identity (`check` + `identity`) it also shows what fixing it
 * earns that identity and the finding's state, with the finding actions the
 * role allows. Opened from a quick win it lists the identities it applies to.
 *
 * The console does not change AWS: the fix is applied in the account, and the
 * score follows at the next scan. So there is no Apply button here.
 */
export function CheckDrawer({
  ruleId,
  check,
  identity,
  finding,
  onFindingAction,
  quickWin,
  onFilter,
  pillarNames,
  onClose,
}) {
  const { can } = useAccess();
  const detailQuery = useQuery((signal) => fetchCheckDetail(ruleId, signal), [ruleId]);
  const detail = detailQuery.data;
  const title = detail?.title ?? check?.title ?? quickWin?.title ?? ruleId;
  const severity = severityMeta(detail?.severity ?? check?.severity);
  const pillar = detail?.pillar ?? check?.pillar ?? quickWin?.pillar;
  const parsed = useMemo(() => artefactsFrom(detail?.remediation), [detail?.remediation]);
  const artefacts = parsed.artefacts;
  /* The guidance often opens by repeating the fix shown just above it. */
  const prose = useMemo(() => {
    const fix = String(detail?.fix ?? '').trim().replace(/\.$/, '');
    const text = String(parsed.prose ?? '');
    if (!fix || !text.trim().toLowerCase().startsWith(fix.toLowerCase())) return text;
    return text.trim().slice(fix.length).replace(/^[.\s:;-]+/, '');
  }, [detail?.fix, parsed.prose]);
  const compliance = detail?.compliance?.length ? detail.compliance : check?.compliance ?? [];
  const failing = check && String(check.status).toUpperCase() === 'FAIL';
  const points = Number(check?.points_if_fixed) || 0;
  const actions = finding && can('posture.findings') ? findingActions(finding.status, onFindingAction) : [];

  return (
    <Drawer
      open
      onClose={onClose}
      eyebrow={identity ? 'Check' : 'Quick win'}
      title={title}
      subtitle={
        <span className="flex flex-wrap items-center gap-1.5">
          {(detail?.severity || check?.severity) && (
            <Tag tone={severity.tone} size="sm">
              {severity.label}
            </Tag>
          )}
          {pillar && (
            <Tag tone="neutral" size="sm">
              {pillarLabel(pillar, pillarNames)}
            </Tag>
          )}
          {identity && <span className="truncate font-mono text-[12px]">{identity.name}</span>}
        </span>
      }
      width="lg"
      footer={
        <>
          {onFilter && (
            <Button variant="secondary" icon={Filter} onClick={onFilter}>
              List identities failing it
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5 px-4 py-4 sm:px-5">
        {identity && check && (
          <section aria-label="Score impact" className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-4 py-3.5">
            <SectionLabel>On this identity</SectionLabel>
            <div className="mt-2.5 flex flex-wrap items-center gap-4">
              <span className="flex items-center gap-2.5">
                <ScoreRing score={identity.score} band={identity.band} size={52} />
                <span className="text-[11.5px] text-ink-3">
                  Now
                  <span className="block text-[12.5px] font-medium text-ink-2">
                    <BandTag score={identity.score} band={identity.band} />
                  </span>
                </span>
              </span>
              {failing && points > 0 && (
                <>
                  <ArrowRight aria-hidden="true" className="size-4 text-ink-3" />
                  <span className="flex items-center gap-2.5">
                    <ScoreRing score={Math.min(100, Number(identity.score) + points)} size={52} />
                    <span className="text-[11.5px] text-ink-3">
                      After this fix
                      <span className="block text-[12.5px] font-medium text-ink-2">at the next scan</span>
                    </span>
                  </span>
                  <span data-numeric="" className="ml-auto text-[22px] font-extrabold tracking-[-0.02em] text-low">
                    +{formatScore(points)}
                  </span>
                </>
              )}
              {!failing && (
                <span className="text-[12.5px] text-ink-2">This check passes on {identity.name}.</span>
              )}
            </div>
            {check.detail && <p className="mt-3 text-[13px] leading-relaxed text-ink-2">{check.detail}</p>}
            {check.failing_since && failing && (
              <p className="mt-1 text-[11.5px] text-ink-3">Failing since {formatDate(check.failing_since)}</p>
            )}
            {failing && points === 0 && (
              <p className="mt-2 text-[11.5px] leading-relaxed text-ink-3">
                This check no longer costs points: its finding was accepted or suppressed.
              </p>
            )}
          </section>
        )}

        {finding && (
          <section>
            <SectionLabel>Finding</SectionLabel>
            <div className="mt-1.5 flex flex-wrap items-center gap-2">
              <FindingStatusTag status={finding.status ?? 'unknown'} size="md" />
              {!finding.status && (
                <span className="text-[12px] text-ink-3">Its state is not in the last 90 days of findings.</span>
              )}
              {actions.length > 0 && (
                <span className="ml-auto flex flex-wrap gap-1.5">
                  {actions.map((action) => (
                    <Button key={action.key} variant="secondary" size="sm" icon={action.icon} onClick={action.onSelect}>
                      {action.label.replace('…', '')}
                    </Button>
                  ))}
                </span>
              )}
            </div>
            {!can('posture.findings') && (
              <p className="mt-2 text-[11.5px] text-ink-3">Accepting, suppressing and assigning findings needs the Analyst role or higher.</p>
            )}
          </section>
        )}

        {detailQuery.isError ? (
          <InlineError error={detailQuery.error} onRetry={detailQuery.refetch} label="Check detail unavailable" />
        ) : detailQuery.isLoading ? (
          <div className="flex flex-col gap-3" aria-busy="true">
            <Skeleton className="h-4 w-40 rounded" />
            <Skeleton className="h-16 w-full rounded" />
            <Skeleton className="h-24 w-full rounded" />
          </div>
        ) : (
          <>
            <section>
              <SectionLabel>The fix</SectionLabel>
              <p className="mt-1.5 flex items-start gap-2 text-[13.5px] font-medium leading-relaxed text-ink">
                <Wrench aria-hidden="true" className="mt-1 size-4 shrink-0 text-brand" />
                {detail?.fix || 'The posture catalog has no fix text for this check.'}
              </p>
              <dl className="mt-3 grid grid-cols-2 gap-3 @min-[28rem]:grid-cols-3">
                <Fact label="Points lost per identity" value={detail?.penalty_points ? `${formatNumber(detail.penalty_points)} points` : '-'} />
                <Fact label="Identities failing" value={formatNumber(detail?.identities_failing)} />
                {quickWin && <Fact label="Overall score gain" value={`+${formatScore(quickWin.fleet_gain)}`} tone="text-low" />}
              </dl>
            </section>

            <section>
              <div className="flex items-center justify-between gap-2">
                <SectionLabel>How to remediate</SectionLabel>
                {detail?.remediation && <CopyButton value={detail.remediation} label="Copy remediation guidance" size="sm" />}
              </div>
              {prose ? (
                <p className="mt-1.5 text-[13px] leading-relaxed whitespace-pre-line text-ink-2">{prose}</p>
              ) : (
                !artefacts.length && <p className="mt-1.5 text-[12.5px] text-ink-3">No remediation guidance is recorded for this check.</p>
              )}
            </section>

            {artefacts.map((artefact, index) => (
              <section key={`${artefact.label}-${index}`}>
                <div className="flex items-center justify-between gap-2">
                  <SectionLabel>{artefact.label}</SectionLabel>
                  <span className="flex items-center gap-1.5">
                    <Tag tone="neutral" size="sm">
                      {artefact.language === 'json' ? 'IAM policy' : 'AWS CLI'}
                    </Tag>
                    <CopyButton value={artefact.content} label={`Copy ${artefact.label}`} size="sm" />
                  </span>
                </div>
                <pre className="mt-1.5 max-h-72 overflow-auto rounded-[var(--radius-control)] border border-line bg-inset p-3 font-mono text-[11.5px] leading-relaxed whitespace-pre text-ink-2">
                  {artefact.content}
                </pre>
              </section>
            ))}

            {compliance.length > 0 && (
              <section>
                <SectionLabel>Compliance</SectionLabel>
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                  {compliance.map((tag) => (
                    <Tag key={tag} tone="neutral" size="sm">
                      {complianceLabel(tag)}
                    </Tag>
                  ))}
                </div>
              </section>
            )}
          </>
        )}

        {quickWin && <QuickWinIdentities ruleId={ruleId} onNavigate={onClose} />}

        <p className="flex items-start gap-2 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2.5 text-[12px] leading-relaxed text-ink-3">
          <Info aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
          The console does not change your AWS accounts. Apply the fix there; the check and the score update after the next
          scan.
        </p>
      </div>
    </Drawer>
  );
}

function Fact({ label, value, tone }) {
  return (
    <div className="min-w-0">
      <dt className="text-[10.5px] font-semibold tracking-[0.1em] text-ink-3 uppercase">{label}</dt>
      <dd data-numeric="" className={`mt-0.5 truncate text-[14px] font-semibold ${tone ?? 'text-ink'}`}>
        {value}
      </dd>
    </div>
  );
}

/** Endpoint 2: who the quick win applies to, worst first, with keyset "load more". */
function QuickWinIdentities({ ruleId, onNavigate }) {
  const { open, opening } = useOpenPostureIdentity();
  const list = useCursorList((cursor, signal) => fetchQuickWinIdentities(ruleId, { limit: 25, cursor }, signal), [ruleId]);
  const query = list.query;
  const rows = list.pages.flatMap((page) => page.identities);
  const next = list.next;

  return (
    <section>
      <SectionLabel>Identities it applies to</SectionLabel>
      {query.isError && rows.length === 0 ? (
        <div className="mt-2">
          <InlineError error={query.error} onRetry={query.refetch} label="Identities unavailable" />
        </div>
      ) : list.loading ? (
        <div className="mt-2">
          <ListSkeleton rows={4} />
        </div>
      ) : rows.length === 0 ? (
        <p className="mt-1.5 text-[12.5px] text-ink-3">No identity fails this check in the latest scan.</p>
      ) : (
        <>
          <ul className="mt-2 divide-y divide-line rounded-[var(--radius-control)] border border-line">
            {rows.map((row) => (
              <li key={row.identity_id}>
                <button
                  type="button"
                  onClick={async () => {
                    await open({ postureId: row.identity_id, name: row.name, arn: row.arn });
                    onNavigate?.();
                  }}
                  className="flex w-full items-center gap-3 px-3 py-2 text-left transition-colors hover:bg-surface-2"
                >
                  <ScoreRing score={row.score} band={row.band} size={34} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-mono text-[12.5px] font-semibold text-ink">{row.name}</span>
                    <span className="block truncate text-[11px] text-ink-3">
                      {row.account || 'Unknown account'}
                      {row.failing_since ? ` · failing since ${formatDate(row.failing_since)}` : ''}
                    </span>
                  </span>
                  {opening === row.identity_id ? (
                    <Loader2 aria-label="Opening" className="size-4 animate-spin text-ink-3" />
                  ) : (
                    <span data-numeric="" className="shrink-0 text-[12.5px] font-bold text-low" title="Points this identity gains">
                      +{formatScore(row.points_if_fixed)} pts
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
          {next && (
            <Button variant="secondary" size="sm" className="mt-2" loading={list.loadingMore} onClick={list.loadMore}>
              Load more
            </Button>
          )}
          {query.isError && rows.length > 0 && (
            <div className="mt-2">
              <InlineError error={query.error} onRetry={query.refetch} label="More identities failed to load" />
            </div>
          )}
        </>
      )}
    </section>
  );
}
