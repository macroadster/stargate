import { useEffect } from 'react';

/**
 * Hosts the Quantum Shell (static files from public/desktop, served at
 * /desktop/shell.html) as the whole viewport. The shell is the only chrome
 * on this route; its brand menu carries the way back to Starlight.
 */
export default function DesktopPage() {
  useEffect(() => {
    const previousTitle = document.title;
    document.title = 'Quantum Shell — Starlight';
    return () => {
      document.title = previousTitle;
    };
  }, []);

  return (
    <iframe
      src="/desktop/shell.html"
      title="Quantum Shell"
      style={{
        display: 'block',
        width: '100vw',
        height: '100dvh',
        border: 'none',
        background: '#08111f',
      }}
    />
  );
}
