/* Display utilities, unprefixed. Two of these on one element is always a bug:
   they set the same property at the same specificity, so the winner is decided
   by the order Tailwind happened to emit them in rather than by the caller. */
const DISPLAY = new Set([
  'block',
  'inline-block',
  'inline',
  'flex',
  'inline-flex',
  'grid',
  'inline-grid',
  'hidden',
  'contents',
  'table',
  'flow-root',
]);

/**
 * Joins class names, with one conflict resolved: where a component's own base
 * class and a caller's `className` both set `display`, the caller wins.
 *
 * This is not a general Tailwind merge and is deliberately not one - the full
 * merge is a large dependency and a lot of behaviour to reason about. It
 * handles the single group where a collision is silent, indistinguishable from
 * working, and decided by stylesheet order: passing `hidden` to a component
 * whose base is `inline-flex` used to leave both classes on the element and
 * render it anyway.
 */
export function cn(...parts) {
  const classes = parts.flat(Infinity).filter(Boolean).join(' ').split(/\s+/).filter(Boolean);

  let lastDisplay = -1;
  for (let i = 0; i < classes.length; i += 1) {
    if (DISPLAY.has(classes[i])) lastDisplay = i;
  }
  if (lastDisplay === -1) return classes.join(' ');

  return classes.filter((name, index) => index === lastDisplay || !DISPLAY.has(name)).join(' ');
}

/** Shared tone palette: risk language stays identical across every surface. */
export const TONE_CLASSES = {
  critical: 'text-critical bg-critical-soft border-critical/25',
  high: 'text-high bg-high-soft border-high/25',
  medium: 'text-medium bg-medium-soft border-medium/25',
  low: 'text-low bg-low-soft border-low/25',
  info: 'text-info bg-info-soft border-info/25',
  brand: 'text-brand bg-info-soft border-brand/25',
  neutral: 'text-ink-2 bg-neutral-soft border-line-strong/60',
};

export const TONE_FG = {
  critical: 'text-critical',
  high: 'text-high',
  medium: 'text-medium',
  low: 'text-low',
  info: 'text-info',
  brand: 'text-brand',
  neutral: 'text-ink-3',
};

export const TONE_BG = {
  critical: 'bg-critical',
  high: 'bg-high',
  medium: 'bg-medium',
  low: 'bg-low',
  info: 'bg-info',
  brand: 'bg-brand',
  neutral: 'bg-neutral',
};

export const TONE_VAR = {
  critical: 'var(--t-critical)',
  high: 'var(--t-high)',
  medium: 'var(--t-medium)',
  low: 'var(--t-low)',
  info: 'var(--t-info)',
  brand: 'var(--t-brand)',
  neutral: 'var(--t-neutral)',
};
