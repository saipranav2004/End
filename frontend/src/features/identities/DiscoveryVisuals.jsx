import { useEffect, useState } from 'react';
import { Check, TriangleAlert } from 'lucide-react';
import { formatDateTime, formatNumber, formatRelative, parseDate, titleCaseEnum } from '../../lib/format';
import { DetailList, DetailRow, SectionLabel } from '../../ui/Panel';
import { TONE_VAR } from '../../ui/cn';

/** "3 days", "5 hours": the gap between two moments, in the biggest unit that fits. */
function gapText(from, to) {
  const minutes = Math.max(0, Math.round((to - from) / 60000));
  if (minutes < 1) return 'moments';
  if (minutes < 90) return `${minutes} minute${minutes === 1 ? '' : 's'}`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours} hours`;
  const days = Math.round(hours / 24);
  if (days < 60) return `${days} days`;
  const months = Math.round(days / 30);
  return months < 24 ? `${months} months` : `${Math.round(days / 365)} years`;
}

/** Grows from empty on first paint (skipped when the viewer prefers reduced motion). */
function useGrown(target) {
  const [value, setValue] = useState(0);
  useEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setValue(target);
      return undefined;
    }
    const frame = requestAnimationFrame(() => setValue(target));
    return () => cancelAnimationFrame(frame);
  }, [target]);
  return value;
}

/* ── How it was found ───────────────────────────────────────────────────── */

const MOMENTS = [
  { key: 'created_at_aws', label: 'Created in AWS', hint: 'The day it was set up' },
  { key: 'first_seen_at', label: 'First found by us', hint: 'Our first discovery scan that saw it' },
  { key: 'last_seen_at', label: 'Last seen', hint: 'The most recent scan that still saw it' },
];

/**
 * The life of an identity as a vertical rail: when it was created, when we first
 * saw it, when we last saw it, with the waiting time between each step.
 */
function Journey({ identity }) {
  const steps = MOMENTS.map((moment) => ({ ...moment, date: parseDate(identity[moment.key]) })).filter((step) => step.date);
  if (steps.length === 0) return <p className="text-[12.5px] text-ink-3">No dates were recorded for this identity.</p>;
  const lastIndex = steps.length - 1;
  const stillSeen = steps[lastIndex].key === 'last_seen_at' && Date.now() - steps[lastIndex].date < 36 * 3600 * 1000;

  return (
    <ol className="flex flex-col">
      {steps.map((step, index) => {
        const isLast = index === lastIndex;
        const wait = index > 0 ? gapText(steps[index - 1].date, step.date) : null;
        return (
          <li key={step.key} className="relative flex gap-3 pb-5 last:pb-0">
            {!isLast && <span aria-hidden="true" className="absolute top-4 bottom-0 left-[7px] w-0.5 rounded-full bg-gradient-to-b from-brand to-brand/25" />}
            <span aria-hidden="true" className="relative mt-0.5 grid size-4 shrink-0 place-items-center rounded-full border-2 border-brand bg-surface">
              <span className={`size-1.5 rounded-full bg-brand ${isLast && stillSeen ? 'motion-safe:animate-pulse' : ''}`} />
            </span>
            <div className="min-w-0">
              <p className="text-[13px] font-medium text-ink">{step.label}</p>
              <p className="text-[12.5px] text-ink-2" title={formatDateTime(step.date)}>
                {formatRelative(step.date)}
                <span className="text-ink-3"> · {formatDateTime(step.date)}</span>
              </p>
              <p className="text-[11.5px] text-ink-3">{wait ? `${wait} after the step above` : step.hint}</p>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function SourceChips({ label, children }) {
  return (
    <div>
      <p className="text-[11.5px] text-ink-3">{label}</p>
      <div className="mt-1 flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
}

function Chip({ children }) {
  return <span className="inline-flex max-w-full items-center rounded-full border border-line bg-surface-2 px-2.5 py-0.5 text-[12px] text-ink-2">{children}</span>;
}

export function HowItWasFound({ identity, discovery, compact }) {
  const sources = String(discovery.data_sources ?? '')
    .split('+')
    .map((part) => part.trim())
    .filter(Boolean);
  return (
    <section aria-label="How it was found" className="rounded-[var(--radius-control)] border border-line p-4">
      <SectionLabel>How it was found</SectionLabel>
      <div className={`mt-3 grid gap-x-8 gap-y-5 ${compact ? '' : 'sm:grid-cols-2'}`}>
        <Journey identity={identity} />
        <div className="flex flex-col gap-3.5">
          <SourceChips label="Found by">
            <Chip>{discovery.discovery_source || 'Not recorded'}</Chip>
          </SourceChips>
          <SourceChips label="Information comes from">
            {sources.length === 0 ? <Chip>Not recorded</Chip> : sources.map((source) => <Chip key={source}>{source}</Chip>)}
          </SourceChips>
          <div>
            <p className="text-[11.5px] text-ink-3">How its credential was matched</p>
            <p className="mt-1 text-[12.5px] text-ink-2">{discovery.credential_resolution || 'Not recorded'}</p>
          </div>
        </div>
      </div>
    </section>
  );
}

/* ── Root account: safety checks and limits ─────────────────────────────── */

/** A ring that fills to the share of an account limit that is used. */
function Ring({ label, used, limit, unit }) {
  const share = limit > 0 ? Math.min(1, used / limit) : 0;
  const grown = useGrown(share);
  const tone = share >= 0.9 ? 'high' : share >= 0.7 ? 'medium' : 'brand';
  const radius = 26;
  const circumference = 2 * Math.PI * radius;
  const arc = used > 0 ? Math.max(grown, 0.03) : 0;
  const percent = limit > 0 ? (used / limit) * 100 : 0;
  return (
    <li className="flex items-center gap-3 rounded-[var(--radius-control)] border border-line bg-surface-2 px-3 py-2.5">
      <svg viewBox="0 0 64 64" className="size-14 shrink-0" role="img" aria-label={`${label}: ${used} of ${limit} ${unit} used`}>
        <circle cx="32" cy="32" r={radius} fill="none" stroke="var(--t-track)" strokeWidth="7" />
        <circle
          cx="32"
          cy="32"
          r={radius}
          fill="none"
          transform="rotate(-90 32 32)"
          stroke={TONE_VAR[tone]}
          strokeWidth="7"
          strokeLinecap="round"
          strokeDasharray={`${arc * circumference} ${circumference}`}
          style={{ transition: 'stroke-dasharray 900ms var(--ease-out-quint)' }}
        />
        <text x="32" y="32" textAnchor="middle" dominantBaseline="central" className="fill-ink text-[15px] font-semibold" style={{ fontVariantNumeric: 'tabular-nums' }}>
          {percent > 0 && percent < 1 ? '<1' : Math.round(percent)}%
        </text>
      </svg>
      <div className="min-w-0">
        <p className="text-[13px] font-medium text-ink">{label}</p>
        <p className="text-[12px] text-ink-3">
          {formatNumber(used)} of {formatNumber(limit)} {unit}
        </p>
      </div>
    </li>
  );
}

const LIMITS = [
  { used: 'Roles', limit: 'RolesQuota', label: 'Roles', unit: 'allowed' },
  { used: 'Users', limit: 'UsersQuota', label: 'Users', unit: 'allowed' },
  { used: 'Groups', limit: 'GroupsQuota', label: 'Groups', unit: 'allowed' },
  { used: 'Policies', limit: 'PoliciesQuota', label: 'Policies', unit: 'allowed' },
  { used: 'InstanceProfiles', limit: 'InstanceProfilesQuota', label: 'Instance profiles', unit: 'allowed' },
  { used: 'ServerCertificates', limit: 'ServerCertificatesQuota', label: 'Server certificates', unit: 'allowed' },
  { used: 'PolicyVersionsInUse', limit: 'PolicyVersionsInUseQuota', label: 'Policy versions in use', unit: 'allowed' },
  { used: 'MFADevicesInUse', limit: 'MFADevices', label: 'MFA devices in use', unit: 'devices' },
];

const SHOWN_AS_RING = new Set(LIMITS.flatMap((entry) => [entry.used, entry.limit]));

/** Each safety check is "good" when the AWS value is the one we want. */
function safetyChecks(attributes, summary) {
  const flag = (value) => Number(value) === 1;
  const checks = [];
  if ('account_mfa_enabled' in attributes) {
    checks.push({ good: flag(attributes.account_mfa_enabled), ok: 'Root sign-in is protected with MFA', bad: 'Root sign-in has no MFA: turn it on' });
  }
  if ('account_access_keys_present' in attributes) {
    const count = Number(attributes.account_access_keys_present);
    checks.push({ good: count === 0, ok: 'The root account has no access keys', bad: `The root account has ${formatNumber(count)} access key${count === 1 ? '' : 's'}: delete them` });
  }
  if ('account_signing_certificates_present' in attributes) {
    const count = Number(attributes.account_signing_certificates_present);
    checks.push({ good: count === 0, ok: 'No signing certificates on the root account', bad: `${formatNumber(count)} signing certificate${count === 1 ? '' : 's'} on the root account` });
  }
  if ('AccountPasswordPresent' in summary) {
    checks.push({ good: true, neutral: true, ok: flag(summary.AccountPasswordPresent) ? 'The root account has a password set' : 'The root account has no password', bad: '' });
  }
  return checks;
}

export const ROOT_SAFETY_KEYS = ['account_mfa_enabled', 'account_access_keys_present', 'account_signing_certificates_present', 'full_summary'];

export function hasAccountSummary(attributes) {
  return Boolean(attributes && typeof attributes.full_summary === 'object' && attributes.full_summary);
}

/** The root account's numbers as safety checks plus rings of how full each limit is. */
export function AccountHealth({ attributes }) {
  const summary = attributes.full_summary;
  const checks = safetyChecks(attributes, summary);
  const judged = checks.filter((check) => !check.neutral);
  const passed = judged.filter((check) => check.good).length;
  const failed = judged.length - passed;
  const rings = LIMITS.filter((entry) => Number(summary[entry.limit]) > 0 && entry.used in summary);
  const rest = Object.entries(summary).filter(([key]) => !SHOWN_AS_RING.has(key) && key !== 'AccountPasswordPresent');
  return (
    <section aria-label="Account safety and limits" className="rounded-[var(--radius-control)] border border-line p-4">
      <SectionLabel>Account safety and limits</SectionLabel>
      <div className="mt-3 grid gap-x-8 gap-y-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.6fr)]">
        <div>
          <p className="mb-2 text-[12.5px] font-medium text-ink">Safety checks</p>
          <p className={`mb-3 flex items-baseline gap-2 ${failed === 0 ? 'text-low' : 'text-high'}`}>
            <span className="text-[28px] leading-none font-semibold" style={{ fontVariantNumeric: 'tabular-nums' }}>{passed}/{judged.length}</span>
            <span className="text-[12.5px]">{failed === 0 ? 'checks passed' : `check${failed === 1 ? '' : 's'} need attention`}</span>
          </p>
          <ul className="flex flex-col gap-2">
            {checks.map((check) => (
              <li key={check.ok} className="flex items-start gap-2 text-[12.5px] text-ink-2">
                {check.good ? (
                  <Check className={`mt-px size-4 shrink-0 ${check.neutral ? 'text-ink-3' : 'text-low'}`} aria-hidden="true" />
                ) : (
                  <TriangleAlert className="mt-px size-4 shrink-0 text-high" aria-hidden="true" />
                )}
                <span className={check.good ? '' : 'font-medium text-ink'}>{check.good ? check.ok : check.bad}</span>
              </li>
            ))}
          </ul>
        </div>
        <div>
          <p className="mb-2 text-[12.5px] font-medium text-ink">How much of each AWS limit is used</p>
          <ul className="grid gap-2.5 sm:grid-cols-2">
            {rings.map((entry) => (
              <Ring key={entry.used} label={entry.label} used={Number(summary[entry.used])} limit={Number(summary[entry.limit])} unit={entry.unit} />
            ))}
          </ul>
        </div>
      </div>
      {rest.length > 0 && (
        <details className="mt-4 text-[12.5px]">
          <summary className="cursor-pointer text-ink-3 hover:text-ink">All other account numbers ({rest.length})</summary>
          <DetailList className="mt-1">
            {rest.map(([key, value]) => (
              <DetailRow key={key} label={titleCaseEnum(key)} mono>
                {String(value)}
              </DetailRow>
            ))}
          </DetailList>
        </details>
      )}
    </section>
  );
}
