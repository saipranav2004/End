import { useAccess } from '../../app/useAccess';
import { useEffect, useState } from 'react';
import { CheckCircle2, ExternalLink, FileWarning, ShieldCheck, XCircle } from 'lucide-react';
import {
  commitSize,
  confidenceMeta,
  findingLink,
  hasCommitContext,
  hasRepoContext,
  missingFieldReason,
  parseCommitAuthoredAt,
  platformMeta,
  recommendedActionMeta,
  repoVisibilityMeta,
  severityMeta,
} from '../../lib/domain';
import {
  formatDateTime,
  formatNumber,
  formatRelative,
  humanizeToken,
  parseAuthor,
  shortBranch,
  shortCommit,
} from '../../lib/format';
import { Drawer, Modal } from '../../ui/Overlay';
import { DetailList, DetailRow, SectionLabel } from '../../ui/Panel';
import { Button } from '../../ui/Button';
import { Field, Input } from '../../ui/Field';
import { Tabs } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { CopyableValue } from '../../ui/Copyable';
import { cn } from '../../ui/cn';
import { findingKey, modelVerdict, verdictMeta } from './scannerState';

/**
 * Finding detail plus its two writes: the analyst's verdict (real secret or
 * false positive) and accepting the finding onto the allowlist. The four
 * identifying fields are sent back verbatim, as the service requires.
 */
/**
 * The four questions, in the order they get asked.
 *
 * The drawer was one column of eight stacked sections, which meant the
 * repository's fork count - the thing that decides whether deleting the commit
 * is enough - sat four scrolls below the value it applies to. Tabs put each
 * question on its own surface:
 *
 *   OVERVIEW           what is it, how certain is the match, how bad is it
 *   LOCATION & COMMIT  where it lives and who put it there
 *   TIMELINE           when it was written, found, and last touched
 *   RAW MATCH          the masked value and the line it sits on
 */
const DRAWER_TABS = [
  { value: 'overview', label: 'Overview' },
  { value: 'location', label: 'Location & commit' },
  { value: 'timeline', label: 'Timeline' },
  { value: 'raw', label: 'Raw match' },
];

