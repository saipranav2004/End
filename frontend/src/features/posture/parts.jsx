import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-react';
import { bandFor, bandMeta, PILLAR_STATUS, PILLARS } from '../../lib/posture';
import { titleCaseEnum } from '../../lib/format';
import { Meter } from '../../ui/Meter';
import { Tag } from '../../ui/Tag';
import { cn, TONE_VAR } from '../../ui/cn';

/**
 * A score as the posture service gives it (a float, or null when the identity
 * has no score yet), printed to at most one decimal.
 */
export function formatScore(value) {
  const n = Number(value);
  if (value === null || value === undefined || !Number.isFinite(n)) return '-';
  return String(Math.round(n * 10) / 10);
}

/**
 * The band to show: the service's own (HEALTHY | FAIR | POOR | CRITICAL) when
 * it sent one, otherwise the band the score falls in.
 */
export function resolveBand({ band, score } = {}) {
  const key = String(band ?? '').trim().toLowerCase();
  if (key) return bandMeta(key);
  const n = Number(score);
  return score === null || score === undefined || !Number.isFinite(n) ? null : bandFor(n);
}

/** Identity type for display: `ec2_instance` -> `EC2 instance`. */
export function identityTypeLabel(type) {
  return titleCaseEnum(type).replace(/\b(Ec2|Ecs|Eks|Rds|Sqs|Sns|Kms)\b/gi, (word) => word.toUpperCase());
}

/** Pillar display name: the catalog's name from the service, else the console's, else the code. */
export function pillarLabel(code, names) {
  if (!code) return '-';
  return names?.[code] ?? PILLARS[code]?.label ?? String(code).replace(/[_-]+/g, ' ').replace(/^./, (c) => c.toUpperCase());
}

/**
 * A score out of 100 as a ring, coloured by its band. The number is the
 * reading; the ring is how much of 100 is left, so two rings side by side
 * compare at a glance without reading either number. Small rings print the
 * whole number; the label carries the exact score.
 */
export function ScoreRing({ score, band, size = 44, stroke, grade, label, className }) {
  const numeric = Number(score);
  const scored = score !== null && score !== undefined && Number.isFinite(numeric);
  const meta = resolveBand({ band, score });
  const width = stroke ?? Math.max(3, Math.round(size / 11));
  const radius = (size - width) / 2;
  const circumference = 2 * Math.PI * radius;
  const filled = scored ? (Math.max(0, Math.min(100, numeric)) / 100) * circumference : 0;
  const large = size >= 96;
  const shown = !scored ? '-' : size >= 56 ? formatScore(numeric) : String(Math.round(numeric));
  return (
    <span
      className={cn('relative inline-grid shrink-0 place-items-center', className)}
      style={{ width: size, height: size }}
      role="img"
      aria-label={
        label ??
        (scored
          ? `Posture score ${formatScore(numeric)} of 100${meta ? `, ${meta.label}` : ''}${grade ? `, grade ${grade}` : ''}`
          : 'Not scored')
      }
    >
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true" className="-rotate-90">
        <circle cx={size / 2} cy={size / 2} r={radius} fill="none" stroke="var(--t-track)" strokeWidth={width} />
        {scored && (
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            stroke={TONE_VAR[meta?.tone ?? 'neutral']}
            strokeWidth={width}
            strokeLinecap="round"
            strokeDasharray={`${filled} ${circumference}`}
            className="transition-[stroke-dasharray] duration-[900ms] ease-[var(--ease-out-quint)]"
          />
        )}
      </svg>
      <span aria-hidden="true" className="absolute inset-0 grid place-items-center text-center leading-none">
        <span>
          <span
            data-numeric=""
            className={cn(
              'block font-display font-extrabold tracking-[-0.03em] text-ink',
              large ? 'text-[34px]' : size >= 56 ? 'text-[17px]' : 'text-[12.5px]',
            )}
          >
            {shown}
          </span>
          {large && <span className="mt-1 block text-[11px] font-medium text-ink-3">of 100{grade ? ` · ${grade}` : ''}</span>}
        </span>
      </span>
    </span>
  );
}

export function BandTag({ score, band, size = 'sm' }) {
  const meta = resolveBand({ band, score });
  if (!meta) {
    return (
      <Tag tone="neutral" size={size}>
        Not scored
      </Tag>
    );
  }
  return (
    <Tag tone={meta.tone} size={size} dot>
      {meta.label}
    </Tag>
  );
}

/** A change in score. Up is better here, the reverse of a risk score. */
export function Delta({ value, suffix = '', className, emptyLabel = 'New' }) {
  if (value === null || value === undefined || !Number.isFinite(Number(value))) {
    return <span className={cn('text-[12px] text-ink-3', className)}>{emptyLabel}</span>;
  }
  const n = Math.round(Number(value) * 10) / 10;
  const Icon = n > 0 ? ArrowUpRight : n < 0 ? ArrowDownRight : Minus;
  const tone = n > 0 ? 'text-low' : n < 0 ? 'text-critical' : 'text-ink-3';
  return (
    <span className={cn('inline-flex items-center gap-0.5 text-[12px] font-semibold', tone, className)} data-numeric="">
      <Icon aria-hidden="true" className="size-3.5" />
      {n > 0 ? `+${n}` : n}
      {suffix}
      <span className="sr-only">{n > 0 ? ' better' : n < 0 ? ' worse' : ' unchanged'}</span>
    </span>
  );
}

export function PillarStatusTag({ status }) {
  const meta = PILLAR_STATUS[String(status ?? '').toLowerCase()] ?? PILLAR_STATUS.na;
  return (
    <Tag tone={meta.tone} size="sm">
      {meta.label}
    </Tag>
  );
}

/**
 * One row per pillar: name, a meter of its score, the score and its status.
 * Accepts the service's shape ({ pillar, name, score, status: PASS|WARN|FAIL })
 * as well as { key, label }.
 */
export function PillarBars({ pillars, detail }) {
  return (
    <ul className="flex flex-col gap-3">
      {pillars.map((pillar) => {
        const key = pillar.key ?? pillar.pillar;
        const name = pillar.label ?? pillar.name ?? pillarLabel(key);
        const status = String(pillar.status ?? '').toLowerCase();
        const hasScore = pillar.score !== null && pillar.score !== undefined;
        return (
          <li key={key} className="grid grid-cols-[minmax(0,9.5rem)_minmax(0,1fr)_2rem_6.75rem] items-center gap-3">
            <span className="min-w-0">
              <span className="block truncate text-[12.5px] font-medium text-ink" title={PILLARS[key]?.summary ?? name}>
                {name}
              </span>
              {detail && <span className="block truncate text-[11px] text-ink-3">{detail(pillar)}</span>}
            </span>
            {!hasScore ? (
              <span className="text-[11.5px] text-ink-3">Not evaluated</span>
            ) : (
              <Meter value={pillar.score} tone={PILLAR_STATUS[status]?.tone ?? 'brand'} label={`${name} ${formatScore(pillar.score)} of 100`} />
            )}
            <span data-numeric="" className="text-right text-[12.5px] font-semibold text-ink">
              {formatScore(pillar.score)}
            </span>
            <span className="justify-self-end">
              <PillarStatusTag status={status || (hasScore ? undefined : 'na')} />
            </span>
          </li>
        );
      })}
    </ul>
  );
}
