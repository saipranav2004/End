import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { fetchAwsIntegration } from '../lib/api/integrations';
import { useAuth } from './AuthContext';

const WorkspaceContext = createContext(null);

/**
 * What this workspace has switched on, from GET /api/v1/integrations/aws:
 *
 *   hasConnectedAccount  an AWS account has connected at least once. Until
 *                        then the console shows only the Integrations setup.
 *   exposureEnabled      GitHub or CodeCommit scanning is on for an account.
 *                        Until then the exposed-credential pages are hidden.
 *   status               loading | ready | error
 *
 * Fails open: if the check itself cannot be made, the console opens as usual
 * rather than locking everyone out of a workspace that is already set up.
 * `refresh()` re-reads it (the Integrations page calls it after a change).
 */
export function WorkspaceProvider({ children }) {
  const { token } = useAuth();
  // `forToken` ties the answer to the session it was loaded for, so a new
  // sign-in never sees the previous session's workspace while its own loads.
  const [state, setState] = useState({ forToken: null, status: 'loading', data: null, error: null });
  const controller = useRef(null);

  const load = useCallback(async (forToken) => {
    controller.current?.abort();
    const c = new AbortController();
    controller.current = c;
    try {
      const data = await fetchAwsIntegration(c.signal);
      if (!c.signal.aborted) setState({ forToken, status: 'ready', data, error: null });
    } catch (error) {
      if (c.signal.aborted || error?.code === 'CANCELLED') return;
      setState((prev) => ({ forToken, status: 'error', data: prev.forToken === forToken ? prev.data : null, error }));
    }
  }, []);

  useEffect(() => {
    if (!token) return undefined;
    load(token);
    return () => controller.current?.abort();
  }, [token, load]);

  const value = useMemo(() => {
    const current = token && state.forToken === token;
    const status = !token ? 'idle' : current ? state.status : 'loading';
    const d = current ? state.data : null;
    const failedOpen = status === 'error' && !d;
    return {
      status,
      error: current ? state.error : null,
      integration: d,
      hasConnectedAccount: failedOpen ? true : Boolean(d?.hasConnectedAccount),
      exposureEnabled: Boolean(d?.exposureEnabled),
      canManage: Boolean(d?.canManage),
      domain: d?.domain || '',
      refresh: () => load(token),
      /** Apply a response the caller already has, without another request. */
      apply: (next) => setState({ forToken: token, status: 'ready', data: next, error: null }),
    };
  }, [state, token, load]);

  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

export function useWorkspace() {
  const context = useContext(WorkspaceContext);
  if (!context) throw new Error('useWorkspace must be used inside a WorkspaceProvider');
  return context;
}
