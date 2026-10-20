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
  if (status === 403) {
    return {
      title: 'Exposed credentials are limited to administrators',
      message: 'Your role cannot open credential exposure findings. Ask an administrator if you need them.',
    };
  }
  if (status === 404) {
    return {
      title: 'Credential exposure is not available here',
      message: 'This server does not serve credential exposure findings. It may not be enabled for this deployment.',
    };
  }
  if (status === 429) {
    return {
      title: 'Too many requests',
      message: 'The API is rate limiting this session. Wait a moment and try again.',
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
      title: 'Credential exposure data failed to load',
      message: `${error?.message || 'The service failed while reading findings.'} Try again in a moment.`,
    };
  }
  return {
    title: 'Credential exposure data failed to load',
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
    const key = `${finding.repository || '?'}@${finding.commit_id || '?'}`;
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
    description: 'No deep scan has ever been requested for this repository.',
  },
  running: {
    label: 'Running',
    tone: 'info',
    description: 'Walking every commit from the first one to HEAD.',
  },
  complete: {
    label: 'Complete',
    tone: 'low',
    description: 'The whole history has been read. Any secret it found is in the live set.',
  },
  failed: {
    label: 'Failed',
    tone: 'critical',
    description: 'The walk itself raised. Requesting it again starts a fresh attempt.',
  },
};

export function deepScanStateMeta(value) {
  const key = String(value || 'not_started').toLowerCase();
  return (
    DEEP_SCAN_STATES[key] ?? {
      label: value ? String(value) : 'Unknown',
      tone: 'neutral',
      description: 'The service reported a state this dashboard does not recognise.',
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
      cause: 'The scanner is missing an IAM permission on this repository.',
      fix: 'Re-check the read permissions granted to the scanner role, then request the scan again.',
      raw,
    };
  }
  if (/installation|revoked|404|Not Found|credentials/i.test(raw)) {
    return {
      cause: 'The GitHub installation for this client looks revoked or removed.',
      fix: 'Re-install the app for this organisation, then request the scan again.',
      raw,
    };
  }
  return {
    cause: 'The history walk raised before it finished.',
    fix: 'Requesting it again starts a fresh attempt and clears this error.',
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
    return { title: 'Administrators only', message: 'Your role cannot request deep scans.', retryable: false };
  }
  if (status === 404) {
    return {
      title: 'Unknown client',
      message: message || 'The scanner has no repositories for this client id in your workspace.',
      retryable: false,
    };
  }
  if (status === 400) {
    return {
      title: 'The service refused the request',
      message: message || 'The repository was given in the wrong shape - a bare name for CodeCommit, owner/repo for GitHub.',
      retryable: false,
    };
  }
  if (error?.code === 'NETWORK') {
    return { title: 'Cannot reach the service', message, retryable: true };
  }
  return { title: 'Could not request the deep scan', message: message || 'The service returned an unexpected error.', retryable: true };
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
  const real = decision === 'TRUE_POSITIVE';
  const flagged = decision === 'FALSE_POSITIVE';
  return {
    label: real ? 'Likely a real secret' : flagged ? 'Likely a false positive' : 'No decision',
    tone: real ? 'high' : 'neutral',
    score,
    version: finding?.ml_model_version || null,
  };
}
