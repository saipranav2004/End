import { useState } from 'react';
import { BellOff, CheckCircle2, MessageSquarePlus, RotateCcw, UserPlus } from 'lucide-react';
import { useAuth } from '../../app/AuthContext';
import { updateFinding } from '../../lib/api/posture';
import { formatDate } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Field, Input, Textarea } from '../../ui/Field';
import { Modal } from '../../ui/Overlay';
import { Tag } from '../../ui/Tag';
import { useToast } from '../../ui/Toast';

/**
 * Posture findings: the one thing the posture service lets a person change.
 * Accepting or suppressing a finding takes its check's points off the score at
 * the next scan; reopening puts them back. Assigning and notes are records.
 * Nothing here changes AWS.
 */
export const FINDING_STATUS = {
  OPEN: { label: 'Open', tone: 'high' },
  ACCEPTED: { label: 'Accepted', tone: 'info' },
  SUPPRESSED: { label: 'Suppressed', tone: 'neutral' },
  RESOLVED: { label: 'Resolved', tone: 'low' },
};

export function FindingStatusTag({ status, size = 'sm' }) {
  const key = String(status ?? '').toUpperCase();
  const meta = FINDING_STATUS[key] ?? { label: status || 'Unknown', tone: 'neutral' };
  return (
    <Tag tone={meta.tone} size={size}>
      {meta.label}
    </Tag>
  );
}

const MODES = {
  accept: {
    title: 'Accept this risk',
    description: 'The check stops costing points from the next scan. The reason is kept with the finding and its history.',
    icon: CheckCircle2,
    submit: 'Accept risk',
  },
  suppress: {
    title: 'Suppress until a date',
    description: 'The check stops costing points until the date passes, then counts again.',
    icon: BellOff,
    submit: 'Suppress',
  },
  reopen: {
    title: 'Reopen this finding',
    description: 'The acceptance or suppression is withdrawn and the check costs its points again from the next scan.',
    icon: RotateCcw,
    submit: 'Reopen',
  },
  assign: {
    title: 'Assign this finding',
    description: 'Records who is working on it. Leave the field empty to clear the assignment.',
    icon: UserPlus,
    submit: 'Save assignment',
  },
  note: {
    title: 'Add a note',
    description: 'Appended to the finding with your user and the time.',
    icon: MessageSquarePlus,
    submit: 'Add note',
  },
};

/**
 * The actions a finding in this status offers, as Menu items. RESOLVED
 * findings belong to the engine and offer none.
 */
export function findingActions(status, onAction) {
  const key = String(status ?? '').toUpperCase();
  if (key === 'RESOLVED') return [];
  return [
    key !== 'ACCEPTED' && { key: 'accept', label: 'Accept risk…', icon: CheckCircle2, onSelect: () => onAction('accept') },
    key !== 'SUPPRESSED' && { key: 'suppress', label: 'Suppress until…', icon: BellOff, onSelect: () => onAction('suppress') },
    (key === 'ACCEPTED' || key === 'SUPPRESSED') && { key: 'reopen', label: 'Reopen', icon: RotateCcw, onSelect: () => onAction('reopen') },
    { key: 'assign', label: 'Assign…', icon: UserPlus, onSelect: () => onAction('assign') },
    { key: 'note', label: 'Add a note…', icon: MessageSquarePlus, onSelect: () => onAction('note') },
  ].filter(Boolean);
}

