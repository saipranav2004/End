import { REFRESH_KEY, TOKEN_KEY, USER_KEY } from './http';

/**
 * Where the signed-in session is kept: this tab first, then the browser.
 *
 * Signing in writes both, so a new tab opens signed in. The session is a
 * short-lived access token plus a single-use refresh token; the client swaps
 * the pair when the access token expires (see client.js).
 *
 * Signing out clears both, and every other tab holding that same session
 * signs out too (see `subscribeSignOut`).
 */
function tabStore() {
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
}

function read(key) {
  try {
    return tabStore()?.getItem(key) ?? localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function readToken() {
  return read(TOKEN_KEY);
}

export function readRefreshToken() {
  return read(REFRESH_KEY);
}

/** The browser-wide copy of the refresh token (what another tab last stored). */
export function readSharedRefreshToken() {
  try {
    return localStorage.getItem(REFRESH_KEY);
  } catch {
    return null;
  }
}

export function readSharedToken() {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

/** A new tab starts from the browser copy. */
export function adoptSession() {
  const store = tabStore();
  try {
    if (!store || store.getItem(TOKEN_KEY)) return;
    for (const key of [TOKEN_KEY, REFRESH_KEY, USER_KEY]) {
      const value = localStorage.getItem(key);
      if (value) store.setItem(key, value);
    }
  } catch {
    /* see writeSession */
  }
}

export function readStoredUser() {
  try {
    const raw = read(USER_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

export function writeSession({ token, refreshToken, user }) {
  for (const store of [tabStore(), localStorage]) {
    try {
      if (token) store?.setItem(TOKEN_KEY, token);
      if (refreshToken) store?.setItem(REFRESH_KEY, refreshToken);
      if (user) store?.setItem(USER_KEY, JSON.stringify(user));
    } catch {
      /* A private window refuses storage; the session lasts for this page. */
    }
  }
}

/** Refresh the stored profile without touching the tokens. */
export function writeStoredUser(user) {
  for (const store of [tabStore(), localStorage]) {
    try {
      if (store?.getItem(TOKEN_KEY) === readToken()) store.setItem(USER_KEY, JSON.stringify(user));
    } catch {
      /* see writeSession */
    }
  }
}

const SIGNED_OUT_KEY = 'dna.signedOut';

export function clearSession() {
  const token = readToken();
  const refresh = readRefreshToken();
  try {
    for (const key of [TOKEN_KEY, REFRESH_KEY, USER_KEY]) tabStore()?.removeItem(key);
  } catch {
    /* see writeSession */
  }
  try {
    /* The browser copy goes only if it is this same session (or its rotated
       successor); a different sign-in from another tab is theirs to keep. */
    if (localStorage.getItem(TOKEN_KEY) === token || localStorage.getItem(REFRESH_KEY) === refresh) {
      for (const key of [TOKEN_KEY, REFRESH_KEY, USER_KEY]) localStorage.removeItem(key);
    }
    if (token) {
      localStorage.setItem(SIGNED_OUT_KEY, JSON.stringify({ token, refresh, at: Date.now() }));
      localStorage.removeItem(SIGNED_OUT_KEY);
    }
  } catch {
    /* see writeSession */
  }
}

/** Calls `handler` when another tab signs out of the session this tab holds. */
export function subscribeSignOut(handler) {
  const onStorage = (event) => {
    if (event.key !== SIGNED_OUT_KEY || !event.newValue) return;
    try {
      const { token, refresh } = JSON.parse(event.newValue);
      if ((token && token === readToken()) || (refresh && refresh === readRefreshToken())) {
        for (const key of [TOKEN_KEY, REFRESH_KEY, USER_KEY]) tabStore()?.removeItem(key);
        handler();
      }
    } catch {
      /* A malformed notice is ignored. */
    }
  };
  window.addEventListener('storage', onStorage);
  return () => window.removeEventListener('storage', onStorage);
}
