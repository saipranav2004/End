import {
  API_ENVIRONMENTS,
  API_FUNCTIONAL_CLASSES,
  API_IDENTITY_STATUSES,
  API_IDENTITY_TYPES,
  environmentLabel,
  functionalClassMeta,
  identityTypeLabel,
  lifecycleMeta,
} from './meta';

/**
 * Identity explorer filters.
 *
 * Every entry maps one-to-one onto a query parameter `GET /api/v1/identities`
 * accepts. Filter state lives in the URL so a drill-through from the
 * dashboard, a bookmark and a shared link all produce the same list. The
 * enum filters take comma lists, so a facet can hold several values at once.
 */

/** Multi-value facets: URL param -> label for the chip. */
export const LIST_PARAMS = [
  { param: 'identity_type', label: 'Type', describe: identityTypeLabel, allowed: API_IDENTITY_TYPES },
  { param: 'functional_class', label: 'Category', describe: (value) => functionalClassMeta(value).label, allowed: API_FUNCTIONAL_CLASSES },
  { param: 'environment', label: 'Environment', describe: environmentLabel, allowed: API_ENVIRONMENTS },
  { param: 'status', label: 'Status', describe: (value) => lifecycleMeta(value).label, allowed: API_IDENTITY_STATUSES },
];

/** Single-value signals, each one API parameter. */
export const SIGNALS = [
  {
    key: 'public',
    param: 'is_publicly_accessible',
    value: 'true',
    label: 'Publicly accessible',
    hint: 'Reachable from the internet',
  },
  {
    key: 'stale',
    param: 'min_idle_days',
    value: '90',
    label: 'Stale 90+ days',
    hint: 'Last recorded activity more than 90 days ago',
  },
  {
    key: 'idle',
    param: 'min_idle_days',
    value: '30',
    label: 'Idle 30+ days',
    hint: 'Last recorded activity more than 30 days ago',
  },
];

export const FILTER_PARAMS = [
  'search',
  'owner',
  'is_publicly_accessible',
  'min_idle_days',
  ...LIST_PARAMS.map((entry) => entry.param),
];

/** `sort` in the URL is `<column>` or `-<column>` (descending). Grid column -> API sort field. */
export const SORTABLE = {
  identity: 'name',
  class: 'functional_class',
  owner: 'owner',
  place: 'environment',
  access: 'permission_utilization_pct',
  activity: 'last_activity_at',
};

/** Every `sort_by` the list API accepts that the sort menu offers, columns or not. */
export const SORT_FIELDS = [
  { value: 'last_activity_at', label: 'Last activity' },
  { value: 'name', label: 'Name' },
  { value: 'identity_type', label: 'Identity type' },
  { value: 'functional_class', label: 'Category' },
  { value: 'owner', label: 'Owner' },
  { value: 'account_id', label: 'Account' },
  { value: 'region', label: 'Region' },
  { value: 'environment', label: 'Environment' },
  { value: 'status', label: 'Status' },
  { value: 'created_at_aws', label: 'Created in AWS' },
  { value: 'age_days', label: 'Age' },
  { value: 'idle_days', label: 'Idle days' },
  { value: 'bound_credentials_count', label: 'Bound credentials' },
  { value: 'permission_utilization_pct', label: 'Permissions used' },
];

export function readSort(searchParams) {
  const raw = searchParams.get('sort') || '';
  const desc = raw.startsWith('-');
  const column = desc ? raw.slice(1) : raw;
  if (!SORT_FIELDS.some((field) => field.value === column)) {
    return { key: 'activity', direction: 'desc', sortBy: 'last_activity_at', sortOrder: 'desc', explicit: false };
  }
  /* A field with no column of its own keeps its name as the key, so no header reads as sorted. */
  const key = Object.keys(SORTABLE).find((entry) => SORTABLE[entry] === column) ?? column;
  return { key, direction: desc ? 'desc' : 'asc', sortBy: column, sortOrder: desc ? 'desc' : 'asc', explicit: true };
}

export function splitList(value, allowed) {
  return String(value || '')
    .split(',')
    .map((entry) => (allowed ? entry.trim().toLowerCase() : entry.trim()))
    .filter((entry) => entry && (!allowed || allowed.has(entry)));
}

/* A value the API would reject (a typo in a shared link) is dropped, not sent. */
const listOf = (searchParams, param) =>
  splitList(searchParams.get(param), LIST_PARAMS.find((entry) => entry.param === param)?.allowed);

/** Reads the explorer's query out of URL search params. */
export function readFilters(searchParams) {
  const sort = readSort(searchParams);
  const pageSize = Number(searchParams.get('page_size'));
  return {
    search: searchParams.get('search') || '',
    identityType: listOf(searchParams, 'identity_type'),
    functionalClass: listOf(searchParams, 'functional_class'),
    environment: listOf(searchParams, 'environment'),
    status: listOf(searchParams, 'status'),
    owner: searchParams.get('owner') || '',
    isPublic: searchParams.get('is_publicly_accessible') === 'true' ? 'true' : '',
    minIdleDays: /^\d+$/.test(searchParams.get('min_idle_days') || '') ? searchParams.get('min_idle_days') : '',
    page: Math.max(1, Number(searchParams.get('page')) || 1),
    pageSize: [10, 25, 50, 100].includes(pageSize) ? pageSize : 25,
    sortBy: sort.sortBy,
    sortOrder: sort.sortOrder,
  };
}

/** Human-readable chips describing what is applied. Keys are `param` or `param=value`. */
export function describeFilters(searchParams) {
  const chips = [];
  const search = searchParams.get('search');
  if (search) chips.push({ key: 'search', label: 'Search', value: search });

  for (const entry of LIST_PARAMS) {
    for (const value of splitList(searchParams.get(entry.param), entry.allowed)) {
      chips.push({ key: `${entry.param}=${value}`, label: entry.label, value: entry.describe(value) });
    }
  }

  const owner = searchParams.get('owner');
  if (owner) chips.push({ key: 'owner', label: 'Owner', value: owner });

  if (searchParams.get('is_publicly_accessible') === 'true') {
    chips.push({ key: 'is_publicly_accessible', label: 'Signal', value: 'Publicly accessible' });
  }
  const idle = searchParams.get('min_idle_days');
  if (idle) chips.push({ key: 'min_idle_days', label: 'Signal', value: `Idle ${idle}+ days` });
  return chips;
}
