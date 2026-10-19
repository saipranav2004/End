import { useCallback, useState } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { severityMeta } from '../../lib/domain';
import { formatNumber, titleCaseEnum } from '../../lib/format';
import { IconButton } from '../../ui/Button';
import { Select } from '../../ui/Field';
import { Tag } from '../../ui/Tag';
import { cn } from '../../ui/cn';

/* ── Vocabulary ───────────────────────────────────────────────────────────── */

/** Genome source families (the service's monitoring registry) in plain words. */
const SOURCE_TYPES = {
  EC2: 'EC2 instance',
  LAMBDA: 'Lambda function',
  ECS: 'ECS task',
  APP_RUNNER: 'App Runner service',
  BEANSTALK: 'Elastic Beanstalk',
  SSM: 'Systems Manager',
  GLUE: 'Glue job',
  EMR: 'EMR cluster',
  BATCH: 'Batch job',
  DMS: 'DMS replication',
  KINESIS: 'Kinesis',
  DATASYNC: 'DataSync task',
  SAGEMAKER: 'SageMaker',
  BEDROCK: 'Bedrock agent',
  CODEBUILD: 'CodeBuild project',
  CODEPIPELINE: 'CodePipeline',
  CODEDEPLOY: 'CodeDeploy',
  CLOUDFORMATION: 'CloudFormation',
  MWAA: 'Airflow (MWAA)',
  GITHUB_ACTIONS: 'GitHub Actions',
  GITLAB_CI: 'GitLab CI',
  TERRAFORM: 'Terraform run',
  STEP_FUNCTIONS: 'Step Functions',
  EVENTBRIDGE: 'EventBridge Scheduler',
  SYNTHETICS: 'Synthetics canary',
  IOT: 'IoT',
  TRANSFER: 'Transfer Family',
  IAM: 'IAM role',
};

export function sourceTypeLabel(value) {
  if (!value) return 'Unknown source';
  return SOURCE_TYPES[String(value).toUpperCase()] ?? titleCaseEnum(value);
}

/**
 * Baseline state. The service stores `active` once the learning period is
 * complete and `inactive` while it is still learning; no row at all means
 * learning has not started for this identity.
 */
export function baselineMeta(status) {
  const key = String(status ?? '').toLowerCase();
  if (key === 'active') return { key, label: 'Established', tone: 'low' };
  if (key === 'inactive') return { key, label: 'Learning', tone: 'medium' };
  if (!key) return { key: 'none', label: 'No baseline', tone: 'neutral' };
  return { key, label: titleCaseEnum(key), tone: 'neutral' };
}

/** Learning progress as a 0-100 share, or null when either side is unknown. */
export function learningPercent(built, required) {
  const b = Number(built);
  const r = Number(required);
  if (built === null || built === undefined || !Number.isFinite(b) || !Number.isFinite(r) || r <= 0) return null;
  return Math.max(0, Math.min(100, (b / r) * 100));
}

/** Readable name for a fingerprint / anomaly dimension key. */
const DIMENSIONS = {
  api_operation: 'API operation',
  api_service: 'API service',
  region: 'Region',
  read_write: 'Read / write',
  result: 'Result',
  resource_type: 'Resource type',
  resource_pattern: 'Resource pattern',
  source_network_class: 'Source network',
  user_agent_family: 'User agent',
  hour_of_week_utc: 'Hour of week (UTC)',
  timing: 'Timing',
  cluster_name: 'ECS cluster',
  launch_type: 'Launch type',
  task_definition_family: 'Task definition',
  state_machine_name: 'State machine',
  execution_status: 'Execution status',
};

const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

/**
 * A fingerprint value as people read it. `hour_of_week_utc` is an hour index
 * from Monday 00:00 UTC (0-167, the weekday convention the schedule also uses);
 * anything else is shown as recorded.
 */
export function dimensionValueLabel(dimension, value) {
  if (dimension === 'hour_of_week_utc' && /^\d{1,3}$/.test(String(value))) {
    const n = Number(value);
    if (n < 168) return `${WEEKDAYS[Math.floor(n / 24)]} ${String(n % 24).padStart(2, '0')}:00`;
  }
  return value === '' || value === null || value === undefined ? '(empty)' : String(value);
}

export function dimensionLabel(value) {
  if (!value) return 'Unspecified';
  return DIMENSIONS[value] ?? titleCaseEnum(value);
}

/**
 * Anomaly timeline items carry `Dimension: <dimension>; <reason>`. Split it so
 * the reason reads as the headline and the dimension as its category.
 */
export function parseAnomaly(item) {
  const text = String(item?.description ?? '').trim();
  const match = /^Dimension:\s*([^;]*);\s*([\s\S]*)$/.exec(text);
  if (match) return { dimension: match[1].trim() || null, reason: match[2].trim() || null };
  return { dimension: null, reason: text || null };
}

