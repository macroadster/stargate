import { API_BASE } from '../apiBase';

/**
 * A wrapper around fetch that includes credentials for httpOnly cookies.
 */
export async function apiFetch(endpoint, options = {}) {
  const url = endpoint.startsWith('http') ? endpoint : `${API_BASE}${endpoint}`;
  
  const mergedOptions = {
    ...options,
    credentials: 'include', // Required for httpOnly cookies cross-origin
    headers: {
      ...options.headers,
    }
  };

  return fetch(url, mergedOptions);
}

/**
 * Extract a human-readable message from API error JSON.
 *
 * Supports several shapes used across the stack:
 * - Flat: { message } | { error: "string" }
 * - Middleware: { success:false, error: { error, message, code } }
 * - Canonical core envelope (double-nested via models.APIResponse):
 *   { success:false, error: { error: { code, message, details, ... } } }
 *
 * Without nested handling, callers that pass errorObj.error into `new Error()`
 * get the useless string "[object Object]".
 */
export function extractApiErrorMessage(data, fallback = 'Request failed') {
  if (data == null || data === '') {
    return fallback;
  }
  if (typeof data === 'string') {
    const trimmed = data.trim();
    return trimmed || fallback;
  }
  if (typeof data !== 'object') {
    return String(data);
  }

  const asNonEmptyString = (value) =>
    typeof value === 'string' && value.trim() ? value.trim() : '';

  const fromDetails = (details) => {
    if (!details || typeof details !== 'object') return '';
    return (
      asNonEmptyString(details.message) ||
      asNonEmptyString(details.error) ||
      asNonEmptyString(details.hint) ||
      ''
    );
  };

  // Prefer explicit top-level message when present.
  const topMessage = asNonEmptyString(data.message);
  if (topMessage) return topMessage;

  const err = data.error ?? data.data?.error;
  if (typeof err === 'string') {
    return asNonEmptyString(err) || fallback;
  }
  if (err && typeof err === 'object') {
    // Flat middleware / legacy: { error: "code", message: "..." }
    const flat = fromDetails(err);
    if (flat) return flat;

    // Nested core.ErrorResponse: { error: { code, message, ... } }
    if (err.error && typeof err.error === 'object') {
      const nested = fromDetails(err.error);
      if (nested) return nested;
    } else if (typeof err.error === 'string') {
      const codeOnly = asNonEmptyString(err.error);
      if (codeOnly) return codeOnly;
    }
  }

  const dataMessage = asNonEmptyString(data.data?.message);
  if (dataMessage) return dataMessage;

  return fallback;
}
