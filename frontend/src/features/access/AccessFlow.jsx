import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
} from '@xyflow/react';
import '@xyflow/react/dist/base.css';
import { Maximize2, Minimize2, PanelRight, RotateCcw } from 'lucide-react';
import { EDGE_STYLE, edgeStyleFor, toneFor } from './graphTheme';
import { edgeKindMeta } from './graphModel';
import { HOP_METRICS, hopLabels, layoutHops } from './hopLayout';
import { nodeTypes } from './GraphNodes';
import { IconButton } from '../../ui/Button';
import { cn } from '../../ui/cn';

/* Below this the 12px node labels stop being readable, so the fit stops
   shrinking and starts cropping instead. */
const MIN_FIT_ZOOM = 0.7;
/* The Fit button's floor, and the canvas's. Low enough that five hops fit a
   phone; the reader zooms back in on the part they want. */
const FIT_ALL_MIN_ZOOM = 0.15;
/* Module-level on purpose. React Flow copies this prop into its store every
   time its identity changes, and `fitView()` from `useReactFlow` is queued
   through that same store slot - so an inline object, re-created on every
   render, overwrote a pending Fit or post-expansion refit with these defaults
   whenever anything re-rendered first (a hover was enough). The Fit button
   then stopped at 0.7 with half the graph off screen. */
const INITIAL_FIT = { padding: 0.18, minZoom: MIN_FIT_ZOOM, maxZoom: 1.15 };

/* Breathing room between the graph and the frame, in screen pixels. */
const EDGE_PAD = 14;

/**
 * The canvas.
 *
 * React Flow handles panning, zooming, the viewport and hit testing; this
 * component decides what is on it. The division matters: everything here is
 * about *which* nodes exist and what they look like, and none of it is about
 * mouse maths, which is the part that was hand-rolled before and got the
 * zoom-space wrong on large displays.
 *
 * Three things this adds on top:
 *
 *   HOP HEADINGS   drawn above the canvas rather than in it, so they stay put
 *                  while the graph is panned - a column heading that scrolls
 *                  away from its column is worse than none.
 *   STABLE ROWS    the previous layout's row assignment is fed back in, so
 *                  expanding a node never moves what is already on screen.
 *   FULL SCREEN    the real Fullscreen API on the graph's own element, with a
 *                  fallback to a fixed overlay where the API is blocked, which
 *                  it is inside some embedded frames.
 */
export function AccessFlow(props) {
  return (
    <ReactFlowProvider>
      <FlowInner {...props} />
    </ReactFlowProvider>
  );
}

