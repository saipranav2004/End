import { Link } from 'react-router-dom';
import { FileWarning } from 'lucide-react';
import { Button } from '../ui/Button';
import { PageHeader } from '../shell/PageHeader';
import { Panel } from '../ui/Panel';
import { EmptyState } from '../ui/States';
import { useAccess } from './useAccess';
import { useWorkspace } from './WorkspaceContext';

const FEATURES = {
  exposure: {
    on: (w) => w.exposureEnabled,
    title: 'Credential scanning is not switched on',
    description: (canManage) =>
      canManage
        ? 'Turn on GitHub or CodeCommit scanning for one of your AWS accounts and secrets committed to code will be listed here.'
        : 'An admin turns on GitHub or CodeCommit scanning on the Integrations page; secrets committed to code are listed here from then on.',
  },
};

/**
 * A screen that belongs to a feature the workspace has not switched on says
 * how to switch it on. The navigation already hides it; this covers a typed
 * or bookmarked URL.
 */
export function RequireFeature({ feature, screen, children }) {
  const workspace = useWorkspace();
  const { can } = useAccess();
  const spec = FEATURES[feature];
  if (!spec || spec.on(workspace)) return children;
  const canManage = can('integrations.manage');
  return (
    <div className="flex flex-col gap-6">
      {screen && <PageHeader title={screen} />}
      <Panel>
        <EmptyState
          icon={FileWarning}
          title={spec.title}
          headingLevel={screen ? 2 : 3}
          description={spec.description(canManage)}
          action={
            <Button variant={canManage ? 'primary' : 'secondary'} size="sm" as={Link} to="/integrations">
              {canManage ? 'Open Integrations' : 'See Integrations'}
            </Button>
          }
        />
      </Panel>
    </div>
  );
}
