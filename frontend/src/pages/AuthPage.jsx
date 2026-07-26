import React, { useEffect, useState } from 'react';
import { QRCodeCanvas } from 'qrcode.react';
import { useAuth } from '../context/AuthContext';
import { apiFetch } from '../utils/api';
import { useNavigate, Link } from 'react-router-dom';

/** Populate QuantumCSS .starlight-stars (lazy routes miss DOMContentLoaded auto-init). */
function useStarfield(selector = '.auth-page .starlight-stars', count = 100) {
  useEffect(() => {
    const container = document.querySelector(selector);
    if (!container) return undefined;
    container.innerHTML = '';
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const n = reduced ? Math.floor(count / 2) : count;
    for (let i = 0; i < n; i += 1) {
      const star = document.createElement('div');
      star.className = 'star';
      star.style.left = `${Math.random() * 100}%`;
      star.style.top = `${Math.random() * 100}%`;
      const size = Math.random() * 2 + 1;
      star.style.width = `${size}px`;
      star.style.height = `${size}px`;
      if (reduced) {
        star.style.animation = 'none';
        star.style.opacity = String(Math.random() * 0.3 + 0.1);
      } else {
        star.style.setProperty('--q-duration', `${Math.random() * 3 + 2}s`);
      }
      container.appendChild(star);
    }
    return () => {
      container.innerHTML = '';
    };
  }, [selector, count]);
}

