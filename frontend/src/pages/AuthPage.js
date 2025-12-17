import React, { useState } from 'react';
import { API_BASE } from '../apiBase';
import { useAuth } from '../context/AuthContext';
import { useNavigate } from 'react-router-dom';

export default function AuthPage() {
  const { auth, signIn, savedKeys } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [apiKey, setApiKey] = useState(auth.apiKey || '');
  const [wallet, setWallet] = useState(auth.wallet || '');
  const [loginKey, setLoginKey] = useState('');
  const [status, setStatus] = useState('');
  const [challenge, setChallenge] = useState('');
  const [signature, setSignature] = useState('');
  const [view, setView] = useState('login'); // login | register | wallet

  const setStatusBubble = (msg) => setStatus(msg || '');

  const handleRegister = async () => {
    const walletAddress = wallet.trim();
    if (!walletAddress) {
      setStatusBubble('Wallet address required to register.');
      return;
    }
    const emailValue = email.trim();
    setStatus('Registering...');
    try {
      const res = await fetch(`${API_BASE}/api/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: emailValue, wallet_address: walletAddress })
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Registration failed');
      const issuedKey = payload.api_key || payload.key || '';
      setApiKey(issuedKey);
      signIn(issuedKey, payload.wallet || walletAddress, payload.email || emailValue);
      setStatus('Registered. Key saved locally.');
      navigate('/');
    } catch (err) {
      setStatusBubble(err.message);
    }
  };

  const handleLogin = async () => {
    setStatus('Signing in...');
    try {
      const res = await fetch(`${API_BASE}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ api_key: loginKey, wallet_address: wallet })
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Invalid key');
      const keyToSave = payload.api_key || loginKey;
      signIn(keyToSave, payload.wallet || wallet, payload.email || '');
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
      const res = await fetch(`${API_BASE}/api/auth/challenge`, {
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
      const res = await fetch(`${API_BASE}/api/auth/verify`, {
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
    <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-2xl p-6 shadow-lg">
      <h2 className="text-xl font-semibold mb-2">Sign In</h2>
      <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">Use an existing API key.</p>
      {savedKeys.length > 0 && (
        <div className="mb-4">
          <label className="block text-sm mb-2">Saved keys</label>
          <select
            className="w-full mb-2 px-3 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700"
            onChange={(e) => setLoginKey(e.target.value)}
            value={loginKey}
          >
            <option value="">Choose saved key</option>
            {savedKeys.map((k) => (
              <option key={k.apiKey} value={k.apiKey}>
                {(k.wallet || k.email || 'Key') + ' …' + k.apiKey.slice(-6)}
              </option>
            ))}
          </select>
        </div>
      )}
      <label className="block text-sm mb-2">API Key</label>
      <input
        className="w-full mb-4 px-3 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700"
        value={loginKey}
        onChange={(e) => setLoginKey(e.target.value)}
        placeholder="paste your key"
      />
      <button
        onClick={handleLogin}
        className="w-full bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg py-2 font-semibold"
      >
        Sign In
      </button>
      {apiKey && (
        <div className="mt-4 text-xs break-all bg-gray-100 dark:bg-gray-800 rounded-lg p-3 border border-dashed border-gray-300 dark:border-gray-700">
          Saved key: {apiKey}
        </div>
      )}
    </div>
  );

  const renderRegister = () => (
    <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-2xl p-6 shadow-lg">
      <h2 className="text-xl font-semibold mb-2">Register</h2>
      <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">Get a new API key for MCP / smart-contract endpoints.</p>
      <label className="block text-sm mb-2">Email (optional)</label>
      <input
        className="w-full mb-4 px-3 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        placeholder="you@example.com"
      />
      <label className="block text-sm mb-2">Wallet address (required)</label>
      <input
        className="w-full mb-4 px-3 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700"
        value={wallet}
        onChange={(e) => setWallet(e.target.value)}
        placeholder="bc1... or m/n/testnet"
      />
      <button
        onClick={handleRegister}
        disabled={!wallet.trim()}
        className={`w-full rounded-lg py-2 font-semibold text-white ${wallet.trim() ? 'bg-indigo-600 hover:bg-indigo-700' : 'bg-indigo-400 cursor-not-allowed'}`}
      >
        Register & Issue Key
      </button>
    </div>
  );

  const renderWallet = () => (
    <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-2xl p-6 shadow-lg">
      <h2 className="text-xl font-semibold mb-2">Wallet Challenge</h2>
      <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">Sign the nonce with Bitcoin signmessage (mainnet or testnet) to issue a key.</p>
      <div className="text-xs text-gray-500 dark:text-gray-400 mb-4 space-y-1">
        <div>1) Click “Get challenge”.</div>
        <div>2) Sign the nonce with your wallet’s <code className="font-mono">signmessage</code>.</div>
        <div>3) Paste the base64 signature and click “Verify & Issue Key”.</div>
        <div className="text-[11px]">Message to sign = nonce exactly as shown (no extra whitespace).</div>
      </div>
      <label className="block text-sm mb-2">Wallet address</label>
      <input
        className="w-full mb-3 px-3 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700"
        value={wallet}
        onChange={(e) => setWallet(e.target.value)}
        placeholder="bc1... (mainnet) or tb1... (testnet)"
      />
      <div className="flex gap-2 mb-3">
        <button
          onClick={handleChallenge}
          className="flex-1 bg-indigo-600 hover:bg-indigo-700 text-white rounded-lg py-2 font-semibold"
        >
          Get challenge
        </button>
        <button
          onClick={() => setChallenge('')}
          className="px-3 py-2 text-xs text-gray-500 dark:text-gray-400"
        >
          Clear
        </button>
      </div>
      {challenge && (
        <div className="mb-3 text-xs bg-gray-100 dark:bg-gray-800 border border-dashed border-gray-300 dark:border-gray-700 rounded-lg p-2 break-all">
          Nonce to sign: {challenge}
        </div>
      )}
      <label className="block text-sm mb-2">Signature (base64)</label>
      <textarea
        className="w-full mb-3 px-3 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700"
        rows={2}
        value={signature}
        onChange={(e) => setSignature(e.target.value)}
        placeholder="paste signature of the nonce"
      />
      <button
        onClick={handleVerify}
        className="w-full bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg py-2 font-semibold"
      >
        Verify & Issue Key
      </button>
    </div>
  );

  const renderCard = () => {
    if (view === 'register') return renderRegister();
    if (view === 'wallet') return renderWallet();
    return renderLogin();
  };

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-950 text-gray-900 dark:text-gray-50 flex items-center justify-center px-4">
      <div className="w-full max-w-xl space-y-4">
        <div className="flex justify-center gap-3 text-sm">
          <button
            className={`px-3 py-1 rounded-full border ${view === 'login' ? 'bg-emerald-600 text-white border-emerald-600' : 'border-gray-300 dark:border-gray-700 text-gray-600 dark:text-gray-300'}`}
            onClick={() => setView('login')}
          >
            Sign In (API key)
          </button>
          <button
            className={`px-3 py-1 rounded-full border ${view === 'register' ? 'bg-indigo-600 text-white border-indigo-600' : 'border-gray-300 dark:border-gray-700 text-gray-600 dark:text-gray-300'}`}
            onClick={() => setView('register')}
          >
            Register
          </button>
          <button
            className={`px-3 py-1 rounded-full border ${view === 'wallet' ? 'bg-amber-600 text-white border-amber-600' : 'border-gray-300 dark:border-gray-700 text-gray-600 dark:text-gray-300'}`}
            onClick={() => setView('wallet')}
          >
            Wallet Challenge
          </button>
        </div>

        {renderCard()}
      </div>

      {status && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 bg-gray-900 text-white px-4 py-2 rounded-full shadow-lg text-sm">
          {status}
        </div>
      )}
    </div>
  );
}
