import { Link } from 'react-router-dom';
import { ArrowRight, ChevronRight, GitCompareArrows, History } from 'lucide-react';
import { scanChangesRoute } from '../../lib/api/changes';
import { formatDate, formatNumber, formatRelative } from '../../lib/format';
import { Button } from '../../ui/Button';
import { Panel, PanelHeader } from '../../ui/Panel';
import { cn } from '../../ui/cn';
import { CHANGE_META, CHANGE_TYPES, ENTITY_META } from './parts';

/** "Jan 15, 2025 at 10:00 AM". */
export function scanTimeLabel(iso) {
  const date = iso ? new Date(iso) : null;
  if (!date || Number.isNaN(date.getTime())) return '-';
  const day = date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  const time = date.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  return `${day} at ${time}`;
}

const COUNT_TONE = { CREATED: 'text-low', DELETED: 'text-critical', MODIFIED: 'text-medium' };
const ROW_TONE = {
  CREATED: 'border-low/25 bg-low-soft/50 hover:bg-low-soft',
  DELETED: 'border-critical/25 bg-critical-soft/50 hover:bg-critical-soft',
  MODIFIED: 'border-medium/30 bg-medium-soft/50 hover:bg-medium-soft',
};

/**
 * Dashboard card: what the latest discovery scan changed. Hidden until a scan
 * has run; the first scan says so instead of claiming "no changes".
 */
export function RecentChangesCard({ recent }) {
  if (!recent) return null;
  const first = !recent.previousScanId;
  const viewAll = scanChangesRoute(recent.lastScanId);

  if (first) {
    return (
      <Panel prominence="quiet" className="animate-rise">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <span className="grid size-10 shrink-0 place-items-center rounded-full border border-line bg-surface-2 text-ink-3">
            <History aria-hidden="true" className="size-4.5" />
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-[13.5px] font-semibold text-ink">First scan completed</p>
            <p className="text-[12.5px] text-ink-3">
              {scanTimeLabel(recent.lastScanAt)}. Changes will appear here after the next scan compares against it.
            </p>
          </div>
        </div>
      </Panel>
    );
  }

  const highlights = recent.highlights.slice(0, 6);

  return (
    <Panel prominence="default" className="animate-rise">
      <PanelHeader
        title="Recent changes"
        subtitle={
          <>
            Last scan: {scanTimeLabel(recent.lastScanAt)} · {formatRelative(recent.lastScanAt)} · compared with the scan of{' '}
            {formatDate(recent.previousScanAt)}
          </>
        }
        actions={
          <Button as={Link} to={viewAll} variant="secondary" size="sm" iconRight={ArrowRight}>
            View all changes
          </Button>
        }
      />

      {recent.total === 0 ? (
        <p className="mt-4 flex items-center gap-2 rounded-[var(--radius-control)] border border-line bg-surface-2 px-4 py-3 text-[13px] text-ink-2">
          <GitCompareArrows aria-hidden="true" className="size-4 shrink-0 text-ink-3" />
          No changes detected in the last scan.
        </p>
      ) : (
        <div className="mt-4 grid gap-5 @min-[52rem]:grid-cols-[minmax(0,13rem)_minmax(0,1fr)]">
          <div className="flex flex-col justify-center gap-3 rounded-[var(--radius-control)] border border-line bg-surface-2 px-4 py-3.5">
            <div>
              <p data-numeric="" className="text-[30px] leading-none font-bold text-ink">
                {formatNumber(recent.total)}
              </p>
              <p className="mt-1 text-[12px] text-ink-3">{recent.total === 1 ? 'change' : 'changes'} in the last scan</p>
            </div>
            <dl className="flex flex-wrap gap-x-4 gap-y-1">
              {CHANGE_TYPES.map((type) => (
                <div key={type} className="flex items-baseline gap-1">
                  <dd data-numeric="" className={cn('text-[13px] font-semibold', COUNT_TONE[type])}>
                    {formatNumber(recent.byChangeType[type] ?? 0)}
                  </dd>
                  <dt className="text-[11.5px] text-ink-3">{CHANGE_META[type].label.toLowerCase()}</dt>
                </div>
              ))}
            </dl>
          </div>

          <ul className="grid content-start gap-2 @min-[40rem]:grid-cols-2">
            {highlights.map((item) => {
              const meta = CHANGE_META[item.changeType] ?? CHANGE_META.MODIFIED;
              const Icon = meta.icon;
              const EntityIcon = ENTITY_META[item.entityType]?.icon;
              return (
                <li key={`${item.changeType}-${item.entityType}`}>
                  <Link
                    to={scanChangesRoute(recent.lastScanId, { change_type: item.changeType, entity_type: item.entityType })}
                    className={cn('group flex items-center gap-2.5 rounded-[var(--radius-control)] border px-3 py-2.5 transition-colors', ROW_TONE[item.changeType])}
                  >
                    <Icon aria-hidden="true" className={cn('size-4 shrink-0', COUNT_TONE[item.changeType])} />
                    <span className="min-w-0 flex-1 text-[12.5px] font-medium text-ink">{item.label}</span>
                    {EntityIcon && <EntityIcon aria-hidden="true" className="size-3.5 shrink-0 text-ink-3" />}
                    <ChevronRight aria-hidden="true" className="size-3.5 shrink-0 text-ink-3 transition-transform group-hover:translate-x-0.5" />
                  </Link>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </Panel>
  );
}
