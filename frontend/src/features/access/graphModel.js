import { arnResource, humanizeToken } from '../../lib/format';

/**
 * The access graph's vocabulary and its pure graph maths.
 *
 * ── Where the graph comes from ───────────────────────────────────────────────
 * The discovery service answers one node's neighbourhood at a time
 * (`?tab=relationships`), naming every node by ARN. Edges come from two
 * stores: trust and binding edges from Postgres (`trust_edges`, written by
 * discovery from trust policies and service bindings), and `accesses` /
 * `created_by` edges from CloudTrail events in ClickHouse. The service prunes
 * both to the caller's saved accounts and regions.
 *
 * Every neighbourhood fetched is merged into one store (`createStore`,
 * `mergeRelationships`), and what is drawn is computed from that store by
 * `buildNeighbourhood`: the focus, plus the next hop of every node the reader
 * opened, each hop capped to a budget with the rest folded behind a count.
 */

/* ── Vocabulary ───────────────────────────────────────────────────────────── */

/**
 * Node kinds. MAIN types nodes as identity | credential | resource | unknown;
 * `unknown` is split by what its ARN says it is, so an account principal or a
 * federation provider is not labelled as a resource.
 */
export const NODE_KINDS = {
  identity: {
    label: 'Identity',
    plural: 'Identities',
    description: 'A workload that acts: a function, a task, an instance, a pipeline, an IAM user.',
  },
  credential: {
    label: 'Credential',
    plural: 'Credentials',
    description: 'What an identity authenticates with: an IAM role, an access key, a secret.',
  },
  resource: {
    label: 'Resource',
    plural: 'Resources',
    description: 'Something an identity called, as recorded by CloudTrail.',
  },
  account: {
    label: 'Account',
    plural: 'Accounts',
    description: 'A whole AWS account, named as a principal in a trust policy.',
  },
  federated: {
    label: 'Federated',
    plural: 'Federated principals',
    description: 'An OIDC or SAML provider that can be trusted to assume a role.',
  },
  service: {
    label: 'AWS service',
    plural: 'AWS services',
    description: 'An AWS service principal trusted to assume a role.',
  },
  other: {
    label: 'Not inventoried',
    plural: 'Other principals',
    description: 'An ARN the inventory has no record of in the current scope.',
  },
};

/**
 * MAIN edge types, as the console names them.
 *
 * `weight` picks the edge a pair of nodes is drawn as when several connect
 * them. `observed` marks edges that come from CloudTrail calls rather than
 * from configuration. No edge type is an escalation: the service does not
 * compute escalation paths, so the graph does not claim any. A cross-account
 * edge is the one real risk signal an edge carries, and it is drawn as such.
 */
export const EDGE_KINDS = {
  cross_account: {
    label: 'Crosses accounts',
    tone: 'high',
    weight: 7,
    description: 'Can assume a role in a different AWS account.',
  },
  assume_role: {
    label: 'Can assume',
    tone: 'high',
    weight: 6,
    description: "The role's trust policy lets this principal call sts:AssumeRole on it.",
  },
  federated_oidc: { label: 'OIDC federation', tone: 'high', weight: 6, description: 'Trusted through an OIDC provider.' },
  federated_saml: { label: 'SAML federation', tone: 'high', weight: 6, description: 'Trusted through a SAML provider.' },
  trusts: {
    label: 'Named in trust policy',
    tone: 'medium',
    weight: 5,
    description: "The role's trust policy names this principal.",
  },
  service_trust: {
    label: 'Service trust',
    tone: 'neutral',
    weight: 4,
    description: 'An AWS service may assume the role on its own behalf.',
  },
  bound_to: {
    label: 'Runs as',
    tone: 'info',
    weight: 4,
    description: 'The workload is bound to this credential (instance profile, execution role, task role).',
  },
  uses: { label: 'Uses', tone: 'info', weight: 3, description: 'Uses this credential.' },
  attached_to: { label: 'Attached to', tone: 'neutral', weight: 3, description: 'Attached to this principal.' },
  manages: { label: 'Manages', tone: 'neutral', weight: 3, description: 'Manages this resource.' },
  contains: { label: 'Contains', tone: 'neutral', weight: 2, description: 'Contains this resource.' },
  accesses: {
    label: 'Called',
    tone: 'info',
    weight: 2,
    observed: true,
    description: 'Successful API calls against this resource, recorded by CloudTrail.',
  },
  created_by: {
    label: 'Created by',
    tone: 'neutral',
    weight: 1,
    observed: true,
    description: 'The principal CloudTrail records as having created it.',
  },
};

