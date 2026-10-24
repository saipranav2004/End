/**
 * Console roles and what each one may do.
 *
 * Four roles, ordered by reach. The split follows the one most security
 * consoles settle on: whoever administers the console itself (people and
 * integrations) is separate from whoever runs the security programme, and
 * accepting a risk - dismissing an alert, allowlisting a leaked secret - is
 * reserved for the people accountable for that decision rather than for
 * everyone who works the queue.
 *
 * The screens read this table to decide what to show. It mirrors what the
 * services enforce - a check in the browser alone is presentation, not
 * security:
 *
 *   credential exposure APIs and integration changes   admin only
 *   posture finding updates (accept, suppress, assign) analyst or admin
 *
 * Server roles map onto these (lib/api/auth.js consoleRoleFor): admin -> admin,
 * analyst -> analyst, anything else -> viewer.
 */

export const ROLES = {
  super_admin: {
    key: 'super_admin',
    label: 'Super admin',
    rank: 4,
    summary: 'Full control of the console, including users, roles and integrations.',
  },
  admin: {
    key: 'admin',
    label: 'Admin',
    rank: 3,
    summary: 'Runs the security programme and the workspace: connects AWS accounts, reviews exposed credentials, accepts posture risks.',
  },
  analyst: {
    key: 'analyst',
    label: 'Analyst',
    rank: 2,
    summary: 'Investigates identities and works posture findings: assign, accept with a reason, suppress for a while.',
  },
  viewer: {
    key: 'viewer',
    label: 'Viewer',
    rank: 1,
    summary: 'Read-only oversight: posture, inventory, access and behaviour. No exposed secrets and no changes.',
  },
};

export const ROLE_ORDER = ['super_admin', 'admin', 'analyst', 'viewer'];

/**
 * Every permission the console checks, grouped the way the matrix on the
 * User management screen shows them. `roles` lists who holds it.
 */
export const PERMISSIONS = [
  { key: 'data.view', group: 'Visibility', label: 'View posture, inventory, access, behaviour, activity and reports', roles: ['super_admin', 'admin', 'analyst', 'viewer'] },
  { key: 'data.export', group: 'Visibility', label: 'Export tables to CSV', roles: ['super_admin', 'admin', 'analyst', 'viewer'] },

  { key: 'alerts.view', group: 'Alerts', label: 'Open the Alerts screen', roles: ['super_admin', 'admin', 'analyst'] },
  { key: 'alerts.viewAll', group: 'Alerts', label: 'See every alert - otherwise only those assigned or escalated to you', roles: ['super_admin', 'admin'] },
  { key: 'alerts.work', group: 'Alerts', label: 'Acknowledge, start, resolve, escalate and add notes', roles: ['super_admin', 'admin', 'analyst'] },
  { key: 'alerts.take', group: 'Alerts', label: 'Assign an alert to themselves', roles: ['super_admin', 'admin', 'analyst'] },
  { key: 'alerts.assign', group: 'Alerts', label: 'Assign alerts to anyone', roles: ['super_admin', 'admin'] },
  { key: 'alerts.dismiss', group: 'Alerts', label: 'Dismiss (accept a risk) and reopen closed alerts', roles: ['super_admin', 'admin'] },
  { key: 'alerts.bulk', group: 'Alerts', label: 'Act on many alerts at once', roles: ['super_admin', 'admin'] },

  { key: 'anomalies.review', group: 'NHI Genome', label: 'Mark an anomaly as real or as a false positive', roles: ['super_admin', 'admin', 'analyst'] },

  { key: 'exposure.view', group: 'Credential exposure', label: 'See exposed credentials - secrets found in code', roles: ['super_admin', 'admin'] },
  { key: 'exposure.deepScan', group: 'Credential exposure', label: 'Request a deep scan of a repository', roles: ['super_admin', 'admin'] },
  { key: 'exposure.review', group: 'Credential exposure', label: 'Accept a finding as reviewed, or restore it', roles: ['super_admin', 'admin'] },

  { key: 'posture.findings', group: 'Posture', label: 'Assign, accept or suppress a posture finding', roles: ['super_admin', 'admin', 'analyst'] },

  { key: 'reports.generate', group: 'Reports', label: 'Generate a report', roles: ['super_admin', 'admin', 'analyst'] },
  { key: 'reports.schedule', group: 'Reports', label: 'Create, pause and delete schedules, and delete past runs', roles: ['super_admin', 'admin'] },

  { key: 'integrations.operate', group: 'Administration', label: 'Re-check connections and request a sync', roles: ['super_admin', 'admin'] },
  { key: 'scan.run', group: 'Administration', label: 'Run a scan of the AWS accounts', roles: ['super_admin', 'admin', 'analyst'] },
  { key: 'integrations.manage', group: 'Administration', label: 'Connect and remove AWS accounts, enable credential scanning', roles: ['super_admin', 'admin'] },
  { key: 'users.manage', group: 'Administration', label: 'Invite users, change roles, deactivate accounts', roles: ['super_admin'] },
];

const BY_KEY = new Map(PERMISSIONS.map((permission) => [permission.key, permission]));

export function roleMeta(role) {
  return ROLES[role] ?? ROLES.viewer;
}

/** Whether `role` holds `permission`. Unknown permissions are refused. */
export function roleCan(role, permission) {
  return Boolean(BY_KEY.get(permission)?.roles.includes(role));
}

/** The least-privileged role that holds `permission`, for "needs X" hints. */
export function minimumRoleFor(permission) {
  const holders = BY_KEY.get(permission)?.roles ?? [];
  return [...ROLE_ORDER].reverse().find((role) => holders.includes(role)) ?? 'super_admin';
}

/** Where a role lands after signing in. */
export function homeFor() {
  return '/overview';
}

/** What `/overview` is for each role, in words - shown on User management. */
export const ROLE_HOME = {
  super_admin: 'Organisation dashboard',
  admin: 'Organisation dashboard',
  analyst: 'Organisation dashboard',
  viewer: 'Dashboard, without credential exposure',
};

/** One sentence for a disabled control: why it is disabled and who can. */
export function deniedReason(permission) {
  const needed = roleMeta(minimumRoleFor(permission)).label;
  return needed === ROLES.super_admin.label
    ? 'Only a super admin can do this.'
    : `Needs the ${needed} role or higher.`;
}

/** A refusal shaped like the API's, so screens handle it as a 403. */
export function forbidden(permission) {
  const error = new Error(`Your role does not allow this. ${deniedReason(permission)}`);
  error.status = 403;
  error.code = 'FORBIDDEN';
  return error;
}
