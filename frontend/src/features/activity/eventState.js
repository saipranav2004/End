import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

/**
 * Filter state of an identity's CloudTrail event views.
 *
 * On a page it lives in the URL, so a filtered timeline - or one open event -
 * can be bookmarked and shared. Inside a drawer it is local, so browsing an
 * identity's events never rewrites the list page behind it.
 *
 * Each key maps onto one parameter of `GET /api/v1/identities/{id}/events`.
 */
export const EVENT_DEFAULTS = {
  range: '7d',
  service: '',
  action: '',
  result: 'all',
  rw: 'all',
  resource: '',
  ip: '',
  cred: '',
  page: 1,
  size: 25,
  view: 'table',
  section: 'events',
  event: '',
};

const RANGES = new Set(['24h', '7d', '30d', '90d']);
const SIZES = new Set([10, 25, 50, 100]);
const FILTER_KEYS = ['service', 'action', 'result', 'rw', 'resource', 'ip', 'cred'];

function normalise(raw, defaults) {
  const state = { ...defaults, ...raw };
  if (!RANGES.has(state.range)) state.range = defaults.range;
  if (!['all', 'success', 'error'].includes(state.result)) state.result = 'all';
  if (!['all', 'read', 'write'].includes(state.rw)) state.rw = 'all';
  state.page = Math.max(1, Number(state.page) || 1);
  state.size = SIZES.has(Number(state.size)) ? Number(state.size) : defaults.size;
  if (!['table', 'timeline'].includes(state.view)) state.view = defaults.view;
  return state;
}

export function useEventState({ urlBacked = false, defaults: overrides } = {}) {
  const defaults = useMemo(() => ({ ...EVENT_DEFAULTS, ...overrides }), [overrides]);
  const [searchParams, setSearchParams] = useSearchParams();
  const [local, setLocal] = useState(defaults);

  const state = useMemo(() => {
    if (!urlBacked) return normalise(local, defaults);
    const raw = {};
    for (const key of Object.keys(EVENT_DEFAULTS)) {
      const value = searchParams.get(key);
      if (value !== null) raw[key] = value;
    }
    return normalise(raw, defaults);
  }, [urlBacked, local, searchParams, defaults]);

  /* Any change other than paging or opening an event goes back to page 1. */
  const patch = useCallback(
    (changes) => {
      const resetPage = Object.keys(changes).some((key) => !['page', 'size', 'event', 'view'].includes(key));
      const nextChanges = { ...changes };
      if (resetPage || 'size' in changes) nextChanges.page = 1;
      if (!urlBacked) {
        setLocal((current) => ({ ...current, ...nextChanges }));
        return;
      }
      setSearchParams(
        (current) => {
          const next = new URLSearchParams(current);
          for (const [key, value] of Object.entries(nextChanges)) {
            if (value === '' || value === null || value === undefined || String(value) === String(defaults[key])) next.delete(key);
            else next.set(key, String(value));
          }
          return next;
        },
        { replace: true },
      );
    },
    [urlBacked, setSearchParams, defaults],
  );

  const clearFilters = useCallback(() => {
    patch(Object.fromEntries(FILTER_KEYS.map((key) => [key, EVENT_DEFAULTS[key]])));
  }, [patch]);

  return { state, patch, clearFilters };
}

/** The query the events endpoint takes, from the state above. */
export function eventQuery(state, extra = {}) {
  return {
    timeRange: state.range,
    service: state.service,
    action: state.action,
    result: state.result,
    readWrite: state.rw,
    resource: state.resource,
    sourceIp: state.ip,
    credential: state.cred,
    ...extra,
  };
}

export function activeEventFilters(state) {
  return FILTER_KEYS.filter((key) => state[key] && state[key] !== 'all');
}
