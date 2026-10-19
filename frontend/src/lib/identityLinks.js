/**
 * How one non-human identity is addressed by each service, and the console
 * routes that open it. Every page links through these so they agree.
 *
 *   discovery (main API)  `id` (uuid of the latest scan's row) + `arn`
 *   posture               `identity_id`, resolved by ARN (see lib/api/posture.js)
 *   genome                `(source_type, identity_name)`: the family below and
 *                         the canonical ARN (Lambda without version qualifier,
 *                         Step Functions as the state machine)
 */

/* Discovery identity_type -> genome source_type. Mirrors the genome service's
   monitoringFamilies; types it does not monitor (IAM users and plain roles,
   EKS, data stores) are absent, so no genome link is offered for them. */
const GENOME_FAMILIES = {
  ec2_instance: 'EC2',
  lambda_function: 'LAMBDA',
  ecs_task: 'ECS',
  ecs_service: 'ECS',
  app_runner_service: 'APP_RUNNER',
  elastic_beanstalk_environment: 'BEANSTALK',
  ssm_managed_node: 'SSM',
  ssm_automation: 'SSM',
  glue_job: 'GLUE',
  glue_crawler: 'GLUE',
  emr_cluster: 'EMR',
  batch_job: 'BATCH',
  dms_replication_instance: 'DMS',
  kinesis_firehose: 'KINESIS',
  kinesis_analytics_app: 'KINESIS',
  datasync_task: 'DATASYNC',
  sagemaker_notebook: 'SAGEMAKER',
  sagemaker_training_job: 'SAGEMAKER',
  sagemaker_processing_job: 'SAGEMAKER',
  sagemaker_endpoint: 'SAGEMAKER',
  bedrock_agent: 'BEDROCK',
  bedrock_knowledge_base: 'BEDROCK',
  codebuild_project: 'CODEBUILD',
  codepipeline: 'CODEPIPELINE',
  codedeploy_service_role: 'CODEDEPLOY',
  cloudformation_stack_role: 'CLOUDFORMATION',
  mwaa_environment: 'MWAA',
  github_actions_workflow: 'GITHUB_ACTIONS',
  gitlab_ci_job: 'GITLAB_CI',
  terraform_run: 'TERRAFORM',
  step_function: 'STEP_FUNCTIONS',
  eventbridge_scheduler: 'EVENTBRIDGE',
  synthetics_canary: 'SYNTHETICS',
  iot_role_alias: 'IOT',
  iot_thing: 'IOT',
  greengrass_core: 'IOT',
  transfer_server: 'TRANSFER',
  iam_roles_anywhere_session: 'IAM',
  saas_vendor_role: 'IAM',
  aws_service_linked_role: 'IAM',
};

/** The genome family for a discovery identity type, or null when it is not monitored. */
export function genomeSourceType(identityType) {
  const key = String(identityType ?? '')
    .trim()
    .toLowerCase()
    .replace(/[-\s]/g, '_');
  return GENOME_FAMILIES[key] ?? null;
}

/** The ARN the genome service keys an identity by. */
export function canonicalGenomeName(identityType, arn) {
  const value = String(arn ?? '').trim();
  const parts = value.split(':');
  if (parts.length < 6) return value;
  const prefix = parts.slice(0, 5).join(':');
  const seg = parts.slice(5);
  if (identityType === 'lambda_function' && seg[0] === 'function' && seg[1]) return `${prefix}:function:${seg[1]}`;
  if (identityType === 'step_function' && ['execution', 'express', 'stateMachine'].includes(seg[0]) && seg[1]) {
    return `${prefix}:stateMachine:${seg[1]}`;
  }
  return value;
}

/** Genome id string: `SOURCE_TYPE:identity_name`. */
export function genomeId(sourceType, identityName) {
  return `${sourceType}:${identityName}`;
}

/** Split a genome id back into its parts (the name itself contains colons). */
export function parseGenomeId(id) {
  const value = String(id ?? '');
  const at = value.indexOf(':');
  if (at <= 0) return null;
  return { sourceType: value.slice(0, at), identityName: value.slice(at + 1) };
}

/** Route of a genome identity page. */
export function genomeRoute(sourceType, identityName) {
  return `/genome/${encodeURIComponent(genomeId(sourceType, identityName))}`;
}

/** Route of the genome page for a discovery identity, or null when it is not monitored. */
export function genomeRouteFor({ identity_type: identityType, identityType: alt, arn } = {}) {
  const type = identityType ?? alt;
  const family = genomeSourceType(type);
  if (!family || !arn) return null;
  return genomeRoute(family, canonicalGenomeName(type, arn));
}

export const identityRoute = (id, tab) =>
  `/identities/${encodeURIComponent(id)}${tab ? `?tab=${encodeURIComponent(tab)}` : ''}`;

export const accessGraphRoute = (id) => `/access-graph?focus=${encodeURIComponent(id)}`;

export const accessDetailRoute = (id) => `/access-graph/${encodeURIComponent(id)}`;
