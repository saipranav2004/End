import axios from 'axios';
import { ApiError, describe } from './http';
import {
  clearSession,
  readRefreshToken,
  readSharedRefreshToken,
  readSharedToken,
  readToken,
  writeSession,
} from './session';

/**
 * One client for the NHI backend, which serves every API on one origin:
 *
 *   /api/v1/ispm/*    posture
 *   /api/v1/genome/*  genome
 *   /api/v1/*         discovery (sign-in, inventory, integrations, exposure ...)
 *
 * The dev server (vite.config.js) and nginx in production proxy /api to it.
 */
const client = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '',
  timeout: 20000,
  headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
});

client.interceptors.request.use((config) => {
  const token = readToken();
  if (token && !config.headers.Authorization) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

let unauthorizedHandler = null;
/** Called once the session cannot be renewed: the router shows sign-in. */
export function onSessionExpired(handler) {
  unauthorizedHandler = handler;
}

const AUTH_PATHS = /\/api\/(v1\/)?(auth\/)?(login|refresh|logout)$/;

/**
 * Swap the refresh token for a new pair. Refresh tokens are single use and
 * the server treats a reused one as theft (it revokes every session of that
 * user), so two tabs must never refresh with the same token: the Web Locks
 * API serialises refreshes across tabs, and inside the lock a tab first
 * checks whether another tab already rotated the pair.
 */
let inflight = null;
/* Resolves true (renewed), false (the refresh token is no longer valid) or
   null (the service could not answer: network, 429, 5xx). Only false ends
   the session; a passing outage must not sign anyone out. */
async function renewSession() {
  const held = readRefreshToken();
  const run = async () => {
    const shared = readSharedRefreshToken();
    if (shared && shared !== held) {
      // Another tab already refreshed: adopt its pair.
      writeSession({ token: readSharedToken(), refreshToken: shared });
      return true;
    }
    if (!held) return false;
    try {
      const res = await axios.post(
        `${client.defaults.baseURL}/api/v1/refresh`,
        { refresh_token: held },
        { headers: { 'Content-Type': 'application/json' }, timeout: 15000 },
      );
      const data = res?.data?.data;
      if (!data?.token || !data?.refresh_token) return false;
      writeSession({ token: data.token, refreshToken: data.refresh_token, user: data.user });
      return true;
    } catch (err) {
      const status = err?.response?.status;
      return status === 400 || status === 401 ? false : null;
    }
  };
  if (typeof navigator !== 'undefined' && navigator.locks?.request) {
    return navigator.locks.request('nhi.session.refresh', run);
  }
  return run();
}

client.interceptors.response.use(
  (response) => response,
  async (error) => {
    const config = error?.config;
    const status = error?.response?.status;
    if (status === 401 && config && !config._retried && !AUTH_PATHS.test(config.url || '')) {
      config._retried = true;
      inflight ??= renewSession().finally(() => {
        inflight = null;
      });
      const renewed = await inflight;
      if (renewed) {
        config.headers.Authorization = `Bearer ${readToken()}`;
        return client.request(config);
      }
      if (renewed === null) {
        return Promise.reject(
          new ApiError({ message: 'Could not renew your session just now. Try again in a moment.', code: 'RENEW_FAILED', retryable: true }),
        );
      }
      clearSession();
      unauthorizedHandler?.();
    }
    return Promise.reject(describe(error));
  },
);

export default client;
