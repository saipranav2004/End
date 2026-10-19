/**
 * Positions for an incrementally expanded graph.
 *
 * React Flow places nothing on its own - it renders nodes at the coordinates
 * you give it - and the layout this graph needs is not a general-purpose one.
 * It is a hop layout: one column per hop from the focus, left to right,
 * because "how many hops in is this" is the question an analyst is holding.
 *
 * Two properties matter more than tidiness, and both follow from the graph
 * being opened a piece at a time:
 *
 *   STABILITY  a node on screen before an expansion keeps its row after it. If
 *              the layout reflowed on every expansion the reader would lose
 *              their place and stop expanding. Rows are assigned by index and
 *              an existing index is reused whenever the column still has it.
 *   ROOM       a fold-away ("+6 more") sits at the foot of its own column, so
 *              opening it grows that column downward and moves nothing else.
 *
 * Deterministic: the same neighbourhood in gives the same coordinates out.
 */

/** Row pitch. The box is shorter than the pitch; the difference is the gutter. */
const ROW_PITCH = 76;
const NODE_HEIGHT = 56;
const MORE_HEIGHT = 32;
const COLUMN_WIDTH = 304;
const NODE_WIDTH = 244;

export const HOP_METRICS = { ROW_PITCH, NODE_HEIGHT, MORE_HEIGHT, COLUMN_WIDTH, NODE_WIDTH };

/**
 * Lays out the neighbourhood as React Flow nodes.
 *
 * `previousRows` maps a node id to the row index it held last time, so nodes
 * stay put across expansions.
 */
export function layoutHops({ nodes = [], edges = [], groups = [] }, previousRows = new Map()) {
  if (nodes.length === 0) {
    return { nodes: [], rows: new Map(), width: 0, height: 0, columnCount: 0 };
  }

  const maxDepth = Math.max(
    ...nodes.map((node) => node.depth ?? 0),
    ...groups.map((group) => group.depth ?? 0),
  );
  const columnCount = maxDepth + 1;

  /* Which node each deeper node was opened from, so a child sits beside its
     parent instead of wherever the sort happened to put it. */
  const byId = new Map(nodes.map((node) => [node.id, node]));
  const parentOf = new Map();
  for (const edge of edges) {
    const from = byId.get(edge.from);
    const to = byId.get(edge.to);
    if (!from || !to) continue;
    const shallower = (from.depth ?? 0) <= (to.depth ?? 0) ? from : to;
    const deeper = shallower === from ? to : from;
    if ((deeper.depth ?? 0) === (shallower.depth ?? 0) + 1 && !parentOf.has(deeper.id)) {
      parentOf.set(deeper.id, shallower.id);
    }
  }

  /* Column contents, ordered parent-first then by interest. */
  const columns = Array.from({ length: columnCount }, () => []);
  for (const node of nodes) columns[node.depth ?? 0].push(node);

  const rowOf = new Map();
  columns.forEach((column, depth) => {
    if (depth > 0) {
      column.sort((a, b) => {
        const parentRowA = rowOf.get(parentOf.get(a.id)) ?? 0;
        const parentRowB = rowOf.get(parentOf.get(b.id)) ?? 0;
        if (parentRowA !== parentRowB) return parentRowA - parentRowB;
        return (b.interest ?? 0) - (a.interest ?? 0) || String(a.name).localeCompare(String(b.name));
      });
    }

    /* Stability pass: a node that held a row in this column last time keeps
       it, and anything new fills the lowest free row. */
    const claimed = new Set();
    const pending = [];
    for (const node of column) {
      const held = previousRows.get(node.id);
      if (held && held.depth === depth && !claimed.has(held.row) && held.row < column.length) {
        rowOf.set(node.id, held.row);
        claimed.add(held.row);
      } else {
        pending.push(node);
      }
    }
    let cursor = 0;
    for (const node of pending) {
      while (claimed.has(cursor)) cursor += 1;
      rowOf.set(node.id, cursor);
      claimed.add(cursor);
      cursor += 1;
    }
  });

  /* Folds take the rows after their column's nodes, ordered by their parent. */
  const foldsByDepth = new Map();
  for (const group of groups) {
    const depth = group.depth ?? 1;
    if (!foldsByDepth.has(depth)) foldsByDepth.set(depth, []);
    foldsByDepth.get(depth).push(group);
  }
  for (const [depth, list] of foldsByDepth) {
    list.sort((a, b) => (rowOf.get(a.parentId) ?? 0) - (rowOf.get(b.parentId) ?? 0));
    const base = columns[depth]?.length ?? 0;
    list.forEach((fold, index) => rowOf.set(fold.id, base + index));
  }

  /* Column heights, for the vertical centring. A fold row is shorter than a
     node row, which is why this counts the two separately. */
  const heights = new Map();
  for (let depth = 0; depth < columnCount; depth += 1) {
    const nodeCount = columns[depth]?.length ?? 0;
    const foldCount = foldsByDepth.get(depth)?.length ?? 0;
    heights.set(depth, nodeCount * ROW_PITCH + foldCount * (MORE_HEIGHT + 18));
  }
  const tallest = Math.max(...heights.values(), ROW_PITCH);

  const rows = new Map();
  const placed = [];

  const place = (item, depth, type) => {
    const row = rowOf.get(item.id) ?? 0;
    const nodeCount = columns[depth]?.length ?? 0;
    const y =
      row < nodeCount
        ? row * ROW_PITCH
        : nodeCount * ROW_PITCH + (row - nodeCount) * (MORE_HEIGHT + 18);
    const offset = (tallest - (heights.get(depth) ?? 0)) / 2;
    const position = { x: depth * COLUMN_WIDTH, y: Math.round(y + Math.max(0, offset)) };

    rows.set(item.id, { depth, row });
    placed.push({
      id: item.id,
      type,
      position,
      data: item,
      /* Draggable. The layout decides where a node starts, not where it has to
         stay: an analyst reading a six-hop path will want to pull two boxes
         apart to see the edge between them, and a canvas that refuses is a
         picture rather than a tool. React Flow keeps a dragged position until
         the graph's shape changes, at which point the layout reasserts itself
         - which is the right trade, because the alternative is remembering
         hand positions for nodes that may no longer be on screen. */
      draggable: true,
      selectable: type === 'access',
      /* Declared so React Flow does not have to measure before it can draw an
         edge, which is what removes the one-frame flicker on expansion. */
      width: NODE_WIDTH,
      height: type === 'access' ? NODE_HEIGHT : MORE_HEIGHT,
    });
  };

  columns.forEach((column, depth) => {
    for (const node of column) place(node, depth, 'access');
  });
  for (const [depth, list] of foldsByDepth) {
    for (const fold of list) place(fold, depth, 'more');
  }

  return {
    nodes: placed,
    rows,
    width: columnCount * COLUMN_WIDTH,
    height: tallest,
    columnCount,
  };
}

/** Column headings: hop distance from the focus, which is what a column means. */
export function hopLabels(columnCount) {
  return Array.from({ length: Math.max(0, columnCount) }, (_, index) =>
    index === 0 ? 'Focus' : index === 1 ? '1 hop' : `${index} hops`,
  );
}
