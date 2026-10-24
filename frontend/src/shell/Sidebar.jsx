import { useEffect, useRef, useState } from 'react';
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


/* The group that is always open. Everything else starts closed, one at a time,
   so the rail reads as a short table of contents rather than a wall of links. */
const DEFAULT_OPEN = 'overview';

/**
 * Scroll state for the nav: whether content hides above / below (drives the
 * edge fades) and an overlay thumb's size and offset in px. The native
 * scrollbar is hidden - with `scrollbar-width` set, Chrome ignores
 * ::-webkit-scrollbar styling and draws its own track and arrow buttons,
 * which is the heavy bar this replaces.
 */
function useScrollAffordance(ref) {
  const [state, setState] = useState({ above: false, below: false, thumb: null });
  useEffect(() => {
    const el = ref.current;
    if (!el) return undefined;
    const measure = () => {
      const { scrollTop, scrollHeight, clientHeight } = el;
      const overflow = scrollHeight - clientHeight > 1;
      const size = overflow ? Math.max(28, (clientHeight / scrollHeight) * clientHeight) : 0;
      const offset = overflow ? (scrollTop / (scrollHeight - clientHeight)) * (clientHeight - size) : 0;
      setState({
        above: scrollTop > 1,
        below: overflow && scrollTop + clientHeight < scrollHeight - 1,
        thumb: overflow ? { size, offset } : null,
      });
    };
    measure();
    el.addEventListener('scroll', measure, { passive: true });
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    /* Groups open and close inside it; their height change is the child's. */
    for (const child of el.children) observer.observe(child);
    return () => {
      el.removeEventListener('scroll', measure);
      observer.disconnect();
    };
  }, [ref]);
  return state;
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
     do nothing on touch screens and are hard to reach by keyboard. It is an
     accordion: Overview is always open, and of the rest only one is open at a
     time - opening a group closes the previous one. The group holding the
     current screen opens when you arrive on it. */
  const [openGroup, setOpenGroup] = useState(activeGroup !== DEFAULT_OPEN ? activeGroup : null);
  const [lastActive, setLastActive] = useState(activeGroup);
  if (activeGroup !== lastActive) {
    setLastActive(activeGroup);
    if (activeGroup && activeGroup !== DEFAULT_OPEN) setOpenGroup(activeGroup);
  }
  const navRef = useRef(null);
  const scroll = useScrollAffordance(navRef);
  const toggleGroup = (key) => setOpenGroup((current) => (current === key ? null : key));

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

      <div className="group/nav relative flex min-h-0 flex-1 flex-col">
      <nav
        ref={navRef}
        aria-label="Primary"
        data-above={scroll.above || undefined}
        data-below={scroll.below || undefined}
        className="nav-scroll min-h-0 flex-1 overflow-y-auto overscroll-contain px-2.5 py-2"
      >
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
          const pinned = group.key === DEFAULT_OPEN;
          const open = pinned || openGroup === group.key;
          const panelId = `nav-group-${group.key}`;
          const holdsActive = group.key === activeGroup;
          return (
            <div key={group.key} className="mb-1 last:mb-0">
              {pinned ? (
                /* Overview is always open: a label, not a control. */
                <p className="px-2.5 py-1.5 text-[10px] font-semibold tracking-[0.14em] text-ink-3 uppercase">{group.label}</p>
              ) : (
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
              )}
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
      {/* Overlay thumb: shown only while there is more to see and the pointer
          is over the rail (or it has focus), so it never competes with the
          links. Decorative - the list scrolls by wheel, touch and keyboard. */}
      {scroll.thumb && (
        <span
          aria-hidden="true"
          className="pointer-events-none absolute right-[3px] w-[3px] rounded-full bg-ink-3/45 opacity-0 transition-opacity duration-300 group-focus-within/nav:opacity-100 group-hover/nav:opacity-100"
          style={{ height: scroll.thumb.size, transform: `translateY(${scroll.thumb.offset}px)`, top: 0 }}
        />
      )}
      </div>
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
