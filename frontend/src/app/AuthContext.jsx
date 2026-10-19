import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { fetchMe, login as loginRequest, logout as logoutRequest, profileFrom } from '../lib/api/auth';
import { onSessionExpired } from '../lib/api/client';
import { adoptSession, clearSession, readStoredUser, readToken, subscribeSignOut, writeSession, writeStoredUser } from '../lib/api/session';

const AuthContext = createContext(null);

/**
 * The signed-in session.
 *
 *   status  verifying (a stored session is being checked) | authenticated | anonymous
 *   user    { email, name, team, role (console role), serverRole, tenantId }
 *
 * The access token is renewed by the API client when it expires; only when
 * that fails does the session end (`expired` tells the sign-in page why).
 */
export function AuthProvider({ children }) {
  const [token, setToken] = useState(() => {
    adoptSession();
    return readToken();
  });
  // Seeded from storage so a refresh renders the shell immediately and the
  // profile request only corrects it.
  const [user, setUser] = useState(readStoredUser);
  const [status, setStatus] = useState(() => (readToken() ? 'verifying' : 'anonymous'));
  const [expired, setExpired] = useState(false);

  const endSession = useCallback(() => {
    setToken(null);
    setUser(null);
    setStatus('anonymous');
  }, []);

  const logout = useCallback(async () => {
    await logoutRequest();
    clearSession();
    endSession();
  }, [endSession]);

  /* Signing out in another tab of the same session signs this one out too. */
  useEffect(() => subscribeSignOut(endSession), [endSession]);

  useEffect(() => {
    onSessionExpired(() => {
      setExpired(true);
      endSession();
    });
  }, [endSession]);

  /* Confirm a stored session and refresh the role/tenant from the server. */
  useEffect(() => {
    if (!token) return undefined;
    const controller = new AbortController();
    (async () => {
      try {
        const me = await fetchMe(controller.signal);
        if (controller.signal.aborted) return;
        const stored = readStoredUser() ?? {};
        const profile = {
          ...stored,
          ...profileFrom({ ...stored, full_name: stored.name, email: stored.email }, me),
          lastLoginAt: stored.lastLoginAt ?? null,
        };
        setUser(profile);
        writeStoredUser(profile);
        setStatus('authenticated');
      } catch (error) {
        if (controller.signal.aborted || error?.code === 'CANCELLED') return;
        // The client already tried to renew the session; a 401 here means it
        // could not. Anything else is the service being unreachable, and
        // signing the person out would be wrong.
        if (error?.status === 401) {
          clearSession();
          setExpired(true);
          endSession();
          return;
        }
        setStatus('authenticated');
      }
    })();
    return () => controller.abort();
  }, [token, endSession]);

  const signIn = useCallback(async ({ email, password }) => {
    const result = await loginRequest({ email, password });
    writeSession({ token: result.token, refreshToken: result.refreshToken, user: result.user });
    setExpired(false);
    setUser(result.user);
    setToken(result.token);
    setStatus('authenticated');
    return result;
  }, []);

  const value = useMemo(
    () => ({
      user,
      token,
      status,
      expired,
      isAuthenticated: Boolean(token),
      signIn,
      logout,
      clearExpired: () => setExpired(false),
    }),
    [user, token, status, expired, signIn, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth must be used inside an AuthProvider');
  return context;
}
