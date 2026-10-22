import { NEUTRAL_SERIES, SERIES_TOKENS } from '../../lib/domain';
import { titleCaseEnum } from '../../lib/format';

/**
 * Vocabulary of the discovery API, in the words this console uses.
 *
 * The API speaks lower_snake enums (`lambda_function`, `iam_role`,
 * `execution_role`). The labels live here so every inventory surface - the
 * list, the drawer, the record, the palette - says the same thing. Values the
 * table does not know fall back to a title-cased form of the raw value rather
 * than to a guess.
 */

/* What the list endpoints accept for their type, category, environment and
   status filters. These are not enums: discovery keeps adding values
   (root_account, login_profile, an "exception" category, any environment
   tag), so a facet offers whatever the data holds and only malformed values
   (a typo'd shared link) are dropped. Shaped like a Set for splitList. */
const FILTER_TOKEN = { has: (value) => /^[a-z0-9][a-z0-9 _.:-]{0,63}$/.test(String(value).toLowerCase()) };
export const API_IDENTITY_TYPES = FILTER_TOKEN;
export const API_FUNCTIONAL_CLASSES = FILTER_TOKEN;
export const API_ENVIRONMENTS = FILTER_TOKEN;
export const API_IDENTITY_STATUSES = FILTER_TOKEN;
export const API_CREDENTIAL_TYPES = FILTER_TOKEN;
export const API_CREDENTIAL_STATUSES = ['active', 'inactive', 'expired', 'revoked', 'rotated', 'pending', 'deleted'];

/* ── Identity type ───────────────────────────────────────────────────────── */

const IDENTITY_TYPES = {
  ec2_instance: 'EC2 instance',
  lambda_function: 'Lambda function',
  ecs_task: 'ECS task',
  ecs_service: 'ECS service',
  eks_pod: 'EKS pod',
  eks_cluster: 'EKS cluster',
  batch_job: 'Batch job',
  iam_user: 'IAM user',
  iam_user_service_account: 'IAM user used by software',
  iam_role: 'IAM role',
  root_account: 'Root account',
  rds_instance: 'RDS instance',
  s3_bucket: 'S3 bucket',
  emr_cluster: 'EMR cluster',
  sagemaker_notebook: 'SageMaker notebook',
  glue_job: 'Glue job',
  step_function: 'Step Functions state machine',
  api_gateway: 'API Gateway',
  cloudformation_stack: 'CloudFormation stack',
  codebuild_project: 'CodeBuild project',
  codepipeline: 'CodePipeline pipeline',
  redshift_cluster: 'Redshift cluster',
  dynamodb_table: 'DynamoDB table',
  kinesis_stream: 'Kinesis stream',
  sqs_queue: 'SQS queue',
  sns_topic: 'SNS topic',
};

export function identityTypeLabel(value) {
  if (!value) return 'Unknown type';
  return IDENTITY_TYPES[value] ?? titleCaseEnum(value);
}

/* ── Functional class ────────────────────────────────────────────────────── */

/* Fixed slot per class so the hue never reshuffles as a filter narrows the
   list; slot 8 (neutral) is kept for "not classified". */
const CLASSES = {
  service: { label: 'Service', badge: 'SVC', slot: 0 },
  application: { label: 'Application', badge: 'APP', slot: 1 },
  infrastructure: { label: 'Infrastructure', badge: 'INFRA', slot: 2 },
  compute: { label: 'Compute', badge: 'CMP', slot: 3 },
  data: { label: 'Data', badge: 'DATA', slot: 4 },
  security: { label: 'Security', badge: 'SEC', slot: 5 },
  storage: { label: 'Storage', badge: 'STO', slot: 6 },
  network: { label: 'Network', badge: 'NET', slot: 6 },
};

export function functionalClassMeta(value) {
  const entry = CLASSES[String(value || '').toLowerCase()];
  if (!entry) {
    return { label: value ? titleCaseEnum(value) : 'Not classified', badge: value ? String(value).slice(0, 3).toUpperCase() : '?', color: NEUTRAL_SERIES };
  }
  return { label: entry.label, badge: entry.badge, color: SERIES_TOKENS[entry.slot] };
}

/* ── Environment / lifecycle status ──────────────────────────────────────── */

