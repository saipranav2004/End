import { Link } from 'react-router-dom';
import { ShieldCheck, ShieldQuestion } from 'lucide-react';
import { fetchIdentity, fetchIdentityEvents, fetchPermissionSource } from '../../lib/api/inventory';
import { useQuery } from '../../lib/hooks';
import { formatDateTime, formatNumber, formatRelative, titleCaseEnum } from '../../lib/format';
import { Drawer } from '../../ui/Overlay';
import { DetailList, DetailRow, SectionLabel } from '../../ui/Panel';
import { DetailSkeleton, Skeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState, InlineError } from '../../ui/States';
import { Tag } from '../../ui/Tag';
import { CopyableValue } from '../../ui/Copyable';
import { bindingLabel, credentialTypeMeta } from '../identities/meta';

/**
 * One CloudTrail event (`section=detail`), with the policy that allowed it.
 *
 * The events API names the policy statement when the event store can match
 * it. When it cannot, the console asks `GET /api/v1/permission-source` - which
 * needs the credential's inventory id, so the credential the event was made
 * with is matched by name against the identity's bound credentials first.
 * A failed call was not allowed by anything, so no lookup is made for it.
 */
export function EventDetailDrawer({ identityId, eventId, onClose }) {
  const open = Boolean(eventId);
  const detailQuery = useQuery(
    (signal) => fetchIdentityEvents(identityId, 'detail', { eventId }, signal),
    [identityId, eventId],
    { enabled: open && Boolean(identityId), keepPrevious: false },
  );
  const detail = detailQuery.data;
  const event = detail?.event;
  const failed = event ? event.status === 'error' || Boolean(event.error_code) : false;
  const needsLookup = Boolean(event && !failed && !detail?.permission_source && event.credential_name);

  const sourceQuery = useQuery(
    async (signal) => {
      const overview = await fetchIdentity(identityId, 'overview', {}, signal);
      const credential = (overview?.bound_credentials ?? []).find(
        (entry) => entry.name === event.credential_name,
      );
      if (!credential) return { matched: false, note: 'The credential this call used is not bound to this identity in the inventory.' };
      return fetchPermissionSource(
        { credentialId: credential.credential_id, eventSource: event.event_source, eventName: event.event_name },
        signal,
      );
    },
    [identityId, event?.event_id],
    { enabled: needsLookup, keepPrevious: false },
  );

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="lg"
      eyebrow="CloudTrail event"
      title={event?.event_name || (detailQuery.isLoading ? 'Loading event…' : 'Event')}
      subtitle={
        event ? (
          <span className="flex flex-wrap items-center gap-1.5">
            <Tag tone={failed ? 'critical' : 'low'} size="sm" dot>
              {failed ? event.error_code || 'Failed' : 'Succeeded'}
            </Tag>
            <Tag tone={event.is_read_only ? 'neutral' : 'high'} size="sm">
              {event.is_read_only ? 'Read-only' : 'Mutating'}
            </Tag>
            <span className="text-[12px] text-ink-3">
              {formatDateTime(event.event_time)} · {formatRelative(event.event_time)}
            </span>
          </span>
        ) : null
      }
    >
      <div className="flex flex-col gap-5 px-4 py-4 sm:px-5">
        {detailQuery.isError ? (
          detailQuery.error?.status === 404 ? (
            <EmptyState compact title="Event not found" description="It may have aged out of the event store." />
          ) : (
            <ErrorState compact error={detailQuery.error} onRetry={detailQuery.refetch} />
          )
        ) : !event ? (
          <DetailSkeleton rows={8} />
        ) : detail.data_available === false ? (
          <EmptyState
            compact
            title="Event store unavailable"
            description="CloudTrail events cannot be read right now. Try again shortly."
          />
        ) : (
          <>
            <section>
              <SectionLabel>What happened</SectionLabel>
              <DetailList className="mt-1">
                <DetailRow label="Action" mono>
                  {event.event_source}:{event.event_name}
                </DetailRow>
                <DetailRow label="Resource">
                  {event.resource_arn ? (
                    <span className="flex flex-col gap-0.5">
                      <span>
                        {event.resource_name || '-'}
                        {event.resource_type && <span className="ml-1.5 text-ink-3">{event.resource_type.replace(/^AWS::/, '')}</span>}
                      </span>
                      <CopyableValue value={event.resource_arn} />
                    </span>
                  ) : (
                    'No resource recorded on the event'
                  )}
                </DetailRow>
                <DetailRow label="Region">{event.aws_region || '-'}</DetailRow>
                {failed && (
                  <DetailRow label="Error">
                    <span className="font-medium text-critical">{event.error_code}</span>
                    {event.error_message && (
                      <span className="mt-1 block text-[12.5px] leading-relaxed text-ink-2">{event.error_message}</span>
                    )}
                  </DetailRow>
                )}
                <DetailRow label="Event id" mono>
                  <CopyableValue value={event.event_id} />
                </DetailRow>
              </DetailList>
            </section>

            <section>
              <SectionLabel>Who made it</SectionLabel>
              <DetailList className="mt-1">
                <DetailRow label="Caller" mono>
                  <CopyableValue value={event.caller_arn} />
                  {event.caller_type && <span className="mt-0.5 block font-sans text-[11.5px] text-ink-3">{event.caller_type}</span>}
                </DetailRow>
                <DetailRow label="Credential">
                  {event.credential_name ? (
                    <>
                      <Link
                        to={`/credentials?search=${encodeURIComponent(event.credential_name)}`}
                        className="font-medium text-brand hover:underline"
                      >
                        {event.credential_name}
                      </Link>
                      <span className="ml-1.5 text-ink-3">
                        {credentialTypeMeta(event.credential_type).label}
                        {event.binding_type ? ` · ${bindingLabel(event.binding_type)}` : ''}
                      </span>
                    </>
                  ) : (
                    'Not resolved to an inventory credential'
                  )}
                </DetailRow>
                {event.session_issuer_arn && (
                  <DetailRow label="Session issuer" mono>
                    <CopyableValue value={event.session_issuer_arn} />
                  </DetailRow>
                )}
                {event.access_key_id && (
                  <DetailRow label="Access key id" mono>
                    {event.access_key_id}
                  </DetailRow>
                )}
                <DetailRow label="Source IP" mono>
                  {event.source_ip || '-'}
                </DetailRow>
                <DetailRow label="User agent">
                  <span className="font-mono text-[12px] break-all text-ink-2">{event.user_agent || '-'}</span>
                </DetailRow>
              </DetailList>
            </section>

            <section>
              <SectionLabel>Permission that allowed it</SectionLabel>
              <div className="mt-2">
                {failed ? (
                  <p className="text-[12.5px] text-ink-3">The call failed, so no permission allowed it.</p>
                ) : detail.permission_source ? (
                  <PermissionSource
                    matched
                    policy={detail.permission_source.policy_name}
                    type={detail.permission_source.policy_type}
                    effect={detail.permission_source.statement_effect}
                    scope={detail.permission_source.resource_scope}
                    usedCount={detail.permission_source.used_count_90d}
                  />
                ) : !event.credential_name ? (
                  <p className="text-[12.5px] text-ink-3">
                    The call is not tied to an inventory credential, so its policy cannot be looked up.
                  </p>
                ) : sourceQuery.isError ? (
                  <InlineError error={sourceQuery.error} onRetry={sourceQuery.refetch} label="Lookup failed" />
                ) : !sourceQuery.data ? (
                  <Skeleton className="h-16 rounded-[var(--radius-control)]" />
                ) : (
                  <PermissionSource
                    matched={sourceQuery.data.matched}
                    policy={sourceQuery.data.policy_source}
                    type={sourceQuery.data.policy_type}
                    effect={sourceQuery.data.effect}
                    scope={sourceQuery.data.resource_arn}
                    action={sourceQuery.data.matched_action}
                    note={sourceQuery.data.note}
                  />
                )}
              </div>
            </section>
          </>
        )}
      </div>
    </Drawer>
  );
}

