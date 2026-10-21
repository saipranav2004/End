import { useCallback } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Activity, ArrowUpRight } from 'lucide-react';
import { fetchIdentities, fetchIdentity } from '../../lib/api/inventory';
import { useQuery } from '../../lib/hooks';
import { identityRoute } from '../../lib/identityLinks';
import { formatDateTime, formatRelative } from '../../lib/format';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { Panel, PanelHeader } from '../../ui/Panel';
import { ListSkeleton } from '../../ui/Skeleton';
import { EmptyState, InlineError } from '../../ui/States';
import { identityTypeLabel } from '../identities/meta';
import { ActivitySummary } from './ActivitySections';
import { EventTimeline } from './EventTimeline';
import { IdentityPicker } from './IdentityPicker';
import { EVENT_DEFAULTS, useEventState } from './eventState';

const DEFAULTS = { range: '7d' };

/**
 * API activity, one identity at a time.
 *
 * The events API is per identity (`/identities/{id}/events`) - there is no
 * tenant-wide feed - so the screen starts from a choice of identity, and
 * offers the most recently active ones to start from. Once one is chosen, the
 * window's summary and its events are the API's own figures for that identity;
 * the identity, range, filters and open event all live in the URL.
 */
export default function ActivityPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const identityId = searchParams.get('identity') || '';
  const { state, patch, clearFilters } = useEventState({ urlBacked: true, defaults: DEFAULTS });

  const identityQuery = useQuery((signal) => fetchIdentity(identityId, 'overview', {}, signal), [identityId], {
    enabled: Boolean(identityId),
  });
  const identity = identityQuery.data?.identity;

  const choose = useCallback(
    (picked) => {
      const next = new URLSearchParams(searchParams);
      Object.keys(EVENT_DEFAULTS).forEach((key) => {
        if (!['range', 'from', 'to', 'view'].includes(key)) next.delete(key);
      });
      next.set('identity', picked.id);
      setSearchParams(next, { replace: false });
    },
    [searchParams, setSearchParams],
  );

  const picker = <IdentityPicker value={identityId} label={identity?.name} onChange={choose} />;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="API activity"
        actions={
          identity ? (
            <Button variant="secondary" as={Link} to={identityRoute(identity.id, 'activity')} iconRight={ArrowUpRight}>
              Open full record
            </Button>
          ) : null
        }
      />

      {!identityId ? (
        <StartHere picker={picker} onChoose={choose} />
      ) : identityQuery.isError && !identity ? (
        <Panel>
          <EmptyState
            icon={Activity}
            title={identityQuery.error?.status === 404 || identityQuery.error?.status === 400 ? 'No identity with that id' : 'The identity could not be loaded'}
            description={
              identityQuery.error?.status === 404 || identityQuery.error?.status === 400
                ? 'It may be outside the accounts and regions you have selected. Choose another.'
                : identityQuery.error?.message
            }
            action={picker}
          />
        </Panel>
      ) : (
        <>
          <ActivitySummary identityId={identityId} state={state} />
          <Panel prominence="lead" flush className="animate-rise overflow-hidden">
            <EventTimeline identityId={identityId} state={state} patch={patch} clearFilters={clearFilters} leading={picker} />
          </Panel>
          {identity && (
            <p className="text-[12px] text-ink-3">
              {identityTypeLabel(identity.identity_type)} in account <span className="font-mono">{identity.account_id}</span>
              {identity.last_activity_at
                ? ` · last call ${formatRelative(identity.last_activity_at)} (${formatDateTime(identity.last_activity_at)})`
                : ' · no call recorded'}
            </p>
          )}
        </>
      )}
    </div>
  );
}

function StartHere({ picker, onChoose }) {
  const recentQuery = useQuery(
    (signal) => fetchIdentities({ page: 1, pageSize: 8, sortBy: 'last_activity_at', sortOrder: 'desc' }, signal),
    [],
  );
  const rows = recentQuery.data?.rows ?? [];
  const activityAvailable = recentQuery.data?.activityAvailable !== false;

  return (
    <Panel prominence="lead" className="animate-rise">
      <PanelHeader
        title="Choose an identity"
        subtitle="Pick an identity to see every AWS API call it made, including calls made with its keys and roles."
        actions={<div className="w-72 max-w-full">{picker}</div>}
      />
      <p className="mt-5 text-[10.5px] font-semibold tracking-[0.11em] text-ink-3 uppercase">Most recently active</p>
      {recentQuery.isError && !recentQuery.data ? (
        <div className="mt-2">
          <InlineError error={recentQuery.error} onRetry={recentQuery.refetch} label="Identities unavailable" />
        </div>
      ) : !recentQuery.data ? (
        <div className="mt-2">
          <ListSkeleton rows={4} />
        </div>
      ) : !activityAvailable ? (
        <p className="mt-2 text-[12.5px] text-ink-3">CloudTrail data cannot be read right now, so recent activity is unknown.</p>
      ) : rows.length === 0 ? (
        <p className="mt-2 text-[12.5px] text-ink-3">No identities in your scope.</p>
      ) : (
        <ul className="mt-2 grid gap-2 @min-[40rem]:grid-cols-2 @min-[70rem]:grid-cols-4">
          {rows.map((row) => (
            <li key={row.id}>
              <button
                type="button"
                onClick={() => onChoose(row)}
                className="flex w-full flex-col rounded-[var(--radius-control)] border border-line bg-surface-2 px-3.5 py-2.5 text-left transition-colors hover:border-line-strong hover:bg-surface-3"
              >
                <span className="truncate text-[13px] font-semibold text-ink">{row.name || row.arn}</span>
                <span className="truncate text-[11.5px] text-ink-3">
                  {identityTypeLabel(row.identity_type)} · {row.last_activity_at ? formatRelative(row.last_activity_at) : 'no activity recorded'}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