const ENVIRONMENTS = {
  production: 'Production',
  prod: 'Production',
  staging: 'Staging',
  development: 'Development',
  dev: 'Development',
  testing: 'Testing',
  qa: 'QA',
  uat: 'UAT',
  sandbox: 'Sandbox',
};

export function environmentLabel(value) {
  if (!value) return 'No environment tag';
  return ENVIRONMENTS[String(value).toLowerCase()] ?? titleCaseEnum(value);
}

const STATUS_TONES = {
  active: 'low',
  running: 'low',
  available: 'low',
  pending: 'info',
  creating: 'info',
  inactive: 'neutral',
  stopped: 'neutral',
  rotated: 'neutral',
  terminated: 'high',
  deleted: 'high',
  failed: 'high',
  expired: 'critical',
  revoked: 'high',
};

/** Lifecycle status as AWS reports it (identity or credential). */
export function lifecycleMeta(value) {
  const key = String(value || '').toLowerCase();
  return { label: key ? titleCaseEnum(key) : 'Unknown', tone: STATUS_TONES[key] ?? 'neutral' };
}

/* ── Credentials ─────────────────────────────────────────────────────────── */

const CREDENTIAL_TYPES = {
  iam_role: {
    label: 'IAM role',
    longLived: false,
    what: 'A role assumed through STS. The credentials it issues expire within the session limit; the role itself has nothing to rotate, so the risk lives in its trust policy and the permissions attached to it.',
  },
  iam_user: {
    label: 'IAM user',
    longLived: true,
    what: 'A user that signs in with its own password or access keys. It never expires on its own.',
  },
  access_key: {
    label: 'Access key',
    longLived: true,
    what: 'A long-lived key pair. It never expires on its own, which is why age, rotation and last use are the figures that matter for it.',
  },
  secret: {
    label: 'Secrets Manager secret',
    longLived: true,
    what: 'Credential material held in Secrets Manager. Long-lived, but managed - a rotation schedule can be attached to it.',
  },
  api_key: {
    label: 'API key',
    longLived: true,
    what: 'A static key for an external or internal API. Long-lived unless someone rotates it.',
  },
  password: { label: 'Password', longLived: true, what: 'A static password. Long-lived until it is changed.' },
  certificate: {
    label: 'Certificate',
    longLived: true,
    what: 'An X.509 certificate. It expires on its own date; what matters is renewing it before then.',
  },
  tls_cert: {
    label: 'TLS certificate',
    longLived: true,
    what: 'A TLS certificate. It expires on its own date; what matters is renewing it before then.',
  },
  ssh_key: { label: 'SSH key', longLived: true, what: 'An SSH key pair. It does not expire unless an expiry was set.' },
  saml_provider: {
    label: 'SAML provider',
    longLived: false,
    what: 'Federation, not a stored secret. The risk lives in which roles trust it and under what conditions.',
  },
  oidc_provider: {
    label: 'OIDC provider',
    longLived: false,
    what: 'Federation, not a stored secret. The risk lives in which roles trust it and under what conditions.',
  },
  oauth_token: { label: 'OAuth token', longLived: true, what: 'A bearer token issued to an integration.' },
  service_account: { label: 'Service account', longLived: true, what: 'An account used by a workload rather than a person.' },
  service_principal: { label: 'Service principal', longLived: false, what: 'An AWS service acting on the account’s behalf.' },
  kms_key: { label: 'KMS key', longLived: true, what: 'A KMS key used to protect other secrets.' },
  managed_policy: { label: 'Managed policy', longLived: true, what: 'A managed IAM policy.' },
  inline_policy: { label: 'Inline policy', longLived: true, what: 'An inline IAM policy.' },
};

export function credentialTypeMeta(value) {
  const key = String(value || '').toLowerCase();
  return (
    CREDENTIAL_TYPES[key] ?? {
      label: key ? titleCaseEnum(key) : 'Credential',
      longLived: true,
      what: 'A credential kind this console has no description for.',
    }
  );
}

const BINDINGS = {
  execution_role: 'Execution role',
  instance_profile: 'Instance profile',
  task_role: 'Task role',
  task_execution_role: 'Task execution role',
  service_role: 'Service role',
  owned_key: 'Owned key',
  access_key: 'Access key',
  self: 'Its own credential',
  reads_secret: 'Reads secret',
  assumes: 'Assumes',
};

