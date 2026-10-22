import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { AlertTriangle, Info, Network, RotateCw } from 'lucide-react';
import { PageHeader } from '../../shell/PageHeader';
import { Button, IconButton } from '../../ui/Button';
import { Modal } from '../../ui/Overlay';
import { Panel } from '../../ui/Panel';
import { DetailSkeleton, Skeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState } from '../../ui/States';
import { AccessFlow } from './AccessFlow';
import { FocusPicker } from './FocusPicker';
import { GraphInputs } from './GraphInputs';
import { IdentityPanel } from './IdentityPanel';
import { REVEAL_STEP, subtypeLabel } from './graphModel';
import { useAccessGraph } from './useAccessGraph';

/**
 * Access graph.
 *
 * ── The shape of the screen ─────────────────────────────────────────────────
 * Graph on the left, one panel on the right. Nothing else.
 *
 * There are no headline counters, on purpose. A count of principals or edges
 * is a fact about the dataset, not about the account: it does not change what
 * anybody does next, and four of them across the top pushed the graph - the
 * only thing on this screen that answers a question - below the fold.
 *
 * ── Why it opens almost empty ───────────────────────────────────────────────
 * Overview first, then zoom and filter, then details on demand. The graph
 * starts on one focus node and its first hop, and grows only where somebody
 * opens it. Each hop shows its three most interesting neighbours and folds the
 * rest behind a count, which keeps the drawn graph inside a fixed budget and
 * represents what was left out rather than dropping it silently.
 *
 * The state that decides what is drawn - focus (a discovery id), what is
 * open and what has been revealed (node ARNs) - all lives in the URL, so a
 * link reproduces exactly what somebody was looking at.
 */