export default function AuthPage() {
  const { auth, signIn, getSavedWallets, deleteWalletKey } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [apiKey, setApiKey] = useState(auth.apiKey || '');
  const [wallet, setWallet] = useState(auth.wallet || '');
  const [loginKey, setLoginKey] = useState('');
  const [status, setStatus] = useState('');
  const [statusTone, setStatusTone] = useState('info'); // info | success | error
  const [challenge, setChallenge] = useState('');
  const [signature, setSignature] = useState('');
  const [view, setView] = useState('wallet'); // login | wallet
  const [busy, setBusy] = useState(false);

  useStarfield();

  const savedWallets = getSavedWallets();

  // When a saved key is chosen, hydrate wallet/email so that binding survives re-login.
  useEffect(() => {
    const selected = savedWallets.find((k) => k.apiKey === loginKey);
    if (selected) {
      if (selected.wallet) {
        setWallet(selected.wallet);
      }
      if (selected.email) {
        setEmail(selected.email);
      }
    }
  }, [loginKey, savedWallets]);

  const showStatus = (msg, tone = 'info') => {
    setStatus(msg || '');
    setStatusTone(tone);
  };

  const handleLogin = async (e) => {
    e?.preventDefault?.();
    setBusy(true);
    showStatus('Signing in…');
    try {
      const saved = savedWallets.find((k) => k.apiKey === loginKey);
      const walletToSend = wallet || saved?.wallet || '';
      const res = await apiFetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ api_key: loginKey, wallet_address: walletToSend }),
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Invalid key');
      const keyToSave = payload.api_key || loginKey;
      const walletToPersist = payload.wallet || walletToSend;
      signIn(keyToSave, walletToPersist, payload.email || saved?.email || '');
      setApiKey(keyToSave);
      showStatus('Signed in. Key saved locally.', 'success');
      navigate('/');
    } catch (err) {
      showStatus(err.message, 'error');
    } finally {
      setBusy(false);
    }
  };

  const handleChallenge = async () => {
    setBusy(true);
    showStatus('Requesting challenge…');
    try {
      const res = await apiFetch('/api/auth/challenge', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ wallet_address: wallet }),
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Challenge failed');
      setChallenge(payload.nonce);
      showStatus(`Challenge issued — sign this nonce: ${payload.nonce}`);
    } catch (err) {
      showStatus(err.message, 'error');
    } finally {
      setBusy(false);
    }
  };

  const handleVerify = async (e) => {
    e?.preventDefault?.();
    setBusy(true);
    showStatus('Verifying signature…');
    try {
      const res = await apiFetch('/api/auth/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ wallet_address: wallet, signature, email }),
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Verify failed');
      const keyToSave = payload.api_key;
      signIn(keyToSave, payload.wallet || wallet, payload.email || email);
      setApiKey(keyToSave);
      showStatus('Verified and signed in.', 'success');
      navigate('/');
    } catch (err) {
      showStatus(err.message, 'error');
    } finally {
      setBusy(false);
    }
  };

  const renderLogin = () => (
    <form className="form-stack" onSubmit={handleLogin} noValidate>
      {savedWallets.length > 0 && (
        <div>
          <label htmlFor="saved-wallet">Saved wallets</label>
          <div className="flex gap-2 items-center">
            <div className="field has-icon flex-1">
              <i className="icon-user field-icon" aria-hidden="true" />
              <select
                id="saved-wallet"
                className="input"
                onChange={(e) => setLoginKey(e.target.value)}
                value={loginKey}
              >
                <option value="">Choose saved wallet</option>
                {savedWallets.map((k) => (
                  <option key={k.wallet || k.apiKey} value={k.apiKey}>
                    {(k.wallet || k.email || 'Key').slice(0, 12) + '…' + k.apiKey.slice(-6)}
                  </option>
                ))}
              </select>
            </div>
            {loginKey && savedWallets.some((k) => k.apiKey === loginKey) && (
              <button
                type="button"
                onClick={() => {
                  const s = savedWallets.find((k) => k.apiKey === loginKey);
                  if (s) {
                    deleteWalletKey(s.wallet);
                    setLoginKey('');
                  }
                }}
                className="btn-ghost"
                title="Delete saved key"
                aria-label="Delete saved key"
              >
                <i className="icon-trash" aria-hidden="true" />
              </button>
            )}
          </div>
        </div>
      )}

      <div>
        <label htmlFor="api-key">API Key</label>
        <div className="field has-icon">
          <i className="icon-security field-icon" aria-hidden="true" />
          <input
            id="api-key"
            className="input"
            type="text"
            autoComplete="off"
            value={loginKey}
            onChange={(e) => setLoginKey(e.target.value)}
            placeholder="Paste your API key"
            required
          />
        </div>
      </div>

      <button type="submit" className="btn-starlight btn-shine w-full gap-2" disabled={busy || !loginKey}>
        <span>{busy ? 'Signing in…' : 'Sign in'}</span>
        <i className="icon-chevron-right" aria-hidden="true" />
      </button>

      {apiKey && (
        <div className="auth-key-preview text-sm text-muted">
          <span className="font-semibold">Saved key:</span>{' '}
          <code className="font-mono break-all">{apiKey}</code>
        </div>
      )}
    </form>
  );

  const renderWallet = () => (
    <form className="form-stack" onSubmit={handleVerify} noValidate>
      <p className="text-secondary text-sm">
        Sign the nonce with Bitcoin <code className="font-mono">signmessage</code> (mainnet or
        testnet3/4) to issue a key.
      </p>

      <ol className="auth-steps text-sm text-muted">
        <li>Click &ldquo;Get challenge&rdquo;.</li>
        <li>
          Sign the nonce with your wallet&rsquo;s <code className="font-mono">signmessage</code>.
        </li>
        <li>Paste the base64 signature and click &ldquo;Verify &amp; issue key&rdquo;.</li>
      </ol>

      <div>
        <label htmlFor="wallet-address">Wallet address</label>
        <div className="field has-icon">
          <i className="icon-globe field-icon" aria-hidden="true" />
          <input
            id="wallet-address"
            className="input"
            type="text"
            autoComplete="off"
            value={wallet}
            onChange={(e) => setWallet(e.target.value)}
            placeholder="bc1… (mainnet) or tb1… (testnet3/4)"
            required
          />
        </div>
      </div>

      <div className="auth-actions flex gap-2">
        <button
          type="button"
          onClick={handleChallenge}
          className="btn-primary flex-1"
          disabled={busy || !wallet}
        >
          Get challenge
        </button>
        <button
          type="button"
          onClick={() => {
            setChallenge('');
            setSignature('');
          }}
          className="btn-outline"
          disabled={!challenge && !signature}
        >
          Clear
        </button>
      </div>

      {challenge && (
        <div className="auth-challenge">
          <div className="text-sm font-semibold mb-2">Nonce to sign</div>
          <code className="auth-challenge-nonce font-mono text-sm break-all">{challenge}</code>
          <div className="flex justify-center mt-4">
            <div className="auth-qr p-2 rounded">
              <QRCodeCanvas value={challenge} size={140} level="M" includeMargin />
            </div>
          </div>
        </div>
      )}

      <div>
        <label htmlFor="signature">Signature (base64)</label>
        <div className="field has-icon">
          <i className="icon-edit field-icon" aria-hidden="true" />
          <textarea
            id="signature"
            className="input"
            rows={2}
            value={signature}
            onChange={(e) => setSignature(e.target.value)}
            placeholder="Paste signature of the nonce"
            required
          />
        </div>
      </div>

      <button
        type="submit"
        className="btn-starlight btn-shine w-full gap-2"
        disabled={busy || !wallet || !signature}
      >
        <span>{busy ? 'Verifying…' : 'Verify & issue key'}</span>
        <i className="icon-chevron-right" aria-hidden="true" />
      </button>
    </form>
  );

  const alertClass =
    statusTone === 'error'
      ? 'alert alert-error'
      : statusTone === 'success'
        ? 'alert alert-success'
        : 'alert alert-info';

  return (
    <div className="auth-page login-demo">
      {/* Static starfield — do not put ani-nebula on this layer (solid bg + transform = swinging box) */}
      <div className="starlight-stars" aria-hidden="true" />

      {/* Transparent ring parents only — rotating them moves the dots, never a filled box */}
      <div className="login-orbit login-orbit--a ani-orbit ani-slow" aria-hidden="true">
        <span className="login-orbit-dot" />
      </div>
      <div className="login-orbit login-orbit--b ani-orbit ani-slower" aria-hidden="true">
        <span className="login-orbit-dot login-orbit-dot--peach" />
      </div>

      <main className="auth-shell">
        {/* Soft glow only — no box-shadow pulse (reads as a colored rectangle behind the card) */}
        <div className="login-glow" aria-hidden="true" />

        <div className="auth-card">
          <header className="text-center mb-8 ani-slide-up ani-stagger-1">
            <Link to="/" className="inline-block" aria-label="Starlight home">
              <i className="icon-starlight login-brand-icon ani-float mb-6" aria-hidden="true" />
            </Link>
            <h1 className="text-2xl font-bold mb-2">
              Welcome <span className="text-gradient">back</span>
            </h1>
            <p className="text-secondary">
              {view === 'login'
                ? 'Sign in with an existing API key'
                : 'Prove wallet ownership to issue a key'}
            </p>
          </header>

          <div className="tab-list auth-tabs mb-4 ani-fade-in ani-stagger-2" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={view === 'login'}
              className={`tab-button${view === 'login' ? ' active' : ''}`}
              onClick={() => setView('login')}
            >
              Sign in
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={view === 'wallet'}
              className={`tab-button${view === 'wallet' ? ' active' : ''}`}
              onClick={() => setView('wallet')}
            >
              Wallet verification
            </button>
          </div>

          <section
            className="starlight-card ani-scale-in ani-stagger-2"
            aria-labelledby="auth-panel-title"
          >
            <h2 id="auth-panel-title" className="hidden">
              {view === 'login' ? 'Sign in' : 'Wallet verification'}
            </h2>
            {view === 'login' ? renderLogin() : renderWallet()}
          </section>

          <p className="text-center text-sm text-muted mt-6 ani-fade-in ani-stagger-5">
            {view === 'login' ? (
              <>
                New here?{' '}
                <button
                  type="button"
                  className="font-semibold auth-inline-link"
                  onClick={() => setView('wallet')}
                >
                  Verify a wallet
                </button>
              </>
            ) : (
              <>
                Already have a key?{' '}
                <button
                  type="button"
                  className="font-semibold auth-inline-link"
                  onClick={() => setView('login')}
                >
                  Sign in
                </button>
              </>
            )}
          </p>

          <div className="flex items-center justify-center gap-3 mt-6 ani-fade-in ani-stagger-5 flex-wrap">
            <span className="badge badge-secondary">
              <i className="icon-security" aria-hidden="true" />
              Wallet signed
            </span>
            <span className="badge badge-secondary">
              <i className="icon-check-circle-fill" aria-hidden="true" />
              Local key vault
            </span>
            <span className="badge badge-secondary">
              <i className="icon-globe" aria-hidden="true" />
              Bitcoin native
            </span>
          </div>
        </div>
      </main>

      {status && (
        <div className={`${alertClass} login-toast`} role="status">
          {statusTone === 'success' && (
            <i className="icon-check-circle-fill" aria-hidden="true" />
          )}
          {statusTone === 'error' && <i className="icon-close" aria-hidden="true" />}
          <span>{status}</span>
        </div>
      )}
    </div>
  );
}
