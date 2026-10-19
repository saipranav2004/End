const DASH = '-';

const numberFmt = new Intl.NumberFormat(undefined);
const compactFmt = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 });

export function formatNumber(value) {
  if (!Number.isFinite(Number(value))) return DASH;
  return numberFmt.format(Number(value));
}

export function formatCompact(value) {
  if (!Number.isFinite(Number(value))) return DASH;
  const n = Number(value);
  return n >= 10000 ? compactFmt.format(n) : numberFmt.format(n);
}

export function formatPercent(part, whole, digits = 0) {
  const p = Number(part);
  const w = Number(whole);
  if (!Number.isFinite(p) || !Number.isFinite(w) || w <= 0) return DASH;
  return `${((p / w) * 100).toFixed(digits)}%`;
}

export function percentValue(part, whole) {
  const p = Number(part);
  const w = Number(whole);
  if (!Number.isFinite(p) || !Number.isFinite(w) || w <= 0) return 0;
  return Math.max(0, Math.min(100, (p / w) * 100));
}

/**
 * The Go API returns RFC3339. The Secret Scanner returns
 * "YYYY-MM-DD HH:MM:SS" in UTC with no zone marker - parsing that string
 * directly is treated as local time by most engines, which silently shifts
 * every timestamp. Normalise it explicitly.
 */
export function parseDate(input) {
  if (!input) return null;
  if (input instanceof Date) return Number.isNaN(input.getTime()) ? null : input;
  if (typeof input !== 'string') return null;

  let value = input.trim();
  if (!value || value.startsWith('0001-01-01')) return null;

  const naive = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(value);
  if (naive) value = `${value.replace(' ', 'T')}Z`;

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function formatDateTime(input) {
  const date = parseDate(input);
  if (!date) return DASH;
  return date.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function formatDate(input) {
  const date = parseDate(input);
  if (!date) return DASH;
  return date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: '2-digit' });
}

export function formatRelative(input) {
  const date = parseDate(input);
  if (!date) return DASH;

  const seconds = Math.round((Date.now() - date.getTime()) / 1000);
  const future = seconds < 0;
  const abs = Math.abs(seconds);

  const steps = [
    [60, 'second', 1],
    [3600, 'minute', 60],
    [86400, 'hour', 3600],
    [2592000, 'day', 86400],
    [31536000, 'month', 2592000],
    [Infinity, 'year', 31536000],
  ];

  for (const [limit, unit, divisor] of steps) {
    if (abs < limit) {
      const amount = Math.max(1, Math.round(abs / divisor));
      const plural = amount === 1 ? unit : `${unit}s`;
      return future ? `in ${amount} ${plural}` : `${amount} ${plural} ago`;
    }
  }
  return DASH;
}

/**
 * Compact relative time for table cells, where the long form ("3 months ago")
 * is wider than the column can honestly give it. The full value always stays
 * available as a title attribute at the call site.
 */
export function formatRelativeShort(input) {
  const date = parseDate(input);
  if (!date) return DASH;

  const seconds = Math.round((Date.now() - date.getTime()) / 1000);
  const abs = Math.abs(seconds);
  /* A future instant used to render as a bare "1h", which is not a shorter way
     of saying "in 1 hour" - it is indistinguishable from "1h ago" except by a
     missing suffix nobody reads as a signal. A clock skew or a bad timestamp
     then looked like ordinary data, and in a list sorted by time it produced
     an order that cannot happen: 1h, 55m, 16m, then 6m ago. It is prefixed
     now, so a future date is visible as one. */
  const future = seconds < 0;

  const steps = [
    [60, 1, 's'],
    [3600, 60, 'm'],
    [86400, 3600, 'h'],
    [2592000, 86400, 'd'],
    [31536000, 2592000, 'mo'],
    [Infinity, 31536000, 'y'],
  ];

  for (const [limit, divisor, unit] of steps) {
    if (abs < limit) {
      const amount = Math.max(1, Math.round(abs / divisor));
      return future ? `in ${amount}${unit}` : `${amount}${unit} ago`;
    }
  }
  return DASH;
}

export function daysSince(input) {
  const date = parseDate(input);
  if (!date) return null;
  return Math.floor((Date.now() - date.getTime()) / 86400000);
}

