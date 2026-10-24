import { useState } from 'react';
import { Link } from 'react-router-dom';
import { LogOut, Menu, Moon, Search, Sun, ArrowLeftRight } from 'lucide-react';
import { useAuth } from '../app/AuthContext';
import { useThemeMode } from '../app/ThemeContext';
import { usePopover } from '../lib/hooks';
import { BrandLockup } from './Brand';
import { SyncButton } from '../features/sync/SyncButton';
// import { ScanSwitcher } from './ScanSwitcher';
import { initialsOf } from '../lib/format';
import { roleMeta } from '../lib/roles';
import { cn } from '../ui/cn';

/**
 * Global top bar - full viewport width, follows the theme.
 *
 * The brand sits in the left corner at a size that reads as a product mark
 * rather than a favicon; everything else sits in the right corner: scan scope,
 * search, then the account avatar. The bar is 64px so those controls can be
 * full-height targets instead of squeezed into a strip.
 *
 * Scan scope is here, not in the context row, because it is global state - it
 * rescopes every screen at once, and a console keeps global state in its global
 * chrome.
 *
 * Appearance is one button in the bar, not a section of the account menu. It
 * used to be a three-way light / dark / system control two clicks deep, which
 * is a lot of ceremony for flipping one thing. Until somebody presses it the
 * theme still follows the operating system; the first press makes the choice
 * explicit and it is remembered from then on.
 */
