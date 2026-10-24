import axios from 'axios';
import { readToken } from './session';

/*
 * The discovery scanner's own scan API, called straight from the console (not
 * through the NHI backend):
 *
 *   POST {BASE}/api/v1/scan/start
 *   GET  {BASE}/api/v1/scan/status?scan_id=...
 *
 * BASE is a same-origin path (/scan-api) that the dev server and nginx forward
 * to the scanner (DISCOVERY_UPSTREAM), so the browser needs no CORS rule and
 * no host name is baked into the bundle. The signed-in user's bearer token is
 * sent along; the scanner decides what it needs.
 *
 * Answers are read by their body, not their HTTP code, because the scanner
 * reports "already running" and "wait before scanning again" as
 * { success: false, ... } with its own status codes:
 *
 *   started   { success: true, data: { scan_id, schema, status: "STARTED" } }
 *   running   { success: false, scan_id, started_at, elapsed, message }
 *   cooldown  { success: false, retry_after_minutes, message }
 */
const BASE = import.meta.env.VITE_DISCOVERY_SCAN_URL || '/scan-api';

const http = axios.create({ baseURL: BASE, timeout: 20000, headers: { Accept: 'application/json' }, validateStatus: () => true });
http.interceptors.request.use((config) => {
  const token = readToken();
  if (token && !config.headers.Authorization) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

const object = (value) => (value && typeof value === 'object' && !Array.isArray(value) ? value : {});

/** The scanner could not be reached at all (network, nginx 502/504, not set up). */
function unreachable(response) {
  const status = response?.status;
  return !response || status === 502 || status === 503 || status === 504 || (status === 404 && !object(response.data).message);
}

/**
 * Start a scan. Resolves to one of
 *   { kind: 'started',  scanId, schema }
 *   { kind: 'running',  scanId, startedAt, elapsed, message }   someone already started one
 *   { kind: 'cooldown', retryAfterMinutes, message }            scanned too recently
 *   { kind: 'error',    message, unreachable }
 */
export async function startDiscoveryScan() {
  let response;
  try {
    response = await http.post('/api/v1/scan/start', {});
    // A scanner that only answers GET on this path says so with 405.
    if (response.status === 405) response = await http.get('/api/v1/scan/start');
  } catch {
    return { kind: 'error', unreachable: true, message: 'The scan service could not be reached.' };
  }
  const body = object(response.data);
  const data = object(body.data);
  if (body.success === true && data.scan_id) return { kind: 'started', scanId: String(data.scan_id), schema: data.schema ? String(data.schema) : '' };
  if (body.success === false && body.scan_id) {
    return { kind: 'running', scanId: String(body.scan_id), startedAt: body.started_at ?? null, elapsed: body.elapsed ?? '', message: body.message ?? '' };
  }
  if (Number.isFinite(Number(body.retry_after_minutes))) {
    return { kind: 'cooldown', retryAfterMinutes: Math.max(1, Math.ceil(Number(body.retry_after_minutes))), message: body.message ?? '' };
  }
  if (unreachable(response)) return { kind: 'error', unreachable: true, message: 'The scan service could not be reached.' };
  return { kind: 'error', unreachable: false, message: body.message || `The scan service answered ${response.status}.` };
}

const FAILED = new Set(['FAILED', 'FAILURE', 'ERROR', 'CANCELLED', 'CANCELED', 'ABORTED', 'TIMEOUT', 'TIMED_OUT']);

/**
 * Where a scan is. Resolves to
 *   { state: 'running' | 'completed' | 'failed', status, identities, credentials, changes, scanEnd, message }
 * or throws when the scanner cannot be reached (the caller retries).
 */
export async function fetchDiscoveryScanStatus(scanId) {
  const response = await http.get('/api/v1/scan/status', { params: { scan_id: scanId } });
  if (unreachable(response)) throw new Error('The scan service could not be reached.');
  const body = object(response.data);
  const d = Object.keys(object(body.data)).length > 0 ? object(body.data) : body;
  const status = String(d.status || '').toUpperCase();
  const num = (value) => (value === null || value === undefined || !Number.isFinite(Number(value)) ? null : Number(value));
  const state = status === 'COMPLETED' ? 'completed' : FAILED.has(status) ? 'failed' : 'running';
  return {
    state,
    status,
    identities: num(d.total_identities),
    credentials: num(d.total_credentials),
    changes: num(d.total_changes),
    scanEnd: d.scan_end ?? null,
    message: typeof d.message === 'string' ? d.message : typeof d.error === 'string' ? d.error : '',
  };
}