function FlowInner({
  data,
  selectedId,
  onSelect,
  onToggleExpand,
  onReveal,
  onFullscreenChange,
  /* Rendered as a dock inside the full-screen shell. In the normal layout the
     page renders it itself, beside the graph. */
  inspector,
  className,
  height = 560,
}) {
  const shellRef = useRef(null);
  const rowsRef = useRef(new Map());
  const { fitView, setViewport } = useReactFlow();
  const [fullscreen, setFullscreen] = useState(false);
  const [nativeFullscreen, setNativeFullscreen] = useState(false);
  const [hoverId, setHoverId] = useState('');
  /* Full screen has to contain everything, or it is a dead end you must leave
     to do anything. The details panel docks inside it - collapsible, because
     the point of full screen is the graph. */
  const [sideDock, setSideDock] = useState(true);
  /* Where the reader has dragged things.
     React Flow is controlled here - the nodes come from `layoutHops` on every
     render - and a controlled graph with no `onNodesChange` silently discards
     every drag, which is why `nodesDraggable` alone did nothing. Positions the
     reader sets are kept here and merged back over the layout. Cleared when
     the graph's shape changes, because a hand-placed position for a node that
     is no longer on screen is not worth keeping. */
  const [moved, setMoved] = useState({});

  const layout = useMemo(() => {
    const result = layoutHops(data ?? { nodes: [], edges: [], groups: [] }, rowsRef.current);
    rowsRef.current = result.rows;
    return result;
  }, [data]);

  /* Hover dims everything not joined to the hovered node. Cheap to compute and
     the single most useful thing a graph can do on hover: it answers "what is
     this connected to" without a click. */
  const hoverSets = useMemo(() => {
    if (!hoverId) return null;
    const nodes = new Set([hoverId]);
    const edges = new Set();
    for (const edge of data?.edges ?? []) {
      if (edge.from === hoverId) {
        edges.add(edge.id);
        nodes.add(edge.to);
      } else if (edge.to === hoverId) {
        edges.add(edge.id);
        nodes.add(edge.from);
      }
    }
    return { nodes, edges };
  }, [hoverId, data]);

  const flowNodes = useMemo(
    () =>
      layout.nodes.map((node) => ({
        ...node,
        selected: node.id === selectedId,
        data: node.data,
        className: cn(
          hoverSets && !hoverSets.nodes.has(node.id) && 'access-node-dim',
        ),
      })),
    [layout.nodes, selectedId, hoverSets],
  );

  /* The layout's position, unless the reader has moved that node. */
  const placedNodes = useMemo(
    () => flowNodes.map((node) => (moved[node.id] ? { ...node, position: moved[node.id] } : node)),
    [flowNodes, moved],
  );

  const flowEdges = useMemo(() => {
    const byId = new Map((data?.nodes ?? []).map((node) => [node.id, node]));
    return (data?.edges ?? []).map((edge) => {
      const meta = edgeKindMeta(edge.kind);
      const style = edgeStyleFor(edge);
      /* Columns run left to right by hop, and React Flow always leaves from a
         node's right side and arrives on the left. An edge that points back
         toward the focus (ci-deployer can assume the focus role) would loop
         round both boxes; it is drawn the way the columns read instead, with
         the arrowhead at its start so the direction stays true. */
      const reversed = (byId.get(edge.from)?.depth ?? 0) > (byId.get(edge.to)?.depth ?? 0);
      const arrow = { type: 'arrowclosed', width: 14, height: 14, color: style.stroke };

      /* The label is on the edge only where the relationship is not obvious
         from the two nodes it joins. "Runs as" between a function and its
         role says nothing new; "cross-account" between two roles is the
         whole point. */
      return {
        id: edge.id,
        source: reversed ? edge.to : edge.from,
        target: reversed ? edge.from : edge.to,
        type: 'smoothstep',
        animated: false,
        label: edge.crossAccount ? 'cross-account' : undefined,
        ariaLabel: `${meta.label}${edge.callCount ? `, ${edge.callCount} calls` : ''}`,
        labelShowBg: true,
        labelBgPadding: [4, 2],
        labelBgBorderRadius: 4,
        markerEnd: reversed ? undefined : arrow,
        markerStart: reversed ? arrow : undefined,
        style: {
          stroke: style.stroke,
          strokeWidth: style.strokeWidth,
          strokeDasharray: style.dash ?? undefined,
        },
        className: cn(
          hoverSets && !hoverSets.edges.has(edge.id) && 'access-edge-dimmed',
          hoverSets?.edges.has(edge.id) && 'access-edge-active',
        ),
        data: {
          kind: edge.kind,
          fromName: byId.get(edge.from)?.name,
          toName: byId.get(edge.to)?.name,
        },
      };
    });
  }, [data, hoverSets]);

  /* Refit whenever the drawn graph changes shape, so an expansion brings the
     new nodes into view instead of leaving them off the right edge.

     With one exception, and it is the whole of the phone case: three columns
     of 304px do not fit in 390px at any zoom worth reading, so fitting the
     lot shrinks the labels past legibility. Below that threshold the fit
     targets the focus and its first hop instead - the reader opens on the
     node they asked for and pans right for the rest, which is the correct
     trade when the alternative is a graph nobody can read. */
  const shape = `${layout.nodes.length}-${layout.columnCount}`;
  const translateExtent = useMemo(
    () => [
      [-600, -600],
      [layout.width + 600, layout.height + 600],
    ],
    [layout.width, layout.height],
  );
  /* The node the reader just opened, or asked to reveal more of. The refit
     after an expansion used to fall through to "fit the first two columns",
     which snapped the view back to the first card and left the hop that had
     just opened off the right edge - so every expansion cost a manual pan.
     With an anchor, the refit brings that node and its neighbours into view
     instead. Consumed by the next refit. */
  const anchorRef = useRef(null);
  const refit = useCallback(
    (duration, { all = false } = {}) => {
      const frame = shellRef.current?.querySelector('.access-canvas');
      const available = frame?.clientWidth ?? 0;
      const height = frame?.clientHeight ?? 0;
      const anchor = anchorRef.current;
      anchorRef.current = null;
      if (available === 0 || layout.nodes.length === 0) return;

      /* The Fit button: the whole graph, however small that makes it. The
         reader asked to see everything, so the readability floor gives way. */
      if (all) {
        fitView({ padding: 0.12, duration, minZoom: FIT_ALL_MIN_ZOOM, maxZoom: 1.15 });
        return;
      }

      /* Everything fits at a readable zoom: show it all. */
      if (layout.width * MIN_FIT_ZOOM <= available - 2 * EDGE_PAD) {
        fitView({ padding: 0.18, duration, minZoom: MIN_FIT_ZOOM, maxZoom: 1.15 });
        return;
      }

      /* It does not, and this refit follows an expansion: frame the node that
         was opened together with everything joined to it, which is exactly
         the hop that just appeared. */
      if (anchor && layout.nodes.some((node) => node.id === anchor)) {
        const ids = new Set([anchor]);
        for (const edge of data?.edges ?? []) {
          if (edge.from === anchor) ids.add(edge.to);
          if (edge.to === anchor) ids.add(edge.from);
        }
        for (const node of layout.nodes) {
          if (node.type === 'more' && node.data?.parentId === anchor) ids.add(node.id);
        }
        /* On a phone even the opened node and its new hop are wider than the
           frame at a readable zoom, and fitView centres the pair - which cut
           the new hop in half. Right-align the new hop instead: it is what
           the reader asked to see, and the opened node peeks in from the
           left so the edge between them still reads. */
        const anchorNode = layout.nodes.find((node) => node.id === anchor);
        const opened = layout.nodes.filter(
          (node) => ids.has(node.id) && node.position.x > anchorNode.position.x,
        );
        const right = Math.max(...opened.map((node) => node.position.x), anchorNode.position.x) + HOP_METRICS.NODE_WIDTH;
        if ((right - anchorNode.position.x) * MIN_FIT_ZOOM > available - 2 * EDGE_PAD && opened.length > 0) {
          const ys = opened.map((node) => node.position.y);
          const middle = (Math.min(...ys) + Math.max(...ys)) / 2;
          setViewport(
            {
              x: available - EDGE_PAD - right * MIN_FIT_ZOOM,
              y: height / 2 - middle * MIN_FIT_ZOOM,
              zoom: MIN_FIT_ZOOM,
            },
            { duration },
          );
          return;
        }
        fitView({
          padding: 0.16,
          duration,
          minZoom: MIN_FIT_ZOOM,
          maxZoom: 1.15,
          nodes: layout.nodes.filter((node) => ids.has(node.id)).map((node) => ({ id: node.id })),
        });
        return;
      }

      /* Does the focus and its first hop fit at a readable zoom? Two columns
         need two node widths and the gap between them. */
      const nearWidth = Math.min(2, layout.columnCount) * HOP_METRICS.COLUMN_WIDTH;
      const fits = nearWidth * MIN_FIT_ZOOM <= available - 2 * EDGE_PAD;

      if (fits && layout.width * MIN_FIT_ZOOM > available) {
        /* The whole graph will not fit but the first two columns will, so fit
           those and let the reader pan for the rest. */
        fitView({
          padding: 0.14,
          duration,
          minZoom: MIN_FIT_ZOOM,
          maxZoom: 1.15,
          nodes: layout.nodes.filter((node) => node.data.depth <= 1).map((node) => ({ id: node.id })),
        });
        return;
      }

      if (!fits) {
        /* Not even two columns fit. fitView always centres, which at phone
           width clipped the focus node off the left edge - the one node the
           reader is definitely looking for. The graph reads left to right, so
           the left edge is the anchor: pin it there and let the overflow fall
           to the right, where panning is the obvious gesture. */
        const focus = layout.nodes.find((node) => node.data.isFocus) ?? layout.nodes[0];
        /* `nodeOrigin` is [0, 0.5], so a node's y is its vertical centre. */
        setViewport(
          {
            x: EDGE_PAD,
            y: height / 2 - focus.position.y * MIN_FIT_ZOOM,
            zoom: MIN_FIT_ZOOM,
          },
          { duration },
        );
        return;
      }

      fitView({ padding: 0.18, duration, minZoom: MIN_FIT_ZOOM, maxZoom: 1.15 });
    },
    [fitView, setViewport, layout.nodes, layout.width, layout.columnCount, data],
  );

  useEffect(() => {
    const frame = requestAnimationFrame(() => refit(420));
    return () => cancelAnimationFrame(frame);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shape, fullscreen, refit]);

  /* An expansion re-runs the layout, so hand positions from the previous shape
     no longer describe this graph. Dropping them is the honest choice: keeping
     them would pin two boxes where the reader left them and lay the new ones
     out around a hole. */
  useEffect(() => {
    setMoved({});
  }, [shape]);

  /* Full screen. The API can be refused - inside a sandboxed frame, or by
     policy - so the promise rejection falls back to a fixed overlay, which
     looks the same to the reader and needs no permission. */
  const toggleFullscreen = useCallback(() => {
    const element = shellRef.current;
    if (!element) return;

    if (document.fullscreenElement) {
      document.exitFullscreen?.().catch(() => {});
      return;
    }
    if (fullscreen) {
      setFullscreen(false);
      return;
    }
    const request = element.requestFullscreen?.({ navigationUI: 'hide' });
    if (request?.catch) {
      request.catch(() => setFullscreen(true));
    } else if (!element.requestFullscreen) {
      setFullscreen(true);
    }
  }, [fullscreen]);

  useEffect(() => {
    const onChange = () => {
      const active = document.fullscreenElement === shellRef.current;
      setNativeFullscreen(active);
      if (!active) setFullscreen(false);
    };
    document.addEventListener('fullscreenchange', onChange);
    return () => document.removeEventListener('fullscreenchange', onChange);
  }, []);

  const expanded = nativeFullscreen || fullscreen;
  useEffect(() => {
    onFullscreenChange?.(expanded);
    /* Back to the default arrangement on the way out, so entering full screen
       twice does not give two different layouts. */
    if (!expanded) setSideDock(true);
  }, [expanded, onFullscreenChange]);

  /* Escape leaves the fallback overlay. The native API already handles it. */
  useEffect(() => {
    if (!fullscreen) return undefined;
    const onKey = (event) => {
      if (event.key === 'Escape') setFullscreen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [fullscreen]);

  /* One click, two meanings, decided by where it landed.
     The chevron is the expand control and the rest of the card selects. This
     was a double-click before, which was wrong twice over: React Flow v12
     never delivers `onNodeDoubleClick` for a custom node type, and a chevron
     that has to be double-clicked is a control nobody finds. */
  const onNodeClick = useCallback(
    (event, node) => {
      if (node.type === 'more') {
        anchorRef.current = node.data.parentId;
        onReveal?.(node.data.parentId);
        return;
      }
      if (event.target instanceof Element && event.target.closest('[data-expand]')) {
        anchorRef.current = node.id;
        onToggleExpand?.(node.data);
        return;
      }
      onSelect?.(node.data);
    },
    [onReveal, onSelect, onToggleExpand],
  );

  const onNodesChange = useCallback((changes) => {
    let next = null;
    for (const change of changes) {
      if (change.type !== 'position' || !change.position) continue;
      next = next ?? {};
      next[change.id] = change.position;
    }
    if (next) setMoved((current) => ({ ...current, ...next }));
  }, []);

  const labels = hopLabels(layout.columnCount);

  return (
    <div
      ref={shellRef}
      className={cn(
        'access-shell @container flex flex-col overflow-hidden rounded-[var(--radius-panel)] border border-line bg-inset',
        !expanded && 'access-shell-sized',
        fullscreen && 'fixed inset-0 z-[80] rounded-none border-0',
        nativeFullscreen && 'rounded-none border-0',
        className,
      )}
      /* Two different problems at the two ends, so two different heights, and
         the switch is in CSS because it is a viewport question.
         On a phone a fixed 560px is most of the screen spent on a graph that
         needs a third of it, and it pushes the panel explaining the graph out
         of sight - so there the frame follows the drawn height. On a desktop
         the opposite is true: the empty canvas is room to expand and pan
         into, and shrinking it to the current contents wastes the space this
         screen exists to use. `--graph-content-h` and `--graph-max-h` carry
         the two figures; `.access-shell-sized` picks between them. */
      style={
        expanded
          ? undefined
          : {
              '--graph-content-h': `${Math.round(layout.height * MIN_FIT_ZOOM) + 190}px`,
              '--graph-max-h': `${height}px`,
            }
      }
    >
      {/* Hop headings. Outside the canvas so they do not pan away from the
          columns they name. */}
      <div className="flex shrink-0 items-center gap-2 border-b border-line bg-surface-2 px-3 py-1.5">
        {/* Scrolls rather than clips. Three labels plus the count plus two
            buttons do not fit in 364px, and `overflow: hidden` resolved that
            by cutting "2 hops" to "2 HOF". */}
        <div className="access-hop-strip flex min-w-0 flex-1 items-center gap-4 overflow-x-auto">
          {labels.map((label, index) => (
            <span key={label} className="access-hop-label shrink-0">
              {index === 0 ? label : `→ ${label}`}
            </span>
          ))}
        </div>
        <span className="hidden shrink-0 text-[10.5px] text-ink-3 @min-[32rem]:inline">
          {(data?.nodes?.length ?? 0)} of {data?.totalNodes ?? 0} found so far
        </span>
        {expanded && inspector && (
          <IconButton
            icon={PanelRight}
            label={sideDock ? 'Hide the details panel' : 'Show the details panel'}
            variant={sideDock ? 'primary' : 'secondary'}
            onClick={() => setSideDock((open) => !open)}
          />
        )}
        {/* Full size, not `sm`. These two are the only controls on the header
            and they were 32px glyphs in a 17px-tall strip - small enough that
            the fullscreen button, the one people look for first, read as
            decoration. */}
        <IconButton
          icon={RotateCcw}
          label="Fit the whole graph to the view"
          variant="secondary"
          onClick={() => refit(420, { all: true })}
        />
        <IconButton
          icon={expanded ? Minimize2 : Maximize2}
          label={expanded ? 'Leave full screen' : 'Show the graph full screen'}
          variant="secondary"
          onClick={toggleFullscreen}
        />
      </div>

      <div className="flex min-h-0 flex-1">
      <div className="access-canvas min-h-0 flex-1">
        <ReactFlow
          nodes={placedNodes}
          edges={flowEdges}
          nodeTypes={nodeTypes}
          onNodesChange={onNodesChange}
          onNodeClick={onNodeClick}
          onNodeMouseEnter={(_event, node) => setHoverId(node.id)}
          onNodeMouseLeave={() => setHoverId('')}
          onPaneClick={() => onSelect?.(null)}
          nodesDraggable
          nodesConnectable={false}
          edgesFocusable={false}
          elevateEdgesOnSelect={false}
          minZoom={FIT_ALL_MIN_ZOOM}
          maxZoom={2}
          proOptions={{ hideAttribution: false }}
          fitView
          fitViewOptions={INITIAL_FIT}
          /* Two-finger and wheel zoom without a modifier: an analyst reaches
             for the wheel before they reach for a button. */
          zoomOnScroll
          panOnScroll={false}
          panOnDrag
          selectionOnDrag={false}
          nodeOrigin={[0, 0.5]}
          translateExtent={translateExtent}
        >
          <Background variant={BackgroundVariant.Dots} gap={22} size={1} />
          <Controls showInteractive={false} position="bottom-left" />
          {layout.nodes.length > 6 && (
            <MiniMap
              pannable
              zoomable
              position="bottom-right"
              nodeStrokeWidth={2}
              nodeColor={(node) =>
                node.type === 'more'
                  ? 'var(--t-line-strong)'
                  : toneFor(node.data) === 'critical'
                    ? 'var(--t-graph-risk)'
                    : 'var(--t-ink-3)'
              }
              maskColor="color-mix(in oklab, var(--t-ink) 10%, transparent)"
              style={{ width: 132, height: 88 }}
            />
          )}
        </ReactFlow>
      </div>

        {/* The details panel, docked. Scrolls on its own so the graph keeps
            the full height beside it. */}
        {expanded && inspector && sideDock && (
          <aside
            aria-label="Selected node"
            className="animate-slide-left min-h-0 w-[340px] shrink-0 overflow-y-auto overscroll-contain border-l border-line bg-surface p-3 @min-[80rem]:w-[380px]"
          >
            {inspector}
          </aside>
        )}
      </div>

      {/* The legend and the gestures, on one strip.
          A directional graph with two line styles needs to say which is which:
          the reader can see that some edges are red and dashed, and nothing on
          screen told them that meant a documented escalation rather than
          "important". Here: dashed is configuration, solid is what CloudTrail
          saw used, red is a step into another AWS account. */}
      <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-t border-line bg-surface-2 px-3 py-1.5 text-[10.5px] text-ink-3">
        <span className="flex items-center gap-3">
          <LegendKey label="Trust or binding" tone="base" />
          <LegendKey label="Observed calls" tone="observed" />
          <LegendKey label="Crosses accounts" tone="crossAccount" />
        </span>
        <span aria-hidden="true" className="hidden text-line-strong @min-[44rem]:inline">
          |
        </span>
        <span className="min-w-0">
          <span className="@min-[32rem]:hidden">
            {(data?.nodes?.length ?? 0)} of {data?.totalNodes ?? 0} found so far ·{' '}
          </span>
          Click a node to inspect · the chevron for its next hop · drag a node to move it
        </span>
      </div>
    </div>
  );
}

/**
 * One line of the legend.
 *
 * A short stroke drawn the way the canvas draws it, rather than a coloured
 * dot: the difference between the three kinds is as much the dash pattern as
 * the hue, and a dot cannot show a dash.
 */
function LegendKey({ label, tone }) {
  const style = EDGE_STYLE[tone] ?? EDGE_STYLE.base;
  return (
    <span className="flex shrink-0 items-center gap-1.5">
      <svg aria-hidden="true" width="20" height="6" viewBox="0 0 20 6" className="shrink-0 overflow-visible">
        <line
          x1="0"
          y1="3"
          x2="20"
          y2="3"
          stroke={style.stroke}
          strokeWidth={style.strokeWidth}
          strokeDasharray={style.dash ?? undefined}
          strokeLinecap="round"
        />
      </svg>
      <span>{label}</span>
    </span>
  );
}