export function formatDuration(startInput, endInput) {
  const start = parseDate(startInput);
  const end = parseDate(endInput);
  if (!start || !end) return DASH;
  const seconds = Math.max(0, Math.round((end - start) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

/** `arn:aws:iam::1234:role/platform/deployer` -> `role/platform/deployer` */
export function arnResource(arn) {
  if (typeof arn !== 'string' || !arn) return DASH;
  const slash = arn.indexOf('/');
  if (slash !== -1) return arn.slice(arn.lastIndexOf(':', slash) + 1);
  const parts = arn.split(':');
  return parts[parts.length - 1] || arn;
}

export function arnAccount(arn) {
  if (typeof arn !== 'string') return DASH;
  const parts = arn.split(':');
  return parts[4] || DASH;
}

export function shortenMiddle(value, head = 22, tail = 16) {
  if (typeof value !== 'string') return DASH;
  if (value.length <= head + tail + 1) return value;
  return `${value.slice(0, head)}…${value.slice(-tail)}`;
}

/** Git author strings arrive as `Name <email>`. */
export function parseAuthor(author) {
  if (typeof author !== 'string' || !author.trim()) return { name: DASH, email: null };
  const match = author.match(/^\s*(.*?)\s*<([^>]+)>\s*$/);
  if (!match) return { name: author.trim(), email: null };
  return { name: match[1] || match[2], email: match[2] };
}

export function shortCommit(commitId) {
  if (typeof commitId !== 'string' || !commitId) return DASH;
  return commitId.slice(0, 7);
}

export function shortBranch(branch) {
  if (typeof branch !== 'string' || !branch) return DASH;
  return branch.replace(/^refs\/heads\//, '');
}

/** `generic_password_assignment` -> `Generic password assignment` */
export function humanizeToken(token) {
  if (typeof token !== 'string' || !token) return DASH;
  /* "aws_access_key" reads "AWS access key", not "Aws access key"; an
     all-caps token ("HIGH") reads "High". */
  const words = token
    .replace(/[_-]+/g, ' ')
    .trim()
    .split(/\s+/)
    .map((word) =>
      BRANDS[word.toLowerCase()] ?? (ACRONYMS.has(word.toUpperCase()) ? word.toUpperCase() : word === word.toUpperCase() ? word.toLowerCase() : word),
    );
  const text = words.join(' ');
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/**
 * Backend enums are SCREAMING_SNAKE. Title-casing them blindly yields "Iam
 * Role" and "Ssh Public Key", so known acronyms keep their casing.
 */
const BRANDS = { github: 'GitHub', gitlab: 'GitLab', codecommit: 'CodeCommit', pypi: 'PyPI', npm: 'npm', openai: 'OpenAI' };

const ACRONYMS = new Set([
  'IAM', 'AWS', 'ARN', 'MFA', 'SSH', 'API', 'SSO', 'SAML', 'OIDC', 'STS',
  'IAC', 'CICD', 'SAAS', 'NHI', 'SOC', 'URI', 'URL', 'ID', 'IP', 'PAT', 'JWT', 'GCP', 'SMTP',
]);

export function titleCaseEnum(value) {
  if (typeof value !== 'string' || !value) return DASH;
  return value
    .split(/[_\s]+/)
    .filter(Boolean)
    .map((word, index) => {
      const upper = word.toUpperCase();
      if (upper === 'CICD') return 'CI/CD';
      if (ACRONYMS.has(upper)) return upper;
      const lower = word.toLowerCase();
      return index === 0 ? lower.charAt(0).toUpperCase() + lower.slice(1) : lower;
    })
    .join(' ');
}

export function initialsOf(value) {
  if (typeof value !== 'string' || !value.trim()) return '-';
  return value
    .trim()
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0].toUpperCase())
    .join('');
}

export { DASH };

/* The permission feed sometimes names how a policy is attached rather than
   the policy itself. Show that as what it is, not as a policy called
   "attached". */
const POLICY_SOURCE_WORDS = {
  attached: 'Attached policy (name not reported)',
  managed: 'Managed policy (name not reported)',
  inline: 'Inline policy (name not reported)',
  unknown: 'Policy not reported',
};

export function policyName(value) {
  const text = String(value ?? '').trim();
  return POLICY_SOURCE_WORDS[text.toLowerCase()] ?? (text || 'Policy not reported');
}
