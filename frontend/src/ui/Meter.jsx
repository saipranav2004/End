import { useEffect, useRef, useState } from 'react';
import { cn, TONE_BG } from './cn';

/** Grows from 0 on first paint so a bar reads as a measurement being taken. */
function useGrow(target) {
  const [width, setWidth] = useState(0);
  const raf = useRef(0);
  useEffect(() => {
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduced) {
      setWidth(target);
      return undefined;
    }
    raf.current = requestAnimationFrame(() => setWidth(target));
    return () => cancelAnimationFrame(raf.current);
  }, [target]);
  return width;
}

export function Meter({ value, tone = 'brand', height = 6, className, label }) {
  const pct = Math.max(0, Math.min(100, Number(value) || 0));
  const width = useGrow(pct);

  return (
    <div
      className={cn('w-full overflow-hidden rounded-full bg-track', className)}
      style={{ height }}
      role="meter"
      aria-valuenow={Math.round(pct)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={label}
    >
      <div
        className={cn('h-full rounded-full transition-[width] duration-[900ms] ease-[var(--ease-out-quint)]', TONE_BG[tone])}
        style={{ width: `${width}%` }}
      />
    </div>
  );
}

/**
 * Stacked composition bar. `segments` is [{ key, label, value, color }].
 * Hovering a segment raises its contrast; clicking drills through.
 */
export function ProportionBar({ segments = [], total, height = 10, onSelect, className, ariaLabel }) {
  const sum = Number(total) || segments.reduce((acc, seg) => acc + (Number(seg.value) || 0), 0);
  const [grown, setGrown] = useState(false);

  useEffect(() => {
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduced) {
      setGrown(true);
      return undefined;
    }
    const id = requestAnimationFrame(() => setGrown(true));
    return () => cancelAnimationFrame(id);
  }, [segments]);

  if (sum <= 0) {
    return (
      <div
        className={cn('w-full rounded-full bg-track', className)}
        style={{ height }}
        aria-label={ariaLabel}
      />
    );
  }

  return (
    <div
      className={cn('flex w-full gap-[2px] overflow-hidden rounded-full bg-track', className)}
      style={{ height }}
      /* A picture when it only shows; a group of buttons when each segment
         filters - an image cannot contain controls. */
      role={onSelect ? 'group' : 'img'}
      aria-label={
        ariaLabel ||
        segments.map((seg) => `${seg.label}: ${seg.value}`).join(', ')
      }
    >
      {segments.map((segment) => {
        const pct = ((Number(segment.value) || 0) / sum) * 100;
        if (pct <= 0) return null;
        const Element = onSelect ? 'button' : 'div';
        return (
          <Element
            key={segment.key}
            type={onSelect ? 'button' : undefined}
            onClick={onSelect ? () => onSelect(segment) : undefined}
            title={`${segment.label} · ${segment.value} (${pct.toFixed(1)}%)`}
            aria-label={onSelect ? `Filter by ${segment.label}` : undefined}
            className={cn(
              'h-full first:rounded-l-full last:rounded-r-full transition-[width,filter] duration-[900ms] ease-[var(--ease-out-quint)]',
              onSelect && 'cursor-pointer hover:brightness-110 focus-visible:brightness-110',
            )}
            style={{ width: grown ? `${pct}%` : '0%', background: segment.color }}
          />
        );
      })}
    </div>
  );
}
