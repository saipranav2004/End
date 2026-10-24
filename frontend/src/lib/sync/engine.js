import client from '../api/client';
import { fetchDiscoveryScanStatus, startDiscoveryScan } from '../api/discoveryScan';
import { fetchPostureScanJob, startPostureScan } from '../api/posture';

/*
 * One click, four stages, one record of progress.
 *
 *   1. start      ask the discovery scanner to scan this workspace
 *   2. discovery  poll it every 10 s until it has read the AWS accounts
 *   3. posture    ask the NHI backend to have the ISPM rule engine score what
 *                 was found (identities and credentials), poll that job
 *   4. refresh    every open screen reloads its data
 *
 * The run is kept in this tab's module state and in localStorage, so a page
 * change, a reload or a closed-and-reopened tab picks it up where it was (the
 * discovery scan has its own id; so does the scoring job). The scanner allows
 * one scan at a time per workspace and one every 10 minutes: an "already
 * running" answer is followed, a "wait N minutes" answer becomes a countdown.
 */

export const STEPS = ['start', 'discovery', 'posture', 'refresh'];

const DISCOVERY_POLL_MS = 10_000;
const POSTURE_POLL_MS = 4_000;
const MAX_RUN_MS = 30 * 60_000; // a run that has not finished by then is reported, not waited on forever
const COOLDOWN_MS = 10 * 60_000; // the scanner's gap between two scans
const MAX_LOST_POLLS = 5;
const STORE_KEY = 'nhi.sync.v1';

let owner = '';
let state = { run: null, cooldownUntil: 0, lastScanAt: null, notice: null };
let timer = null;
let lostPolls = 0;
const listeners = new Set();
const eventListeners = new Set();
let epoch = 0;
const epochListeners = new Set();

/* ── Store ────────────────────────────────────────────────────────────── */

export const getState = () => state;
export function subscribe(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
/** Called with { type: 'done' | 'failed', run } when a run ends. */
export function onEvent(listener) {
  eventListeners.add(listener);
  return () => eventListeners.delete(listener);
}

/** Bumped when a run ends: every screen's queries reload (see lib/hooks/useQuery). */
export const getEpoch = () => epoch;
export function subscribeEpoch(listener) {
  epochListeners.add(listener);
  return () => epochListeners.delete(listener);
}

function set(patch) {
  state = { ...state, ...patch };
  persist();
  listeners.forEach((listener) => listener());
}

function patchRun(patch) {
  if (!state.run) return;
  set({ run: { ...state.run, ...patch } });
}

function persist() {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify({ owner, run: state.run, cooldownUntil: state.cooldownUntil }));
  } catch {
    /* a per-browser convenience; progress is still shown without it */
  }
}

function emit(type) {
  eventListeners.forEach((listener) => listener({ type, run: state.run }));
}

const isActive = (run) => Boolean(run) && run.stage !== 'done' && run.stage !== 'failed';
export const isRunActive = () => isActive(state.run);

function fail(step, message, extra = {}) {
  clearTimeout(timer);
  patchRun({ stage: 'failed', failedStep: step, error: message, finishedAt: Date.now(), ...extra });
  emit('failed');
}

/* ── Start ────────────────────────────────────────────────────────────── */

/** Begin a run (or follow the one already going). */
export async function start() {
  if (isActive(state.run)) return;
  if (state.cooldownUntil > Date.now()) {
    set({ notice: { kind: 'cooldown', until: state.cooldownUntil } });
    return;
  }
  set({ notice: null, run: newRun() });
  const result = await startDiscoveryScan();
  if (!state.run || state.run.stage !== 'start') return;
  switch (result.kind) {
    case 'started':
      patchRun({ stage: 'discovery', discovery: { ...state.run.discovery, scanId: result.scanId, state: 'running', schema: result.schema } });
      break;
    case 'running':
      // Somebody (or an earlier click) already started this workspace's scan: follow it.
      patchRun({
        stage: 'discovery',
        startedAt: result.startedAt ? Date.parse(result.startedAt) || Date.now() : Date.now(),
        discovery: { ...state.run.discovery, scanId: result.scanId, state: 'running', attached: true, elapsed: result.elapsed },
      });
      break;
    case 'cooldown': {
      const until = Date.now() + result.retryAfterMinutes * 60_000;
      set({ run: null, cooldownUntil: until, notice: { kind: 'cooldown', until } });
      return;
    }
    default:
      fail('start', result.unreachable ? 'The scan service could not be reached. Check that it is running, then try again.' : result.message);
      return;
  }
  schedule(0);
}

