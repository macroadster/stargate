import { useNavigate } from 'react-router-dom';
import AppHeader from '../components/Common/AppHeader';

/**
 * Hosts the Quantum Shell (static files from public/desktop, served at
 * /desktop/index.html) inside the app chrome so the Starlight nav — including
 * the Desktop item — is always one click away.
 */
export default function DesktopPage() {
  const navigate = useNavigate();

  return (
    <div
      className="bg-app-main text-primary"
      style={{ display: 'grid', gridTemplateRows: 'auto 1fr', height: '100dvh', overflow: 'hidden' }}
    >
      <AppHeader onInscribe={() => navigate('/')} />
      <iframe
        src="/desktop/shell.html"
        title="Quantum Shell"
        style={{ width: '100%', height: '100%', minWidth: 0, border: 'none', display: 'block' }}
      />
    </div>
  );
}
