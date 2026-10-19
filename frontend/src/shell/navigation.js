import {
  Activity,
  Dna,
  Waypoints,
  FileWarning,
  Fingerprint,
  Gauge,
  // History, - reinstate with the Scans nav item
  KeyRound,
  Plug,
  ShieldCheck,
  ShieldOff,
} from 'lucide-react';

/**
 * Information architecture.
 *
 * Grouped by the question an operator is answering, not by the API surface:
 * "how exposed am I" (Overview), "what exists" (Inventory), "what leaked"
 * (Credential exposure), how it behaves (Behaviour), "what happened"
 * (Operations).
 */
export const NAV_GROUPS = [
  {
    key: 'overview',
    label: 'Overview',
    /* The dashboard says what the estate holds; Posture scores how exposed
       each identity is. */
    items: [
      { to: '/overview', label: 'Dashboard', icon: Gauge, end: true },
      { to: '/posture', label: 'Posture', icon: ShieldCheck },
    ],
  },
  {
    key: 'inventory',
    label: 'Inventory',
    items: [
      { to: '/identities', label: 'Identities', icon: Fingerprint },
      { to: '/credentials', label: 'Credentials', icon: KeyRound },
    ],
  },
  {
    key: 'exposure',
    label: 'Credential exposure',
    /* Shown once GitHub or CodeCommit scanning is switched on for an AWS
       account on the Integrations page (`feature: 'exposure'`). */
    items: [
      { to: '/exposure', label: 'Exposed credentials', icon: FileWarning, end: true, permission: 'exposure.view', feature: 'exposure' },
      { to: '/exposure/dismissed', label: 'Accepted', icon: ShieldOff, permission: 'exposure.view', feature: 'exposure' },
    ],
  },
  {
    key: 'access',
    label: 'Access',
    /* Separate from Behaviour on purpose. The genome is what an identity has
       done; the graph is what it could do. Conflating observed behaviour with
       granted entitlement is how a quiet identity with administrator access
       gets mistaken for a safe one. */
    items: [{ to: '/access-graph', label: 'Access graph', icon: Waypoints, end: true }],
  },
  {
    key: 'behaviour',
    label: 'Behaviour',
    items: [{ to: '/genome', label: 'NHI Genome', icon: Dna }],
  },
  {
    key: 'operations',
    label: 'Operations',
    /* Alerts, Reports, Scans and User management have no API yet; they
       return here with their endpoints. */
    items: [{ to: '/activity', label: 'Activity', icon: Activity }],
  },
  {
    key: 'settings',
    label: 'Settings',
    /* Last in the rail on purpose. Everything above reads the estate; this is
       the one group that changes how the estate is collected. */
    /* User management is reached from the account menu, not from here: it
       administers the console, not the estate. */
    items: [{ to: '/integrations', label: 'Integrations', icon: Plug }],
  },
];

/**
 * The navigation a role can use: items it lacks the permission for, items
 * whose feature the workspace has not switched on, and groups left empty,
 * removed. `features` is `{ exposure: boolean }`.
 */
export function navGroupsFor(can, role, features = {}) {
  return NAV_GROUPS.map((group) => ({
    ...group,
    items: group.items
      .filter((item) => (!item.permission || can(item.permission)) && (!item.feature || features[item.feature]))
      .map((item) => ({ ...item, label: item.roleLabels?.[role] ?? item.label })),
  })).filter((group) => group.items.length > 0);
}

export const ALL_NAV_ITEMS = NAV_GROUPS.flatMap((group) =>
  group.items.map((item) => ({ ...item, group: group.label || 'Overview' })),
);

/**
 * Breadcrumb trail per route. A console is a place people get lost in, so
 * every screen states where it sits. Kept as data rather than derived from the
 * path so the labels match the navigation exactly.
 */
export const BREADCRUMBS = {
  '/overview': ['Overview', 'Dashboard'],
  '/posture': ['Overview', 'Posture'],
  '/identities': ['Inventory', 'Identities'],
  '/credentials': ['Inventory', 'Credentials'],
  '/exposure': ['Credential exposure', 'Exposed credentials'],
  '/exposure/dismissed': ['Credential exposure', 'Accepted'],
  '/access-graph': ['Access', 'Access graph'],
  '/genome': ['Behaviour', 'NHI Genome'],
  '/activity': ['Operations', 'Activity'],
  '/integrations': ['Settings', 'Integrations'],
};

/**
 * Breadcrumbs for routes with a parameter. Matched by prefix after the exact
 * table misses, so a record screen still says where it sits.
 */
export const BREADCRUMB_PREFIXES = [
  { prefix: '/posture/', trail: ['Overview', 'Posture', 'Identity'] },
  { prefix: '/scans/', trail: ['Overview', 'Dashboard', 'Scan changes'] },
  { prefix: '/identities/', trail: ['Inventory', 'Identities', 'Identity'] },
  { prefix: '/access-graph/', trail: ['Access', 'Access graph', 'Identity'] },
  { prefix: '/genome/', trail: ['Behaviour', 'NHI Genome', 'Identity'] },
];