function newRun() {
  return {
    id: `${Date.now()}`,
    owner,
    stage: 'start',
    startedAt: Date.now(),
    discovery: { state: 'starting', scanId: '', attached: false },
    posture: { state: 'waiting', jobId: '' },
  };
}

/* ── Driver ───────────────────────────────────────────────────────────── */

function schedule(delay) {
  clearTimeout(timer);
  timer = setTimeout(tick, delay);
}

async function tick() {
  const run = state.run;
  if (!isActive(run)) return;
  if (Date.now() - run.startedAt > MAX_RUN_MS) {
    fail(run.stage === 'posture' ? 'posture' : 'discovery', 'This is taking longer than expected. The scan may still be running; check again in a few minutes.', { timedOut: true });
    return;
  }
  if (run.stage === 'discovery') await pollDiscovery(run);
  else if (run.stage === 'posture') await pollPosture(run);
}

async function pollDiscovery(run) {
  let result;
  try {
    result = await fetchDiscoveryScanStatus(run.discovery.scanId);
    lostPolls = 0;
  } catch {
    lostPolls += 1;
    if (lostPolls >= MAX_LOST_POLLS) {
      fail('discovery', 'We lost contact with the scan service. The scan may still be running; try again to pick it up.', { resumable: true });
    } else schedule(DISCOVERY_POLL_MS);
    return;
  }
  if (state.run?.id !== run.id) return;
  if (result.state === 'completed') {
    const scanEndMs = Date.parse(result.scanEnd) || Date.now();
    set({ cooldownUntil: Math.max(state.cooldownUntil, scanEndMs + COOLDOWN_MS), lastScanAt: result.scanEnd ?? new Date().toISOString() });
    patchRun({
      stage: 'posture',
      discovery: { ...state.run.discovery, state: 'completed', identities: result.identities, credentials: result.credentials, changes: result.changes, scanEnd: result.scanEnd, finishedAt: Date.now() },
    });
    await beginPosture();
  } else if (result.state === 'failed') {
    patchRun({ discovery: { ...state.run.discovery, state: 'failed' } });
    fail('discovery', result.message || 'The scan could not read your AWS accounts. Check the account connections, then try again.');
  } else {
    patchRun({ discovery: { ...state.run.discovery, status: result.status } });
    schedule(DISCOVERY_POLL_MS);
  }
}

async function beginPosture() {
  const run = state.run;
  patchRun({ stage: 'posture', posture: { state: 'starting', jobId: '' } });
  try {
    const { jobId } = await startPostureScan();
    if (state.run?.id !== run.id) return;
    if (!jobId) throw new Error('no job id');
    patchRun({ posture: { state: 'running', jobId } });
    schedule(POSTURE_POLL_MS);
  } catch (error) {
    if (state.run?.id !== run.id) return;
    // Scoring not set up (503) or not allowed for this role (403): the inventory is
    // already fresh, so finish with that and say scoring was skipped.
    if (error?.status === 503 || error?.status === 403) {
      patchRun({ posture: { state: 'skipped', reason: error.status === 403 ? 'Your role cannot start posture scoring.' : 'Posture scoring is not set up on this server.' } });
      finish();
      return;
    }
    fail('posture', 'The posture scoring could not be started. Your inventory is up to date; try scoring again.', { postureRetry: true });
  }
}

