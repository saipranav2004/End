/**
 * The AWS connection, as data: the documents the client pastes and the
 * checks the API runs. Every value comes from `GET /api/v1/integrations/aws`,
 * so what the page tells the client to type is exactly what the API verifies.
 */

export const AWS_CONSOLE = {
  iamRoles: 'https://console.aws.amazon.com/iam/home#/roles',
  eventBridgeRules: 'https://console.aws.amazon.com/events/home#/rules',
  secretsManager: 'https://console.aws.amazon.com/secretsmanager/listsecrets',
  codeCommit: 'https://console.aws.amazon.com/codesuite/codecommit/repositories',
};

export const GITHUB_NEW_TOKEN = 'https://github.com/settings/personal-access-tokens/new';

/** The trust policy for "Edit trust relationship", as an alternative to the clicks. */
export function trustPolicyJson(setup) {
  return JSON.stringify(
    {
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Principal: { AWS: setup.assumerRoleArn },
          Action: 'sts:AssumeRole',
          Condition: { StringEquals: { 'sts:ExternalId': setup.externalId } },
        },
      ],
    },
    null,
    2,
  );
}

/** The EventBridge rule's custom event pattern. */
export function eventPatternJson(setup) {
  /* Four sources to a line: the same JSON as JSON.stringify, laid out to be
     read at a glance rather than one value per line. */
  const quoted = setup.eventSources.map((source) => JSON.stringify(source));
  const lines = [];
  for (let i = 0; i < quoted.length; i += 4) lines.push(quoted.slice(i, i + 4).join(', '));
  return `{\n  "source": [\n    ${lines.join(',\n    ')}\n  ]\n}`;
}

/** What a pasted role ARN should look like, with the client's account left open. */
export function exampleRoleArn(setup) {
  return `arn:aws:iam::<your-account-id>:role/${setup?.roleName || 'NHIPlatformCollector'}`;
}

const ROLE_ARN = /^arn:aws:iam::(\d{12}):role\/(?:[\x21-\x7E]+\/)?([\w+=,.@-]{1,64})$/;
const OTHER_IAM = /^arn:aws[\w-]*:iam::\d{12}:(user|group|policy|instance-profile|oidc-provider|saml-provider)\//;

/**
 * Checks a pasted role ARN before it is sent, with the same rules as the API
 * (`internal/awsconnect/arn.go`), so the common slips are caught as they are
 * typed. Returns '' when it looks right. The API checks again.
 */
export function roleArnProblem(value, platformAccountId) {
  const arn = String(value ?? '').trim();
  if (!arn) return 'Paste the ARN of the role you created.';
  const other = OTHER_IAM.exec(arn);
  if (other) return `That is an IAM ${other[1].replace('-', ' ')} ARN. Paste the role's ARN - it contains ":role/".`;
  const match = ROLE_ARN.exec(arn);
  if (!match) {
    if (arn.startsWith('arn:aws-')) return 'Only accounts in the standard AWS partition can be connected.';
    return 'That is not a role ARN. It looks like arn:aws:iam::123456789012:role/RoleName.';
  }
  if (platformAccountId && match[1] === platformAccountId) {
    return `That ARN is in our account (${platformAccountId}). Paste the ARN of the role you created in your own AWS account.`;
  }
  return '';
}

/** The account id inside a role ARN, or ''. */
export function accountOfRoleArn(value) {
  return ROLE_ARN.exec(String(value ?? '').trim())?.[1] ?? '';
}

/** The role's name (without its path) from a role ARN, or ''. */
export function roleNameOfArn(value) {
  return ROLE_ARN.exec(String(value ?? '').trim())?.[2] ?? '';
}

/**
 * The three connection checks, in the order the API runs them, and the setup
 * step that fixes each one - so a failed check can open the step it points at.
 */
export const AWS_CHECKS = [
  { key: 'assume_role', label: 'Assume your role', step: 1, proves: 'sts:AssumeRole with your External ID' },
  { key: 'read_access', label: 'Read your account', step: 1, proves: 'One read: iam:ListRoles' },
  { key: 'events', label: 'Receive your events', step: 2, proves: 'An event from your account reached our bus' },
];

/** Workspace-level state, from `status` on the integration. */
export const AWS_STATUS = {
  not_connected: { label: 'Not connected', tone: 'neutral' },
  action_needed: { label: 'Needs attention', tone: 'medium' },
  connected: { label: 'Connected', tone: 'low' },
};

