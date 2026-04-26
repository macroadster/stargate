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
