import { Panel } from '../../ui/Panel';
import { SegmentedControl, Tabs } from '../../ui/Tabs';
import { EVENT_RANGES } from '../identities/meta';
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
import { EventTimeline } from './EventTimeline';
import { useEventState } from './eventState';

const SECTIONS = [
  { value: 'events', label: 'Events' },
  { value: 'heatmap', label: 'Heatmap' },
  { value: 'resources', label: 'Resources' },
  { value: 'ips', label: 'Source IPs' },
  { value: 'errors', label: 'Errors' },
  { value: 'cross', label: 'Cross-account' },
  { value: 'sessions', label: 'Sessions' },
  { value: 'credentials', label: 'Credentials used' },
];

const DEFAULTS = { range: '7d' };

/**
 * An identity's CloudTrail activity on its record page: the summary of the
 * window, then one section of the events API at a time. Choosing a resource,
 * a source IP or a credential in a section filters the event list by it.
 * Range, section, filters and the open event are all in the URL.
 */
export function IdentityActivity({ identityId }) {
  const { state, patch, clearFilters } = useEventState({ urlBacked: true, defaults: DEFAULTS });
  const section = SECTIONS.some((entry) => entry.value === state.section) ? state.section : 'events';
  const filterEvents = (changes) => patch({ ...changes, section: 'events' });

  return (
    <div className="flex flex-col gap-5">
      <ActivitySummary identityId={identityId} range={state.range} />

      <Panel prominence="lead" flush className="animate-rise overflow-hidden">
        <div className="border-b border-line px-4 pt-2 sm:px-5">
          <Tabs size="sm" tabs={SECTIONS} value={section} onChange={(value) => patch({ section: value, event: '' })} />
        </div>
        {section === 'events' ? (
          <EventTimeline identityId={identityId} state={state} patch={patch} clearFilters={clearFilters} />
        ) : (
          <div className="p-4 sm:p-5">
            <RangeNote
              range={state.range}
              onChange={(range) => patch({ range })}
              hint={['resources', 'ips', 'credentials'].includes(section) ? 'Select a row to list its events.' : null}
            />
            {section === 'heatmap' && <HeatmapSection identityId={identityId} range={state.range} />}
            {section === 'resources' && <ResourcesSection identityId={identityId} range={state.range} onFilter={filterEvents} />}
            {section === 'ips' && <IpsSection identityId={identityId} range={state.range} onFilter={filterEvents} />}
            {section === 'errors' && <ErrorsSection identityId={identityId} range={state.range} />}
            {section === 'cross' && <CrossAccountSection identityId={identityId} range={state.range} />}
            {section === 'sessions' && <SessionsSection identityId={identityId} range={state.range} />}
            {section === 'credentials' && <CredentialsUsedSection identityId={identityId} range={state.range} onFilter={filterEvents} />}
          </div>
        )}
      </Panel>
    </div>
  );
}

function RangeNote({ range, onChange, hint }) {
  return (
    <div className="mb-4 flex flex-wrap items-center gap-3">
      <SegmentedControl
        label="Time range"
        options={EVENT_RANGES.map((entry) => ({ value: entry.value, label: entry.label, title: entry.title }))}
        value={range}
        onChange={onChange}
      />
      {hint && <span className="text-[12px] text-ink-3">{hint}</span>}
    </div>
  );
}
