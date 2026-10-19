import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, ArrowUpRight, Gauge } from 'lucide-react';
import { fetchPostureHeader, identitySearchRoute, resolveDiscoveryId } from '../../lib/api/posture';
import { titleCaseEnum } from '../../lib/format';
import { useQuery } from '../../lib/hooks';
import { identityRoute } from '../../lib/identityLinks';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { CopyableValue } from '../../ui/Copyable';
import { Panel } from '../../ui/Panel';
import { DetailSkeleton, Skeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState } from '../../ui/States';
import { useToast } from '../../ui/Toast';
import { PostureBody } from './PostureRecord';
import { identityTypeLabel } from './parts';

/**
 * One identity's posture, on its own: `/posture/:id` (the posture id).
 *
 * Opened from the Posture page, so it shows only posture - the score, why it
 * is what it is, the checks, history and findings. "Open full record" leads to
 * the identity page for everything else (permissions, credentials, activity).
 */
export default function PostureIdentityPage() {
  const { id } = useParams();
  const headerQuery = useQuery((signal) => fetchPostureHeader(id, signal), [id]);
  const header = headerQuery.data?.header;

  const back = (
    <Button variant="secondary" as={Link} to="/posture" icon={ArrowLeft}>
      Posture
    </Button>
  );

  if (headerQuery.isError && !header) {
    const missing = headerQuery.error?.status === 404 || headerQuery.error?.status === 400;
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Identity posture" actions={back} />
        <Panel>
          {missing ? (
            <EmptyState
              icon={Gauge}
              title="No posture score for this identity"
              description="It has not been scored yet, or a later scan no longer found it. Identities are scored after each discovery scan."
              action={back}
            />
          ) : (
            <ErrorState error={headerQuery.error} onRetry={headerQuery.refetch} title="The identity's posture failed to load" />
          )}
        </Panel>
      </div>
    );
  }

  if (!header) {
    return (
      <div className="flex flex-col gap-6" aria-busy="true">
        <PageHeader title={<Skeleton className="h-7 w-64 rounded" />} actions={back} />
        <Panel prominence="lead">
          <DetailSkeleton rows={6} />
        </Panel>
        <span role="status" className="sr-only">
          Loading posture
        </span>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<span className="block min-w-0 truncate font-mono">{header.name}</span>}
        lede={[
          identityTypeLabel(header.type),
          header.functional_class ? titleCaseEnum(header.functional_class) : null,
          header.account?.name || header.account?.id,
          header.region,
        ]
          .filter(Boolean)
          .join(' · ')}
        actions={
          <>
            {back}
            <OpenFullRecord header={header} />
          </>
        }
        meta={
          header.arn && (
            <span className="max-w-full min-w-0 text-[11.5px] text-ink-3">
              <CopyableValue value={header.arn} />
            </span>
          )
        }
      />
      <PostureBody key={header.identity_id} header={header} identity={{ name: header.name, arn: header.arn }} />
    </div>
  );
}

/** The identity page needs the discovery id; it is matched by exact ARN. */
function OpenFullRecord({ header }) {
  const navigate = useNavigate();
  const { notify } = useToast();
  const [opening, setOpening] = useState(false);

  const open = async () => {
    setOpening(true);
    try {
      const discoveryId = await resolveDiscoveryId({ postureId: header.identity_id, name: header.name, arn: header.arn });
      if (discoveryId) {
        navigate(identityRoute(discoveryId));
        return;
      }
      notify({
        variant: 'info',
        title: 'Not in the current inventory',
        description: `${header.name} is scored by posture but is not in the inventory you can see. Showing the closest matches by name.`,
      });
      navigate(identitySearchRoute(header.name));
    } catch (error) {
      notify({ variant: 'error', title: 'Could not open the full record', description: error?.message });
      setOpening(false);
    }
  };

  return (
    <Button variant="primary" icon={ArrowUpRight} loading={opening} onClick={open} title="Permissions, credentials, role use and activity of this identity">
      Open full record
    </Button>
  );
}
