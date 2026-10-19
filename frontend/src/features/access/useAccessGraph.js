import { useCallback, useEffect, useMemo, useReducer } from 'react';
import { useAuth } from '../../app/AuthContext';
import {
  getCredentialTab,
  getRelationships,
  loadInventoryIndex,
  recordForArn,
  resolveArn,
  resolveId,
  scopeCacheTo,
} from '../../lib/api/access';
import { useQuery } from '../../lib/hooks/useQuery';
import { buildNeighbourhood, createStore, describeNode, edgesOf, mergeRelationships } from './graphModel';

/**
 * The access graph's data, grown one neighbourhood at a time.
 *
 * One store per signed-in tenant, kept for the session (and dropped after
 * STALE_MS) so that moving between the graph, a node's detail page and back
 * does not refetch what was already read. Every visible node's own
 * neighbourhood is read in the background, a few at a time, so its chevron
 * can say how much is behind it before anybody clicks.
 */

const CONCURRENCY = 4;
const STALE_MS = 5 * 60 * 1000;
/* Default-focus measurement cap: see `pickDefaultFocus`. */
const MEASURE_LIMIT = 24;

let shared = null;

function sharedFor(owner) {
  scopeCacheTo(owner);
  if (!shared || shared.owner !== owner || Date.now() - shared.createdAt > STALE_MS) {
    shared = {
      owner,
      createdAt: Date.now(),
      store: createStore(),
      status: new Map(), // arn -> loading | done | error | none
      errors: new Map(),
      /* Nodes whose CloudTrail edges could not be read (activity_data_available
         false): their "calls" edges may be missing, not absent. */
      activityGaps: new Set(),
      profiles: new Map(), // arn -> { privilegeLevel, attachedPolicies } | null
      profileStatus: new Map(),
      queue: [],
      active: 0,
      listeners: new Set(),
      defaultFocus: null,
    };
  }
  return shared;
}

function notify(state) {
  for (const listener of state.listeners) listener();
}

function schedule(state, task) {
  return new Promise((resolve, reject) => {
    state.queue.push({ task, resolve, reject });
    pump(state);
  });
}

function pump(state) {
  while (state.active < CONCURRENCY && state.queue.length > 0) {
    const { task, resolve, reject } = state.queue.shift();
    state.active += 1;
    task()
      .then(resolve, reject)
      .finally(() => {
        state.active -= 1;
        pump(state);
      });
  }
}

/** The discovery record a neighbourhood is read through: the identity when
    there is one (its graph adds CloudTrail `created_by`), else the credential. */
function pickRecord(entry) {
  if (entry?.identity) return { kind: 'identity', row: entry.identity };
  if (entry?.credential) return { kind: 'credential', row: entry.credential };
  return null;
}

function ensureLoaded(state, arn, known) {
  const current = state.status.get(arn);
  if (current === 'loading' || current === 'done' || current === 'none') return;
  state.status.set(arn, 'loading');
  state.errors.delete(arn);
  notify(state);
  schedule(state, async () => {
    const record = known ?? pickRecord(recordForArn(arn)) ?? pickRecord(await resolveArn(arn));
    if (!record) {
      state.status.set(arn, 'none');
      return;
    }
    const response = await getRelationships(record.kind, record.row.id);
    mergeRelationships(state.store, arn, response);
    if (response?.activity_data_available === false) state.activityGaps.add(arn);
    else state.activityGaps.delete(arn);
    state.status.set(arn, 'done');
  })
    .catch((error) => {
      state.status.set(arn, 'error');
      state.errors.set(arn, error);
    })
    .finally(() => notify(state));
}

/**
 * What discovery says about a credential's privilege. Only the raw tab carries
 * `privilege_level` (4 admin, 3 high, 2 medium, 1 low), so it is read once per
 * credential on screen; only the level and the attached policy names are kept.
 */
function ensureProfile(state, arn) {
  if (state.profileStatus.has(arn)) return;
  const credential = recordForArn(arn)?.credential;
  if (!credential) return;
  state.profileStatus.set(arn, 'loading');
  schedule(state, async () => {
    const raw = await getCredentialTab(credential.id, 'raw');
    const level = Number(raw?.credential_record?.privilege_level);
    const policies = Array.isArray(raw?.attached_policies) ? raw.attached_policies : [];
    state.profiles.set(arn, {
      privilegeLevel: Number.isFinite(level) && level > 0 ? level : null,
      attachedPolicies: policies.map((policy) => policy?.policy_name).filter(Boolean),
    });
    state.profileStatus.set(arn, 'done');
  })
    .catch(() => state.profileStatus.set(arn, 'error'))
    .finally(() => notify(state));
}

/**
 * Where the graph opens when the URL names no focus.
 *
 * The most connected identity among those most likely to matter. Connection
 * count is the only honest measure of "most connected" and it costs one
 * relationships call per identity, so it is measured for at most
 * MEASURE_LIMIT identities, chosen by the risk signals the inventory list
 * already carries: publicly accessible, trusted from another account (the
 * credential with the same ARN has `has_external_trust`), IAM roles and users
 * (the principals other identities assume, so the hubs of a trust graph), then
 * credential count. Of those, the winner has the most distinct neighbours,
 * with each edge that crosses an account counting extra, because that is the
 * path an analyst opens this screen to follow. On an estate of fewer than
 * MEASURE_LIMIT identities every identity is measured. The measured
 * neighbourhoods stay in the store, so this costs nothing later.
 */
