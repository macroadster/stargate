import { useEffect } from 'react';

/**
 * Hosts the Starlight Shell (static files from public/desktop, served at
 * /desktop/shell.html) as the whole viewport. The shell is the only chrome
 * on this route; its brand menu carries the way back to Starlight.
 */
export default function DesktopPage() {
  useEffect(() => {
    const previousTitle = document.title;
    document.title = 'Starlight Shell';
    return () => {
      document.title = previousTitle;
    };
  }, []);

  return (
    <iframe
      src="/desktop/shell.html"
      title="Starlight Shell"
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
