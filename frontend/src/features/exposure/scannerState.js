/**
 * A finding's identity.
 *
 * `finding_id` is the service's own unique id and the guide names it as the
 * React list key, so it is used first. The four-field composite behind it is
 * the fallback, and it is not arbitrary: those are exactly the four fields the
 * allowlist endpoints require to identify a finding, which is the service
 * telling us what a finding's identity is. It covers a row recorded before
 * `finding_id` existed - without it those rows all key on `undefined`, React
 * sees one duplicated key for the whole list, reconciliation falls back to
 * index order, and selecting a row after a dismiss opens its neighbour.
 *
 * Allowlist writes still send the four fields (see `toAllowlistPayload`);
 * `finding_id` is a read-side key, not an accepted request parameter.
 */
export function findingKey(finding) {
  if (finding?.finding_id !== undefined && finding?.finding_id !== null) {
    return `id:${finding.finding_id}`;
  }
  return [finding?.client_id, finding?.file_path, finding?.detector, finding?.redacted].join('|');
}

/**
 * What went wrong loading credential exposure data, in terms of what to do
 * about it. The findings are served by the discovery API (admin only); the
 * shared client has already normalised the error, so this only chooses the
 * words. Server text is used only where the client kept it (it drops 5xx
 * internals).
 */
export function describeExposureError(error) {
  const status = error?.status;
  if (error?.code === 'TENANT_NOT_PROVISIONED') {
    return {
      title: 'Your workspace is still being set up',
      message: 'Secrets show up here once the first scan of your code repositories has finished.',
    };
  }
  if (status === 403) {
    return {
      title: 'Exposed secrets are limited to administrators',
      message: 'Your role cannot see secrets found in code. Ask an administrator if you need them.',
    };
  }
  if (status === 404) {
    return {
      title: 'Exposed secrets are not available here',
      message: 'Code scanning is not switched on for this installation. Ask your administrator or contact support.',
    };
  }
  if (status === 429) {
    return {
      title: 'Too many requests',
      message: 'The service is limiting how often this session can ask. Wait a moment and try again.',
    };
  }
  if (error?.code === 'NETWORK' || error?.code === 'ECONNABORTED' || error?.code === 'ETIMEDOUT') {
    return {
      title: 'Cannot reach the service',
      message: error?.message || 'Check your connection and try again.',
    };
  }
  if (status >= 500) {
    return {
      title: 'Exposed secrets could not be loaded',
      message: `${error?.message || 'The service failed while reading them.'} Try again in a moment.`,
    };
  }
  return {
    title: 'Exposed secrets could not be loaded',
    message: error?.message || 'The service returned an unexpected error.',
  };
}

/**
 * Counts by risk tier, platform and repository - the guide's own recipe.
 *
 * `CRITICAL` is counted but the screens do not give it a tile of its own: the
 * service cannot currently emit it, so a tile for it would be a permanent
 * zero. Counted anyway, because the day it starts emitting one the number has
 * to appear somewhere rather than be silently dropped.
 */
export function summariseFindings(findings = []) {
  const byTier = { CRITICAL: 0, HIGH: 0, MEDIUM: 0, LOW: 0 };
  const byPlatform = { github: 0, codecommit: 0 };
  const repositories = new Set();
  const detectors = new Map();

  for (const finding of findings) {
    const tier = String(finding.risk_tier || '').toUpperCase();
    if (tier in byTier) byTier[tier] += 1;

    // A finding recorded before platform tracking existed reports null and is
    // always CodeCommit, so anything that is not explicitly GitHub counts there.
    if (String(finding.platform || '').toLowerCase() === 'github') byPlatform.github += 1;
    else byPlatform.codecommit += 1;

    if (finding.repository) repositories.add(finding.repository);
    if (finding.detector) detectors.set(finding.detector, (detectors.get(finding.detector) || 0) + 1);
  }

  return {
    total: findings.length,
    byTier,
    byPlatform,
    repositoryCount: repositories.size,
    detectors: [...detectors.entries()]
      .map(([key, value]) => ({ key, value }))
      .sort((a, b) => b.value - a.value),
  };
}

/** Groups findings by (repository, commit_id) so one push reads as one push. */
export function groupByPush(findings = []) {
  const groups = new Map();
  for (const finding of findings) {
    const key = `${finding.platform || ''}|${finding.repository || '?'}@${finding.commit_id || '?'}`;
    if (!groups.has(key)) {
      groups.set(key, {
        key,
        repository: finding.repository,
        commitId: finding.commit_id,
        branch: finding.branch,
        author: finding.author,
        createdAt: finding.created_at,
        platform: finding.platform,
        findings: [],
      });
    }
    groups.get(key).findings.push(finding);
  }
  return [...groups.values()];
}

/* ── Deep scan ────────────────────────────────────────────────────────────── */

/**
 * How often to poll `GET /api/v1/credential-exposure/deep-scan/status`.
 *
 * Five seconds, which is the interval the integration guide states the
 * service's own dashboard uses. There is no push notification for a deep
 * scan, so polling is the only mechanism available.
 */
export const DEEP_SCAN_POLL_MS = 5000;

