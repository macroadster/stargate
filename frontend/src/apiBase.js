const deriveApiBase = () => {
  if (import.meta.env.VITE_API_BASE) {
    return import.meta.env.VITE_API_BASE.replace(/\/$/, '');
  }
  const { origin } = window.location;
  // Same-origin by default (single-binary serves frontend+API); local dev may proxy :3000 -> :3001.
  if (origin.endsWith(':3000')) {
    return origin.replace(':3000', ':3001');
  }
  return origin;
};

export const API_BASE = deriveApiBase();
// Content requests should ride the same origin/ingress; backend is reached via frontend proxy.
export const CONTENT_BASE = API_BASE;
