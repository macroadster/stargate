const deriveApiBase = () => {
  if (process.env.REACT_APP_API_BASE) {
    return process.env.REACT_APP_API_BASE.replace(/\/$/, '');
  }
  const { origin, protocol, hostname } = window.location;

  // Ingress: prefer same origin to avoid CORS; backend also routed on starlight.local via /api paths
  if (hostname === 'starlight.local') {
    return `${protocol}//${hostname}`;
  }

  // Local dev/port-forward: UI on 3000/8081, backend on 3001
  if (origin.endsWith(':3000') || origin.endsWith(':8081')) {
    return origin.replace(':3000', ':3001');
  }
  return origin;
};

export const API_BASE = deriveApiBase();
