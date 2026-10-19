import { useCallback, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { identitySearchRoute, postureRoute, resolveDiscoveryId } from '../../lib/api/posture';
import { identityRoute } from '../../lib/identityLinks';
import { useToast } from '../../ui/Toast';

/**
 * Opens a posture identity. With its posture id it opens the identity's
 * posture page (`/posture/:id`). Without one (a row from elsewhere that only
 * has a name and ARN) the identity page is found by exact ARN, and when
 * discovery does not hold it the inventory search for its name opens instead.
 */
export function useOpenPostureIdentity() {
  const navigate = useNavigate();
  const { notify } = useToast();
  const [opening, setOpening] = useState(null);

  const open = useCallback(
    async ({ postureId, name, arn }) => {
      /* A scored identity opens on its own posture page; that page links on
         to the full identity record. */
      if (postureId) {
        navigate(postureRoute(postureId));
        return;
      }
      setOpening(name);
      try {
        const id = await resolveDiscoveryId({ postureId, name, arn });
        if (id) {
          navigate(identityRoute(id, 'posture'));
          return;
        }
        notify({
          variant: 'info',
          title: 'Not in the current inventory',
          description: `${name} is scored by posture but is not in the inventory you can see. Showing the closest matches by name.`,
        });
        navigate(identitySearchRoute(name));
      } catch (error) {
        notify({ variant: 'error', title: 'Could not open the identity', description: error?.message });
      } finally {
        setOpening(null);
      }
    },
    [navigate, notify],
  );

  return { open, opening };
}
