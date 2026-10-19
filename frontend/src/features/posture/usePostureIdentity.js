import { resolvePostureIdentity } from '../../lib/api/posture';
import { useQuery } from '../../lib/hooks';

/**
 * The posture header of a discovery identity { id, arn, name }, matched by
 * exact ARN: { identity_id, name, arn, type, functional_class, account,
 * region, score, band, grade, badges, counts }. `data` is null when the
 * identity has not been scored. For the identity page's header ring and band.
 */
export function usePostureIdentity(identity) {
  return useQuery((signal) => resolvePostureIdentity(identity, signal), [identity?.arn ?? '', identity?.name ?? ''], {
    enabled: Boolean(identity?.arn),
  });
}