function todayInput() {
  const now = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

/**
 * The dialog for one change to one finding. `finding` is
 * { finding_id, title, status, row } where row is whatever list the finding
 * came from (it may carry the version the If-Match needs).
 * `onConflict` reloads the caller's data after a 409 (finding already resolved).
 */
export function FindingDialog({ finding, mode, onClose, onDone, onConflict }) {
  const { user } = useAuth();
  const { notify } = useToast();
  const meta = MODES[mode];
  const [reason, setReason] = useState('');
  const [until, setUntil] = useState('');
  const [assignee, setAssignee] = useState('');
  const [note, setNote] = useState('');
  const [errors, setErrors] = useState({});
  const [failure, setFailure] = useState('');
  const [busy, setBusy] = useState(false);

  if (!meta || !finding) return null;

  const validate = () => {
    const next = {};
    if (mode === 'accept' && !reason.trim()) next.reason = 'Say why this risk is accepted.';
    if (mode === 'accept' && reason.length > 1000) next.reason = 'At most 1,000 characters.';
    if (mode === 'suppress') {
      const end = until ? new Date(`${until}T23:59:59`) : null;
      if (!end || Number.isNaN(end.getTime())) next.until = 'Choose the last day of the suppression.';
      else if (end.getTime() <= Date.now()) next.until = 'The date must be in the future.';
    }
    if (mode === 'assign' && assignee.length > 254) next.assignee = 'At most 254 characters.';
    if (mode === 'note' && !note.trim()) next.note = 'Write the note first.';
    if (note.length > 2000) next.note = 'At most 2,000 characters.';
    setErrors(next);
    return Object.keys(next).length === 0;
  };

  const patch = () => {
    const withNote = note.trim() ? note.trim() : undefined;
    switch (mode) {
      case 'accept':
        return { status: 'ACCEPTED', accepted_reason: reason.trim(), note: withNote };
      case 'suppress':
        return { status: 'SUPPRESSED', suppressed_until: new Date(`${until}T23:59:59`).toISOString(), note: withNote };
      case 'reopen':
        return { status: 'OPEN', note: withNote };
      case 'assign':
        return { assigned_to: assignee.trim() };
      default:
        return { note: note.trim() };
    }
  };

  const submit = async () => {
    setFailure('');
    if (!validate()) return;
    setBusy(true);
    try {
      const seen = finding.row?.updated_at ?? finding.row?.finding_updated_at ?? finding.updated_at ?? null;
      const state = await updateFinding(finding.finding_id, patch(), { ifMatch: seen });
      const status = FINDING_STATUS[state.status]?.label ?? state.status;
      notify({
        variant: 'success',
        title: mode === 'note' ? 'Note added' : mode === 'assign' ? (state.assigned_to ? `Assigned to ${state.assigned_to}` : 'Assignment cleared') : `Finding ${status.toLowerCase()}`,
        description:
          mode === 'accept' || mode === 'suppress' || mode === 'reopen'
            ? `${finding.title}. The score follows at the next scan.${state.suppressed_until ? ` Suppressed until ${formatDate(state.suppressed_until)}.` : ''}`
            : finding.title,
      });
      onDone?.(state);
      onClose();
    } catch (error) {
      setBusy(false);
      if (error?.status === 409 && error.field === 'If-Match') {
        setFailure('Not saved: someone else changed this finding while you were looking at it. The page has been reloaded - check their change, then try again.');
        onConflict?.();
      } else if (error?.status === 409) {
        setFailure('Not saved: the latest scan already resolved this finding, so there is nothing left to change. The page has been reloaded.');
        onConflict?.();
      } else if (error?.status === 403) {
        setFailure('Your role cannot change posture findings. It needs the Analyst role or higher.');
      } else if (error?.status === 429) {
        setFailure(`Too many requests just now. Try again in ${error.retryAfter ?? 'a few'} seconds.`);
      } else if (error?.status === 400 && error.field) {
        const field = { accepted_reason: 'reason', suppressed_until: 'until', assigned_to: 'assignee', note: 'note' }[error.field];
        if (field) setErrors({ [field]: error.message });
        else setFailure(error.message);
      } else {
        setFailure(error?.message || 'The change could not be saved.');
      }
    }
  };

  const showNote = mode !== 'assign';

  return (
    <Modal
      open
      onClose={busy ? () => {} : onClose}
      title={meta.title}
      description={meta.description}
      icon={meta.icon}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" icon={meta.icon} loading={busy} onClick={submit}>
            {meta.submit}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2">
          <span className="min-w-0 flex-1 truncate text-[12.5px] font-medium text-ink" title={finding.title}>
            {finding.title}
          </span>
          {finding.status && <FindingStatusTag status={finding.status} />}
        </div>

        {mode === 'accept' && (
          <Field label="Reason" htmlFor="finding-reason" required error={errors.reason} hint="Who decided, and why the risk is acceptable.">
            <Textarea id="finding-reason" rows={3} value={reason} invalid={Boolean(errors.reason)} onChange={(event) => setReason(event.target.value)} maxLength={1000} />
          </Field>
        )}

        {mode === 'suppress' && (
          <Field label="Suppressed until (end of day)" htmlFor="finding-until" required error={errors.until}>
            <Input id="finding-until" type="date" min={todayInput()} value={until} invalid={Boolean(errors.until)} onChange={(event) => setUntil(event.target.value)} />
          </Field>
        )}

        {mode === 'assign' && (
          <Field
            label="Assignee"
            htmlFor="finding-assignee"
            error={errors.assignee}
            labelSuffix={
              user?.email ? (
                <Button variant="link" size="sm" onClick={() => setAssignee(user.email)}>
                  Assign to me
                </Button>
              ) : null
            }
            hint="An email address or a team name."
          >
            <Input id="finding-assignee" value={assignee} invalid={Boolean(errors.assignee)} onChange={(event) => setAssignee(event.target.value)} maxLength={254} />
          </Field>
        )}

        {showNote && (
          <Field label={mode === 'note' ? 'Note' : 'Note (optional)'} htmlFor="finding-note" required={mode === 'note'} error={errors.note}>
            <Textarea id="finding-note" rows={mode === 'note' ? 4 : 2} value={note} invalid={Boolean(errors.note)} onChange={(event) => setNote(event.target.value)} maxLength={2000} />
          </Field>
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