export function FindingDrawer({ finding, onClose, onDismiss, dismissing, onDecide, deciding }) {
  const { lock } = useAccess();
  const [reason, setReason] = useState('');
  const [confirming, setConfirming] = useState(false);
  const [tab, setTab] = useState('overview');

  /* A different finding is a different record, so it opens on Overview rather
     than on whichever tab the last one was left on. */
  const recordKey = finding ? findingKey(finding) : '';
  useEffect(() => {
    setTab('overview');
    setConfirming(false);
    setReason('');
  }, [recordKey]);

  if (!finding) return null;

  const risk = severityMeta(finding.risk_tier);
  const action = recommendedActionMeta(finding.recommended_action);
  const platform = platformMeta(finding.platform);
  const link = findingLink(finding);
  const author = parseAuthor(finding.author);
  const confidence = confidenceMeta(finding.confidence);
  const visibility = repoVisibilityMeta(finding.repo_visibility);
  const authoredAt = parseCommitAuthoredAt(finding.commit_authored_at);
  const size = commitSize(finding);
  const committer = finding.committer ? parseAuthor(finding.committer) : null;
  /* Same person in the overwhelming majority of commits; worth a row only when
     they differ, which is the case that tells an operator something. */
  const committerDiffers = Boolean(committer?.name && committer.name !== author.name);

  return (
    <Drawer
      open
      onClose={onClose}
      width="lg"
      eyebrow={humanizeToken(finding.detector)}
      title={finding.file_path || 'Finding'}
      subtitle={
        <span className="flex flex-wrap items-center gap-1.5">
          <Tag tone={risk.tone} size="sm" dot>
            {risk.label} risk
          </Tag>
          <Tag tone={platform.tone} size="sm">
            {platform.label}
          </Tag>
          <Tag tone={action.tone} size="sm">
            {action.label}
          </Tag>
        </span>
      }
      footer={
        <>
          {link && (
            <Button
              as="a"
              href={link.href}
              target="_blank"
              rel="noreferrer noopener"
              variant="secondary"
              iconRight={ExternalLink}
            >
              {link.label}
            </Button>
          )}
          <Button
            variant="primary"
            icon={ShieldCheck}
            locked={lock('exposure.review')}
            onClick={() => setConfirming(true)}
          >
            Mark as safe
          </Button>
        </>
      }
    >
      {/* Tabs sit above the scrolling body so the row stays put while a pane
          scrolls under it. */}
      <div className="shrink-0 border-b border-line px-4 sm:px-5">
        <Tabs tabs={DRAWER_TABS} value={tab} onChange={setTab} size="sm" />
      </div>

      <div className="flex flex-col gap-5 px-4 py-4 sm:px-5">
        {tab === 'overview' && (
          <>
          <VerdictSection finding={finding} onDecide={onDecide} deciding={deciding} />

          <div>
            <SectionLabel>Assessment</SectionLabel>
            <DetailList className="mt-1">
              <DetailRow label="Detector">{humanizeToken(finding.detector)}</DetailRow>
              <DetailRow label="Detector severity">
                {finding.severity ? humanizeToken(finding.severity) : '-'}
              </DetailRow>
              <DetailRow label="Match confidence">
                {confidence ? (
                  <span className="flex flex-wrap items-center gap-1.5">
                    <Tag tone={confidence.tone} size="sm">
                      {confidence.short}
                    </Tag>
                    <span className="text-[11.5px] text-ink-3">
                      How certain the pattern match is, not how bad the secret is.
                    </span>
                  </span>
                ) : (
                  <MissingValue finding={finding} field="confidence" />
                )}
              </DetailRow>
              <DetailRow label="Category">
                {finding.category ? (
                  humanizeToken(finding.category)
                ) : (
                  <MissingValue finding={finding} field="category" />
                )}
              </DetailRow>
              <DetailRow label="Actionable risk">{risk.label}</DetailRow>
              <DetailRow label="Recommended action">{action.label}</DetailRow>
              <DetailRow label="Verification">
                {finding.verification_status === 'UNSUPPORTED'
                  ? 'Not checked - liveness verification is unavailable on this deployment'
                  : humanizeToken(finding.verification_status)}
              </DetailRow>
            </DetailList>
            <p className="mt-2 text-[11.5px] leading-relaxed text-ink-3">
              “Not checked” means the scanner has no permission to test whether the credential still
              works. It is not a statement that the secret is inactive.
            </p>
          </div>


          {hasRepoContext(finding) && (
            <div>
              <SectionLabel>The repository it sits in</SectionLabel>
              <DetailList className="mt-1">
                <DetailRow label="Visibility">
                  {visibility ? (
                    <span className="flex flex-col gap-0.5">
                      <Tag tone={visibility.tone} size="sm" dot={visibility.tone !== 'neutral'}>
                        {visibility.label}
                      </Tag>
                      <span className="text-[11.5px] leading-relaxed text-ink-3">{visibility.note}</span>
                    </span>
                  ) : (
                    <MissingValue finding={finding} field="repo_visibility" />
                  )}
                </DetailRow>
                <DetailRow label="Stars">
                  {Number.isFinite(finding.repo_stars) ? (
                    <span data-numeric="">{formatNumber(finding.repo_stars)}</span>
                  ) : (
                    <MissingValue finding={finding} field="repo_stars" />
                  )}
                </DetailRow>
                <DetailRow label="Forks">
                  {Number.isFinite(finding.repo_forks) ? (
                    <>
                      <span data-numeric="">{formatNumber(finding.repo_forks)}</span>
                      {finding.repo_forks > 0 && (
                        <span className="mt-0.5 block text-[11.5px] leading-relaxed text-ink-3">
                          A fork keeps its own copy of the history, so rotating the secret matters more
                          than deleting the commit.
                        </span>
                      )}
                    </>
                  ) : (
                    <MissingValue finding={finding} field="repo_forks" />
                  )}
                </DetailRow>
                <DetailRow label="Repository last pushed">
                  {finding.repo_pushed_at ? (
                    formatRelative(finding.repo_pushed_at)
                  ) : (
                    <MissingValue finding={finding} field="repo_pushed_at" />
                  )}
                </DetailRow>
              </DetailList>
            </div>
          )}

          </>
        )}

        {tab === 'location' && (
          <>
          <div>
            <SectionLabel>Location</SectionLabel>
            <DetailList className="mt-1">
              <DetailRow label="Repository" mono>
                <CopyableValue value={finding.repository} />
              </DetailRow>
              <DetailRow label="File">
                <CopyableValue value={finding.file_path} />
              </DetailRow>
              <DetailRow label="Line">
                <span data-numeric="">{finding.line_number ?? '-'}</span>
              </DetailRow>
              <DetailRow label="Branch" mono>
                {shortBranch(finding.branch)}
              </DetailRow>
              <DetailRow label="Commit" mono>
                <CopyableValue value={finding.commit_id}>{shortCommit(finding.commit_id)}</CopyableValue>
              </DetailRow>
              <DetailRow label="Platform">{platform.label}</DetailRow>
            </DetailList>
          </div>


          <div>
            <SectionLabel>Attribution</SectionLabel>
            <DetailList className="mt-1">
              <DetailRow label="Author">{author.name}</DetailRow>
              <DetailRow label="Email" mono>
                {author.email || '-'}
              </DetailRow>
              <DetailRow label="Detected">
                {formatRelative(finding.created_at)}
                <span className="ml-1.5 text-ink-3">({formatDateTime(finding.created_at)})</span>
              </DetailRow>
              <DetailRow label="AWS account" mono>
                {finding.client_id || '-'}
              </DetailRow>
            </DetailList>
          </div>


          {hasCommitContext(finding) && (
            <div>
              <SectionLabel>The commit that introduced it</SectionLabel>
              {finding.commit_message && (
                <p className="mt-1.5 rounded-[var(--radius-control)] border border-line bg-surface-2 p-3 text-[12.5px] leading-relaxed whitespace-pre-wrap text-ink-2">
                  {finding.commit_message}
                </p>
              )}
              <DetailList className="mt-1">
                {committerDiffers && (
                  <DetailRow label="Committed by">
                    {committer.name}
                    {committer.email && <span className="ml-1.5 text-ink-3">{committer.email}</span>}
                    <span className="mt-0.5 block text-[11.5px] text-ink-3">
                      Different from the author, which happens on a rebase, a squash or a bot commit.
                    </span>
                  </DetailRow>
                )}
                <DetailRow label="Written">
                  {authoredAt ? (
                    <>
                      {formatRelative(authoredAt)}
                      <span className="ml-1.5 text-ink-3">({formatDateTime(authoredAt)})</span>
                    </>
                  ) : (
                    <MissingValue finding={finding} field="commit_authored_at" />
                  )}
                  <span className="mt-0.5 block text-[11.5px] text-ink-3">
                    When the commit was written, not when the scanner found it.
                  </span>
                </DetailRow>
                <DetailRow label="Files in the commit">
                  {size.files !== null ? (
                    <>
                      <span data-numeric="">{formatNumber(size.files)}</span>
                      {!size.filesAreExact && (
                        <span className="mt-0.5 block text-[11.5px] text-ink-3">
                          CodeCommit reports no diff stats, so this counts the files the scanner read.
                          It can undercount a commit that also touched a binary or oversized file.
                        </span>
                      )}
                    </>
                  ) : (
                    <MissingValue finding={finding} field="files_changed" />
                  )}
                </DetailRow>
                <DetailRow label="Lines changed">
                  {size.hasLineStats ? (
                    <span data-numeric="">
                      {size.additions !== null ? `+${formatNumber(size.additions)}` : '+?'}
                      {' / '}
                      {size.deletions !== null ? `-${formatNumber(size.deletions)}` : '-?'}
                    </span>
                  ) : (
                    <MissingValue finding={finding} field="additions" />
                  )}
                </DetailRow>
              </DetailList>
            </div>
          )}

          </>
        )}

        {tab === 'timeline' && <FindingTimeline finding={finding} authoredAt={authoredAt} />}

        {tab === 'raw' && (
          <>
          <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
            <SectionLabel>Redacted value</SectionLabel>
            <p className="mt-1.5 font-mono text-[13px] break-all text-ink">{finding.redacted || '-'}</p>
            <p className="mt-2 text-[11.5px] leading-relaxed text-ink-3">
              The scanner never returns the real secret - this masked form is the only version
              available through the API.
            </p>
          </div>


          {/* The service's own id for this record.
              On the Raw tab rather than the Overview because it is not part of
              triage - it is what somebody quotes in a support ticket, and the
              first thing they are asked for. A finding recorded before the
              field existed has none, which is why it is guarded. */}
          {finding.finding_id !== undefined && finding.finding_id !== null && (
            <div>
              <SectionLabel>Record id</SectionLabel>
              <p className="mt-1.5 font-mono text-[12px] break-all text-ink-2">{finding.finding_id}</p>
              <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-3">The scanner's reference for this finding, for support requests.</p>
            </div>
          )}

          {/* The line the secret sits on, masked by the scanner the same way the
              value is. Shown next to the value because "what does this code
              actually look like" is the first question a reviewer asks, and the
              answer decides whether it is a fixture or a live credential. */}
          {finding.line_preview && (
            <div>
              <SectionLabel>The line it was found on</SectionLabel>
              <pre className="mt-1.5 overflow-x-auto rounded-[var(--radius-control)] border border-line bg-inset p-3 font-mono text-[12px] leading-relaxed whitespace-pre-wrap text-ink-2">
                {finding.line_preview}
              </pre>
              <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-3">
                Line {finding.line_number ?? '-'} of {finding.file_path || 'the file'}. The secret is
                masked here exactly as it is in the value above.
              </p>
            </div>
          )}

          </>
        )}

        {!link && (
          <p className="flex items-start gap-2 text-[12px] text-ink-3">
            <FileWarning aria-hidden="true" className="mt-px size-3.5 shrink-0" />
            No console link is available for this finding - the scanner had no region on file for the
            tenant when it was recorded.
          </p>
        )}
      </div>

      {confirming && (
        <Modal
          open
          onClose={dismissing ? () => {} : () => setConfirming(false)}
          title="Mark this finding as safe?"
          description="Use this when the secret is a test value, already rotated, or not a real credential. The finding moves to Accepted and stops appearing in Exposed credentials. You can restore it from Accepted at any time."
          icon={ShieldCheck}
          footer={
            <>
              <Button variant="ghost" onClick={() => setConfirming(false)} disabled={dismissing}>
                Cancel
              </Button>
              <Button variant="primary" icon={ShieldCheck} loading={dismissing} onClick={() => onDismiss(finding, reason.trim())}>
                Mark as safe
              </Button>
            </>
          }
        >
          <div className="flex flex-col gap-4">
            <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2.5">
              <p className="truncate font-mono text-[12px] text-ink" title={finding.file_path}>
                {finding.file_path || 'Finding'}
              </p>
              <p className="mt-0.5 text-[12px] text-ink-3">
                {humanizeToken(finding.detector)}
                {finding.repository ? ` · ${finding.repository}` : ''}
                {finding.redacted ? ` · ${finding.redacted}` : ''}
              </p>
            </div>
            {finding.analyst_decision === 'real_secret' && (
              <p className="rounded-[var(--radius-control)] border border-medium/30 bg-medium-soft px-3 py-2 text-[12.5px] leading-relaxed text-ink-2">
                This finding is marked as a real secret. Rotate it at the source before marking it safe - removing
                it from the code does not make a leaked secret safe.
              </p>
            )}
            <Field label="Reason" htmlFor="dismiss-reason" hint="Why it is safe. Shown to whoever reviews the Accepted list later.">
              <Input
                id="dismiss-reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                placeholder="e.g. rotated on 12 Oct, test fixture, example value"
                maxLength={200}
                autoFocus
              />
            </Field>
          </div>
        </Modal>
      )}
    </Drawer>
  );
}