const ANOMALY_STATUSES = {
  open: { label: 'Open', tone: 'high' },
  acknowledged: { label: 'Acknowledged', tone: 'info' },
  expected: { label: 'Expected', tone: 'low' },
  suppressed: { label: 'Suppressed', tone: 'neutral' },
};

export function anomalyStatusMeta(status) {
  const key = String(status ?? '').toLowerCase();
  if (ANOMALY_STATUSES[key]) return ANOMALY_STATUSES[key];
  if (!key) return { label: 'Unknown', tone: 'neutral' };
  return { label: titleCaseEnum(key), tone: 'neutral' };
}

/** Score + severity, or a quiet "not scored" when the identity has no snapshot. */
export function RiskCell({ score, severity, compact = false }) {
  const meta = severityMeta(severity);
  if (score === null || score === undefined) {
    return <span className="text-[12px] text-ink-3">Not scored</span>;
  }
  return (
    <span className="flex min-w-0 items-center gap-2">
      <span data-numeric="" className="w-7 shrink-0 text-[13px] font-semibold text-ink">
        {formatNumber(score)}
      </span>
      {severity && !compact && (
        <Tag tone={meta.tone} size="sm" dot>
          {meta.label}
        </Tag>
      )}
    </span>
  );
}

/** A number the service may not have measured: null reads as "not measured". */
export function Measured({ value, children, empty = 'Not measured yet' }) {
  if (value === null || value === undefined) return <span className="text-ink-3">{empty}</span>;
  return children ?? formatNumber(value);
}

/* ── Cursor paging ────────────────────────────────────────────────────────── */

/**
 * The genome API pages with opaque cursors and no totals. This keeps the
 * cursors already walked so Previous works without re-reading from the start.
 * A change of `key` (the filters the cursors belong to) starts again at page 1
 * in the same render, so a stale cursor is never sent with new filters.
 */
export function useCursorPager(key) {
  const [state, setState] = useState({ key, stack: [null] });
  const stack = state.key === key ? state.stack : [null];
  const next = useCallback(
    (nextCursor) => {
      if (nextCursor) setState((value) => ({ key, stack: [...(value.key === key ? value.stack : [null]), nextCursor] }));
    },
    [key],
  );
  const prev = useCallback(
    () =>
      setState((value) => {
        const current = value.key === key ? value.stack : [null];
        return { key, stack: current.length > 1 ? current.slice(0, -1) : current };
      }),
    [key],
  );
  return { cursor: stack[stack.length - 1], page: stack.length, next, prev };
}

const PAGE_SIZES = [10, 25, 50, 100];

export function CursorPager({ page, shown, pageSize, hasNext, onPrev, onNext, onPageSizeChange, unit = 'records', className }) {
  const from = shown === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = (page - 1) * pageSize + shown;
  return (
    <div
      className={cn(
        'flex flex-wrap items-center justify-between gap-x-4 gap-y-3 border-t border-line px-4 py-3',
        className,
      )}
    >
      <p className="text-[12.5px] text-ink-3" data-numeric="" aria-live="polite">
        <span className="font-medium text-ink-2">
          {formatNumber(from)}-{formatNumber(to)}
        </span>{' '}
        {unit}
        {hasNext ? ', more on the next page' : ''}
      </p>
      <div className="flex items-center gap-3">
        {onPageSizeChange && (
          <label className="flex items-center gap-2 text-[12.5px] text-ink-3">
            <span className="hidden sm:inline">Rows</span>
            <Select
              size="sm"
              value={String(pageSize)}
              onChange={(event) => onPageSizeChange(Number(event.target.value))}
              options={PAGE_SIZES.map((size) => ({ value: String(size), label: String(size) }))}
              className="w-18"
              aria-label="Rows per page"
            />
          </label>
        )}
        <div className="flex items-center gap-1.5">
          <IconButton icon={ChevronLeft} label="Previous page" variant="secondary" disabled={page <= 1} onClick={onPrev} />
          <span className="min-w-[4.5rem] px-1 text-center text-[13px] font-medium text-ink-2" data-numeric="">
            Page {page}
          </span>
          <IconButton icon={ChevronRight} label="Next page" variant="secondary" disabled={!hasNext} onClick={onNext} />
        </div>
      </div>
    </div>
  );
}

/** Label/value row used by the genome panels and the quick view. */
export function Row({ label, children }) {
  return (
    <div className="grid gap-0.5 border-b border-line py-2 last:border-0 @min-[26rem]:grid-cols-[minmax(0,160px)_minmax(0,1fr)] @min-[26rem]:gap-4">
      <dt className="text-[11.5px] text-ink-3">{label}</dt>
      <dd className="min-w-0 text-[12.5px] leading-relaxed break-words text-ink-2">{children}</dd>
    </div>
  );
}