export const DEEP_SCAN_STATES = {
  not_started: {
    label: 'Not started',
    tone: 'neutral',
    description: 'Its full history has never been scanned.',
  },
  running: {
    label: 'Running',
    tone: 'info',
    description: 'Reading every commit, from the first one to the latest.',
  },
  complete: {
    label: 'Finished',
    tone: 'low',
    description: 'The whole history has been read. Any secret it found is on Exposed secrets.',
  },
  failed: {
    label: 'Failed',
    tone: 'critical',
    description: 'The scan stopped before it finished. Starting it again makes a fresh attempt.',
  },
};

export function deepScanStateMeta(value) {
  const key = String(value || 'not_started').toLowerCase();
  return (
    DEEP_SCAN_STATES[key] ?? {
      label: value ? String(value) : 'Unknown',
      tone: 'neutral',
      description: 'The service reported a state this console does not recognise.',
    }
  );
}

/**
 * What went wrong with a deep scan, in words an operator can act on.
 *
 * `error` from the service is the raw exception message. The guide is explicit
 * that it is fine for an operator and not something to put in front of an end
 * user as-is, so it is shown under a heading that says what it is rather than
 * as the page's own error text - and the two commonest causes are named,
 * because the raw string does not say what to do about either.
 */
export function describeDeepScanFailure(status) {
  const raw = String(status?.error || '');
  if (/AccessDenied|UnauthorizedOperation|not authorized/i.test(raw)) {
    return {
      cause: 'The scanner is not allowed to read this repository.',
      fix: 'Check the read permissions given to the scanner role on the Integrations page, then start the scan again.',
      raw,
    };
  }
  if (/installation|revoked|404|Not Found|credentials/i.test(raw)) {
    return {
      cause: 'GitHub access for this repository looks revoked or removed.',
      fix: 'Give the scanner access to the GitHub organisation again, then start the scan again.',
      raw,
    };
  }
  return {
    cause: 'The scan stopped before it finished.',
    fix: 'Starting it again makes a fresh attempt and clears this error.',
    raw,
  };
}

/**
 * What a request to start a deep scan failed on:
 *   404 CLIENT_NOT_FOUND   this workspace's scanner has never seen the client
 *                          on that platform, so nothing could scan it
 *   400                    the request itself is malformed
 *   403                    not an administrator
 */
export function describeDeepScanRequestError(error) {
  const status = error?.status;
  const message = error?.message || '';
  if (status === 403) {
    return { title: 'Administrators only', message: 'Your role cannot start full-history scans.', retryable: false };
  }
  if (status === 404) {
    return {
      title: 'Client ID not found',
      message: message || 'The scanner has no repositories for this client ID in your workspace. Check the ID and try again.',
      retryable: false,
    };
  }
  if (status === 400) {
    return {
      title: 'This repository cannot be scanned',
      message: message || 'Check the repository name: just the name for CodeCommit, owner/name for GitHub.',
      retryable: false,
    };
  }
  if (error?.code === 'NETWORK') {
    return { title: 'Cannot reach the service', message, retryable: true };
  }
  return { title: 'Could not start the scan', message: message || 'The service returned an unexpected error. Try again.', retryable: true };

}

/* ── Verdicts ─────────────────────────────────────────────────────────────── */

/**
 * The analyst's verdict on a finding (POST decisions), and the detection
 * model's. They answer the same question - is this a live secret - from two
 * sources, so they share wording; the model's is a score, the analyst's is
 * the record.
 */
export const VERDICTS = {
  real_secret: { key: 'real_secret', label: 'Real secret', short: 'Real', tone: 'critical' },
  false_positive: { key: 'false_positive', label: 'False positive', short: 'False positive', tone: 'neutral' },
  unreviewed: { key: 'unreviewed', label: 'Not reviewed', short: 'Not reviewed', tone: 'neutral' },
};

export function verdictMeta(value) {
  const key = String(value || '').toLowerCase();
  return VERDICTS[key] ?? VERDICTS.unreviewed;
}

/** The model's call: TRUE_POSITIVE / FALSE_POSITIVE plus a 0-1 score. */
export function modelVerdict(finding) {
  const decision = String(finding?.ml_decision || '').toUpperCase();
  const score = Number.isFinite(finding?.ml_score) ? finding.ml_score : null;
  if (!decision && score === null) return null;
  /* The service has sent both spellings for "real". */
  const real = decision === 'TRUE_POSITIVE' || decision === 'REAL_SECRET';
  const flagged = decision === 'FALSE_POSITIVE';
  return {
    label: real ? 'Likely a real secret' : flagged ? 'Likely a false alarm' : 'No opinion',
    tone: real ? 'high' : 'neutral',
    score,
    version: finding?.ml_model_version || null,
  };
}

/* ── The five lists ───────────────────────────────────────────────────────── */

/**
 * Every finding that is not marked safe sits in exactly one list. `live` is
 * the one to act on; the others are what the scanner set aside, and each says
 * why so nobody has to guess.
 */
