import { useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowUpRight, Globe, UserX } from 'lucide-react';
import { identityRoute } from '../../lib/identityLinks';
import { Button } from '../../ui/Button';
import { Drawer } from '../../ui/Overlay';
import { Tabs } from '../../ui/Tabs';
import { Tag } from '../../ui/Tag';
import { CopyableValue } from '../../ui/Copyable';
import { IdentityRecordPanels, recordTabs, useIdentityTab } from './IdentityRecord';
import { environmentLabel, functionalClassMeta, identityTypeLabel, lifecycleMeta } from './meta';

/**
 * Identity record preview.
 *
 * A drawer rather than a page: triage means scanning a list and opening
 * records in sequence, and a full navigation would lose the list position and
 * the filters behind it. The full record - posture, permissions and the whole
 * activity history - is one click away on the identity page. The caller keys
 * it by identity, so each record opens on its overview.
 */
export function IdentityDrawer({ identity, open, onClose }) {
  const [tab, setTab] = useState('overview');
  const overviewQuery = useIdentityTab(identity?.id, 'overview', { enabled: open });

  if (!identity) return null;

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="xl"
      eyebrow={functionalClassMeta(identity.functional_class).label}
      title={identity.name || identity.arn}
      subtitle={<CopyableValue value={identity.arn} />}
      header={
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
          <IdentityTags identity={identity} />
          {identity.id && (
            <Button
              as={Link}
              to={identityRoute(identity.id)}
              variant="link"
              size="sm"
              iconRight={ArrowUpRight}
              className="ml-auto text-[12px]"
            >
              Open identity page
            </Button>
          )}
        </div>
      }
    >
      <div className="px-4 sm:px-5">
        <Tabs tabs={recordTabs(overviewQuery.data, { withPermissions: false })} value={tab} onChange={setTab} size="sm" />
      </div>
      <div className="px-4 py-4 sm:px-5">
        <IdentityRecordPanels identityId={identity.id} tab={tab} active={open} overviewQuery={overviewQuery} compact />
      </div>
    </Drawer>
  );
}

/** Type, exposure, environment and lifecycle at a glance - the same tags on the drawer and the page. */
export function IdentityTags({ identity }) {
  const status = lifecycleMeta(identity.status);
  return (
    <>
      {identity.identity_type && (
        <Tag tone="neutral" size="sm">
          {identityTypeLabel(identity.identity_type)}
        </Tag>
      )}
      {identity.is_publicly_accessible && (
        <Tag tone="critical" size="sm" icon={Globe}>
          Publicly accessible
        </Tag>
      )}
      {identity.environment && (
        <Tag tone="info" size="sm">
          {environmentLabel(identity.environment)}
        </Tag>
      )}
      {identity.status && !['active', 'running', 'available'].includes(String(identity.status).toLowerCase()) && (
        <Tag tone={status.tone} size="sm" dot>
          {status.label}
        </Tag>
      )}
      {!identity.owner && (
        <Tag tone="medium" size="sm" icon={UserX}>
          No owner
        </Tag>
      )}
    </>
  );
}