export default function AccessGraphPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [selectedId, setSelectedId] = useState('');
  const [fullscreen, setFullscreen] = useState(false);
  const [inputsOpen, setInputsOpen] = useState(false);

  const focusId = searchParams.get('focus') || '';
  const openParam = searchParams.get('open') || '';
  const moreParam = searchParams.get('more') || '';
  const expanded = useMemo(() => openParam.split(',').filter(Boolean), [openParam]);
  /* `more` is `<arn>~<count>` pairs. ARNs are full of colons, so the count
     takes the last separator. */
  const revealed = useMemo(() => {
    const out = {};
    for (const part of moreParam.split(',').filter(Boolean)) {
      const at = part.lastIndexOf('~');
      const count = Number(part.slice(at + 1));
      if (at > 0 && Number.isFinite(count) && count > 0) out[part.slice(0, at)] = count;
    }
    return out;
  }, [moreParam]);

  const setParams = useCallback(
    (changes) => {
      setSearchParams(
        (current) => {
          const next = new URLSearchParams(current);
          for (const [key, value] of Object.entries(changes)) {
            if (value) next.set(key, value);
            else next.delete(key);
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const access = useAccessGraph({ focusId, expanded, revealed });
  const { graph, focusRecord, statusOf, isSettled } = access;

  /* The chosen default focus goes into the URL, so the link in the address
     bar always reproduces the screen. */
  useEffect(() => {
    if (!focusId && focusRecord?.row?.id) setParams({ focus: focusRecord.row.id });
  }, [focusId, focusRecord, setParams]);

  /* Nodes whose own neighbourhood is still being read show a spinner rather
     than a chevron that might be wrong. */
  const data = useMemo(() => {
    if (!graph) return null;
    return {
      ...graph,
      nodes: graph.nodes.map((node) => (statusOf(node.id) === 'loading' ? { ...node, pending: true } : node)),
    };
  }, [graph, statusOf]);

  /* The first hop opens itself, so the graph arrives with a shape to read
     rather than as two boxes and a line: every role the focus uses (bound_to),
     because what an identity can reach is what its roles reach - or, when it
     uses none, its most interesting neighbour. Once per focus, and only after
     every first-hop node has been read - otherwise the choice would go to
     whichever answered first, not to the best one. */
  const autoOpened = useRef(null);
  useEffect(() => {
    /* The URL can name the new focus a render before the graph has switched
       to it; never auto-open on the previous focus's graph. */
    if (!data || expanded.length > 0 || autoOpened.current === focusId) return;
    if (!focusId || focusRecord?.row?.id !== focusId) return;
    const firstHop = data.nodes.filter((node) => node.depth === 1);
    if (firstHop.some((node) => node.pending || !isSettled(node.id))) return;
    autoOpened.current = focusId;
    const openable = firstHop.filter((node) => node.unseenCount > 0);
    const focusArn = data.focus?.id;
    const used = new Set(
      data.edges.filter((edge) => edge.from === focusArn && edge.kinds.includes('bound_to')).map((edge) => edge.to),
    );
    const roles = openable.filter((node) => used.has(node.id));
    const first = roles.length > 0 ? roles : openable.sort((a, b) => b.interest - a.interest).slice(0, 1);
    if (first.length > 0) setParams({ open: first.map((node) => node.id).join(',') });
  }, [data, expanded.length, focusId, focusRecord, setParams, isSettled]);

  const focusNode = useMemo(() => data?.nodes.find((node) => node.isFocus) ?? null, [data]);
  const selected = useMemo(
    () => (selectedId ? (data?.nodes.find((node) => node.id === selectedId) ?? null) : null),
    [data, selectedId],
  );

  const onToggleExpand = useCallback(
    (node) => {
      const isOpen = expanded.includes(node.id);
      const next = isOpen ? expanded.filter((id) => id !== node.id) : [...expanded, node.id];
      setParams({ open: next.join(',') });
    },
    [expanded, setParams],
  );

  const onReveal = useCallback(
    (parentId) => {
      const next = { ...revealed, [parentId]: (revealed[parentId] ?? 0) + REVEAL_STEP };
      setParams({
        more: Object.entries(next)
          .map(([id, count]) => `${id}~${count}`)
          .join(','),
      });
    },
    [revealed, setParams],
  );

  const options = useMemo(() => focusOptions(access.index), [access.index]);

  const inspectorPanel = (
    <IdentityPanel
      node={selected ?? focusNode}
      isFocusNode={!selected}
      store={access.store}
      status={statusOf((selected ?? focusNode)?.id)}
      error={access.errorOf((selected ?? focusNode)?.id)}
      onRetry={access.retryNode}
      onExpand={onToggleExpand}
      onFocus={(id) => {
        setSelectedId('');
        setParams({ focus: id, open: '', more: '' });
      }}
      expandedIds={expanded}
    />
  );

  /* A focus id that does not exist here, or is not an id at all (a hand-edited
     link), is the same thing to the reader. */
  const missingFocus = access.error?.status === 404 || access.error?.status === 400;

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Access graph"
        lede="What an identity can reach: the roles it uses, who may use those roles, and what it called. Pick a starting point, then open any box to follow it further."
        actions={
          <>
            <FocusPicker
              options={options.rows}
              partial={options.partial}
              value={focusRecord?.row?.id ?? focusId}
              onChange={(id) => {
                setSelectedId('');
                setParams({ focus: id, open: '', more: '' });
              }}
            />
            <IconButton
              icon={Info}
              label="What this graph is built from"
              onClick={() => setInputsOpen(true)}
            />
          </>
        }
      />

      {access.activityGap && !access.error && (
        <div role="status" className="flex flex-wrap items-center gap-3 rounded-[var(--radius-control)] border border-medium/30 bg-medium-soft px-4 py-2.5">
          <AlertTriangle aria-hidden="true" className="size-4 shrink-0 text-medium" />
          <p className="min-w-0 flex-1 text-[12.5px] text-ink-2">
            The AWS activity log (CloudTrail) could not be read just now, so some grey &ldquo;Called&rdquo; lines (what an
            identity used) may be missing. Roles, keys and trust relationships are complete.
          </p>
          <Button variant="secondary" size="sm" icon={RotateCw} onClick={access.retryActivity}>
            Try again
          </Button>
        </div>
      )}

      {access.error ? (
        <Panel prominence="quiet">
          <ErrorState
            error={access.error}
            title={missingFocus ? 'That identity is not in the accounts and regions you selected' : undefined}
            onRetry={missingFocus ? undefined : access.retry}
          />
        </Panel>
      ) : access.empty ? (
        <Panel prominence="quiet">
          <EmptyState
            icon={Network}
            title="Nothing to draw yet"
            description="No identity or credential was found in the accounts and regions you selected. Add accounts or regions under Integrations, or wait for the next discovery scan."
            action={
              <Button variant="secondary" as={Link} to="/identities">
                Open identities
              </Button>
            }
          />
        </Panel>
      ) : !data ? (
        <div className="grid gap-4 @min-[70rem]:grid-cols-[minmax(0,1fr)_320px] @min-[90rem]:grid-cols-[minmax(0,1fr)_364px]">
          <div
            className="rounded-[var(--radius-panel)] border border-line bg-inset p-4"
            role="status"
            aria-label="Loading the access graph"
          >
            <Skeleton className="h-[420px] w-full" />
          </div>
          <Panel prominence="quiet">
            <DetailSkeleton rows={5} />
          </Panel>
        </div>
      ) : (
        <div
          className={
            fullscreen
              ? 'contents'
              : 'grid gap-4 @min-[70rem]:grid-cols-[minmax(0,1fr)_320px] @min-[90rem]:grid-cols-[minmax(0,1fr)_364px]'
          }
        >
          <div className="flex min-w-0 flex-col">
            <AccessFlow
              data={data}
              selectedId={selected?.id ?? ''}
              onSelect={(node) => setSelectedId(node?.id ?? '')}
              onToggleExpand={onToggleExpand}
              onReveal={onReveal}
              onFullscreenChange={setFullscreen}
              inspector={inspectorPanel}
              height={560}
            />
          </div>

          {!fullscreen && inspectorPanel}
        </div>
      )}

      <Modal
        open={inputsOpen}
        onClose={() => setInputsOpen(false)}
        title="What this graph is built from"
        description="The boxes are what discovery found in your AWS accounts. Every line comes from your trust policies or from the AWS activity log, limited to the accounts and regions you selected."
        icon={Info}
        tone="brand"
        footer={
          <Button variant="secondary" onClick={() => setInputsOpen(false)}>
            Close
          </Button>
        }
      >
        {inputsOpen && <GraphInputs activityAvailable={access.index?.identities?.activityAvailable} />}
      </Modal>
    </div>
  );
}

/** The focus picker's rows, from the inventory index. */
function focusOptions(index) {
  if (!index) return { rows: [], partial: false };
  const identities = index.identities?.rows ?? [];
  const credentials = index.credentials?.rows ?? [];
  const credentialByArn = new Map(credentials.map((row) => [row.arn, row]));
  const identityArns = new Set(identities.map((row) => row.arn));
  const rows = [
    ...identities.map((row) => ({
      id: row.id,
      arn: row.arn,
      name: row.name || row.arn,
      kind: 'identity',
      typeLabel: subtypeLabel('identity', row.identity_type),
      accountName: row.account_id,
      publicPolicy: Boolean(row.is_publicly_accessible),
      externalTrust: Boolean(credentialByArn.get(row.arn)?.has_external_trust),
    })),
    ...credentials
      .filter((row) => !identityArns.has(row.arn))
      .map((row) => ({
        id: row.id,
        arn: row.arn,
        name: row.name || row.arn,
        kind: 'credential',
        typeLabel: subtypeLabel('credential', row.credential_type),
        accountName: row.account_id,
        publicPolicy: false,
        externalTrust: Boolean(row.has_external_trust),
      })),
  ].sort((a, b) => a.name.localeCompare(b.name));
  return { rows, partial: !index.identities?.complete || !index.credentials?.complete };
}

export { AccessGraphPage };