/** One account's state, from `account_status`. */
export const ACCOUNT_STATUS = {
  pending: { label: 'Setting up', tone: 'info' },
  action_needed: { label: 'Needs attention', tone: 'medium' },
  connected: { label: 'Connected', tone: 'low' },
};

/**
 * The whole setup as Markdown, with this workspace's values filled in - for
 * the common case where the admin connecting the account is not the person
 * with access to that AWS account.
 */
export function setupGuideMarkdown(setup, regions = []) {
  const ruleRegions = regions.length
    ? `Create this rule in each of these regions: ${regions.map((code) => `\`${code}\``).join(', ')}${regions.includes('us-east-1') ? '' : ', and in `us-east-1` for IAM events'}.`
    : 'Create this rule in every region you use, and in us-east-1 for IAM events.';
  return `# Connect AWS to the NHI console

Two read-only changes in the AWS account you want to connect. About 10 minutes.

| Value | |
|---|---|
| Our account ID | \`${setup.accountId}\` |
| External ID | \`${setup.externalId}\` (unique to this workspace; keep it within your team) |
| Event bus ARN | \`${setup.eventBusArn}\` |
| Region | \`${setup.region}\` |

## Step 1 - Create the read-only IAM role

1. IAM → Roles → **Create role**.
2. Trusted entity: **AWS account** → **Another AWS account**.
3. Account ID: \`${setup.accountId}\`
4. Tick **Require external ID** and paste \`${setup.externalId}\`
5. Attach the AWS-managed policies: ${setup.managedPolicies.map((policy) => `\`${policy}\``).join(' and ')}
6. Role name: \`${setup.roleName}\` → **Create role**.
7. Open the role and copy its **ARN**. Send it back to whoever is connecting the account.

Alternatively, paste this trust policy under **Trust relationships → Edit trust policy**:

\`\`\`json
${trustPolicyJson(setup)}
\`\`\`

## Step 2 - Forward CloudTrail events

1. EventBridge → Rules → **Create rule**.
2. Name: \`${setup.ruleName}\`. Event bus: **default**. Rule type: **Rule with an event pattern**.
3. Event pattern → **Custom pattern (JSON)**:

\`\`\`json
${eventPatternJson(setup)}
\`\`\`

4. Target: **EventBridge event bus in a different account or Region**.
5. Event bus ARN: \`${setup.eventBusArn}\`
6. Keep **Create a new role for this specific resource** → **Create rule**.

${ruleRegions}

## Step 3 - Verify

On the console's Integrations page, paste the role ARN, choose the regions to read${regions.length ? ` (${regions.join(', ')})` : ''}, and choose **Verify connection**.
`;
}

/** The three setup steps: what each produces and roughly how long it takes. */
export const SETUP_STEPS = [
  { number: 1, title: 'Create the read-only IAM role', where: 'IAM console', minutes: 4 },
  { number: 2, title: 'Forward CloudTrail events', where: 'EventBridge console', minutes: 4 },
  { number: 3, title: 'Verify the connection', where: 'This page', minutes: 1 },
];

/* ── Exposed credentials ────────────────────────────────────────────────── */

/** The checks the API runs when a platform is switched on, per platform. */
export const EXPOSURE_CHECKS = {
  codecommit: [
    { key: 'codecommit_list', label: 'List your repositories', proves: 'codecommit:ListRepositories in your regions' },
    { key: 'codecommit_read', label: 'Read repository contents', proves: 'codecommit:GetFolder on one repository' },
  ],
  github: [
    { key: 'github_secret', label: 'Read the token secret', proves: 'secretsmanager:GetSecretValue with the role' },
    { key: 'github_token', label: 'GitHub accepts the token', proves: 'Lists repositories and reads one' },
  ],
};

export const EXPOSURE_PLATFORMS = {
  github: { key: 'github', name: 'GitHub', summary: 'Repositories a read-only GitHub token can see.' },
  codecommit: { key: 'codecommit', name: 'AWS CodeCommit', summary: 'Repositories in this AWS account, in the regions it reads.' },
};

const SECRET_ARN = /^arn:(aws[a-z-]*):secretsmanager:([a-z]{2}(-[a-z]+)+-\d):(\d{12}):secret:([A-Za-z0-9/_+=.@-]{1,512})$/;

