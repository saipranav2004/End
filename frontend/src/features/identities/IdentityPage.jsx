import { Component, useMemo } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ArrowLeft, Braces, Dna, Fingerprint, GitCompareArrows, ShieldCheck, Waypoints } from 'lucide-react';
import { accessGraphRoute, genomeRouteFor } from '../../lib/identityLinks';
import { BehaviourBaseline } from './BehaviourBaseline';
import { EntityChangesTab } from '../changes/EntityChangesTab';
import { PageHeader } from '../../shell/PageHeader';
import { Button } from '../../ui/Button';
import { CopyableValue } from '../../ui/Copyable';
import { Panel } from '../../ui/Panel';
import { DetailSkeleton, Skeleton } from '../../ui/Skeleton';
import { EmptyState, ErrorState } from '../../ui/States';
import { Tabs } from '../../ui/Tabs';
import { PostureRecord } from '../posture/PostureRecord';
import { usePostureIdentity } from '../posture/usePostureIdentity';
import { BandTag, ScoreRing } from '../posture/parts';
import { IdentityActivity } from '../activity/IdentityActivity';
import { IdentityTags } from './IdentityDrawer';
import { IdentityRecordPanels, recordTabs, useIdentityTab } from './IdentityRecord';
import { functionalClassMeta, identityTypeLabel } from './meta';

const RECORD_KEYS = new Set(['overview', 'permissions', 'credentials', 'roles', 'raw']);
/* Event-view keys the Activity tab keeps in the URL; dropped when leaving it. */
const ACTIVITY_KEYS = ['range', 'service', 'action', 'result', 'rw', 'resource', 'ip', 'cred', 'page', 'size', 'view', 'section', 'event'];

/**
 * One identity, in one place: `/identities/:id` (the discovery id).
 *
 * The inventory record (what it is, what it may do, what it authenticates
 * with, who can take it on), its posture score and fixes, and everything
 * CloudTrail recorded it doing. The access graph and the behaviour genome stay
 * specialist views, linked from the header, because a reachability map and a
 * behaviour baseline each need a whole screen.
 *
 * The tab lives in the URL (`?tab=`), so a link from posture, the graph or the
 * palette lands on the part that matters.
 */
export default function IdentityPage() {
  const { id } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const overviewQuery = useIdentityTab(id, 'overview');
  const overview = overviewQuery.data;
  const identity = overview?.identity;
  const postureRef = useMemo(
    () => (identity ? { id: identity.id, arn: identity.arn, name: identity.name } : null),
    [identity],
  );
  const posture = usePostureIdentity(postureRef);
  const scored = posture.data;

  const tabs = useMemo(() => {
    const base = recordTabs(overview);
    return [base[0], { value: 'posture', label: 'Posture', icon: ShieldCheck }, ...base.slice(1), { value: 'changes', label: 'Changes', icon: GitCompareArrows }, { value: 'raw', label: 'Raw', icon: Braces }];
  }, [overview]);
  const requested = searchParams.get('tab') || 'overview';
  const tab = tabs.some((entry) => entry.value === requested) ? requested : 'overview';
  const setTab = (value) => {
    const next = new URLSearchParams(searchParams);
    if (value !== 'activity') ACTIVITY_KEYS.forEach((key) => next.delete(key));
    if (value === 'overview') next.delete('tab');
    else next.set('tab', value);
    setSearchParams(next, { replace: true });
  };

  const back = (
    <Button variant="secondary" as={Link} to="/identities" icon={ArrowLeft}>
      Identities
    </Button>
  );

  if (overviewQuery.isError && !overview) {
    const missing = overviewQuery.error?.status === 404 || overviewQuery.error?.status === 400;
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Identity" actions={back} />
        <Panel>
          {missing ? (
            <EmptyState
              icon={Fingerprint}
              title="No identity with that id"
              description="It may be outside the accounts and regions you have selected, or a later discovery run removed it. Find it again from Identities."
              action={back}
            />
          ) : (
            <ErrorState error={overviewQuery.error} onRetry={overviewQuery.refetch} />
          )}
        </Panel>
      </div>
    );
  }

  if (!identity) {
    return (
      <div className="flex flex-col gap-6" aria-busy="true">
        <PageHeader title={<Skeleton className="h-7 w-64 rounded" />} actions={back} />
        <Panel prominence="lead">
          <DetailSkeleton rows={8} />
        </Panel>
        <span role="status" className="sr-only">
          Loading identity
        </span>
      </div>
    );
  }

  const genome = genomeRouteFor({ identity_type: identity.identity_type, arn: identity.arn });

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <span className="flex min-w-0 items-center gap-3">
            {scored && Number.isFinite(scored.score) && (
              <Link to="?tab=posture" replace aria-label={`Posture score ${scored.score}, open the Posture tab`} className="shrink-0">
                <ScoreRing score={scored.score} band={scored.band} grade={scored.grade} size={40} />
              </Link>
            )}
            <span className="min-w-0 truncate font-mono">{identity.name}</span>
          </span>
        }
        lede={`${identityTypeLabel(identity.identity_type)} · ${functionalClassMeta(identity.functional_class).label} · ${identity.account_id} · ${identity.region}`}
        actions={
          <>
            {back}
            <Button variant="secondary" as={Link} to={accessGraphRoute(identity.id)} icon={Waypoints}>
              Access graph
            </Button>
            {genome && (
              <Button variant="secondary" as={Link} to={genome} icon={Dna}>
                NHI Genome
              </Button>
            )}
          </>
        }
        meta={
          <div className="flex flex-wrap items-center gap-2">
            {scored && Number.isFinite(scored.score) && <BandTag score={scored.score} band={scored.band} />}
            <IdentityTags identity={identity} />
            <span className="max-w-full min-w-0 text-[11.5px] text-ink-3">
              <CopyableValue value={identity.arn} />
            </span>
          </div>
        }
        tabs={<Tabs size="sm" tabs={tabs} value={tab} onChange={setTab} />}
      />

      {tab === 'overview' && <BehaviourBaseline identity={identity} />}

      {RECORD_KEYS.has(tab) && (
        <Panel className="animate-rise">
          <IdentityRecordPanels identityId={identity.id} tab={tab} overviewQuery={overviewQuery} />
        </Panel>
      )}

      {tab === 'posture' && (
        <TabBoundary label="Posture">
          <PostureRecord identity={postureRef} />
        </TabBoundary>
      )}

      {tab === 'activity' && <IdentityActivity identityId={identity.id} />}

      {tab === 'changes' && (
        <Panel className="animate-rise">
          <EntityChangesTab kind="identity" id={identity.id} />
        </Panel>
      )}
    </div>
  );
}

/** Keeps one failing tab from taking the whole record down with it. */
class TabBoundary extends Component {
  constructor(props) {
    super(props);
    this.state = { failed: false };
  }

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch() {
    /* Rendered as a panel below; nothing is logged, so no internals reach the console. */
  }

  render() {
    if (this.state.failed) {
      return (
        <Panel>
          <ErrorState
            title={`${this.props.label} could not be shown`}
            error={{ message: 'This part of the record failed to render. The rest of the record is unaffected.' }}
            onRetry={() => this.setState({ failed: false })}
          />
        </Panel>
      );
    }
    return this.props.children;
  }
}