export const LISTS = {
  live: {
    key: 'live',
    label: 'Exposed secrets',
    title: 'Exposed secrets',
    unit: 'exposed secrets',
    countKey: 'main',
    lede: 'Passwords, keys and tokens found in the code of your connected repositories. Values are masked. To fix one, replace (rotate) the secret where it was issued, then remove it from the code.',
    caption: 'Not counting secrets marked safe or set aside in the other lists',
    emptyTitle: 'No secrets exposed in code',
    emptyText: 'The scanner found nothing open in your connected repositories. Secrets someone marked as safe are listed under Marked safe.',
  },
  review: {
    key: 'review',
    label: 'Needs review',
    title: 'Needs review',
    unit: 'to review',
    countKey: 'review',
    lede: 'Our detection models are not sure about these. Open each one and say whether it is a real secret: your answer also teaches the models.',
    caption: 'The models are unsure, so a person should decide',
    emptyTitle: 'Nothing needs review',
    emptyText: 'The detection models are confident about everything they scored.',
  },
  repeated: {
    key: 'repeated',
    label: 'Same value repeated',
    title: 'Same value repeated',
    unit: 'repeated copies',
    countKey: 'repeated',
    lede: 'The same value appears in three or more places in one commit, which usually means a test or sample value was copied around. They are grouped by how many times the value repeats; check one and you have checked them all.',
    caption: 'Copies of one value in the same commit',
    emptyTitle: 'No repeated values',
    emptyText: 'No value was found in three or more places within the same commit.',
  },
  noise: {
    key: 'noise',
    label: 'Known harmless',
    title: 'Known harmless values',
    unit: 'harmless values',
    countKey: 'noise',
    lede: 'Values that look like secrets but match something harmless: keys published in AWS documentation, checksums, password hashes, resource names or plain links. Each row says what it was recognised as.',
    caption: 'Recognised as harmless by the scanner',
    emptyTitle: 'No known harmless values',
    emptyText: 'The scanner did not set aside anything as a recognised harmless value.',
  },
  flagged: {
    key: 'flagged',
    label: 'Likely false alarms',
    title: 'Likely false alarms',
    unit: 'likely false alarms',
    countKey: 'ml_flagged',
    lede: 'Matches our detection models think are not real secrets (false positives). They are kept off the main list. Open one to agree, or record it as a real secret.',
    caption: 'Our detection models think these are not real secrets',
    emptyTitle: 'No likely false alarms',
    emptyText: 'Our detection models have not marked anything as a false alarm, so everything the scanner found is on the main list.',
  },
};

export const LIST_ORDER = ['live', 'review', 'repeated', 'noise', 'flagged'];

export function listFromParam(value) {
  return Object.hasOwn(LISTS, value) && value !== 'live' ? value : 'live';
}

const NOISE_LABELS = {
  known_example_value: 'Published example key',
  integrity_hash: 'Integrity checksum',
  arn_identifier: 'AWS resource name (ARN)',
  password_hash: 'Password hash, not the password',
  plain_url: 'Plain web link',
  expired_signed_url: 'Expired signed link',
  vector_image_file: 'Vector image file',
};

/** "known_example_value,plain_url" -> readable labels. Unknown tags stay readable. */
export function noiseLabels(flags) {
  return String(flags || '')
    .split(',')
    .map((tag) => tag.trim())
    .filter(Boolean)
    .map((tag) => NOISE_LABELS[tag] ?? tag.replaceAll('_', ' '));
}

/** The repeat-count ranges the Repeated list is grouped by. */
export const REPEAT_BUCKETS = [
  { key: '3', label: '3 times', test: (n) => n === 3 },
  { key: '4-5', label: '4 to 5 times', test: (n) => n >= 4 && n <= 5 },
  { key: '6-10', label: '6 to 10 times', test: (n) => n >= 6 && n <= 10 },
  { key: '11+', label: 'More than 10 times', test: (n) => n > 10 },
];

export function repeatBucket(count) {
  return REPEAT_BUCKETS.find((bucket) => bucket.test(count))?.key ?? '3';
}

/**
 * One group per (platform, repository, commit, value): the same secret value
 * copied into several places. The fingerprint is a one-way hash, so equal
 * fingerprints mean equal values without revealing them. A finding without one
 * (older rows) stays a group of its own.
 */
export function groupRepeated(findings = []) {
  const groups = new Map();
  for (const finding of findings) {
    const key = finding.value_fingerprint
      ? `${finding.platform || ''}|${finding.client_id || ''}|${finding.repository || ''}|${finding.commit_id || ''}|${finding.value_fingerprint}`
      : findingKey(finding);
    if (!groups.has(key)) {
      groups.set(key, {
        key,
        repository: finding.repository,
        platform: finding.platform,
        commitId: finding.commit_id,
        commitMessage: finding.commit_message,
        branch: finding.branch,
        author: finding.author,
        detector: finding.detector,
        redacted: finding.redacted,
        createdAt: finding.created_at,
        findings: [],
      });
    }
    groups.get(key).findings.push(finding);
  }
  return [...groups.values()].map((group) => ({
    ...group,
    count: group.findings.length,
    files: new Set(group.findings.map((finding) => finding.file_path)).size,
    bucket: repeatBucket(group.findings.length),
  }));
}
