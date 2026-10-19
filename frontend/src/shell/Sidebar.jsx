import { useEffect, useState } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { ChevronDown, PanelLeftClose, PanelLeftOpen, X } from 'lucide-react';
import { useAccess } from '../app/useAccess';
import { navGroupsFor } from './navigation';
import { useWorkspace } from '../app/WorkspaceContext';
import { IconButton } from '../ui/Button';
import { cn } from '../ui/cn';

/**
 * Navigation only - no brand, no user chrome, no search. Those belong to the
 * top bar, which spans the viewport above this. The sidebar is an application
 * surface that recedes behind the content it navigates to.
 */
function SidebarLink({ item, collapsed, onNavigate, badge }) {
  return (
    <NavLink
      to={item.to}
      end={item.end}
      onClick={onNavigate}
      title={collapsed ? item.label : undefined}
      className={({ isActive }) =>
        cn(
          'group relative flex items-center gap-2.5 rounded-[var(--radius-control)] px-2.5 py-2 text-[13px] transition-colors duration-150',
          collapsed && 'justify-center px-0',
          isActive
            ? 'bg-info-soft font-semibold text-brand'
            : 'font-medium text-ink-2 hover:bg-surface-3 hover:text-ink',
        )
      }
    >
      {({ isActive }) => (
        <>
          <span
            aria-hidden="true"
            className={cn(
              'absolute top-1/2 left-0 w-[3px] -translate-y-1/2 rounded-r-full bg-brand transition-[height,opacity] duration-200',
              isActive ? 'h-5 opacity-100' : 'h-2 opacity-0',
            )}
          />
          <item.icon aria-hidden="true" className="size-4 shrink-0 opacity-80" />
          {!collapsed && (
            <>
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              {badge}
            </>
          )}
        </>
      )}
    </NavLink>
  );
}


const OPEN_KEY = 'dna.nav.openGroups';

function readOpenGroups() {
  try {
    const raw = JSON.parse(localStorage.getItem(OPEN_KEY) ?? 'null');
    return Array.isArray(raw) ? new Set(raw) : null;
  } catch {
    return null;
  }
}

function isActiveItem(item, pathname) {
  return pathname === item.to || (!item.end && pathname.startsWith(`${item.to}/`));
}

