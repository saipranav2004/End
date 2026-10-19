import axios from 'axios';

export const TOKEN_KEY = 'dna.token';
export const REFRESH_KEY = 'dna.refresh';
export const USER_KEY = 'dna.user';

/**
 * Normalised error surfaced to every screen so states stay consistent.
 *
 *   status     HTTP status, or null when the service was not reached
 *   code       the service's machine code (e.g. AWS_ACCOUNT_IN_USE), if any
 *   field      the request field a validation error names (posture API)
 *   retryable  true for network failures, 429 and 5xx
 */
export class ApiError extends Error {
  constructor({ message, status, code, field, retryable, retryAfter }) {
    super(message);
    this.name = 'ApiError';
    this.status = status ?? null;
    this.code = code ?? null;
    this.field = field ?? null;
    this.retryable = retryable ?? false;
    this.retryAfter = retryAfter ?? null;
  }
}

/**
 * The three services answer errors in four shapes:
 *
 *   discovery API          { success: false, message, error: "CODE" }
 *   credential exposure    { error: "CODE", message }
 *   posture API            { error: { code, message, field } }
 *   genome API             { error: { code, message, request_id } }
 *
 * plus the shared auth middleware, which uses the first. This reads all of them.
 */
function readPayload(payload) {
  if (!payload || typeof payload !== 'object') return {};
  if (payload.error && typeof payload.error === 'object') {
    return { message: payload.error.message, code: payload.error.code, field: payload.error.field };
  }
  return {
    message: typeof payload.message === 'string' ? payload.message : undefined,
    code: typeof payload.error === 'string' ? payload.error : undefined,
  };
}

const BY_STATUS = {
  400: 'The request was rejected as invalid.',
  401: 'Your session is no longer valid. Sign in again to continue.',
  403: 'You do not have access to this.',
  404: 'That no longer exists.',
  409: 'That changed since you loaded it. Refresh and try again.',
  429: 'Too many requests just now. Try again in a moment.',
  500: 'The service failed while handling this request.',
  502: 'The service is not reachable right now.',
  503: 'The service is not available right now.',
  504: 'The service did not respond in time.',
};

export function describe(error) {
  if (error instanceof ApiError) return error;
  if (axios.isCancel?.(error) || error?.code === 'ERR_CANCELED') {
    return new ApiError({ message: 'Request cancelled', code: 'CANCELLED' });
  }
  if (!error?.response) {
    return new ApiError({
      message:
        error?.code === 'ECONNABORTED'
          ? 'The request timed out before the service responded.'
          : 'Could not reach the service. Check your connection and try again.',
      code: error?.code || 'NETWORK',
      retryable: true,
    });
  }
  const status = error.response.status;
  const { message, code, field } = readPayload(error.response.data);
  const retryAfter = Number(error.response.headers?.['retry-after']) || null;
  /* Server messages are shown as written for client errors. A 5xx message
     can carry internals, so those get the generic sentence unless the server
     wrote one meant for people (the discovery API does, e.g. "AWS cannot be
     reached right now"). */
  const usable = message && !/^(failed to|unable to|internal|unexpected error)/i.test(message) ? message : null;
  return new ApiError({
    message: (status < 500 ? message : usable) || BY_STATUS[status] || `Request failed with status ${status}.`,
    status,
    code: code ?? null,
    field,
    retryable: status >= 500 || status === 429,
    retryAfter,
  });
}

/** Discovery API: { success, message, data } -> data. */
export function unwrapMain(response) {
  const body = response?.data;
  if (!body || typeof body !== 'object' || body.success === false) {
    const { message, code } = readPayload(body);
    throw new ApiError({ message: message || 'The service reported a failure.', status: response?.status ?? null, code });
  }
  return body.data;
}

/**
 * Posture API: { data, page?, meta } -> { data, page, meta }.
 * meta = { scan_id, computed_at, window, data_available }.
 */
export function unwrapPosture(response) {
  const body = response?.data;
  if (!body || typeof body !== 'object' || !('data' in body)) {
    throw new ApiError({ message: 'The posture service answered with an unexpected response.', status: response?.status ?? null });
  }
  return { data: body.data, page: body.page ?? null, meta: body.meta ?? {} };
}

/**
 * Genome API: { data, meta } -> { data, meta }.
 * meta = { request_id, generated_at, next_cursor }.
 */
export function unwrapGenome(response) {
  const body = response?.data;
  if (!body || typeof body !== 'object' || !('data' in body)) {
    throw new ApiError({ message: 'The behaviour service answered with an unexpected response.', status: response?.status ?? null });
  }
  return { data: body.data, meta: body.meta ?? {} };
}

/** Credential exposure answers bare objects; check the field the screen needs. */
export function expectField(response, field) {
  const body = response?.data;
  if (!body || typeof body !== 'object' || !(field in body)) {
    throw new ApiError({ message: 'The service answered with an unexpected response.', status: response?.status ?? null });
  }
  return body;
}

/** Drops empty values so they are not sent (the APIs reject unknown or blank params). */
export function params(query = {}) {
  const out = {};
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue;
    if (Array.isArray(value)) {
      if (value.length) out[key] = value.join(',');
      continue;
    }
    out[key] = value;
  }
  return out;
}