async function pickDefaultFocus(state, index) {
  if (state.defaultFocus) return state.defaultFocus;
  const identities = index?.identities?.rows ?? [];
  if (identities.length === 0) return null;
  const signal = (row) => {
    const credential = recordForArn(row.arn)?.credential;
    let score = 0;
    if (row.is_publicly_accessible) score += 3;
    if (credential?.has_external_trust) score += 3;
    if (row.identity_type === 'iam_role' || row.identity_type === 'iam_user') score += 2;
    return score + Math.min(3, row.bound_credentials_count ?? 0);
  };
  const candidates = [...identities].sort((a, b) => signal(b) - signal(a)).slice(0, MEASURE_LIMIT);
  await Promise.all(
    candidates.map((row) =>
      schedule(state, async () => {
        if (state.status.get(row.arn) === 'done') return;
        const response = await getRelationships('identity', row.id);
        mergeRelationships(state.store, row.arn, response);
        state.status.set(row.arn, 'done');
      }).catch(() => {}),
    ),
  );
  let best = null;
  let bestScore = -1;
  for (const row of candidates) {
    const edges = edgesOf(state.store, row.arn);
    const score = new Set(edges.map((edge) => edge.otherArn)).size + 4 * edges.filter((edge) => edge.crossAccount).length;
    if (score > bestScore) {
      best = row;
      bestScore = score;
    }
  }
  state.defaultFocus = best ? { kind: 'identity', row: best } : null;
  notify(state);
  return state.defaultFocus;
}

export function useAccessGraph({ focusId, expanded, revealed }) {
  const { user } = useAuth();
  const owner = `${user?.tenantId ?? ''}|${user?.email ?? ''}`;
  const state = sharedFor(owner);
  const [version, bump] = useReducer((value) => value + 1, 0);

  useEffect(() => {
    state.listeners.add(bump);
    return () => state.listeners.delete(bump);
  }, [state]);

  const index = useQuery(() => loadInventoryIndex(), [owner]);

  const focus = useQuery(
    async (signal) => {
      if (focusId) return resolveId(focusId, signal);
      return pickDefaultFocus(state, index.data);
    },
    [focusId, owner, index.isSuccess],
    { enabled: index.isSuccess, keepPrevious: false },
  );

  const focusRecord = focus.data ?? null;
  const focusArn = focusRecord?.row?.arn ?? '';

  useEffect(() => {
    if (focusRecord) ensureLoaded(state, focusArn, focusRecord);
  }, [state, focusArn, focusRecord]);

  const describe = useCallback(
    (arn) => {
      const base = state.store.nodes.get(arn) ?? { id: arn, arn, kind: 'other', name: arn, subtype: '' };
      const entry = recordForArn(arn);
      return describeNode(base, { ...entry, profile: state.profiles.get(arn) });
    },
    // `version` re-creates this whenever the store learns something.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [state, version],
  );

  const focusStatus = focusArn ? state.status.get(focusArn) : undefined;

  const graph = useMemo(() => {
    if (!focusArn || focusStatus !== 'done') return null;
    return buildNeighbourhood({ store: state.store, focusArn, expanded, revealed, describe });
  }, [state, focusArn, focusStatus, expanded, revealed, describe]);

  /* Read every visible node's own neighbourhood, and its privilege. */
  useEffect(() => {
    if (!graph) return;
    for (const node of graph.nodes) {
      if (node.kind === 'identity' || node.kind === 'credential') ensureLoaded(state, node.id);
      if (node.kind === 'credential' || recordForArn(node.id)?.credential) ensureProfile(state, node.id);
    }
  }, [state, graph]);

  const retryNode = useCallback(
    (arn) => {
      state.status.delete(arn);
      ensureLoaded(state, arn);
    },
    [state],
  );

  /* Re-reads every neighbourhood whose CloudTrail edges were missing. */
  const retryActivity = useCallback(() => {
    for (const arn of [...state.activityGaps]) {
      state.activityGaps.delete(arn);
      state.status.delete(arn);
      ensureLoaded(state, arn);
    }
  }, [state]);

  const focusError = focusArn && focusStatus === 'error' ? state.errors.get(focusArn) : null;
  const error = index.error ?? focus.error ?? focusError ?? null;
  const retry = useCallback(() => {
    if (index.isError) index.refetch();
    else if (focus.isError) focus.refetch();
    else if (focusArn) retryNode(focusArn);
  }, [index, focus, focusArn, retryNode]);

  // `version` makes these change identity whenever the store learns something.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const statusOf = useCallback((arn) => state.status.get(arn), [state, version]);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const errorOf = useCallback((arn) => state.errors.get(arn), [state, version]);

  /* Nothing more is coming for this node: its neighbourhood and privilege
     have both been read (or cannot be). Read live, not from a render. */
  const isSettled = useCallback(
    (arn) => state.status.get(arn) !== 'loading' && state.profileStatus.get(arn) !== 'loading',
    [state],
  );

  /* CloudTrail could not be read for part of what is drawn. */
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const activityGap = useMemo(() => state.activityGaps.size > 0, [state, version]);

  return {
    graph,
    isSettled,
    index: index.data ?? null,
    focusRecord,
    focusArn,
    /* No focus at all: the estate has no identity in scope. */
    empty: index.isSuccess && focus.isSuccess && !focusRecord,
    loading: !error && !graph && !(index.isSuccess && focus.isSuccess && !focusRecord),
    error,
    retry,
    store: state.store,
    statusOf,
    errorOf,
    retryNode,
    activityGap,
    retryActivity,
    describe,
  };
}
