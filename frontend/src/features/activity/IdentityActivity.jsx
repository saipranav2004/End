import { Panel } from '../../ui/Panel';
import { Tabs } from '../../ui/Tabs';
import {
  ActivitySummary,
  CredentialsUsedSection,
  CrossAccountSection,
  ErrorsSection,
  HeatmapSection,
  IpsSection,
  ResourcesSection,
  SessionsSection,
} from './ActivitySections';
import { EventFilters, EventTimeline, RangePicker } from './EventTimeline';
import { useEventState } from './eventState';

const SECTIONS = [
  { value: 'events', label: 'Events' },
  { value: 'heatmap', label: 'Busiest hours' },
  { value: 'resources', label: 'Resources used' },
  { value: 'ips', label: 'Source IPs' },
  { value: 'errors', label: 'Failed calls' },
  { value: 'cross', label: 'Other accounts' },
  { value: 'sessions', label: 'Sessions' },
  { value: 'credentials', label: 'Credentials used' },
];

const DEFAULTS = { range: '7d' };

/**
 * An identity's CloudTrail activity on its record page: the summary of the
 * window, then one section of the events API at a time. The range and the
 * filters narrow every section alike, so switching section keeps the same
 * question; choosing a resource, a source IP or a credential in a section
 * filters by it and opens the event list. Range, section, filters and the
 * open event are all in the URL.
 */
export function IdentityActivity({ identityId }) {
  const { state, patch, clearFilters } = useEventState({ urlBacked: true, defaults: DEFAULTS });
  const section = SECTIONS.some((entry) => entry.value === state.section) ? state.section : 'events';
  const filterEvents = (changes) => patch({ ...changes, section: 'events' });

  return (
    <div className="flex flex-col gap-5">
      <ActivitySummary identityId={identityId} state={state} />

      <Panel prominence="lead" flush className="animate-rise overflow-hidden">
        <div className="border-b border-line px-4 pt-2 sm:px-5">
          <Tabs size="sm" tabs={SECTIONS} value={section} onChange={(value) => patch({ section: value, event: '' })} />
        </div>
        {section === 'events' ? (
          <EventTimeline identityId={identityId} state={state} patch={patch} clearFilters={clearFilters} />
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-3 border-b border-line px-4 py-3 sm:px-5">
              <RangePicker state={state} patch={patch} />
              {['resources', 'ips', 'credentials'].includes(section) && (
                <span className="text-[12px] text-ink-3">Select a row to see its calls.</span>
              )}
            </div>
            <EventFilters identityId={identityId} state={state} patch={patch} clearFilters={clearFilters} />
            <div className="p-4 sm:p-5">
              {section === 'heatmap' && <HeatmapSection identityId={identityId} state={state} />}
              {section === 'resources' && <ResourcesSection identityId={identityId} state={state} onFilter={filterEvents} />}
              {section === 'ips' && <IpsSection identityId={identityId} state={state} onFilter={filterEvents} />}
              {section === 'errors' && <ErrorsSection identityId={identityId} state={state} patch={patch} />}
              {section === 'cross' && <CrossAccountSection identityId={identityId} state={state} />}
              {section === 'sessions' && <SessionsSection identityId={identityId} state={state} />}
              {section === 'credentials' && <CredentialsUsedSection identityId={identityId} state={state} onFilter={filterEvents} />}
            </div>
          </>
        )}
      </Panel>
    </div>
  );
}