export function edgeKindMeta(type) {
  return EDGE_KINDS[type] ?? { label: humanizeToken(type || 'related'), tone: 'neutral', weight: 0, description: '' };
}

/** How many neighbours a hop shows before folding, and how many a click on the fold adds. */
export const VISIBLE_PER_GROUP = 3;
export const REVEAL_STEP = 2;

/* ── ARN helpers ─────────────────────────────────────────────────────────── */

export function accountOf(arn) {
  const parts = String(arn ?? '').split(':');
  return parts.length >= 5 && /^\d{12}$/.test(parts[4]) ? parts[4] : '';
}

function kindFromApi(type, arn) {
  if (type === 'identity' || type === 'credential' || type === 'resource' || type === 'service') return type;
  const value = String(arn ?? '');
  if (/^arn:aws[\w-]*:iam::\d{12}:root$/.test(value)) return 'account';
  if (/:(oidc-provider|saml-provider)\//.test(value)) return 'federated';
  if (!value.startsWith('arn:') && value.endsWith('.amazonaws.com')) return 'service';
  return 'other';
}

/** The name a node is drawn with. Resource names from CloudTrail are the ARN's
    last segment ("42" for a task definition), so the resource path is used. */
function displayName(kind, name, arn) {
  if (kind === 'account') return `Account ${accountOf(arn)}`;
  if (kind === 'resource' || kind === 'other') {
    const path = arnResource(arn);
    return path && path !== '-' ? path : name || arn;
  }
  return name || arnResource(arn) || arn;
}

/** A short type label for the node's second line, from MAIN's subtype. */
export function subtypeLabel(kind, subtype) {
  if (!subtype) return '';
  if (kind === 'resource' && subtype.startsWith('AWS::')) {
    return subtype.split('::').slice(1).join(' ');
  }
  if (kind === 'other') return subtype.replace(':', ' ');
  return humanizeToken(subtype)
    .replace(/\bIam\b/g, 'IAM')
    .replace(/\bEc2\b/g, 'EC2')
    .replace(/\bEcs\b/g, 'ECS')
    .replace(/\bTls\b/g, 'TLS')
    .replace(/\bSsh\b/g, 'SSH')
    .replace(/\bApi\b/g, 'API');
}

/* ── The store ───────────────────────────────────────────────────────────── */

/**
 * Everything learnt so far: node facts by ARN, raw edges by key, and which
 * ARNs have had their own neighbourhood read (`loaded`).
 */
export function createStore() {
  return { nodes: new Map(), edges: new Map(), loaded: new Set() };
}

const SPECIFICITY = { identity: 3, credential: 3, resource: 2, account: 1, federated: 1, service: 1, other: 0 };

export function mergeRelationships(store, ownerArn, response) {
  const graph = response?.graph ?? {};
  for (const node of graph.nodes ?? []) {
    if (!node?.arn) continue;
    const kind = kindFromApi(node.type, node.arn);
    const held = store.nodes.get(node.arn);
    if (held && SPECIFICITY[held.kind] > SPECIFICITY[kind]) continue;
    store.nodes.set(node.arn, {
      id: node.arn,
      arn: node.arn,
      kind,
      apiType: node.type,
      subtype: node.subtype || '',
      name: displayName(kind, node.name, node.arn),
      accountId: accountOf(node.arn),
    });
  }
  for (const edge of graph.edges ?? []) {
    if (!edge?.source || !edge?.target || edge.source === edge.target) continue;
    const key = `${edge.source}|${edge.target}|${edge.edge_type}`;
    const held = store.edges.get(key);
    store.edges.set(key, {
      source: edge.source,
      target: edge.target,
      type: edge.edge_type,
      lastObservedAt: edge.last_observed_at ?? held?.lastObservedAt ?? null,
      callCount: edge.call_count ?? held?.callCount ?? null,
    });
  }
  if (ownerArn) store.loaded.add(ownerArn);
}

/**
 * Raw edges collapsed to one drawn edge per ordered pair. Two roles joined by
 * both `assume_role` and `cross_account` (and the CloudTrail `accesses` edge
 * of the AssumeRole calls) are one relationship to read, named after its
 * heaviest kind and carrying the rest as facts.
 */
export function pairEdges(store) {
  const pairs = new Map();
  for (const edge of store.edges.values()) {
    const id = `${edge.source}->${edge.target}`;
    const meta = edgeKindMeta(edge.type);
    let pair = pairs.get(id);
    if (!pair) {
      pair = {
        id,
        from: edge.source,
        to: edge.target,
        kinds: [],
        kind: edge.type,
        callCount: null,
        lastObservedAt: null,
        configured: false,
        observed: false,
      };
      pairs.set(id, pair);
    }
    pair.kinds.push(edge.type);
    if (meta.weight > edgeKindMeta(pair.kind).weight) pair.kind = edge.type;
    if (meta.observed) pair.observed = true;
    else pair.configured = true;
    if (edge.callCount) pair.callCount = (pair.callCount ?? 0) + edge.callCount;
    if (edge.lastObservedAt && (!pair.lastObservedAt || edge.lastObservedAt > pair.lastObservedAt)) {
      pair.lastObservedAt = edge.lastObservedAt;
    }
  }
  for (const pair of pairs.values()) {
    const a = accountOf(pair.from);
    const b = accountOf(pair.to);
    /* Two ARNs with account ids settle it. The service's `cross_account` kind
       decides only when one side has none: it also tags trust in the
       account's own root, which would label a same-account pair. */
    pair.crossAccount = a && b ? a !== b : pair.kinds.includes('cross_account');
    if (pair.kind === 'cross_account' && pair.kinds.includes('assume_role')) pair.kind = 'assume_role';
  }
  return [...pairs.values()];
}

/* ── Risk and interest ───────────────────────────────────────────────────── */

const STALE_DAYS = 90;

/**
 * A node as drawn: the store's facts plus what the inventory says about it.
 * `facts` is { identity, credential, profile } for the ARN (any may be
 * missing). Every flag here is read from a real field:
 *   isAdmin       credential privilege level 4 (discovery's assessment), or
 *                 AdministratorAccess attached to it
 *   publicPolicy  identity is_publicly_accessible
 *   stale         credential unused for more than 90 days (idle_days)
 *   externalTrust credential has_external_trust (trust policy names another
 *                 account)
 */
export function describeNode(base, facts) {
  const identity = facts?.identity ?? null;
  const credential = facts?.credential ?? null;
  const profile = facts?.profile ?? null;
  const policies = profile?.attachedPolicies ?? [];
  const idle = credential?.idle_days;
  return {
    ...base,
    typeLabel: subtypeLabel(base.kind, base.subtype),
    accountName: base.accountId || identity?.account_id || credential?.account_id || '',
    region: identity?.region ?? credential?.region ?? '',
    identityId: identity?.id ?? null,
    credentialId: credential?.id ?? null,
    identityType: identity?.identity_type ?? null,
    status: identity?.status ?? credential?.status ?? null,
    owner: identity?.owner ?? null,
    environment: identity?.environment ?? null,
    privilegeLevel: profile?.privilegeLevel ?? null,
    isAdmin: profile?.privilegeLevel === 4 || policies.includes('AdministratorAccess'),
    publicPolicy: Boolean(identity?.is_publicly_accessible),
    stale: typeof idle === 'number' && idle > STALE_DAYS,
    lastUsedDays: typeof idle === 'number' ? idle : null,
    externalTrust: Boolean(credential?.has_external_trust),
    orphaned: Boolean(credential?.is_orphaned),
    expandable: Boolean(identity || credential),
  };
}

/** Which neighbours earn the budgeted slots: risk first, then reach. */
export function interestOf(node) {
  let score = 10;
  if (node.isAdmin) score += 30;
  if (node.publicPolicy) score += 22;
  if (node.stale) score += 15;
  if (node.externalTrust) score += 12;
  if (node.kind === 'identity' || node.kind === 'credential') score += 6;
  if (node.kind === 'account' || node.kind === 'federated') score += 8;
  return score;
}

function edgeInterest(pair) {
  if (!pair) return 0;
  let score = pair.crossAccount ? 16 : 0;
  score += Math.min(4, edgeKindMeta(pair.kind).weight);
  if (pair.callCount) score += Math.min(6, Math.log2(pair.callCount + 1));
  return score;
}

function summariseHidden(nodes) {
  const counts = new Map();
  for (const node of nodes) {
    const label = NODE_KINDS[node.kind]?.plural ?? 'Other';
    counts.set(label, (counts.get(label) ?? 0) + 1);
  }
  const words = [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([label, count]) => `${count} ${(count === 1 ? label.replace(/ies$/, 'y').replace(/s$/, '') : label).toLowerCase()}`);
  /* "3 credentials, 2 resources and 1 identity" - commas, "and" only before the last. */
  return words.length <= 1 ? words.join('') : `${words.slice(0, -1).join(', ')} and ${words.at(-1)}`;
}

/* ── The drawn neighbourhood ─────────────────────────────────────────────── */

/**
 * The graph as one focus plus whatever has been opened from it.
 *
 * Breadth first from the focus, through opened nodes only, both directions
 * ("what can reach this" matters as much as "what this reaches"). Each hop
 * keeps its VISIBLE_PER_GROUP most interesting neighbours plus whatever was
 * revealed, and folds the rest. `describe(arn)` returns a drawn node for an
 * ARN (see `describeNode`).
 */
export function buildNeighbourhood({ store, focusArn, expanded = [], revealed = {}, describe }) {
  if (!focusArn || !store.nodes.has(focusArn)) return null;
  const pairs = pairEdges(store);
  const out = new Map();
  const into = new Map();
  for (const pair of pairs) {
    if (!out.has(pair.from)) out.set(pair.from, []);
    if (!into.has(pair.to)) into.set(pair.to, []);
    out.get(pair.from).push(pair);
    into.get(pair.to).push(pair);
  }
  const neighboursOf = (arn) => {
    const list = [];
    for (const pair of out.get(arn) ?? []) list.push({ pair, other: pair.to });
    for (const pair of into.get(arn) ?? []) list.push({ pair, other: pair.from });
    return list.filter((entry) => store.nodes.has(entry.other));
  };

  const openSet = new Set([focusArn, ...expanded]);
  const depth = new Map([[focusArn, 0]]);
  const visible = new Map([[focusArn, describe(focusArn)]]);
  const shown = new Map();
  const groups = [];
  const queue = [focusArn];
  const walked = new Set();

  while (queue.length > 0) {
    const current = queue.shift();
    if (walked.has(current)) continue;
    walked.add(current);
    if (!openSet.has(current)) continue;
    const currentDepth = depth.get(current) ?? 0;

    const byNeighbour = new Map();
    for (const entry of neighboursOf(current)) {
      if (visible.has(entry.other)) {
        shown.set(entry.pair.id, entry.pair);
        continue;
      }
      const held = byNeighbour.get(entry.other);
      if (!held || edgeInterest(entry.pair) > edgeInterest(held.pair)) byNeighbour.set(entry.other, entry);
    }

    const ranked = [...byNeighbour.values()]
      .map((entry) => {
        const node = describe(entry.other);
        return { ...entry, node, rank: interestOf(node) + edgeInterest(entry.pair) };
      })
      .sort((a, b) => b.rank - a.rank || String(a.node.name).localeCompare(String(b.node.name)));

    const budget = VISIBLE_PER_GROUP + (revealed[current] ?? 0);
    for (const { pair, node } of ranked.slice(0, budget)) {
      visible.set(node.id, node);
      depth.set(node.id, currentDepth + 1);
      shown.set(pair.id, pair);
      queue.push(node.id);
    }
    const hidden = ranked.slice(budget);
    if (hidden.length > 0) {
      groups.push({
        id: `more:${current}`,
        kind: 'more',
        parentId: current,
        parentName: visible.get(current)?.name,
        hiddenCount: hidden.length,
        depth: currentDepth + 1,
        summary: summariseHidden(hidden.map((entry) => entry.node)),
      });
    }
  }

  /* Edges between nodes that are both on screen are drawn even when neither
     end was the one opened, so a triangle reads as a triangle. */
  for (const pair of pairs) {
    if (visible.has(pair.from) && visible.has(pair.to)) shown.set(pair.id, pair);
  }

  const nodes = [...visible.values()].map((node) => {
    const neighbourIds = new Set(neighboursOf(node.id).map((entry) => entry.other));
    const unseen = [...neighbourIds].filter((id) => !visible.has(id)).length;
    return {
      ...node,
      depth: depth.get(node.id) ?? 0,
      isFocus: node.id === focusArn,
      expanded: openSet.has(node.id),
      loaded: store.loaded.has(node.id),
      neighbourCount: neighbourIds.size,
      unseenCount: unseen,
      interest: interestOf(node),
    };
  });

  return {
    focus: visible.get(focusArn),
    nodes,
    edges: [...shown.values()],
    groups,
    totalNodes: store.nodes.size,
  };
}

/** Every drawn edge touching one ARN, described from that node's side. */
export function edgesOf(store, arn) {
  return pairEdges(store)
    .filter((pair) => pair.from === arn || pair.to === arn)
    .map((pair) => {
      const otherArn = pair.from === arn ? pair.to : pair.from;
      return { ...pair, direction: pair.from === arn ? 'out' : 'in', otherArn, other: store.nodes.get(otherArn) };
    });
}