export function Sidebar({ collapsed, onToggleCollapse, mobileOpen, onCloseMobile }) {
  const { can, role } = useAccess();
  const { exposureEnabled } = useWorkspace();
  const badges = {};
  const { pathname } = useLocation();
  const groups = navGroupsFor(can, role, { exposure: exposureEnabled });
  const activeGroup = groups.find((group) => group.items.some((item) => isActiveItem(item, pathname)))?.key ?? null;

  /* Groups open and close on click, not hover: hover menus open by accident,
     do nothing on touch screens and are hard to reach by keyboard. The group
     holding the current screen is always opened when you arrive on it, and
     the rest remember how you left them. */
  const [openGroups, setOpenGroups] = useState(() => {
    /* The remembered groups, plus the one holding the page loaded - a
       reload or a deep link lands with its own group open too. */
    const remembered = readOpenGroups() ?? new Set();
    if (activeGroup) remembered.add(activeGroup);
    return remembered;
  });
  const [lastActive, setLastActive] = useState(activeGroup);
  if (activeGroup !== lastActive) {
    setLastActive(activeGroup);
    if (activeGroup && !openGroups.has(activeGroup)) setOpenGroups(new Set([...openGroups, activeGroup]));
  }
  useEffect(() => {
    try {
      localStorage.setItem(OPEN_KEY, JSON.stringify([...openGroups]));
    } catch {
      /* A remembered layout is a convenience; losing it costs nothing. */
    }
  }, [openGroups]);
  const toggleGroup = (key) =>
    setOpenGroups((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  const content = (
    <>
      {/* Module header. The collapse control belongs at the top, where an
          operator reaches for it - not buried at the bottom of the list. */}
      <div
        className={cn(
          'flex h-12 shrink-0 items-center',
          collapsed ? 'justify-center px-0' : 'gap-2 px-3.5',
        )}
      >
        {/* Collapsed, the rail shows icons only: an abbreviation of the module
            name would be one more thing to decode, and the expand control
            already says what the rail is. */}
        {!collapsed && (
          <span className="min-w-0 truncate font-display text-[12px] leading-tight font-extrabold tracking-[0.1em] text-ink uppercase">
            NHI Discovery
          </span>
        )}
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label={collapsed ? 'Expand navigation' : 'Collapse navigation'}
          title={collapsed ? 'Expand navigation' : 'Collapse navigation'}
          className={cn(
            'hidden size-7 shrink-0 place-items-center rounded-md text-ink-3 transition-colors hover:bg-surface-3 hover:text-ink lg:grid',
            !collapsed && 'ml-auto',
          )}
        >
          {collapsed ? (
            <PanelLeftOpen aria-hidden="true" className="size-4" />
          ) : (
            <PanelLeftClose aria-hidden="true" className="size-4" />
          )}
        </button>
      </div>

      <nav aria-label="Primary" className="min-h-0 flex-1 overflow-y-auto px-2.5 py-2">
        {groups.map((group, groupIndex) => {
          /* Collapsed to an icon rail there are no headings to click, so every
             item shows; a rule stands in for the group label, but never above
             the first group. */
          if (collapsed) {
            return (
              <div key={group.key} className="mb-3.5 last:mb-0">
                {groupIndex > 0 && <div aria-hidden="true" className="mx-3 mb-2 border-t border-line" />}
                <div className="flex flex-col gap-0.5">
                  {group.items.map((item) => (
                    <SidebarLink key={item.to} item={item} collapsed onNavigate={onCloseMobile} badge={badges?.[item.to]} />
                  ))}
                </div>
              </div>
            );
          }
          const open = openGroups.has(group.key);
          const panelId = `nav-group-${group.key}`;
          const holdsActive = group.key === activeGroup;
          return (
            <div key={group.key} className="mb-1 last:mb-0">
              <button
                type="button"
                onClick={() => toggleGroup(group.key)}
                aria-expanded={open}
                aria-controls={panelId}
                className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-left text-[10px] font-semibold tracking-[0.14em] text-ink-3 uppercase transition-colors hover:bg-surface-3 hover:text-ink-2"
              >
                <span className="min-w-0 flex-1 truncate">{group.label}</span>
                {/* A closed group that holds the current screen says so. */}
                {holdsActive && !open && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-brand" />}
                <ChevronDown
                  aria-hidden="true"
                  className={cn('size-3.5 shrink-0 transition-transform duration-200', open ? 'rotate-0' : '-rotate-90')}
                />
              </button>
              {/* Height animates through grid rows, which needs no measured
                  height. `inert` keeps the links of a closed group out of the
                  tab order and away from screen readers. */}
              <div
                id={panelId}
                inert={!open}
                className={cn(
                  'grid transition-[grid-template-rows,opacity] duration-250 ease-[var(--ease-out-quint)]',
                  open ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0',
                )}
              >
                <div className="min-h-0 overflow-hidden">
                  <div className="flex flex-col gap-0.5 pt-0.5 pb-2">
                    {group.items.map((item) => (
                      <SidebarLink
                        key={item.to}
                        item={item}
                        collapsed={false}
                        onNavigate={onCloseMobile}
                        badge={badges?.[item.to]}
                      />
                    ))}
                  </div>
                </div>
              </div>
            </div>
          );
        })}
      </nav>

    </>
  );

  return (
    <>
      <aside
        className={cn(
          'fixed top-16 bottom-0 left-0 z-30 hidden shrink-0 flex-col border-r border-sidebar-line bg-sidebar transition-[width] duration-250 ease-[var(--ease-out-quint)] lg:flex',
          collapsed ? 'w-[60px]' : 'w-[232px]',
        )}
      >
        {content}
      </aside>

      {mobileOpen && (
        <div className="fixed inset-0 top-16 z-50 flex lg:hidden">
          <button
            type="button"
            aria-label="Close navigation"
            onClick={onCloseMobile}
            className="animate-fade absolute inset-0 bg-[var(--t-overlay)] backdrop-blur-[2px]"
          />
          <aside
            className="animate-fade relative flex w-[268px] flex-col border-r border-sidebar-line bg-sidebar shadow-lg"
            aria-label="Primary navigation"
          >
            <div className="flex items-center justify-between border-b border-line px-3 py-2">
              <span className="text-[10.5px] font-semibold tracking-[0.14em] text-ink-3 uppercase">
                Navigate
              </span>
              <IconButton icon={X} label="Close navigation" size="sm" onClick={onCloseMobile} />
            </div>
            {content}
          </aside>
        </div>
      )}
    </>
  );
}