export function bindingLabel(value) {
  if (!value) return 'Bound';
  return BINDINGS[String(value).toLowerCase()] ?? titleCaseEnum(value);
}

/** Privilege level as the raw credential record carries it (4 admin ... 1 low). */
export function privilegeLabel(value) {
  if (value === null || value === undefined || value === '') return null;
  const labels = { 4: 'Admin', 3: 'High', 2: 'Medium', 1: 'Low' };
  return labels[Number(value)] ?? String(value);
}

/* ── Relationships ───────────────────────────────────────────────────────── */

/* Plain words for each edge type (see the console glossary), read from the
   identity's side: `out` when it is the source, `in` when it is the target. */
export const EDGE_WORDS = {
  assume_role: { out: 'Can switch to (assume)', in: 'Can switch to it (assume)' },
  bound_to: { out: 'Uses', in: 'Also uses it' },
  accesses: { out: 'Used', in: 'Was used by it' },
  trusts: { out: 'Trusts', in: 'Allowed by its trust policy' },
  contains: { out: 'Contains', in: 'Is part of' },
  manages: { out: 'Manages', in: 'Manages it' },
  attached_to: { out: 'Attached to', in: 'Attached to it' },
  uses: { out: 'Uses', in: 'Uses it' },
  service_trust: { out: 'An AWS service may use it', in: 'An AWS service may use it' },
  cross_account: { out: 'Reaches another AWS account', in: 'Another AWS account may use it' },
  federated_oidc: { out: 'Signs in through web identity (OIDC)', in: 'Web identity sign-in (OIDC) may use it' },
  federated_saml: { out: 'Signs in through company single sign-on (SAML)', in: 'Company single sign-on (SAML) may use it' },
  created_by: { out: 'Created by', in: 'Created' },
  pass_role: { out: 'Can hand a role to a service', in: 'Can hand it to a service' },
};

/** Outbound wording, keyed by edge type; shared with the credential drawer. */
export const EDGE_LABELS = Object.fromEntries(Object.entries(EDGE_WORDS).map(([key, words]) => [key, words.out]));

/** The plain phrase for one edge type in one direction. */
export function edgeWords(type, direction = 'out') {
  return EDGE_WORDS[type]?.[direction] ?? titleCaseEnum(type);
}

export function nodeTypeLabel(type, subtype) {
  if (type === 'identity') return identityTypeLabel(subtype);
  if (type === 'credential') return credentialTypeMeta(subtype).label;
  if (type === 'resource') return subtype ? subtype.replace(/^AWS::/, '').replace(/::/g, ' ') : 'Resource';
  if (type === 'service') return subtype ? `AWS service (${subtype.toUpperCase()})` : 'AWS service';
  if (type === 'account') return 'AWS account';
  return subtype ? titleCaseEnum(subtype) : 'Not found in the inventory';
}

/* ── Severity of the API's own risk signals ──────────────────────────────── */

export function signalTone(severity) {
  const key = String(severity || '').toLowerCase();
  return ['critical', 'high', 'medium', 'low'].includes(key) ? key : 'neutral';
}

/* ── Event time ranges ───────────────────────────────────────────────────── */

export const EVENT_RANGES = [
  { value: '24h', label: '24h', title: 'Last 24 hours', days: 1 },
  { value: '7d', label: '7d', title: 'Last 7 days', days: 7 },
  { value: '30d', label: '30d', title: 'Last 30 days', days: 30 },
  { value: '90d', label: '90d', title: 'Last 90 days', days: 90 },
];

export function eventRange(value) {
  return EVENT_RANGES.find((range) => range.value === value) ?? EVENT_RANGES[2];
}

/** A share as text: whole percent, "<1%" for a small non-zero share. */
export function shareText(part, whole) {
  const p = Number(part);
  const w = Number(whole);
  if (!Number.isFinite(p) || !Number.isFinite(w) || w <= 0) return '-';
  const pct = (p / w) * 100;
  if (pct > 0 && pct < 1) return '<1%';
  return `${Math.round(pct)}%`;
}