async function pollPosture(run) {
  const jobId = run.posture.jobId;
  if (!jobId) return;
  let job;
  try {
    job = await fetchPostureScanJob(jobId);
    lostPolls = 0;
  } catch (error) {
    if (error?.status === 404) {
      fail('posture', 'The scoring job is no longer known to the rule engine (it may have restarted). Try scoring again.', { postureRetry: true });
      return;
    }
    lostPolls += 1;
    if (lostPolls >= MAX_LOST_POLLS) fail('posture', 'We lost contact with the scoring service. Try scoring again.', { postureRetry: true });
    else schedule(POSTURE_POLL_MS);
    return;
  }
  if (state.run?.id !== run.id) return;
  // Undefined when the worker reports no such count, so the panel shows nothing rather than a false 0.
  const sum = (key) => {
    const seen = job.tenants.filter((tenant) => tenant?.[key] != null && Number.isFinite(Number(tenant[key])));
    return seen.length ? seen.reduce((total, tenant) => total + Number(tenant[key]), 0) : undefined;
  };
  if (job.status === 'COMPLETED' || job.status === 'PARTIAL') {
    patchRun({ posture: { state: job.status === 'PARTIAL' ? 'partial' : 'completed', jobId, newFindings: sum('new_findings'), resolvedFindings: sum('resolved_findings'), openFindings: sum('open_findings'), finishedAt: Date.now() } });
    finish();
  } else if (job.status === 'FAILED' || job.status === 'SKIPPED') {
    const tenantError = job.tenants.find((tenant) => tenant?.error)?.error;
    fail('posture', job.status === 'SKIPPED' ? 'Scoring was skipped because another scoring run was already going. Try again in a minute.' : tenantError || 'The rule engine could not score this run.', { postureRetry: true });
  } else {
    patchRun({ posture: { ...state.run.posture, state: 'running', status: job.status } });
    schedule(POSTURE_POLL_MS);
  }
}

function finish() {
  clearTimeout(timer);
  patchRun({ stage: 'done', finishedAt: Date.now() });
  epoch += 1;
  epochListeners.forEach((listener) => listener());
  emit('done');
}

/* ── Recovery and housekeeping ────────────────────────────────────────── */

/** Score again after a failed scoring step; the inventory step is not repeated. */
export async function retryPosture() {
  const run = state.run;
  if (!run || run.stage !== 'failed' || run.discovery.state !== 'completed') return;
  patchRun({ stage: 'posture', error: null, failedStep: null, postureRetry: false, finishedAt: null, startedAt: Date.now() });
  await beginPosture();
}

/** Pick a stalled run back up (the scan service answered again). */
export function resume() {
  const run = state.run;
  if (!run) return;
  if (run.stage === 'failed' && run.discovery.scanId && run.discovery.state !== 'completed' && run.discovery.state !== 'failed') {
    lostPolls = 0;
    patchRun({ stage: 'discovery', error: null, failedStep: null, finishedAt: null, startedAt: Date.now() });
    schedule(0);
  } else if (isActive(run)) {
    schedule(0);
  }
}

/** Clear a finished or failed run from view. */
export function dismiss() {
  if (isActive(state.run)) return;
  set({ run: null });
}

export function clearNotice() {
  set({ notice: null });
}

/** When the last completed scan finished, from the NHI backend. */
export async function loadLastScan() {
  try {
    const res = await client.get('/api/v1/scans/latest/status');
    const when = res?.data?.data?.scan_time;
    if (when && !state.lastScanAt) set({ lastScanAt: when });
  } catch {
    /* no scan yet, or change tracking is not set up */
  }
}

/** Sign-in: bind to this user and continue a run left behind by a reload. */
export function init(user) {
  if (owner === user) return;
  stop();
  owner = user;
  let saved = null;
  try {
    saved = JSON.parse(localStorage.getItem(STORE_KEY) || 'null');
  } catch {
    saved = null;
  }
  if (saved && saved.owner === owner) {
    state = { ...state, run: saved.run ?? null, cooldownUntil: Number(saved.cooldownUntil) || 0 };
  }
  listeners.forEach((listener) => listener());
  if (isActive(state.run)) {
    if (state.run.stage === 'start') set({ run: null }); // the start request died with the old page
    else resume();
  }
  loadLastScan();
}

/** Sign-out: stop polling and forget this user's run. */
export function stop() {
  clearTimeout(timer);
  owner = '';
  state = { run: null, cooldownUntil: 0, lastScanAt: null, notice: null };
  listeners.forEach((listener) => listener());
}
