import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { Loader2 } from 'lucide-react';
import { AppShell } from '../shell/AppShell';
import { OnboardingShell } from '../shell/OnboardingShell';
import { useWorkspace } from './WorkspaceContext';

/**
 * Until a workspace has connected an AWS account there is nothing for any
 * other screen to show, so the console is reduced to one screen: connect an
 * account. No navigation, no empty dashboard - the setup, in a focused
 * layout. The first successful connection opens the full console.
 *
 * "Connected" means connected at least once. A later check that fails does
 * not lock a workspace out of the data it already has; Integrations reports
 * that account as needing attention instead. If the check itself fails, the
 * console opens as usual (see WorkspaceContext) - every screen has its own
 * error state, and a gate that failed closed would strand people.
 */
export function ConnectionGate() {
  const { status, hasConnectedAccount } = useWorkspace();
  const { pathname } = useLocation();

  if (status === 'loading' || status === 'idle') {
    return (
      <div className="grid min-h-dvh place-items-center bg-canvas" aria-busy="true">
        <span className="inline-flex items-center gap-2.5 text-[13px] text-ink-3">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" />
          <span role="status">Opening your workspace…</span>
        </span>
      </div>
    );
  }

  if (!hasConnectedAccount) {
    /* One address for the setup, so a deep link to any other screen lands
       here and the back button has nowhere empty to go. */
    if (pathname !== '/integrations') return <Navigate to="/integrations" replace />;
    return (
      <OnboardingShell>
        <Outlet />
      </OnboardingShell>
    );
  }

  return <AppShell />;
}
