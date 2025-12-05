const deriveApiBase = () => {
  if (process.env.REACT_APP_API_BASE) {
    return process.env.REACT_APP_API_BASE.replace(/\/$/, '');
  }
  const { origin } = window.location;
  // Same-origin by default; local dev uses port swap.
  if (origin.endsWith(':3000') || origin.endsWith(':8081')) {
    return origin.replace(':3000', ':3001');
  }
  return origin;
};

export const API_BASE = deriveApiBase();
// Content requests should ride the same origin/ingress; backend is reached via frontend proxy.
export const CONTENT_BASE = API_BASE;
