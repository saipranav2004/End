import { Link } from 'react-router-dom';
import { Lock } from 'lucide-react';
import { Button } from '../ui/Button';
import { roleMeta } from '../lib/roles';
import { PageHeader } from '../shell/PageHeader';
import { Panel } from '../ui/Panel';
import { EmptyState } from '../ui/States';
import { useAccess } from './useAccess';

/**
 * A screen a role cannot use at all says so, with the role that can - rather
 * than rendering controls that would all be locked, or a 404 that implies the
 * screen does not exist. The navigation already hides it; this covers a
 * typed or bookmarked URL.
 */
export function RequirePermission({ permission, title, screen, children }) {
  const { can, lock, role } = useAccess();
  if (can(permission)) return children;
  const locked = (
    <Panel>
      <EmptyState
        icon={Lock}
        title={title ?? 'Your role cannot open this screen'}
        headingLevel={screen ? 2 : 3}
        description={`You are signed in as ${roleMeta(role).label}. ${lock(permission)}`}
        action={
          <Button variant="secondary" size="sm" as={Link} to="/overview">
            Go to your home
          </Button>
        }
      />
    </Panel>
  );
  /* A route guard stands in for the whole screen, so it keeps the screen's
     heading: the page still says where you are. */
  if (!screen) return locked;
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={screen} />
      {locked}
    </div>
  );
}