export function TopBar({ onOpenNav, onOpenCommand, minimal = false }) {
  const { user, logout } = useAuth();
  const { theme, setPreference } = useThemeMode();
  const nextTheme = theme === 'dark' ? 'light' : 'dark';
  const [menuOpen, setMenuOpen] = useState(false);
  const { wrapperRef: menuRef, triggerRef, panelProps } = usePopover(menuOpen, () =>
    setMenuOpen(false),
  );

  const iconButton =
    'grid size-10 shrink-0 place-items-center rounded-[var(--radius-control)] text-topbar-muted transition-colors hover:bg-topbar-hover hover:text-topbar-ink';

  return (
    <header className="topbar-surface fixed inset-x-0 top-0 z-40 flex h-16 items-center gap-2 border-b border-topbar-line px-3 sm:px-4 lg:px-5">
      {!minimal && (
        <button
          type="button"
          onClick={onOpenNav}
          aria-label="Open navigation"
          className={cn(iconButton, 'lg:hidden')}
        >
          <Menu aria-hidden="true" className="size-5" />
        </button>
      )}

      <Link
        to={minimal ? '/integrations' : '/overview'}
        aria-label="Deep Algorithms - go to posture overview"
        className="shrink-0 rounded px-1"
      >
        {/* The display toggle lives on a wrapper, not on the lockup itself.
            `cn` is a plain join, so passing `hidden` to a component whose own
            base class is `inline-flex` left both on the element and let CSS
            source order decide - which rendered the wordmark AND the mark
            together below 640px, with the wordmark overflowing the bar. */}
        <span className="hidden sm:block">
          <BrandLockup height={34} />
        </span>
        <img
          src="/brand/mark.png"
          alt="Deep Algorithms"
          className="block size-8 object-contain sm:hidden"
          draggable="false"
        />
      </Link>

      <div aria-hidden="true" className="flex-1" />

      {/* Global scan scope. Out of this build along with the scans screen -
          one discovery run means nothing to switch between. See the note in
          `shell/navigation.js`. */}
      {/* <ScanSwitcher /> */}

      {/* The scan: one click reads the AWS accounts and scores the result. */}
      {!minimal && <SyncButton />}

      {/* Search - a real control, not an afterthought. The palette behind it
          carries section jumps and identity lookup. Not during setup: there
          is nothing to search yet. */}
      {!minimal && (
      <button
        type="button"
        onClick={onOpenCommand}
        className="hidden h-9 min-w-0 shrink-0 items-center gap-2.5 rounded-[var(--radius-control)] border border-topbar-line bg-topbar-field px-3 text-left text-[13px] text-topbar-muted transition-[background-color,border-color] duration-150 hover:border-ink-3/45 hover:text-topbar-ink lg:flex lg:w-56 xl:w-64"
      >
        <Search aria-hidden="true" className="size-4 shrink-0" />
        <span className="min-w-0 flex-1 truncate">Search</span>
        <kbd className="shrink-0 rounded border border-topbar-line bg-topbar px-1.5 py-0.5 font-mono text-[10.5px]">
          ⌘K
        </kbd>
      </button>

      )}
      {!minimal && (
        <button type="button" onClick={onOpenCommand} aria-label="Search" className={cn(iconButton, 'lg:hidden')}>
          <Search aria-hidden="true" className="size-5" />
        </button>
      )}

      {/* Shows the theme it switches TO - a sun in dark mode, a moon in light -
          which is the convention people already know from every other app. */}
      <button
        type="button"
        onClick={() => setPreference(nextTheme)}
        aria-label={`Switch to ${nextTheme} theme`}
        title={`Switch to ${nextTheme} theme`}
        className={iconButton}
      >
        {nextTheme === 'light' ? (
          <Sun aria-hidden="true" className="size-5" />
        ) : (
          <Moon aria-hidden="true" className="size-5" />
        )}
      </button>

      {/* Account - the avatar only. Identity details belong in the menu. */}
      <div ref={menuRef} className="relative shrink-0">
        <button
          ref={triggerRef}
          type="button"
          onClick={() => setMenuOpen((value) => !value)}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          aria-label={`Account menu for ${user?.name || user?.username || 'signed-in user'}`}
          className={cn(
            'grid size-10 shrink-0 place-items-center rounded-full transition-[box-shadow,transform] duration-150',
            menuOpen ? 'ring-2 ring-brand/45' : 'hover:ring-2 hover:ring-line-strong',
          )}
        >
          <span
            aria-hidden="true"
            className="grid size-9 place-items-center rounded-full bg-[linear-gradient(135deg,var(--t-brand)_0%,var(--t-accent)_100%)] text-[13px] font-bold text-white"
          >
            {initialsOf(user?.name || user?.username)}
          </span>
        </button>

        {menuOpen && (
          <div
            role="menu"
            {...panelProps}
            className="animate-pop absolute right-0 z-50 w-72 overflow-y-auto rounded-[var(--radius-panel)] border border-line bg-surface shadow-lg"
          >
            <div className="flex items-start gap-3 border-b border-line bg-surface-2 px-3.5 py-3.5">
              <span
                aria-hidden="true"
                className="grid size-10 shrink-0 place-items-center rounded-full bg-[linear-gradient(135deg,var(--t-brand)_0%,var(--t-accent)_100%)] text-[13px] font-bold text-white"
              >
                {initialsOf(user?.name || user?.username)}
              </span>
              <span className="min-w-0">
                <span className="block truncate text-[13.5px] font-semibold text-ink">
                  {user?.name || 'Signed in'}
                </span>
                {user?.username && (
                  <span className="mt-0.5 block truncate font-mono text-[12px] text-ink-3">{user.username}</span>
                )}
                <span className="mt-1.5 flex flex-wrap gap-1.5">
                  {user?.role && (
                    <span className="rounded-full border border-brand/25 bg-info-soft px-1.5 py-0.5 text-[10.5px] font-semibold text-brand">
                      {roleMeta(user.role).label}
                    </span>
                  )}
                  {user?.team && (
                    <span className="rounded-full border border-line-strong bg-surface-3 px-1.5 py-0.5 text-[10.5px] font-medium text-ink-2">
                      {user.team}
                    </span>
                  )}
                </span>
              </span>
            </div>

            <div className="p-1">
              {/* Administration of the console itself lives with the account,
                  not in the navigation of the estate. Super admins only. */}
              <Link
                role="menuitem"
                to="/login?switch=1"
                onClick={() => setMenuOpen(false)}
                className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-[13px] text-ink-2 transition-colors duration-150 hover:bg-surface-2 hover:text-ink focus-visible:bg-surface-2 focus-visible:text-ink"
              >
                <ArrowLeftRight aria-hidden="true" className="size-4" />
                Sign in as another user
              </Link>
              <button
                role="menuitem"
                type="button"
                onClick={logout}
                /* Destructive-by-intent, so it colours red on hover rather
                   than taking the neutral hover every other menu item gets.
                   The resting state stays neutral: a permanently red row in a
                   profile menu reads as an error, not an action. */
                className="flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left text-[13px] text-ink-2 transition-colors duration-150 hover:bg-critical-soft hover:text-critical focus-visible:bg-critical-soft focus-visible:text-critical"
              >
                <LogOut aria-hidden="true" className="size-4" />
                Sign out
              </button>
            </div>
          </div>
        )}
      </div>
    </header>
  );
}