/**
 * Is it a live secret? The model's score and the analyst's recorded verdict,
 * side by side, with the control to record or change the verdict. The verdict
 * is stored against the finding by the API, with who made it and when.
 */
function VerdictSection({ finding, onDecide, deciding }) {
  const { lock } = useAccess();
  const model = modelVerdict(finding);
  const recorded = finding.analyst_decision ? verdictMeta(finding.analyst_decision) : null;
  const canDecide = Boolean(onDecide && finding.finding_id);

  return (
    <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
      <SectionLabel>Verdict</SectionLabel>
      <DetailList className="mt-1">
        <DetailRow label="Detection model">
          {model ? (
            <span className="flex flex-wrap items-center gap-1.5">
              <Tag tone={model.tone} size="sm">
                {model.label}
              </Tag>
              {model.score !== null && (
                <span data-numeric="" className="text-[11.5px] text-ink-3" title={model.version ? `Model ${model.version}` : undefined}>
                  {Math.round(model.score * 100)}% confidence
                </span>
              )}
            </span>
          ) : (
            <span className="text-[12px] text-ink-3">Not scored</span>
          )}
        </DetailRow>
        <DetailRow label="Analyst">
          {recorded ? (
            <span className="flex flex-col gap-0.5">
              <span>
                <Tag tone={recorded.tone} size="sm" dot={recorded.tone !== 'neutral'}>
                  {recorded.label}
                </Tag>
              </span>
              <span className="text-[11.5px] text-ink-3">
                {finding.analyst_decided_by ? `${finding.analyst_decided_by}, ` : ''}
                {finding.analyst_decided_at ? formatRelative(finding.analyst_decided_at) : ''}
              </span>
            </span>
          ) : (
            <span className="text-[12px] text-ink-3">Not reviewed yet</span>
          )}
        </DetailRow>
      </DetailList>
      {canDecide && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            variant={finding.analyst_decision === 'real_secret' ? 'primary' : 'secondary'}
            icon={CheckCircle2}
            locked={lock('exposure.review')}
            disabled={deciding || finding.analyst_decision === 'real_secret'}
            aria-pressed={finding.analyst_decision === 'real_secret'}
            onClick={() => onDecide(finding, 'real_secret')}
          >
            Real secret
          </Button>
          <Button
            size="sm"
            variant={finding.analyst_decision === 'false_positive' ? 'primary' : 'secondary'}
            icon={XCircle}
            locked={lock('exposure.review')}
            disabled={deciding || finding.analyst_decision === 'false_positive'}
            aria-pressed={finding.analyst_decision === 'false_positive'}
            onClick={() => onDecide(finding, 'false_positive')}
          >
            False positive
          </Button>
          <span className="text-[11.5px] text-ink-3">
            {recorded ? 'Changing it replaces the recorded verdict.' : 'Recorded with your name.'}
          </span>
        </div>
      )}
    </div>
  );
}

