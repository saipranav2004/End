import client from './client';
import { ApiError, expectField } from './http';

/*
 * Credential exposure (discovery API, admin only). The secret scanner writes
 * its findings into the tenant's schema; the API serves them here, and proxies
 * deep scans to the scanner with its key - the key never reaches the browser.
 *
 *   GET    /api/v1/credential-exposure/findings            ?limit (<=500) &offset | ?finding_id
 *   GET    /api/v1/credential-exposure/findings/flagged    findings the model marked false positive
 *   GET    /api/v1/credential-exposure/findings/summary
 *   POST   /api/v1/credential-exposure/decisions           { finding_id, decision }
 *   GET/POST/DELETE /api/v1/credential-exposure/allowlist  accept / list / restore
 *   POST   /api/v1/credential-exposure/deep-scan           { client_id, repository }
 *   GET    /api/v1/credential-exposure/deep-scan/status    ?client_id &repository [&platform]
 *
 * These answer bare JSON, not the { success, data } envelope.
 */
const CE = '/api/v1/credential-exposure';
const PAGE = 500;
const MAX_FINDINGS = 20000;

/**
 * Every finding. The API pages at most 500 at a time and reports the full
 * `count`, so every page is read: the screens filter, count and group the
 * whole set, and a partial set would undercount. `flagged` returns the ones
 * the model marked as false positives.
 */
export async function fetchFindings(signal, { flagged = false } = {}) {
  const path = flagged ? `${CE}/findings/flagged` : `${CE}/findings`;
  const findings = [];
  let count = 0;
  for (let offset = 0; offset < MAX_FINDINGS; offset += PAGE) {
    const body = expectField(await client.get(path, { params: { limit: PAGE, offset }, signal }), 'findings');
    if (!Array.isArray(body.findings)) {
      throw new ApiError({ message: 'The service answered with an unexpected response.' });
    }
    findings.push(...body.findings);
    count = Number.isFinite(body.count) ? body.count : findings.length;
    if (body.findings.length < PAGE || findings.length >= count) break;
  }
  return { findings, count };
}

/** { total_findings, high_count, medium_count, low_count, affected_repositories } */
export async function fetchFindingsSummary(signal) {
  return expectField(await client.get(`${CE}/findings/summary`, { signal }), 'total_findings');
}

/** One finding (ids may contain slashes). */
export async function fetchFinding(findingId, signal) {
  return expectField(await client.get(`${CE}/findings`, { params: { finding_id: findingId }, signal }), 'finding_id');
}

/** The analyst's verdict; the API records who from the token. */
export async function decideFinding({ findingId, decision }) {
  const res = await client.post(`${CE}/decisions`, { finding_id: findingId, decision });
  return res?.data ?? {};
}

export async function fetchAllowlist(signal) {
  const body = expectField(await client.get(`${CE}/allowlist`, { signal }), 'entries');
  return Array.isArray(body.entries) ? body.entries : [];
}

/** Accept a finding. All four identifying fields are required; the API records who. */
export async function dismissFinding({ clientId, filePath, detector, redacted, reason }) {
  const res = await client.post(`${CE}/allowlist`, {
    client_id: clientId,
    file_path: filePath,
    detector,
    redacted,
    ...(reason ? { reason } : {}),
  });
  return res?.data ?? {};
}

/** Back into the live findings. */
export async function restoreFinding({ clientId, filePath, detector, redacted }) {
  const res = await client.delete(`${CE}/allowlist`, {
    data: { client_id: clientId, file_path: filePath, detector, redacted },
  });
  return res?.data ?? {};
}

/**
 * Start the full commit-by-commit history walk of one repository. 202 means
 * accepted, not finished - poll `fetchDeepScanStatus`. `repository` is a bare
 * name for CodeCommit, `owner/repo` for GitHub.
 */
export async function startDeepScan({ clientId, repository }) {
  const res = await client.post(`${CE}/deep-scan`, { client_id: clientId, repository });
  return res?.data ?? {};
}

/** `not_started` | `running` | `complete` | `failed`, plus counts and times. */
export async function fetchDeepScanStatus({ clientId, repository, platform }, signal) {
  const params = { client_id: clientId, repository };
  if (platform) params.platform = platform;
  const res = await client.get(`${CE}/deep-scan/status`, { params, signal });
  return res?.data ?? {};
}