function PermissionSource({ matched, policy, type, effect, scope, action, usedCount, note }) {
  if (!matched) {
    return (
      <div className="flex items-start gap-2.5 rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
        <ShieldQuestion aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-ink-3" />
        <p className="text-[12.5px] leading-relaxed text-ink-2">
          No granted permission in the inventory matches this call.
          {note && <span className="block text-ink-3">{note}.</span>}
        </p>
      </div>
    );
  }
  return (
    <div className="rounded-[var(--radius-control)] border border-line bg-surface-2 p-3.5">
      <p className="flex items-center gap-2 text-[13px] font-semibold text-ink">
        <ShieldCheck aria-hidden="true" className="size-4 shrink-0 text-low" />
        {policy || 'Unnamed policy'}
        {type && <span className="text-[11.5px] font-normal text-ink-3">{titleCaseEnum(type)} policy</span>}
      </p>
      <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-2 text-[12px] sm:grid-cols-3">
        {action && (
          <div>
            <dt className="text-ink-3">Matched action</dt>
            <dd className="font-mono text-ink-2">{action}</dd>
          </div>
        )}
        <div>
          <dt className="text-ink-3">Effect</dt>
          <dd className="text-ink-2">{effect || '-'}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-ink-3">Resource scope</dt>
          <dd className="truncate font-mono text-ink-2" title={scope || undefined}>
            {scope || '-'}
          </dd>
        </div>
        {usedCount !== undefined && usedCount !== null && (
          <div>
            <dt className="text-ink-3">Used in 90 days</dt>
            <dd className="text-ink-2">{formatNumber(usedCount)} times</dd>
          </div>
        )}
      </dl>
    </div>
  );
}