/**
 * Why a value is absent.
 *
 * A dash is ambiguous: it could mean the platform cannot report this, or that
 * the finding predates the field. Those lead to different conclusions, so each
 * says which it is rather than leaving the reader to guess.
 */
function MissingValue({ finding, field }) {
  const reason = missingFieldReason(finding, field);
  const platform = platformMeta(finding.platform);
  return (
    <span className="text-[12px] text-ink-3">
      {reason === 'not-applicable'
        ? `Not reported by ${platform.label}`
        : 'Not recorded for this finding'}
    </span>
  );
}

/**
 * The finding as a sequence.
 *
 * Three timestamps exist on a finding and they answer different questions:
 * when the secret was written, when the scanner found it, and when the
 * repository was last touched. Read as three rows in a table they are just
 * dates; read in order they say how long the credential has been exposed and
 * whether anybody has been near the code since.
 *
 * Only the events that have a timestamp are drawn - a scanner deployment that
 * does not report commit dates gets a shorter timeline rather than a row of
 * dashes.
 */
function FindingTimeline({ finding, authoredAt }) {
  const events = [
    authoredAt && {
      key: 'authored',
      label: 'Secret committed',
      at: authoredAt,
      detail: finding.commit_id
        ? `In ${shortCommit(finding.commit_id)} on ${shortBranch(finding.branch) || 'the default branch'}.`
        : 'The commit that introduced the value.',
      tone: 'critical',
    },
    finding.created_at && {
      key: 'detected',
      label: 'Detected by the scanner',
      at: finding.created_at,
      detail: `${humanizeToken(finding.detector)} matched this value.`,
      tone: 'high',
    },
    finding.repo_pushed_at && {
      key: 'pushed',
      label: 'Repository last pushed',
      at: finding.repo_pushed_at,
      detail: 'The most recent push to the repository, secret or not.',
      tone: 'neutral',
    },
  ].filter(Boolean);

  /* `authoredAt` is already a Date - `parseCommitAuthoredAt` had to be, because
     CodeCommit passes git's raw "1758454920 +0530" through and that is not a
     parseable date string - while the other two are ISO strings straight from
     the service. Normalising to milliseconds keeps a Date and a string from
     being compared through `Date.parse`, which stringifies a Date and loses
     its sub-second part on the way. */
  const millis = (value) => (value instanceof Date ? value.getTime() : Date.parse(value));

  events.sort((a, b) => millis(a.at) - millis(b.at));

  /* How long it sat there before anybody knew. The single most useful figure
     on this tab, and it is not on any of the others. */
  const exposedDays =
    authoredAt && finding.created_at
      ? Math.max(0, Math.round((millis(finding.created_at) - millis(authoredAt)) / 86_400_000))
      : null;

  if (events.length === 0) {
    return (
      <p className="text-[12.5px] leading-relaxed text-ink-3">
        This deployment reports no timestamps for the commit or the repository, so there is no
        sequence to draw. The detection time is on the Overview tab.
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {exposedDays !== null && (
        <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
          <SectionLabel>Undetected for</SectionLabel>
          <p className="mt-1 text-[20px] leading-none font-semibold text-ink">
            <span data-numeric="">{formatNumber(exposedDays)}</span>{' '}
            <span className="text-[13px] font-normal text-ink-2">
              day{exposedDays === 1 ? '' : 's'}
            </span>
          </p>
          <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-3">
            Between the commit being written and the scanner finding it. Anyone with read access to
            the repository could have read the value for that whole period, which is why rotating it
            matters more than deleting the commit.
          </p>
        </div>
      )}

      <ol className="flex flex-col">
        {events.map((event, index) => (
          <li key={event.key} className="relative flex gap-3">
            <span className="relative flex shrink-0 flex-col items-center">
              <span
                aria-hidden="true"
                className={cn(
                  'mt-1 size-2.5 shrink-0 rounded-full ring-2 ring-surface',
                  event.tone === 'critical'
                    ? 'bg-critical'
                    : event.tone === 'high'
                      ? 'bg-high'
                      : 'bg-line-strong',
                )}
              />
              {index < events.length - 1 && (
                <span aria-hidden="true" className="mt-1 w-px flex-1 bg-line" />
              )}
            </span>
            <span className={cn('min-w-0 flex-1', index < events.length - 1 && 'pb-4')}>
              <span className="block text-[12.5px] font-semibold text-ink">{event.label}</span>
              <span className="mt-0.5 block text-[11.5px] text-ink-2">
                {formatRelative(event.at)}
                <span className="ml-1.5 text-ink-3">({formatDateTime(event.at)})</span>
              </span>
              <span className="mt-0.5 block text-[11.5px] leading-relaxed text-ink-3">
                {event.detail}
              </span>
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}
