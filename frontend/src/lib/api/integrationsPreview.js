import { ApiError } from './http';
import { readStoredUser } from './session';
import { AWS_REGION_CATALOGUE } from './awsRegions';

/*
 * Integrations preview: the Connect AWS Account API answered in the browser,
 * from sample accounts, for a build made with VITE_INTEGRATIONS_PREVIEW=true.
 *
 * It exists so the console can be reviewed against a real backend before the
 * platform's AWS side is deployed (verifying a real account needs it). Every
 * other API is the real one. Nothing here reaches the server or AWS: the
 * sample lives in this tab's sessionStorage, so a reload keeps changes and a
 * new tab starts from the sample again.
 *
 * Each function returns what the real endpoint returns in `data`, so the
 * normalisers in integrations.js read both the same way.
 */

export const INTEGRATIONS_PREVIEW = import.meta.env.VITE_INTEGRATIONS_PREVIEW === 'true';

const STORE_KEY = 'nhi.integrationsPreview.v1';
const DELAY = { read: 250, write: 600, verify: 1600 };

/* AWS's documentation account and obviously sample values - never a real one. */
const SETUP = {
  account_id: '111122223333',
  external_id: 'ext-sample-4f2c9a7e1b6d4e3fa0c58b2d7e9f1a6c',
  event_bus_arn: 'arn:aws:events:ap-south-1:111122223333:event-bus/nhi-central-bus',
  region: 'ap-south-1',
  assumer_role_arn: 'arn:aws:iam::111122223333:role/NHIPlatformAssumer',
  role_name: 'NHIPlatformCollector',
  rule_name: 'nhi-forward-events',
  managed_policies: ['SecurityAudit', 'ViewOnlyAccess'],
  event_sources: ['aws.ec2', 'aws.ecs', 'aws.iam', 'aws.sts', 'aws.lambda', 'aws.eks', 'aws.secretsmanager', 'aws.kms'],
  regions: AWS_REGION_CATALOGUE,
  exposure: {
    codecommit: {
      managed_policy: '',
      actions: ['codecommit:GetBranch', 'codecommit:GetCommit', 'codecommit:GetDifferences', 'codecommit:GetBlob'],
    },
    github: {
      secret_prefix: 'nhi/github',
      actions: ['secretsmanager:GetSecretValue', 'secretsmanager:DescribeSecret'],
      token_permissions: ['Contents: Read-only', 'Metadata: Read-only'],
    },
  },
};

const MIN = 60 * 1000;
const HOUR = 60 * MIN;
const ago = (ms) => new Date(Date.now() - ms).toISOString();

const PASS = {
  assume_role: 'Signed in to your account with your External ID.',
  read_access: 'Read access works.',
  events: 'Events from your account are arriving.',
};
const LABELS = { assume_role: 'Assume your role', read_access: 'Read your account', events: 'Receive your events' };

function awsChecks(failing = null) {
  return ['assume_role', 'read_access', 'events'].map((key) =>
    key === failing?.key
      ? { key, label: LABELS[key], state: 'fail', message: failing.message, fix: failing.fix }
      : { key, label: LABELS[key], state: 'pass', message: PASS[key] },
  );
}

function platformOff() {
  return { connected: false, checks: [], verified_at: null };
}

function githubOn(at, secretArn) {
  return {
    connected: true,
    verified_at: at,
    secret_arn: secretArn,
    checks: [
      { key: 'github_secret', label: 'Read the token secret', state: 'pass', message: 'Read the secret with your role.' },
      { key: 'github_token', label: 'GitHub accepts the token', state: 'pass', message: '6 repositories visible as acme-security-bot.' },
    ],
  };
}

function codecommitOn(at, repos = 4) {
  return {
    connected: true,
    verified_at: at,
    checks: [
      { key: 'codecommit_list', label: 'List your repositories', state: 'pass', message: `${repos} repositories in 2 regions.` },
      { key: 'codecommit_read', label: 'Read repository contents', state: 'pass', message: 'Read payments-api.' },
    ],
  };
}

/* What each sample account "discovered", for the counts the page shows. */
const SAMPLE_INVENTORY = {
  210987654321: { identities: 38, credentials: 52 },
  345678901234: { identities: 17, credentials: 21 },
  456789012345: { identities: 9, credentials: 11 },
};

