import { Link } from 'react-router-dom';
import { Compass } from 'lucide-react';
import { Button } from '../ui/Button';
import { Panel } from '../ui/Panel';
import { EmptyState } from '../ui/States';

export default function NotFoundPage() {
  return (
    <Panel className="animate-rise">
      <EmptyState
        icon={Compass}
        title="That screen does not exist"
        description="The address you followed is not part of the console. It may have been renamed in a newer version."
        action={
          <Button as={Link} to="/overview" variant="primary" size="sm">
            Go to posture
          </Button>
        }
        secondaryAction={
          <Button as={Link} to="/identities" variant="secondary" size="sm">
            Open identity explorer
          </Button>
        }
      />
    </Panel>
  );
}
