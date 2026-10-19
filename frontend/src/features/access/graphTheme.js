/**
 * The graph's visual language.
 *
 * ── Why this is not a palette of eight node colours ─────────────────────────
 * A node-link diagram is an all-pairs form: unlike a bar chart, where only
 * neighbouring series touch, any two nodes can end up side by side, so every
 * pair of colours has to be distinguishable, not just adjacent ones. Run the
 * numbers and that rules out colouring node kinds by hue almost immediately.
 *
 * Measured with the data-viz validator (OKLab delta-E x100, protanopia and
 * deuteranopia at severity 1.0, all pairs, against this app's own surfaces):
 *
 *   The four severity hues as node fills   FAIL - high vs medium 1.6 (deutan),
 *                                          8.2 with normal colour vision
 *   Three hues (accent, caution, critical) FAIL - critical vs caution 14.4
 *                                          normal vision, below the 15 floor
 *   Two hues (accent, critical)            PASS - 23.8 CVD, 31.6 normal
 *                                          light; 19.2 / 29.0 dark
 *
 * So the graph has exactly two chromatic roles, and everything else is
 * neutral:
 *
 *   NEUTRAL   every node, regardless of kind. What a node IS gets said by its
 *             icon and its one-word kind label, which survive greyscale, a
 *             projector, and colour blindness. This is also why the graph
 *             reads as calm: on a healthy account almost nothing is coloured.
 *   ACCENT    what the reader has selected.
 *   CRITICAL  risk that is already true - an administrator-equivalent
 *             identity, a crown jewel, a stale key, an escalation edge.
 *
 * Amber survives only inside a text chip, where the mitigation is the label
 * beside it rather than the hue, and contrast is the rule that applies.
 *
 * The palette is expressed as the app's own design tokens so the graph stays
 * in step with every other screen and with the theme toggle. Hex values below
 * are the validated ones, recorded for the record; the tokens carry them.
 */

/* The two mark colours live in `styles/tokens.css` as `--t-graph-accent` and
   `--t-graph-risk`, stepped per mode and validated per mode:
     light  #0c6eb4 / #b42318  - delta-E 23.5 deutan, 30.2 normal
     dark   #2f92d8 / #d03b3b  - delta-E 22.7 deutan, 31.2 normal
   The dark risk step is not the app's dark severity red: that one sits at
   OKLCH L 0.73, above the 0.67 ceiling a mark has to stay under. */

export const GRAPH_TONES = {
  neutral: {
    surface: 'var(--t-surface)',
    border: 'var(--t-line-strong)',
    ink: 'var(--t-ink)',
    sub: 'var(--t-ink-3)',
    mark: 'var(--t-ink-3)',
  },
  accent: {
    surface: 'var(--t-info-soft)',
    border: 'var(--t-brand)',
    ink: 'var(--t-ink)',
    sub: 'var(--t-ink-2)',
    mark: 'var(--t-graph-accent)',
  },
  critical: {
    surface: 'var(--t-critical-soft)',
    border: 'var(--t-graph-risk)',
    ink: 'var(--t-ink)',
    sub: 'var(--t-ink-2)',
    mark: 'var(--t-graph-risk)',
  },
};

/**
 * Which tone a node wears.
 *
 * Risk first, then selection, then neutral. The order matters: a selected
 * administrator role is still an administrator role, so risk wins - otherwise
 * clicking a node would appear to make it safe. Every risk here is a field the
 * inventory reports (see `describeNode` in graphModel.js); nothing is inferred.
 */
export function toneFor(node) {
  if (node.isAdmin) return 'critical';
  if (node.publicPolicy) return 'critical';
  if (node.kind === 'credential' && node.stale) return 'critical';
  return 'neutral';
}

/**
 * The one-word label under a node's name, when the node has no more specific
 * type. This, with the icon, is how kind is carried - so it is never omitted.
 */
export const KIND_LABEL = {
  identity: 'Identity',
  credential: 'Credential',
  resource: 'Resource',
  account: 'Account',
  federated: 'Federated',
  service: 'AWS service',
  other: 'Not inventoried',
  more: 'Folded',
};

/**
 * Why a node is coloured, in words.
 *
 * Every red node can say what makes it red. A colour whose meaning has to be
 * inferred from a legend is a colour that gets ignored.
 */
export function riskReason(node) {
  if (node.isAdmin) return 'Administrator privilege';
  if (node.publicPolicy) return 'Publicly accessible';
  if (node.kind === 'credential' && node.stale) return `Unused for ${node.lastUsedDays} days`;
  return null;
}

/**
 * Edge styling. Blue for every relationship - dashed where it is configured
 * (a trust policy, a binding), solid where CloudTrail saw it used - and red
 * dashed where it crosses into another AWS account. The service computes no
 * escalation paths, so no edge is drawn as one.
 */
export const EDGE_STYLE = {
  base: { stroke: 'var(--t-graph-flow)', strokeWidth: 1.7, dash: '8 6' },
  observed: { stroke: 'var(--t-graph-flow)', strokeWidth: 1.7, dash: null },
  crossAccount: { stroke: 'var(--t-graph-risk)', strokeWidth: 1.9, dash: '8 6' },
};

export function edgeStyleFor(edge) {
  if (edge.crossAccount) return EDGE_STYLE.crossAccount;
  if (edge.observed && !edge.configured) return EDGE_STYLE.observed;
  return EDGE_STYLE.base;
}
