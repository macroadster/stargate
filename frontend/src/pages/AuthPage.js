import React, { useState } from 'react';
import { API_BASE } from '../apiBase';

export default function AuthPage() {
  const [email, setEmail] = useState('');
  const [apiKey, setApiKey] = useState(localStorage.getItem('X-API-Key') || '');
  const [wallet, setWallet] = useState('');
  const [loginKey, setLoginKey] = useState('');
  const [status, setStatus] = useState('');

  const handleRegister = async () => {
    setStatus('Registering...');
    try {
      const res = await fetch(`${API_BASE}/api/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, wallet_address: wallet })
      });
      const data = await res.json();
      const payload = data?.data || data;
      if (!res.ok) throw new Error(data?.message || payload?.message || 'Registration failed');
      const issuedKey = payload.api_key || payload.key || '';
      setApiKey(issuedKey);
      localStorage.setItem('X-API-Key', issuedKey);
      setStatus('Registered. Key saved locally.');
    } catch (err) {
      setStatus(err.message);
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
      localStorage.setItem('X-API-Key', keyToSave);
      setApiKey(keyToSave);
      setStatus('Signed in. Key saved locally.');
    } catch (err) {
      setStatus(err.message);
    }
  };

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-950 text-gray-900 dark:text-gray-50 flex items-center justify-center px-4">
      <div className="max-w-3xl w-full grid md:grid-cols-2 gap-8">
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
          <label className="block text-sm mb-2">Wallet address (optional)</label>
          <input
            className="w-full mb-4 px-3 py-2 rounded-lg bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700"
            value={wallet}
            onChange={(e) => setWallet(e.target.value)}
            placeholder="bc1... or 0x..."
          />
          <button
            onClick={handleRegister}
            className="w-full bg-indigo-600 hover:bg-indigo-700 text-white rounded-lg py-2 font-semibold"
          >
            Register & Issue Key
          </button>
        </div>

        <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-2xl p-6 shadow-lg">
          <h2 className="text-xl font-semibold mb-2">Sign In</h2>
          <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">Validate an existing API key and store it locally.</p>
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
      </div>

      {status && (
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 bg-gray-900 text-white px-4 py-2 rounded-full shadow-lg text-sm">
          {status}
        </div>
      )}
    </div>
  );
}
