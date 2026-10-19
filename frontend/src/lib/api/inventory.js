import client from './client';
import { params, unwrapMain } from './http';

/*
 * Inventory (discovery API): identities, credentials and the CloudTrail
 * events attributed to an identity. Envelope { success, message, data }.
 *
 *   GET /api/v1/filters                          facet values with counts
 *   GET /api/v1/identities                       paged list (search, filters, sort)
 *   GET /api/v1/identities/{id}?tab=             overview | permissions | credentials |
 *                                                relationships | activity | raw
 *   GET /api/v1/identities/{id}/events?section=  timeline | detail | credentials | resources |
 *                                                heatmap | sessions | errors | cross_account | ips
 *   GET /api/v1/credentials                      paged list (search, filters, sort)
 *   GET /api/v1/credentials/{id}?tab=            overview | trust | rotation | activity |
 *                                                relationships | raw
 *   GET /api/v1/permission-source                which policy allowed one event
 *
 * Every handler rejects unknown or malformed query parameters, so only values
 * that are set are sent (see `params`).
 */
const V1 = '/api/v1';

function pageOf(data) {
  const pagination = data?.pagination ?? {};
  return {
    rows: Array.isArray(data?.data) ? data.data : [],
    total: Number(pagination.total_count) || 0,
    page: Number(pagination.page) || 1,
    pageSize: Number(pagination.page_size) || 25,
    totalPages: Number(pagination.total_pages) || 0,
    /* false when CloudTrail could not be read: activity-derived fields are
       then unknown, not zero. */
    activityAvailable: data?.activity_data_available !== false,
  };
}

/** Facet values and counts for the user's scope. */
export async function fetchFilters(signal) {
  return unwrapMain(await client.get(`${V1}/filters`, { signal }));
}

/**
 * The owners with the most identities, from the dashboard aggregate - the only
 * place the API lists owner values. [{ owner, count }]
 */
export async function fetchTopOwners(signal) {
  const data = unwrapMain(await client.get(`${V1}/dashboard/overview`, { signal }));
  return Array.isArray(data?.charts?.top_owners) ? data.charts.top_owners : [];
}

/* ── Identities ───────────────────────────────────────────────────────────── */

/**
 * query: { page, pageSize, search, identityType[], functionalClass[], environment[],
 *          status[], owner, vpcId, isPublic, minIdleDays, sortBy, sortOrder }
 */
export async function fetchIdentities(query = {}, signal) {
  const res = await client.get(`${V1}/identities`, {
    signal,
    params: params({
      page: query.page,
      page_size: query.pageSize,
      search: query.search?.trim(),
      identity_type: query.identityType,
      functional_class: query.functionalClass,
      environment: query.environment,
      status: query.status,
      owner: query.owner,
      vpc_id: query.vpcId,
      is_publicly_accessible: query.isPublic,
      min_idle_days: query.minIdleDays,
      sort_by: query.sortBy,
      sort_order: query.sortOrder,
    }),
  });
  return pageOf(unwrapMain(res));
}

/** How many identities match a filter - one row requested, the total read. */
export async function countIdentities(query = {}, signal) {
  const page = await fetchIdentities({ ...query, page: 1, pageSize: 1 }, signal);
  return page.total;
}

/** One tab of an identity's record. `extra`: time_range, page, page_size, depth, edge_type, service, used_only. */
export async function fetchIdentity(id, tab = 'overview', extra = {}, signal) {
  const res = await client.get(`${V1}/identities/${encodeURIComponent(id)}`, {
    signal,
    params: params({ tab, ...extra }),
  });
  return unwrapMain(res);
}

/**
 * One section of the identity's CloudTrail events.
 * query: { timeRange, service, action, result, resource, sourceIp, credential,
 *          readWrite, page, pageSize, eventId }
 */
export async function fetchIdentityEvents(id, section, query = {}, signal) {
  const res = await client.get(`${V1}/identities/${encodeURIComponent(id)}/events`, {
    signal,
    params: params({
      section,
      time_range: query.timeRange,
      service: query.service,
      action: query.action,
      result: query.result && query.result !== 'all' ? query.result : undefined,
      resource: query.resource?.trim(),
      source_ip: query.sourceIp,
      credential: query.credential,
      read_write: query.readWrite && query.readWrite !== 'all' ? query.readWrite : undefined,
      page: query.page,
      page_size: query.pageSize,
      event_id: query.eventId,
    }),
  });
  return unwrapMain(res);
}

/* ── Credentials ──────────────────────────────────────────────────────────── */

/**
 * query: { page, pageSize, search, credentialType[], status[], ownerPrincipalArn,
 *          rotationEnabled, rotationOverdue, isOrphaned, hasExternalTrust,
 *          minIdleDays, maxDaysUntilExpiry, sortBy, sortOrder }
 */
export async function fetchCredentials(query = {}, signal) {
  const res = await client.get(`${V1}/credentials`, {
    signal,
    params: params({
      page: query.page,
      page_size: query.pageSize,
      search: query.search?.trim(),
      credential_type: query.credentialType,
      status: query.status,
      owner_principal_arn: query.ownerPrincipalArn,
      rotation_enabled: query.rotationEnabled,
      rotation_overdue: query.rotationOverdue,
      is_orphaned: query.isOrphaned,
      has_external_trust: query.hasExternalTrust,
      min_idle_days: query.minIdleDays,
      max_days_until_expiry: query.maxDaysUntilExpiry,
      sort_by: query.sortBy,
      sort_order: query.sortOrder,
    }),
  });
  return pageOf(unwrapMain(res));
}

export async function countCredentials(query = {}, signal) {
  const page = await fetchCredentials({ ...query, page: 1, pageSize: 1 }, signal);
  return page.total;
}

/** One tab of a credential's record. */
export async function fetchCredential(id, tab = 'overview', extra = {}, signal) {
  const res = await client.get(`${V1}/credentials/${encodeURIComponent(id)}`, {
    signal,
    params: params({ tab, ...extra }),
  });
  return unwrapMain(res);
}

/** Which granted permission allowed an event: { matched, policy_source, policy_type, effect, resource_arn, matched_action, note }. */
export async function fetchPermissionSource({ credentialId, eventSource, eventName }, signal) {
  const res = await client.get(`${V1}/permission-source`, {
    signal,
    params: params({ credential_id: credentialId, event_source: eventSource, event_name: eventName }),
  });
  return unwrapMain(res);
}
