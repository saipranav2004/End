import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { CheckCircle2, CircleHelp, ShieldAlert, ShieldCheck, Undo2 } from 'lucide-react';
import { useAccess } from '../../app/useAccess';
import { ANOMALY_VERDICTS, fetchAnomalies, submitAnomalyFeedback } from '../../lib/api/genome';
import { genomeRoute } from '../../lib/identityLinks';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Field, Textarea } from '../../ui/Field';
import { Modal } from '../../ui/Overlay';
import { ListSkeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';
import { cn } from '../../ui/cn';
import { dimensionLabel, dimensionValueLabel, sourceTypeLabel } from './meta';

/*
 * Reviewing anomalies: a reviewer says whether each one is a real anomaly or
 * a false positive. The verdict is stored with who gave it and why; it does
 * not close the anomaly or change the baseline. A false positive can also be
 * offered to the pipeline as normal behaviour for the next baseline update.
 */

/** What the identity did, in words, per behaviour the detector measures. */
const HEADLINES = {
  api_operation: 'Called an API it does not normally call',
  api_service: 'Used an AWS service it does not normally use',
  region: 'Worked in a region it does not normally use',
  resource_pattern: 'Touched a resource it does not normally touch',
  resource_type: 'Touched a kind of resource it does not normally touch',
  source_network_class: 'Called AWS from an unusual network',
  user_agent_family: 'Called AWS with an unusual tool or SDK',
  hour_of_week_utc: 'Was active at an unusual time',
  timing: 'Was active at an unusual time',
  read_write: 'Changed its usual mix of reads and writes',
  result: 'Got unusual results from its calls',
  cluster_name: 'Ran in an unusual ECS cluster',
  launch_type: 'Ran with an unusual launch type',
  task_definition_family: 'Ran an unusual task definition',
  state_machine_name: 'Ran an unusual state machine',
  execution_status: 'Ended with an unusual execution status',
};

/* Categorical values the detector records as tokens, in words. */
const VALUE_WORDS = {
  public_internet: 'Public internet',
  vpc_private: 'Private network (VPC)',
  tor_exit: 'Tor exit node',
  off_hours: 'Outside business hours',
  business_hours: 'Business hours',
};

function valueText(dimension, value) {
  const text = dimensionValueLabel(dimension, value);
  return VALUE_WORDS[text] ?? text;
}

export function anomalyHeadline(dimension) {
  return HEADLINES[dimension] ?? `Behaved outside its baseline (${dimensionLabel(dimension).toLowerCase()})`;
}

/** Colour for the event's risk score; the number itself is always shown. */
function riskTone(score) {
  if (score === null || score === undefined) return 'neutral';
  if (score >= 80) return 'critical';
  if (score >= 60) return 'high';
  if (score >= 30) return 'medium';
  return 'low';
}

/**
 * Anomalies with load-more paging. `params` is { review, identity, search };
 * a change of params starts again from the first page.
 */
export function useAnomalyList(params, pageSize = 6) {
  const key = JSON.stringify(params);
  const [state, setState] = useState({ items: [], counts: null, nextCursor: null, feedbackAvailable: true, status: 'loading', error: null });
  const [reload, setReload] = useState(0);
  const request = useRef(0);

  useEffect(() => {
    const id = ++request.current;
    const controller = new AbortController();
    setState((current) => ({ ...current, status: current.counts ? 'refreshing' : 'loading', error: null }));
    fetchAnomalies({ ...JSON.parse(key), limit: pageSize }, controller.signal)
      .then((page) => {
        if (id !== request.current) return;
        setState({ items: page.items, counts: page.counts, nextCursor: page.nextCursor, feedbackAvailable: page.feedbackAvailable, status: 'ready', error: null });
      })
      .catch((error) => {
        if (id !== request.current || controller.signal.aborted) return;
        setState((current) => ({ ...current, status: 'error', error }));
      });
    return () => controller.abort();
  }, [key, pageSize, reload]);

  const loadMore = useCallback(async () => {
    const cursor = state.nextCursor;
    if (!cursor) return;
    const id = request.current;
    setState((current) => ({ ...current, status: 'more' }));
    try {
      const page = await fetchAnomalies({ ...JSON.parse(key), limit: pageSize, cursor });
      if (id !== request.current) return;
      setState((current) => ({ ...current, items: [...current.items, ...page.items], nextCursor: page.nextCursor, status: 'ready' }));
    } catch (error) {
      if (id !== request.current) return;
      setState((current) => ({ ...current, status: 'ready', moreError: error }));
    }
  }, [key, pageSize, state.nextCursor]);

  /* After a verdict the row is updated in place - it does not jump out of the
     list under the reviewer's cursor, and its verdict can still be changed -
     and the counts move with it. The next load lists it where it now belongs. */
  const applyFeedback = useCallback((alertId, feedback) => {
    setState((current) => {
      const previous = current.items.find((item) => item.id === alertId)?.feedback?.decision;
      const counts = current.counts ? { ...current.counts } : null;
      if (counts) {
        const bucket = { malicious: 'real_anomaly', legitimate: 'false_positive' };
        if (!previous) {
          counts.needs_review = Math.max(0, counts.needs_review - 1);
          counts.reviewed += 1;
        } else if (bucket[previous]) {
          counts[bucket[previous]] = Math.max(0, counts[bucket[previous]] - 1);
        }
        if (bucket[feedback.decision]) counts[bucket[feedback.decision]] += 1;
      }
      return {
        ...current,
        counts,
        items: current.items.map((item) => (item.id === alertId ? { ...item, feedback } : item)),
      };
    });
  }, []);

  return {
    ...state,
    loading: state.status === 'loading',
    refreshing: state.status === 'refreshing',
    loadingMore: state.status === 'more',
    refetch: () => setReload((n) => n + 1),
    loadMore,
    applyFeedback,
  };
}

/**
 * A list of anomalies, each with its verdict or the buttons to give one.
 * `showIdentity` is false on an identity's own page.
 */
export function AnomalyList({ list, showIdentity = true, empty, onReviewed }) {
  const { can } = useAccess();
  const canReview = can('anomalies.review');
  const [dialog, setDialog] = useState(null);

  if (list.status === 'error' && list.items.length === 0) {
    return <ErrorState error={list.error} onRetry={list.refetch} title="Anomalies failed to load" />;
  }
  if (list.loading && list.items.length === 0) return <ListSkeleton rows={4} />;
  if (list.items.length === 0) return empty;

  return (
    <>
      <ul className={cn('divide-y divide-line', list.refreshing && 'opacity-60 transition-opacity')}>
        {list.items.map((item) => (
          <AnomalyRow
            key={item.id}
            item={item}
            showIdentity={showIdentity}
            canReview={canReview && item.can_review && list.feedbackAvailable}
            onReview={(decision) => setDialog({ item, decision })}
          />
        ))}
      </ul>
      {list.nextCursor && (
        <div className="border-t border-line px-4 py-3 text-center">
          <Button variant="secondary" size="sm" loading={list.loadingMore} onClick={list.loadMore}>
            Show more anomalies
          </Button>
        </div>
      )}
      {!list.feedbackAvailable && (
        <p className="border-t border-line px-4 py-3 text-[12px] text-ink-3">
          Reviews cannot be saved yet: the behaviour pipeline has not created its feedback table for this workspace.
        </p>
      )}
      {dialog && (
        <FeedbackDialog
          item={dialog.item}
          decision={dialog.decision}
          onClose={() => setDialog(null)}
          onSaved={(feedback) => {
            list.applyFeedback(dialog.item.id, feedback);
            onReviewed?.();
          }}
        />
      )}
    </>
  );
}

function AnomalyRow({ item, showIdentity, canReview, onReview }) {
  const verdict = item.feedback ? ANOMALY_VERDICTS[item.feedback.decision] ?? ANOMALY_VERDICTS.unknown : null;
  const event = item.event;
  const score = event?.risk_score;
  const observed = item.observed !== null && item.observed !== undefined ? valueText(item.dimension, item.observed) : null;
  const usual = item.baseline !== null && item.baseline !== undefined ? valueText(item.dimension, item.baseline) : null;
  const where = [event?.region, event?.source_ip && `from ${event.source_ip}`].filter(Boolean).join(' · ');

  return (
    <li className="flex flex-col gap-3 px-4 py-3.5 @min-[52rem]:flex-row @min-[52rem]:items-start">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          {showIdentity && (
            <Link
              to={`${genomeRoute(item.identity.source_type, item.identity.identity_name)}?tab=anomalies`}
              className="text-[13px] font-semibold text-ink hover:underline"
              title={item.identity.identity_name}
            >
              {item.identity.display_name || item.identity.identity_name}
            </Link>
          )}
          {showIdentity && (
            <span className="text-[11.5px] text-ink-3">
              {sourceTypeLabel(item.identity.source_type)}
              {item.account_id ? ` · ${item.account_id}` : ''}
            </span>
          )}
          <span className="text-[11.5px] text-ink-3" title={formatDateTime(item.detected_at)}>
            {showIdentity ? '· ' : ''}
            {formatRelative(item.detected_at)}
          </span>
        </div>

        <p className={cn('text-[13px] font-medium text-ink', showIdentity && 'mt-1')}>{anomalyHeadline(item.dimension)}</p>
        <dl className="mt-1.5 grid gap-x-4 gap-y-1 text-[12px] @min-[40rem]:grid-cols-[auto_minmax(0,1fr)]">
          {observed && (
            <>
              <dt className="text-ink-3">Seen</dt>
              <dd className="min-w-0 truncate font-mono text-[11.5px] text-ink" title={observed}>
                {observed}
              </dd>
            </>
          )}
          {usual && (
            <>
              <dt className="text-ink-3">Usually</dt>
              <dd className="min-w-0 truncate font-mono text-[11.5px] text-ink-2" title={usual}>
                {usual}
              </dd>
            </>
          )}
          {item.what_changed && (
            <>
              <dt className="text-ink-3">Detector note</dt>
              <dd className="min-w-0 text-ink-2">{item.what_changed}</dd>
            </>
          )}
          {(event?.event_name || where) && (
            <>
              <dt className="text-ink-3">Call</dt>
              <dd className="min-w-0 truncate text-ink-2">
                {[event?.event_source && event?.event_name ? `${event.event_source.replace('.amazonaws.com', '')}:${event.event_name}` : event?.event_name, where]
                  .filter(Boolean)
                  .join(' · ')}
              </dd>
            </>
          )}
        </dl>
      </div>

      <div className="flex shrink-0 flex-col items-start gap-2 @min-[52rem]:w-[17rem] @min-[52rem]:items-end">
        <div className="flex flex-wrap items-center gap-1.5">
          {score !== null && score !== undefined && (
            <span title="Risk score the detector gave this call, out of 100">
              <Tag tone={riskTone(score)} size="sm">
                Risk {formatNumber(score)}
              </Tag>
            </span>
          )}
          <Tag tone="neutral" size="sm">
            {dimensionLabel(item.dimension)}
          </Tag>
        </div>
        {verdict ? (
          <div className="flex flex-col items-start gap-1 @min-[52rem]:items-end">
            <Tag tone={verdict.tone} size="sm" dot>
              {verdict.label}
            </Tag>
            <span className="text-[11.5px] text-ink-3" title={formatDateTime(item.feedback.updated_at)}>
              {item.feedback.reviewer ? `${item.feedback.reviewer} · ` : ''}
              {formatRelative(item.feedback.updated_at)}
              {item.feedback.approved_for_baseline ? ' · learned as normal' : ''}
            </span>
            {item.feedback.reason && (
              <span className="max-w-[17rem] text-[11.5px] text-ink-2 @min-[52rem]:text-right">“{item.feedback.reason}”</span>
            )}
            {canReview && (
              <Button
                variant="link"
                size="sm"
                icon={Undo2}
                onClick={() => onReview(item.feedback.decision === 'malicious' ? 'legitimate' : 'malicious')}
              >
                Change verdict
              </Button>
            )}
          </div>
        ) : canReview ? (
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" size="sm" icon={ShieldAlert} className="!text-critical" onClick={() => onReview('malicious')}>
              Real anomaly
            </Button>
            <Button variant="secondary" size="sm" icon={ShieldCheck} onClick={() => onReview('legitimate')}>
              False positive
            </Button>
          </div>
        ) : (
          <Tag tone="medium" size="sm">
            Needs review
          </Tag>
        )}
      </div>
    </li>
  );
}

const DIALOG = {
  malicious: {
    title: 'Mark as a real anomaly?',
    description:
      'You are confirming this behaviour was not expected. The anomaly stays open: investigate what the identity did, and rotate its credentials if they may be misused.',
    icon: ShieldAlert,
    tone: 'critical',
    submit: 'Mark as real anomaly',
    reasonHint: 'What makes it suspicious, or what you are doing about it. Optional.',
  },
  legitimate: {
    title: 'Mark as a false positive?',
    description:
      'You are confirming this behaviour was expected, for example a planned change. The verdict is recorded with your name; the anomaly itself is not deleted.',
    icon: ShieldCheck,
    tone: 'brand',
    submit: 'Mark as false positive',
    reasonHint: 'Why it was expected - a deployment, a new feature, a known job. Optional.',
  },
};

function FeedbackDialog({ item, decision, onClose, onSaved }) {
  const { notify } = useToast();
  const meta = DIALOG[decision];
  const [reason, setReason] = useState(item.feedback?.reason ?? '');
  const [learn, setLearn] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState('');

  const submit = async () => {
    setBusy(true);
    setFailure('');
    try {
      const feedback = await submitAnomalyFeedback(item.id, {
        decision,
        approvedForBaseline: decision === 'legitimate' && learn,
        reason,
      });
      notify({
        variant: 'success',
        title: decision === 'malicious' ? 'Marked as a real anomaly' : 'Marked as a false positive',
        description: `${item.identity.display_name || item.identity.identity_name}: ${anomalyHeadline(item.dimension).toLowerCase()}.`,
      });
      onSaved(feedback);
      onClose();
    } catch (error) {
      setBusy(false);
      if (error?.status === 403) setFailure('Your role cannot review anomalies. It needs the Analyst role or higher.');
      else if (error?.status === 404) setFailure('This anomaly is no longer in your scope. Reload the page.');
      else if (error?.status === 409) setFailure('This anomaly has no recorded event, so a verdict cannot be attached to it.');
      else setFailure(error?.message || 'The verdict could not be saved.');
    }
  };

  return (
    <Modal
      open
      onClose={busy ? () => {} : onClose}
      title={meta.title}
      description={meta.description}
      icon={meta.icon}
      tone={meta.tone}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant={decision === 'malicious' ? 'danger' : 'primary'} icon={meta.icon} loading={busy} onClick={submit}>
            {meta.submit}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2.5">
          <p className="truncate text-[12.5px] font-semibold text-ink" title={item.identity.identity_name}>
            {item.identity.display_name || item.identity.identity_name}
          </p>
          <p className="mt-0.5 text-[12.5px] text-ink-2">{anomalyHeadline(item.dimension)}</p>
          {item.what_changed && <p className="mt-0.5 text-[12px] text-ink-3">{item.what_changed}</p>}
          <p className="mt-0.5 text-[11.5px] text-ink-3">Raised {formatDateTime(item.detected_at)}</p>
        </div>

        <Field label="Reason (optional)" htmlFor="anomaly-reason" hint={meta.reasonHint}>
          <Textarea id="anomaly-reason" rows={3} value={reason} onChange={(event) => setReason(event.target.value)} maxLength={1000} />
        </Field>

        {decision === 'legitimate' && (
          <label className="flex cursor-pointer items-start gap-2.5 rounded-[var(--radius-control)] border border-line px-3 py-2.5">
            <input
              type="checkbox"
              checked={learn}
              onChange={(event) => setLearn(event.target.checked)}
              className="mt-0.5 size-4 shrink-0 accent-[var(--t-brand)]"
            />
            <span className="text-[12.5px] leading-relaxed text-ink-2">
              <span className="font-medium text-ink">Learn this as normal behaviour</span>
              <span className="block text-[12px] text-ink-3">
                The next baseline update includes it, so the same behaviour stops raising anomalies. Leave it off for a
                one-off.
              </span>
            </span>
          </label>
        )}

        {failure && (
          <p role="alert" className="rounded-[var(--radius-control)] border border-critical/30 bg-critical-soft px-3 py-2 text-[12.5px] leading-relaxed text-critical">
            {failure}
          </p>
        )}
      </div>
    </Modal>
  );
}

/** The empty states the lists use. */
export function NoAnomalies({ review, total, identityPage = false, learning = false }) {
  if (learning) {
    return (
      <EmptyState
        icon={CircleHelp}
        title="No anomaly can be raised yet"
        description="This identity is still learning its normal behaviour. Anomalies are raised once its baseline is established."
      />
    );
  }
  // "Nothing waiting" only makes sense once something has been raised.
  if (review === 'pending' && total !== 0) {
    return (
      <EmptyState
        icon={CheckCircle2}
        title="Nothing waiting for review"
        description={identityPage ? 'Every anomaly of this identity has a verdict.' : 'Every anomaly in your scope has a verdict. New ones appear here as they are raised.'}
      />
    );
  }
  if (review === 'reviewed' && total !== 0) {
    return <EmptyState icon={CircleHelp} title="No verdicts yet" description="Anomalies marked real or false positive are listed here." />;
  }
  return (
    <EmptyState
      icon={ShieldCheck}
      title="No anomalies"
      description={identityPage ? "Every call this identity made fell inside its own baseline." : 'No identity in your scope has behaved outside its baseline.'}
    />
  );
}
