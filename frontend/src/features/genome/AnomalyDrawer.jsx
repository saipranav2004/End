import { useState } from 'react';
import { CheckCircle2, HelpCircle, ShieldAlert } from 'lucide-react';
import { fetchAnomaly, fetchAnomalyEvidence, saveAnomalyFeedback } from '../../lib/api/genome';
import { useMutation, useQuery } from '../../lib/hooks';
import { formatDateTime, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Drawer } from '../../ui/Overlay';
import { DetailList, DetailRow, SectionLabel } from '../../ui/Panel';
import { InlineError } from '../../ui/States';
import { DetailSkeleton } from '../../ui/Skeleton';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';
import { dimensionLabel, sourceTypeLabel } from './meta';

const VERDICTS = [
  { key: 'legitimate', label: 'Expected activity', icon: CheckCircle2, hint: 'It was a planned or normal change.' },
  { key: 'malicious', label: 'Suspicious', icon: ShieldAlert, hint: 'Nobody should have done this.' },
  { key: 'unknown', label: 'Not sure', icon: HelpCircle, hint: 'Needs another look.' },
];

const asText = (value) => {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (Array.isArray(value)) return value.map(asText).join(', ');
  const entries = Object.entries(value);
  return entries.length === 0 ? '' : entries.map(([k, v]) => `${k}: ${asText(v)}`).join('; ');
};

/** One anomaly: what changed, the evidence behind it, and the reviewer's verdict. */
export function AnomalyDrawer({ anomaly, onClose, onReviewed }) {
  const id = anomaly?.id;
  const detail = useQuery((signal) => (id ? fetchAnomaly(id, signal) : Promise.resolve(null)), [id]);
  const evidence = useQuery((signal) => (id ? fetchAnomalyEvidence(id, signal) : Promise.resolve(null)), [id]);
  const { notify } = useToast();
  const [reason, setReason] = useState('');
  const [approve, setApprove] = useState(false);
  const save = useMutation((input) => saveAnomalyFeedback(id, input));

  const submit = async (decision) => {
    const outcome = await save.mutate({ decision, approvedForBaseline: decision === 'legitimate' && approve, reason: reason.trim() });
    if (!outcome.ok) {
      notify({ variant: 'error', title: 'Could not save the verdict', description: outcome.error?.message });
      return;
    }
    notify({ variant: 'success', title: 'Verdict saved', description: 'It is recorded with your name and used to tune the baseline.' });
    evidence.refetch();
    onReviewed?.();
  };

  const e = evidence.data;
  const d = detail.data;
  const feedback = e?.feedback;
  const identity = anomaly?.identity;

  return (
    <Drawer
      open={Boolean(anomaly)}
      onClose={onClose}
      eyebrow={identity ? sourceTypeLabel(identity.source_type) : 'Departure from baseline'}
      title={identity?.display_name || identity?.identity_name || 'Anomaly'}
      subtitle={anomaly?.what_changed}
    >
      {detail.isError && !d ? (
        <div className="px-4 py-4 sm:px-5"><InlineError error={detail.error} onRetry={detail.refetch} /></div>
      ) : !d || !e ? (
        <div className="px-4 py-4 sm:px-5"><DetailSkeleton rows={6} /></div>
      ) : (
        <div className="flex flex-col gap-5 px-4 py-4 sm:px-5">
          <div className="flex flex-wrap items-center gap-2">
            <Tag tone="high" size="sm" dot>{dimensionLabel(e.comparison?.dimension ?? anomaly.dimension)}</Tag>
            <Tag tone={d.state === 'open' ? 'high' : 'neutral'} size="sm">{d.state === 'open' ? 'Open' : String(d.state)}</Tag>
            {e.comparison?.score_0_1 != null && Number.isFinite(Number(e.comparison.score_0_1)) && (
              <Tag tone="neutral" size="sm">Strangeness {Math.round(e.comparison.score_0_1 * 100)}%</Tag>
            )}
            <span className="text-[12px] text-ink-3" title={formatDateTime(d.detail?.detected_at)}>
              Detected {formatRelative(d.detail?.detected_at)}
            </span>
          </div>

          <div>
            <SectionLabel>Normal versus what happened</SectionLabel>
            <DetailList className="mt-1">
              <DetailRow label="Why it was flagged">{e.comparison?.reason || 'Not recorded'}</DetailRow>
              <DetailRow label="Normally">{asText(e.comparison?.baseline_value) || <span className="text-ink-3">Not recorded</span>}</DetailRow>
              <DetailRow label="This time">{asText(e.comparison?.observed_value) || <span className="text-ink-3">Not recorded</span>}</DetailRow>
            </DetailList>
          </div>

          <div>
            <SectionLabel>The call itself</SectionLabel>
            {e.event ? (
              <DetailList className="mt-1">
                <DetailRow label="What it called" mono>{e.event.event_source} · {e.event.event_name}</DetailRow>
                <DetailRow label="When">{formatDateTime(e.event.event_time)}</DetailRow>
                <DetailRow label="Where from" mono>{[e.event.source_ip, e.event.region].filter(Boolean).join(' · ') || 'Not recorded'}</DetailRow>
                <DetailRow label="Touched" mono>{(e.event.resource_arns ?? []).join(', ') || 'No resources recorded'}</DetailRow>
                <DetailRow label="Tool used" mono>{e.event.user_agent || 'Not recorded'}</DetailRow>
              </DetailList>
            ) : (
              <p className="mt-1 text-[12.5px] text-ink-3">The event behind this anomaly could not be read from the activity log just now.</p>
            )}
            {e.risk && (
              <p className="mt-2 text-[12.5px] text-ink-2">
                Call risk: <b className="font-semibold text-ink">{e.risk.severity || 'unrated'}</b>
                {e.risk.score != null && Number.isFinite(Number(e.risk.score)) ? ` (score ${formatNumber(e.risk.score)})` : ''}
              </p>
            )}
          </div>

          <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
            <SectionLabel>Your verdict</SectionLabel>
            {feedback && (
              <p className="mt-1.5 text-[12.5px] text-ink-2">
                Last marked <b className="font-semibold text-ink">{feedback.decision}</b>
                {feedback.reviewer ? ` by ${feedback.reviewer}` : ''}
                {feedback.updated_at ? `, ${formatRelative(feedback.updated_at)}` : ''}.
              </p>
            )}
            <label className="mt-3 block text-[12px] text-ink-3" htmlFor="anomaly-reason">Reason (optional)</label>
            <textarea
              id="anomaly-reason"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              maxLength={500}
              rows={2}
              className="mt-1 w-full rounded-[var(--radius-control)] border border-line bg-surface px-2.5 py-1.5 text-[13px] text-ink"
              placeholder="For example: approved deployment"
            />
            <label className="mt-2 flex items-center gap-2 text-[12.5px] text-ink-2">
              <input type="checkbox" checked={approve} onChange={(event) => setApprove(event.target.checked)} />
              If expected, also learn it as normal behaviour
            </label>
            <div className="mt-3 flex flex-wrap gap-2">
              {VERDICTS.map(({ key, label, icon: Icon, hint }) => (
                <Button key={key} variant="secondary" size="sm" icon={Icon} onClick={() => submit(key)} loading={save.pending} title={hint}>
                  {label}
                </Button>
              ))}
            </div>
          </div>
        </div>
      )}
    </Drawer>
  );
}
