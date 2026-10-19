/*
 * Leaf-level diff of two JSON values, for the scan change details: the
 * scanner stores a changed attribute document as the whole before / after
 * JSON string, and people need the few keys that actually moved.
 */

/** A value the scanner stored as a JSON document string, parsed; else as is. */
export function parseJsonish(value) {
  if (typeof value !== 'string') return value;
  const text = value.trim();
  if (!/^[[{]/.test(text)) return value;
  try {
    return JSON.parse(text);
  } catch {
    return value;
  }
}

export const isPlainObject = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);

/** JSON with sorted keys, so key order never reads as a change. */
function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`;
  if (isPlainObject(value)) {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`)
      .join(',')}}`;
  }
  return JSON.stringify(value ?? null);
}

export const DIFF_LIMIT = 24;

/**
 * The leaves that differ between two JSON values, as { path, before, after }.
 * Objects are compared key by key and equal-length arrays item by item, so a
 * 2 KB attribute document that changed in one place shows that one place.
 */
export function diffValues(before, after, path = [], out = []) {
  if (out.length >= DIFF_LIMIT || canonical(before) === canonical(after)) return out;
  if (isPlainObject(before) && isPlainObject(after)) {
    for (const key of [...new Set([...Object.keys(before), ...Object.keys(after)])].sort()) {
      diffValues(before[key], after[key], [...path, key], out);
    }
    return out;
  }
  if (Array.isArray(before) && Array.isArray(after) && before.length === after.length) {
    before.forEach((item, index) => diffValues(item, after[index], [...path, index], out));
    return out;
  }
  out.push({ path, before, after });
  return out;
}