/**
 * Checks a pasted secret ARN with the API's rules
 * (`awsconnect.ParseSecretARN`), so a slip is named before the round trip.
 */
export function secretArnProblem(value, accountId, prefix) {
  const arn = String(value ?? '').trim();
  if (!arn) return 'Paste the ARN of the secret that holds your GitHub token.';
  const match = SECRET_ARN.exec(arn);
  if (!match) {
    if (/^arn:aws[\w-]*:secretsmanager:/.test(arn) && !arn.includes(':secret:')) {
      return 'That is not a secret ARN. Copy the "Secret ARN" from the secret\'s page.';
    }
    return 'That is not a Secrets Manager secret ARN. It looks like arn:aws:secretsmanager:<region>:<account>:secret:<name>.';
  }
  if (accountId && match[4] !== accountId) {
    return `The secret is in account ${match[4]}; store it in ${accountId}, the account this role belongs to.`;
  }
  if (prefix && !match[5].startsWith(prefix)) {
    return `Name the secret starting with ${prefix} - the read permission is limited to that name.`;
  }
  return '';
}

/** The secret name the setup suggests. */
export const suggestedSecretName = (prefix) => `${prefix || 'nhi/github'}/token`;

/**
 * Inline policy with the four CodeCommit reads the secret scanner uses. The
 * resources are the repositories of the regions the account reads (one ARN
 * pattern per region); narrow them to named repositories to grant less.
 */
export function codeCommitPolicyJson(setup, accountId, regions = []) {
  const list = regions.length > 0 ? regions : [setup.region || '<region>'];
  return JSON.stringify(
    {
      Version: '2012-10-17',
      Statement: [
        {
          Sid: 'SecretScannerReadCode',
          Effect: 'Allow',
          Action: setup.exposure.codecommit.actions,
          Resource: list.map((region) => `arn:aws:codecommit:${region}:${accountId}:*`),
        },
      ],
    },
    null,
    2,
  );
}

/**
 * Inline policy letting the role read only the token secret. The resource is
 * the name prefix rather than one ARN, so replacing the secret later keeps
 * working.
 */
export function githubSecretPolicyJson(setup, accountId) {
  return JSON.stringify(
    {
      Version: '2012-10-17',
      Statement: [
        {
          Sid: 'NHIExposedCredentialsGitHubToken',
          Effect: 'Allow',
          Action: setup.exposure.github.actions,
          Resource: `arn:aws:secretsmanager:*:${accountId}:secret:${setup.exposure.github.secretPrefix}*`,
        },
      ],
    },
    null,
    2,
  );
}

/** Only when the secret is encrypted with a customer managed KMS key. */
export function kmsDecryptPolicyJson(accountId) {
  return JSON.stringify(
    {
      Version: '2012-10-17',
      Statement: [
        {
          Sid: 'NHIExposedCredentialsDecryptToken',
          Effect: 'Allow',
          Action: 'kms:Decrypt',
          Resource: `arn:aws:kms:<region>:${accountId}:key/<key-id>`,
          Condition: { StringLike: { 'kms:ViaService': 'secretsmanager.*.amazonaws.com' } },
        },
      ],
    },
    null,
    2,
  );
}

/** Save the policy above as codecommit-read.json, then add it to the role. */
export function codeCommitCli(roleName) {
  return `aws iam put-role-policy \\
  --role-name ${roleName} \\
  --policy-name SecretScannerReadCode \\
  --policy-document file://codecommit-read.json`;
}

/**
 * Store the token and grant the read, from a terminal signed in to the
 * client's account. `read -s` keeps the token out of shell history and off
 * the screen.
 */
export function githubCli(setup, accountId, roleName, region) {
  const name = suggestedSecretName(setup.exposure.github.secretPrefix);
  return `# 1. Store the token (typed, not echoed, not kept in history)
read -rs GITHUB_TOKEN
aws secretsmanager create-secret \\
  --region ${region} \\
  --name ${name} \\
  --secret-string "$GITHUB_TOKEN" \\
  --query ARN --output text
unset GITHUB_TOKEN

# 2. Let the role read only that secret
cat > nhi-github-secret-read.json <<'EOF'
${githubSecretPolicyJson(setup, accountId)}
EOF
aws iam put-role-policy \\
  --role-name ${roleName} \\
  --policy-name nhi-github-secret-read \\
  --policy-document file://nhi-github-secret-read.json`;
}
