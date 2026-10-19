import client from './client';
import { unwrapMain } from './http';
import * as preview from './integrationsPreview';
import { INTEGRATIONS_PREVIEW } from './integrationsPreview';

/*
 * Connect AWS Account (discovery API).
 *
 *   GET    /api/v1/integrations/aws
 *   POST   /api/v1/integrations/aws/verify                         { role_arn, regions }
 *   PUT    /api/v1/integrations/aws/accounts/{id}/regions          { regions }
 *   PUT    /api/v1/integrations/aws/accounts/{id}/exposure         { github, codecommit, github_secret_arn }
 *   POST   /api/v1/integrations/aws/accounts/{id}/sync
 *   DELETE /api/v1/integrations/aws/accounts/{id}
 *
 * A build with VITE_INTEGRATIONS_PREVIEW=true answers all of these in the
 * browser from sample accounts instead (integrationsPreview.js); the
 * normalisers below read both the same way.
 */

const BASE = '/api/v1/integrations/aws';
const enc = encodeURIComponent;

function checks(list) {
  return (Array.isArray(list) ? list : [])
    .filter((c) => c && typeof c.key === 'string')
    .map((c) => ({
      key: c.key,
      label: String(c.label ?? c.key),
      state: String(c.state ?? ''),
      message: String(c.message ?? ''),
      fix: c.fix ? String(c.fix) : '',
    }));
}

function platform(p = {}) {
  return {
    connected: Boolean(p.connected),
    checks: checks(p.checks),
    verifiedAt: p.verified_at ?? null,
    secretArn: p.secret_arn || '',
  };
}

export function awsAccount(a = {}) {
  const sync = a.sync ?? {};
  return {
    accountId: String(a.account_id ?? ''),
    roleArn: String(a.role_arn ?? ''),
    regions: Array.isArray(a.regions) ? a.regions : [],
    status: a.status || 'pending', // pending | connected | action_needed
    isConnected: Boolean(a.is_connected),
    checks: checks(a.checks),
    lastVerifiedAt: a.last_verified_at ?? null,
    lastVerifiedBy: a.last_verified_by ?? '',
    connectedAt: a.connected_at ?? null,
    exposure: {
      github: platform(a.exposure?.github),
      codecommit: platform(a.exposure?.codecommit),
      updatedBy: a.exposure?.updated_by ?? '',
    },
    sync: {
      status: sync.status || 'idle', // idle | queued | running | succeeded | failed
      requestedAt: sync.requested_at ?? null,
      requestedBy: sync.requested_by ?? '',
      startedAt: sync.started_at ?? null,
      lastSyncedAt: sync.last_synced_at ?? null,
      lastError: sync.last_error ?? '',
    },
  };
}

export async function fetchAwsIntegration(signal) {
  const d = (INTEGRATIONS_PREVIEW ? await preview.getIntegration() : unwrapMain(await client.get(BASE, { signal }))) ?? {};
  const s = d.setup;
  return {
    status: d.status || 'not_connected',
    hasConnectedAccount: Boolean(d.has_connected_account),
    exposureEnabled: Boolean(d.exposure_enabled),
    canManage: Boolean(d.can_manage),
    domain: d.workspace?.domain || '',
    setup: s
      ? {
          accountId: s.account_id,
          externalId: s.external_id,
          eventBusArn: s.event_bus_arn,
          region: s.region,
          assumerRoleArn: s.assumer_role_arn,
          roleName: s.role_name,
          ruleName: s.rule_name,
          managedPolicies: s.managed_policies ?? [],
          eventSources: s.event_sources ?? [],
          regions: (s.regions ?? []).map((r) => ({ code: r.code, name: r.name, group: r.group, optIn: Boolean(r.opt_in) })),
          exposure: {
            codecommit: {
              managedPolicy: s.exposure?.codecommit?.managed_policy ?? 'AWSCodeCommitReadOnly',
              actions: s.exposure?.codecommit?.actions ?? [],
            },
            github: {
              secretPrefix: s.exposure?.github?.secret_prefix ?? 'nhi/github',
              actions: s.exposure?.github?.actions ?? [],
              tokenPermissions: s.exposure?.github?.token_permissions ?? [],
            },
          },
        }
      : null,
    accounts: (d.accounts ?? []).map(awsAccount),
  };
}

/** Checks can take a few seconds against AWS; allow for it. */
const SLOW = { timeout: 30000 };

export async function verifyAwsConnection({ roleArn, regions }) {
  const body = { role_arn: roleArn, regions };
  const d = INTEGRATIONS_PREVIEW ? await preview.verify(body) : unwrapMain(await client.post(`${BASE}/verify`, body, SLOW));
  return {
    status: d.status,
    accountId: d.account_id,
    roleArn: d.role_arn,
    regions: d.regions ?? [],
    checks: checks(d.checks),
    verifiedAt: d.verified_at,
    saved: Boolean(d.saved),
    syncQueued: Boolean(d.sync_queued),
  };
}

export async function updateAwsAccountRegions({ accountId, regions }) {
  if (INTEGRATIONS_PREVIEW) return awsAccount(await preview.updateRegions(accountId, { regions }));
  return awsAccount(unwrapMain(await client.put(`${BASE}/accounts/${enc(accountId)}/regions`, { regions })));
}

export async function setAwsExposure({ accountId, github, codecommit, githubSecretArn }) {
  const body = { github: Boolean(github), codecommit: Boolean(codecommit), github_secret_arn: github ? githubSecretArn || '' : '' };
  const d = INTEGRATIONS_PREVIEW
    ? await preview.setExposure(accountId, body)
    : unwrapMain(await client.put(`${BASE}/accounts/${enc(accountId)}/exposure`, body, SLOW));
  return { account: awsAccount(d.account), exposureEnabled: Boolean(d.exposure_enabled) };
}

export async function syncAwsAccount({ accountId }) {
  const d = INTEGRATIONS_PREVIEW ? await preview.sync(accountId) : unwrapMain(await client.post(`${BASE}/accounts/${enc(accountId)}/sync`));
  return { account: awsAccount(d.account), alreadyQueued: Boolean(d.already_queued) };
}

export async function removeAwsAccount({ accountId }) {
  if (INTEGRATIONS_PREVIEW) return preview.remove(accountId);
  return unwrapMain(await client.delete(`${BASE}/accounts/${enc(accountId)}`));
}
