import { useState } from 'react';
import { ChevronRight, ShieldCheck } from 'lucide-react';
import { fetchAnomalies } from '../../lib/api/genome';
import { useQuery } from '../../lib/hooks';
import { formatDateTime, formatRelative } from '../../lib/format';
import { Panel, PanelHeader } from '../../ui/Panel';
import { SegmentedControl } from '../../ui/Tabs';
import { EmptyState, InlineError } from '../../ui/States';
import { ListSkeleton } from '../../ui/Skeleton';
import { Tag } from '../../ui/Tag';
import { AnomalyDrawer } from './AnomalyDrawer';
import { dimensionLabel, sourceTypeLabel } from './meta';

const STATES = [
  { value: 'open', label: 'Open' },
  { value: '', label: 'All' },
];

/**
 * The fleet's departures from baseline, newest first. Open one to see what
 * changed, the call behind it, and to record whether it was expected.
 */
export function AnomaliesPanel({ sourceType }) {
  const [state, setState] = useState('open');
  const [selected, setSelected] = useState(null);
  const query = useQuery((signal) => fetchAnomalies({ state, sourceType, limit: 10 }, signal), [state, sourceType]);
  const items = query.data?.items ?? [];

  return (
    <Panel prominence="quiet" className="animate-rise">
      <PanelHeader
        prominence="quiet"
        title="Departures from baseline"
        subtitle="Calls that did not match what an identity normally does. Open one to review it."
        actions={<SegmentedControl label="Which departures" options={STATES} value={state} onChange={setState} />}
      />
      {query.isError && !query.data ? (
        <InlineError error={query.error} onRetry={query.refetch} />
      ) : !query.data ? (
        <ListSkeleton rows={4} />
      ) : items.length === 0 ? (
        <EmptyState
          compact
          icon={ShieldCheck}
          title={state === 'open' ? 'No open departures' : 'No departures recorded'}
          description="Everything the watched identities did matched their normal behaviour."
        />
      ) : (
        <ul className="mt-3 divide-y divide-line">
          {items.map((item) => (
            <li key={item.id}>
              <button type="button" onClick={() => setSelected(item)} className="flex w-full items-center gap-3 py-2.5 text-left hover:bg-surface-2">
                <Tag tone="high" size="sm" dot>{dimensionLabel(item.dimension)}</Tag>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] text-ink">{item.what_changed}</span>
                  <span className="block truncate text-[11.5px] text-ink-3">
                    {item.identity.display_name || item.identity.identity_name} · {sourceTypeLabel(item.identity.source_type)}
                  </span>
                </span>
                <span className="shrink-0 text-[11.5px] text-ink-3" title={formatDateTime(item.detected_at)}>{formatRelative(item.detected_at)}</span>
                <ChevronRight aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <AnomalyDrawer anomaly={selected} onClose={() => setSelected(null)} onReviewed={query.refetch} />
    </Panel>
  );
}
