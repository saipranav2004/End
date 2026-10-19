/**
 * Identity posture vocabulary: the six pillars, what a failed check costs,
 * and the bands and grades a score falls into. Shared by the screens and the
 * data layer so a band or grade means the same thing everywhere.
 */

export const PILLARS = {
  least_privilege: { label: 'Least privilege', summary: 'Holds only the access it uses.' },
  credential_hygiene: { label: 'Credential hygiene', summary: 'Keys are rotated, short-lived and never left expired.' },
  trust_access: { label: 'Trust & access', summary: 'Who can sign in as it or assume it, and on what terms.' },
  escalation: { label: 'Escalation', summary: 'Cannot widen its own access.' },
  exposure: { label: 'Exposure', summary: 'Its credentials are used only by it, from where they should be.' },
  lifecycle: { label: 'Lifecycle', summary: 'Has an owner and is still in use.' },
};
export const PILLAR_ORDER = Object.keys(PILLARS);

/* Service-wide managed policies: every action on the service, or every secret. */
export const BROAD_POLICIES = ['AmazonDynamoDBFullAccess', 'AmazonSQSFullAccess', 'SecretsManagerReadWrite'];

/** Points a failed check costs, by severity. */
export const CHECK_WEIGHTS = { CRITICAL: 25, HIGH: 15, MEDIUM: 8, LOW: 3 };

export const BANDS = [
  { key: 'healthy', label: 'Healthy', min: 80, tone: 'low' },
  { key: 'fair', label: 'Fair', min: 60, tone: 'medium' },
  { key: 'poor', label: 'Poor', min: 40, tone: 'high' },
  { key: 'critical', label: 'Critical', min: 0, tone: 'critical' },
];

export function bandFor(score) {
  return BANDS.find((band) => score >= band.min) ?? BANDS[BANDS.length - 1];
}

/* Grades sit inside the bands, so a grade and a band never disagree:
   A and B are Healthy, C is Fair, D is Poor, F is Critical. */
export function gradeFor(score) {
  if (score >= 90) return 'A';
  if (score >= 80) return 'B';
  if (score >= 60) return 'C';
  if (score >= 40) return 'D';
  return 'F';
}

export const PILLAR_STATUS = {
  pass: { label: 'No issues', tone: 'low' },
  warn: { label: 'Minor issues', tone: 'medium' },
  fail: { label: 'Serious issues', tone: 'critical' },
  na: { label: 'Not applicable', tone: 'neutral' },
};

export function bandMeta(key) {
  return BANDS.find((band) => band.key === key) ?? BANDS[BANDS.length - 1];
}

/* Compliance references as people write them: "NIST-AC-6(1)" reads as
   "NIST 800-53 AC-6(1)", "SOC2-CC6.3" as "SOC 2 CC6.3". */
const FRAMEWORKS = [
  ['NIST-', 'NIST 800-53 '],
  ['SOC2-', 'SOC 2 '],
  ['CIS-AWS-', 'CIS AWS '],
  ['PCI-DSS-', 'PCI DSS '],
  ['ISO27001-', 'ISO 27001 '],
];

export function complianceLabel(code) {
  const text = String(code ?? '');
  for (const [prefix, name] of FRAMEWORKS) {
    if (text.toUpperCase().startsWith(prefix)) return name + text.slice(prefix.length);
  }
  return text;
}
