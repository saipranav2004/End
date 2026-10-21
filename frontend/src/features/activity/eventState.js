import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { formatDate } from '../../lib/format';

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
  from: '',
  to: '',
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

/* `hours` places a range on the activity tab, which only takes whole days
   or a custom window (see `activityRange`). */
export const EVENT_RANGES = [
  { value: '1h', label: '1h', title: 'Last hour', hours: 1 },
  { value: '6h', label: '6h', title: 'Last 6 hours', hours: 6 },
  { value: '24h', label: '24h', title: 'Last 24 hours', days: 1 },
  { value: '7d', label: '7d', title: 'Last 7 days', days: 7 },
  { value: '30d', label: '30d', title: 'Last 30 days', days: 30 },
  { value: '90d', label: '90d', title: 'Last 90 days', days: 90 },
  { value: 'custom', label: 'Custom', title: 'Chosen dates (UTC)' },
];

const RANGES = new Set(EVENT_RANGES.map((range) => range.value));
const SIZES = new Set([10, 25, 50, 100]);
const FILTER_KEYS = ['service', 'action', 'result', 'rw', 'resource', 'ip', 'cred'];

function normalise(raw, defaults) {
  const state = { ...defaults, ...raw };
  if (!RANGES.has(state.range) || (state.range === 'custom' && !validDates(state.from, state.to))) state.range = defaults.range;
  if (state.range !== 'custom') {
    state.from = '';
    state.to = '';
  }
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

/** Today as a UTC calendar day - the API reads bare dates as UTC days. */
export function todayUtc() {
  return new Date().toISOString().slice(0, 10);
}

const DAY = /^\d{4}-\d{2}-\d{2}$/;

/** A custom window the API accepts: two days, start on or before end, end not after today. */
export function validDates(from, to) {
  return DAY.test(from || '') && DAY.test(to || '') && from <= to && to <= todayUtc();
}

/** The selected range, with `phrase` for sentences ("... in the last 7 days"). */
export function eventRange(state) {
  const range = EVENT_RANGES.find((entry) => entry.value === state.range) ?? EVENT_RANGES[3];
  if (range.value !== 'custom') return { ...range, phrase: `in the ${range.title.toLowerCase()}` };
  /* Noon UTC, so the local date shown is the UTC day chosen. */
  const day = (value) => formatDate(`${value}T12:00:00Z`);
  if (state.from === state.to) return { ...range, title: day(state.from), phrase: `on ${day(state.from)}` };
  return { ...range, title: `${day(state.from)} - ${day(state.to)}`, phrase: `between ${day(state.from)} and ${day(state.to)}` };
}

/** The window alone, for queries that must not narrow by the filters (filter choices, the summary). */
export function rangeQuery(state) {
  return { timeRange: state.range, startDate: state.from, endDate: state.to };
}

/**
 * The same window as params of the identity's activity tab, which takes days
 * or a custom window but no hours: 1h and 6h go as a custom window from that
 * many hours ago (to the minute) until now. Memoise on the range - the start
 * moves with the clock.
 */
export function activityRange(state) {
  const range = eventRange(state);
  if (range.value === 'custom') return { time_range: 'custom', start_date: state.from, end_date: state.to };
  if (range.hours) {
    const start = new Date(Math.floor((Date.now() - range.hours * 3600000) / 60000) * 60000);
    return { time_range: 'custom', start_date: start.toISOString().replace('.000Z', 'Z'), end_date: todayUtc() };
  }
  return { time_range: `${range.days}d` };
}

/** The query the events endpoint takes, from the state above. */
export function eventQuery(state, extra = {}) {
  return {
    ...rangeQuery(state),
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