function sampleAccounts() {
  const owner = 'admin@example.com';
  return [
    {
      account_id: '210987654321',
      role_arn: 'arn:aws:iam::210987654321:role/NHIPlatformCollector',
      regions: ['us-east-1', 'eu-west-1'],
      status: 'connected',
      is_connected: true,
      checks: awsChecks(),
      last_verified_at: ago(5 * HOUR),
      last_verified_by: owner,
      connected_at: ago(9 * 24 * HOUR),
      exposure: {
        github: githubOn(ago(8 * 24 * HOUR), 'arn:aws:secretsmanager:us-east-1:210987654321:secret:nhi/github/token-Ab12Cd'),
        codecommit: codecommitOn(ago(8 * 24 * HOUR)),
        updated_by: owner,
      },
      sync: { status: 'succeeded', requested_at: ago(3 * HOUR), requested_by: owner, started_at: ago(3 * HOUR - 2 * MIN), last_synced_at: ago(2 * HOUR + 40 * MIN), last_error: '' },
    },
    {
      account_id: '345678901234',
      role_arn: 'arn:aws:iam::345678901234:role/NHIPlatformCollector',
      regions: ['ap-south-1'],
      status: 'connected',
      is_connected: true,
      checks: awsChecks(),
      last_verified_at: ago(26 * HOUR),
      last_verified_by: owner,
      connected_at: ago(4 * 24 * HOUR),
      exposure: { github: platformOff(), codecommit: codecommitOn(ago(3 * 24 * HOUR), 2), updated_by: owner },
      sync: { status: 'running', requested_at: ago(6 * MIN), requested_by: owner, started_at: ago(4 * MIN), last_synced_at: ago(25 * HOUR), last_error: '' },
    },
    {
      account_id: '456789012345',
      role_arn: 'arn:aws:iam::456789012345:role/NHIPlatformCollector',
      regions: ['us-west-2'],
      status: 'action_needed',
      is_connected: false,
      checks: awsChecks({
        key: 'events',
        message: 'No events from this account in the last 24 hours.',
        fix: 'Check the EventBridge rule target ARN',
      }),
      last_verified_at: ago(50 * MIN),
      last_verified_by: owner,
      connected_at: ago(2 * 24 * HOUR),
      exposure: { github: platformOff(), codecommit: platformOff(), updated_by: '' },
      sync: { status: 'succeeded', requested_at: ago(2 * 24 * HOUR), requested_by: owner, started_at: ago(2 * 24 * HOUR - 3 * MIN), last_synced_at: ago(2 * 24 * HOUR - 12 * MIN), last_error: '' },
    },
  ];
}

/* ── Store ─────────────────────────────────────────────────────────────── */

function load() {
  try {
    const raw = sessionStorage.getItem(STORE_KEY);
    if (raw) return JSON.parse(raw);
  } catch {
    /* fall through to the sample */
  }
  return { accounts: sampleAccounts() };
}

function save(state) {
  try {
    sessionStorage.setItem(STORE_KEY, JSON.stringify(state));
  } catch {
    /* the preview still works for this page view */
  }
}

let state = null;
const store = () => (state ??= load());
const commit = () => save(store());

const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const clone = (value) => JSON.parse(JSON.stringify(value));

function actor() {
  return readStoredUser()?.email || readStoredUser()?.username || 'you';
}

function canManage() {
  const role = String(readStoredUser()?.role ?? '').toLowerCase();
  return role === 'admin' || role === 'super_admin';
}

function requireManage() {
  if (!canManage()) {
    throw new ApiError({ status: 403, code: 'FORBIDDEN', message: 'Only an admin can change integrations.' });
  }
}

function findAccount(accountId) {
  const account = store().accounts.find((entry) => entry.account_id === accountId);
  if (!account) throw new ApiError({ status: 404, code: 'AWS_ACCOUNT_NOT_FOUND', message: 'That account is not connected to this workspace.' });
  return account;
}

/* A sync moves queued -> running -> succeeded on its own, as the discovery
   worker would, so the page's polling shows it progress. */
function scheduleSync(accountId) {
  setTimeout(() => {
    const account = store().accounts.find((entry) => entry.account_id === accountId);
    if (!account || account.sync.status !== 'queued') return;
    account.sync.status = 'running';
    account.sync.started_at = new Date().toISOString();
    commit();
    setTimeout(() => {
      const again = store().accounts.find((entry) => entry.account_id === accountId);
      if (!again || again.sync.status !== 'running') return;
      again.sync.status = 'succeeded';
      again.sync.last_synced_at = new Date().toISOString();
      again.sync.last_error = '';
      commit();
    }, 20000);
  }, 6000);
}

function overall(accounts) {
  if (accounts.some((account) => account.status === 'action_needed')) return 'action_needed';
  if (accounts.some((account) => account.status === 'connected')) return 'connected';
  return 'not_connected';
}

/* ── Endpoints ─────────────────────────────────────────────────────────── */

export async function getIntegration() {
  await wait(DELAY.read);
  const { accounts } = store();
  return clone({
    status: overall(accounts),
    /* The point of the preview: the console behaves as a connected workspace. */
    has_connected_account: true,
    exposure_enabled: accounts.some((account) => account.exposure.github.connected || account.exposure.codecommit.connected),
    can_manage: canManage(),
    workspace: { domain: 'example.com' },
    setup: SETUP,
    accounts,
  });
}

