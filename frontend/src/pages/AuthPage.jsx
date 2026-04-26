import React, { useState } from 'react';
import { QRCodeCanvas } from 'qrcode.react';
import { API_BASE } from '../apiBase';
import { useAuth } from '../context/AuthContext';
import { apiFetch } from '../utils/api';
import { useNavigate } from 'react-router-dom';

export default function AuthPage() {
  const { auth, signIn, getSavedWallets, deleteWalletKey } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [apiKey, setApiKey] = useState(auth.apiKey || '');
  const [wallet, setWallet] = useState(auth.wallet || '');
  const [loginKey, setLoginKey] = useState('');
  const [status, setStatus] = useState('');
  const [challenge, setChallenge] = useState('');
  const [signature, setSignature] = useState('');
  const [view, setView] = useState('wallet'); // login | wallet

  const savedWallets = getSavedWallets();

  // When a saved key is chosen, hydrate wallet/email so that binding survives re-login.
  React.useEffect(() => {
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

  const setStatusBubble = (msg) => setStatus(msg || '');


  const handleLogin = async () => {
    setStatus('Signing in...');
    try {
      const saved = savedWallets.find((k) => k.apiKey === loginKey);
      const walletToSend = wallet || saved?.wallet || '';
      const res = await apiFetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ api_key: loginKey, wallet_address: walletToSend })
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Invalid key');
      const keyToSave = payload.api_key || loginKey;
      const walletToPersist = payload.wallet || walletToSend;
      signIn(keyToSave, walletToPersist, payload.email || saved?.email || '');
      setApiKey(keyToSave);
      setStatus('Signed in. Key saved locally.');
      navigate('/');
    } catch (err) {
      setStatusBubble(err.message);
    }
  };

  const handleChallenge = async () => {
    setStatus('Requesting challenge...');
    try {
      const res = await apiFetch('/api/auth/challenge', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ wallet_address: wallet })
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Challenge failed');
      setChallenge(payload.nonce);
      setStatus(`Challenge issued; sign this nonce: ${payload.nonce}`);
    } catch (err) {
      setStatusBubble(err.message);
    }
  };

  const handleVerify = async () => {
    setStatus('Verifying signature...');
    try {
      const res = await apiFetch('/api/auth/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ wallet_address: wallet, signature, email })
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Verify failed');
      const keyToSave = payload.api_key;
      signIn(keyToSave, payload.wallet || wallet, payload.email || email);
      setApiKey(keyToSave);
      setStatus('Verified and signed in.');
      navigate('/');
    } catch (err) {
      setStatusBubble(err.message);
    }
  };

  const renderLogin = () => (
    <div className="auth-card">
      <h2 className="text-xl font-semibold mb-2 auth-card-title">Sign In</h2>
      <p className="text-sm mb-4 auth-card-text">Use an existing API key.</p>
      {savedWallets.length > 0 && (
        <div className="mb-4">
          <label className="block text-sm mb-2 auth-card-label">Saved wallets</label>
          <div className="flex gap-2">
            <select
              className="form-select flex-1 h-10 px-3 rounded-lg"
              onChange={(e) => setLoginKey(e.target.value)}
              value={loginKey}
            >
              <option value="">Choose saved wallet</option>
              {savedWallets.map((k) => (
                <option key={k.wallet} value={k.apiKey}>
                  {(k.wallet || k.email || 'Key').slice(0, 12) + '…' + k.apiKey.slice(-6)}
                </option>
              ))}
            </select>
            {loginKey && savedWallets.some(k => k.apiKey === loginKey) && (
              <button 
                onClick={() => {
                  const s = savedWallets.find(k => k.apiKey === loginKey);
                  if (s) {
                    deleteWalletKey(s.wallet);
                    setLoginKey('');
                  }
                }}
                className="p-2 text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-lg transition-colors"
                title="Delete saved key"
              >
                🗑️
              </button>
            )}
          </div>
        </div>
      )}
      <label className="block text-sm mb-2 auth-card-label">API Key</label>
      <input
        className="form-input w-full mb-4 px-3 py-2 rounded-lg"
        value={loginKey}
        onChange={(e) => setLoginKey(e.target.value)}
        placeholder="paste your key"
      />
      <button
        onClick={handleLogin}
        className="w-full auth-btn-primary"
      >
        Sign In
      </button>
      {apiKey && (
        <div className="mt-4 text-xs break-all auth-card-highlight">
          Saved key: {apiKey}
        </div>
      )}
    </div>
  );


  const renderWallet = () => (
    <div className="auth-card">
      <h2 className="text-xl font-semibold mb-2 auth-card-title">Wallet Challenge</h2>
      <p className="text-sm mb-4 auth-card-text">Sign the nonce with Bitcoin signmessage (mainnet or testnet3/4) to issue a key.</p>
      <div className="text-xs mb-4 space-y-1 auth-card-text">
        <div>1) Click "Get challenge".</div>
        <div>2) Sign the nonce with your wallet's <code className="font-mono">signmessage</code>.</div>
        <div>3) Paste the base64 signature and click "Verify & Issue Key".</div>
        <div className="text-[11px]">Message to sign = nonce exactly as shown (no extra whitespace).</div>
      </div>
      <label className="block text-sm mb-2 auth-card-label">Wallet address</label>
      <input
        className="w-full mb-3 px-3 py-2 rounded-lg auth-card-input"
        value={wallet}
        onChange={(e) => setWallet(e.target.value)}
        placeholder="bc1... (mainnet) or tb1... (testnet3/4)"
      />
      <div className="flex gap-2 mb-3">
        <button
          onClick={handleChallenge}
          className="flex-1 auth-btn-secondary"
        >
          Get challenge
        </button>
        <button
          onClick={() => setChallenge('')}
          className="auth-btn-clear"
        >
          Clear
        </button>
      </div>
      {challenge && (
        <div className="mb-3 text-xs auth-card-highlight">
          <div>Nonce to sign: {challenge}</div>
          <div className="mt-2 flex justify-center">
            <div className="auth-card-qr-bg p-2 rounded">
              <QRCodeCanvas value={challenge} size={140} level="M" includeMargin />
            </div>
          </div>
        </div>
      )}
      <label className="block text-sm mb-2 auth-card-label">Signature (base64)</label>
      <textarea
        className="w-full mb-3 px-3 py-2 rounded-lg auth-card-input"
        rows={2}
        value={signature}
        onChange={(e) => setSignature(e.target.value)}
        placeholder="paste signature of the nonce"
      />
      <button
        onClick={handleVerify}
        className="w-full auth-btn-primary"
      >
        Verify & Issue Key
      </button>
    </div>
  );

  const renderCard = () => {
    if (view === 'wallet') return renderWallet();
    return renderLogin();
  };

  return (
    <div className="auth-page-container pt-12">
      <div className="w-full max-w-xl space-y-6">
        <div className="flex justify-center gap-3">
          <button
            className={`px-6 py-2 rounded-full transition-all ${view === 'login' ? 'bg-starlight text-white shadow-lg glow-blue' : 'bg-white/5 text-gray-400 hover:text-white'}`}
            onClick={() => setView('login')}
          >
            Sign In
          </button>
          <button
            className={`px-6 py-2 rounded-full transition-all ${view === 'wallet' ? 'bg-indigo-600 text-white shadow-lg' : 'bg-white/5 text-gray-400 hover:text-white'}`}
            onClick={() => setView('wallet')}
          >
            Wallet Verification
          </button>
        </div>

        <div className="animate-in fade-in slide-in-from-bottom-4 duration-500">
          {renderCard()}
        </div>
      </div>

      {status && (
        <div className="auth-status-toast">
          {status}
        </div>
      )}
    </div>
  );
}
