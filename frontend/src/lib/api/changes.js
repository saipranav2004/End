import client from './client';
import { params, unwrapMain } from './http';
import { identityRoute } from '../identityLinks';

/*
 * Scan change tracking - what each discovery scan found had changed since the
 * scan before it.
 *
 *   GET /api/v1/dashboard/overview          `recent_changes` (see overview.js)
 *   GET /api/v1/scans                       recent completed scans
 *   GET /api/v1/scans/{scan_id}/changes     one scan's changes, filtered and paged
 *   GET /api/v1/identities/{id}?tab=changes  one identity's changes (max 50)
 *   GET /api/v1/credentials/{id}?tab=changes one credential's changes (max 50)
 *
 * Every answer is limited to the accounts and regions the user has selected.
 */

const list = (value) => (Array.isArray(value) ? value : []);

/** One change, with its details object guaranteed. */
function shapeChange(row) {
  return {
    id: row.change_id,
    scanId: row.scan_id,
    scanAt: row.scan_at ?? null,
    changeType: String(row.change_type || '').toUpperCase(),
    entityType: String(row.entity_type || '').toLowerCase(),
    arn: row.entity_arn || '',
    name: row.entity_name || '',
    identityId: row.identity_id ?? null,
    credentialId: row.credential_id ?? null,
    accountId: row.account_id || '',
    region: row.region || '',
    // The scanner writes "(1 fields changed)"; read it as English.
    summary: (row.summary || '').replace(/\b1 fields\b/g, '1 field'),
    details: row.details && typeof row.details === 'object' && !Array.isArray(row.details) ? row.details : {},
    detectedAt: row.detected_at ?? null,
    evidence: row.cloudtrail_evidence
      ? {
          callerArn: row.cloudtrail_evidence.caller_arn || '',
          eventName: row.cloudtrail_evidence.event_name || '',
          sourceIp: row.cloudtrail_evidence.source_ip || '',
          eventTime: row.cloudtrail_evidence.event_time ?? null,
        }
      : null,
  };
}

function shapeScan(scan) {
  if (!scan) return null;
  return {
    id: scan.scan_id,
    status: scan.status,
    accountId: scan.account_id ?? null,
    startedAt: scan.started_at ?? null,
    completedAt: scan.completed_at ?? null,
    previousScanId: scan.previous_scan_id ?? null,
    previousScanAt: scan.previous_scan_at ?? null,
    totalChanges: Number(scan.total_changes) || 0,
  };
}

/** The dashboard's `recent_changes`, or null before the first scan. */
export function shapeRecentChanges(rc) {
  if (!rc || !rc.last_scan_id) return null;
  return {
    lastScanId: rc.last_scan_id,
    lastScanAt: rc.last_scan_at ?? null,
    previousScanId: rc.previous_scan_id ?? null,
    previousScanAt: rc.previous_scan_at ?? null,
    total: Number(rc.total_changes) || 0,
    byChangeType: rc.by_change_type ?? {},
    highlights: list(rc.highlights).map((h) => ({
      changeType: String(h.change_type || '').toUpperCase(),
      entityType: String(h.entity_type || '').toLowerCase(),
      count: Number(h.count) || 0,
      label: h.label || '',
    })),
  };
}

/** Newest completed scans, for the scan picker. */
export async function fetchScans({ limit = 20 } = {}, signal) {
  const data = unwrapMain(await client.get('/api/v1/scans', { params: params({ limit }), signal }));
  return list(data?.scans).map(shapeScan);
}

/**
 * One scan's changes. scanId may be "latest". changeType: CREATED | DELETED |
 * MODIFIED | ''; entityType: identity | credential | permission | binding |
 * trust_edge | ''.
 */
export async function fetchScanChanges({ scanId, changeType, entityType, page = 1, limit = 25 }, signal) {
  const data = unwrapMain(
    await client.get(`/api/v1/scans/${encodeURIComponent(scanId)}/changes`, {
      params: params({ change_type: changeType || undefined, entity_type: entityType || undefined, page, limit }),
      signal,
    }),
  );
  const summary = data?.summary ?? {};
  return {
    scan: shapeScan(data?.scan),
    summary: {
      total: Number(summary.total) || 0,
      byChangeType: summary.by_change_type ?? {},
      byEntityType: summary.by_entity_type ?? {},
    },
    changes: list(data?.changes).map(shapeChange),
    pagination: {
      page: Number(data?.pagination?.page) || page,
      limit: Number(data?.pagination?.limit) || limit,
      total: Number(data?.pagination?.total) || 0,
      totalPages: Number(data?.pagination?.total_pages) || 0,
    },
  };
}

/** tab=changes of an identity ('identity') or credential ('credential'). */
export async function fetchEntityChanges(kind, id, signal) {
  const base = kind === 'credential' ? '/api/v1/credentials/' : '/api/v1/identities/';
  const data = unwrapMain(await client.get(base + encodeURIComponent(id), { params: { tab: 'changes' }, signal }));
  return {
    changes: list(data?.changes).map(shapeChange),
    truncated: Boolean(data?.truncated),
    available: data?.change_tracking_available !== false,
  };
}

/**
 * Where a change's entity opens: the identity page, or the credential in the
 * register's drawer (`/credentials?search=&credential=`, the console-wide
 * deep link). Null for an identity or credential the scan deleted - it is no
 * longer in the inventory to open.
 */
export function changeEntityRoute(change) {
  const gone = change.changeType === 'DELETED' && (change.entityType === 'identity' || change.entityType === 'credential');
  if (gone) return null;
  const credential = change.credentialId
    ? `/credentials?search=${encodeURIComponent(change.name || change.arn)}&credential=${encodeURIComponent(change.credentialId)}`
    : null;
  if (change.entityType === 'credential' && credential) return credential;
  if (change.identityId) return identityRoute(change.identityId);
  return credential;
}

export const scanChangesRoute = (scanId, query = {}) => {
  const search = new URLSearchParams(Object.entries(query).filter(([, value]) => value));
  const qs = search.toString();
  return `/scans/${encodeURIComponent(scanId)}/changes${qs ? `?${qs}` : ''}`;
};
