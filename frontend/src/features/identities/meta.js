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

/* The values each list endpoint accepts for its enum filters. A facet only
   offers values in these sets: anything else would be answered with 400. */
export const API_IDENTITY_TYPES = new Set([
  'ec2_instance', 'lambda_function', 'ecs_task', 'eks_pod', 'batch_job', 'iam_user', 'iam_role',
  'rds_instance', 's3_bucket', 'ebs_volume', 'eni', 'vpc', 'subnet', 'security_group', 'emr_cluster',
  'sagemaker_notebook', 'glue_job', 'step_function', 'api_gateway', 'cloudformation_stack',
  'codebuild_project', 'codepipeline', 'ecs_service', 'eks_cluster', 'elasticache_cluster',
  'redshift_cluster', 'dynamodb_table', 'kinesis_stream', 'sqs_queue', 'sns_topic', 'cloudfront_distribution',
]);
export const API_FUNCTIONAL_CLASSES = new Set([
  'service', 'application', 'infrastructure', 'data', 'security', 'compute', 'storage', 'network',
]);
export const API_ENVIRONMENTS = new Set([
  'production', 'staging', 'development', 'testing', 'sandbox', 'prod', 'dev', 'qa', 'uat',
]);
export const API_IDENTITY_STATUSES = new Set([
  'active', 'inactive', 'stopped', 'terminated', 'running', 'pending', 'deleted', 'available', 'creating', 'failed',
]);
export const API_CREDENTIAL_TYPES = new Set([
  'iam_role', 'iam_user', 'access_key', 'certificate', 'api_key', 'secret', 'password', 'saml_provider',
  'oidc_provider', 'managed_policy', 'inline_policy', 'service_account', 'service_principal', 'oauth_token',
  'ssh_key', 'tls_cert', 'kms_key',
]);
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
  iam_role: 'IAM role',
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

export const EDGE_LABELS = {
  assume_role: 'Assumes role',
  bound_to: 'Bound to',
  accesses: 'Accesses',
  trusts: 'Trusts',
  contains: 'Contains',
  manages: 'Manages',
  attached_to: 'Attached to',
  uses: 'Uses',
  service_trust: 'Service trust',
  cross_account: 'Cross-account trust',
  federated_oidc: 'OIDC federation',
  federated_saml: 'SAML federation',
  created_by: 'Created by',
};

export function nodeTypeLabel(type, subtype) {
  if (type === 'identity') return identityTypeLabel(subtype);
  if (type === 'credential') return credentialTypeMeta(subtype).label;
  if (type === 'resource') return subtype ? subtype.replace(/^AWS::/, '').replace(/::/g, ' ') : 'Resource';
  return subtype ? titleCaseEnum(subtype) : 'Unresolved principal';
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