const ROLE_ARN = /^arn:aws:iam::(\d{12}):role\/[\w+=,.@\-/]+$/;

export async function verify({ role_arn: roleArn, regions }) {
  requireManage();
  await wait(DELAY.verify);
  const match = ROLE_ARN.exec(String(roleArn ?? '').trim());
  if (!match) throw new ApiError({ status: 400, code: 'VALIDATION_ERROR', field: 'role_arn', message: 'Enter the role ARN, e.g. arn:aws:iam::123456789012:role/NHIPlatformCollector.' });
  if (!Array.isArray(regions) || regions.length === 0) {
    throw new ApiError({ status: 400, code: 'VALIDATION_ERROR', field: 'regions', message: 'Choose at least one region.' });
  }
  const accountId = match[1];
  const now = new Date().toISOString();
  const checks = awsChecks();
  let account = store().accounts.find((entry) => entry.account_id === accountId);
  const firstTime = !account || !account.connected_at;
  if (!account) {
    account = {
      account_id: accountId,
      exposure: { github: platformOff(), codecommit: platformOff(), updated_by: '' },
      sync: { status: 'idle', requested_at: null, requested_by: '', started_at: null, last_synced_at: null, last_error: '' },
    };
    store().accounts.push(account);
  }
  Object.assign(account, {
    role_arn: match[0],
    regions: [...regions],
    status: 'connected',
    is_connected: true,
    checks,
    last_verified_at: now,
    last_verified_by: actor(),
    connected_at: account.connected_at || now,
  });
  if (firstTime) {
    account.sync = { ...account.sync, status: 'queued', requested_at: now, requested_by: actor(), last_error: '' };
    scheduleSync(accountId);
  }
  commit();
  return clone({ status: 'connected', account_id: accountId, role_arn: match[0], regions, checks, verified_at: now, saved: true, sync_queued: firstTime });
}

export async function updateRegions(accountId, { regions }) {
  requireManage();
  await wait(DELAY.write);
  if (!Array.isArray(regions) || regions.length === 0) {
    throw new ApiError({ status: 400, code: 'VALIDATION_ERROR', field: 'regions', message: 'Choose at least one region.' });
  }
  const account = findAccount(accountId);
  account.regions = [...regions];
  commit();
  return clone(account);
}

export async function setExposure(accountId, { github, codecommit, github_secret_arn: secretArn }) {
  requireManage();
  await wait(DELAY.verify);
  const account = findAccount(accountId);
  const now = new Date().toISOString();
  if (github) {
    if (!/^arn:aws:secretsmanager:[a-z0-9-]+:\d{12}:secret:.+$/.test(String(secretArn ?? ''))) {
      throw new ApiError({ status: 400, code: 'VALIDATION_ERROR', field: 'github_secret_arn', message: 'Enter the secret ARN, e.g. arn:aws:secretsmanager:us-east-1:123456789012:secret:nhi/github/token-AbCdEf.' });
    }
    account.exposure.github = githubOn(now, secretArn);
  } else {
    account.exposure.github = platformOff();
  }
  account.exposure.codecommit = codecommit ? codecommitOn(now) : platformOff();
  account.exposure.updated_by = actor();
  commit();
  const accounts = store().accounts;
  return clone({
    account,
    exposure_enabled: accounts.some((entry) => entry.exposure.github.connected || entry.exposure.codecommit.connected),
  });
}

export async function sync(accountId) {
  requireManage();
  await wait(DELAY.write);
  const account = findAccount(accountId);
  if (account.status !== 'connected') {
    throw new ApiError({ status: 409, code: 'AWS_ACCOUNT_NOT_CONNECTED', message: 'Fix the connection before syncing.' });
  }
  if (account.sync.status === 'queued' || account.sync.status === 'running') {
    return clone({ account, already_queued: true });
  }
  account.sync = { ...account.sync, status: 'queued', requested_at: new Date().toISOString(), requested_by: actor(), started_at: null, last_error: '' };
  commit();
  scheduleSync(accountId);
  return clone({ account, already_queued: false });
}

export async function remove(accountId) {
  requireManage();
  await wait(DELAY.write);
  findAccount(accountId);
  store().accounts = store().accounts.filter((entry) => entry.account_id !== accountId);
  commit();
  return { account_id: accountId };
}

/** What each account discovered, for the counts on the Integrations page. */
export function previewInventory(accounts) {
  return new Map(
    accounts.map((account) => [account.accountId, SAMPLE_INVENTORY[account.accountId] ?? { identities: 0, credentials: 0 }]),
  );
}

/** Puts the sample back as it started. */
export function resetPreview() {
  state = { accounts: sampleAccounts() };
  commit();
}
