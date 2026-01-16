import React, { createContext, useContext, useEffect, useState } from 'react';

const AuthContext = createContext(null);

const STORAGE_KEY = 'X-API-Key';
const STORAGE_WALLET_MAP = 'X-Wallet-Key-Map';
const STALE_THRESHOLD_MS = 7 * 24 * 60 * 60 * 1000;

export function AuthProvider({ children }) {
  const [auth, setAuth] = useState(() => {
    const key = localStorage.getItem(STORAGE_KEY) || '';
    const wallet = localStorage.getItem('X-Wallet-Address') || '';
    const email = localStorage.getItem('X-User-Email') || '';
    return { apiKey: key, wallet, email };
  });

  const [walletKeys, setWalletKeys] = useState(() => {
    try {
      const raw = localStorage.getItem(STORAGE_WALLET_MAP) || '{}';
      const parsed = JSON.parse(raw);
      const now = Date.now();
      const cleaned = {};
      for (const [wallet, data] of Object.entries(parsed)) {
        if (now - data.lastUsed < STALE_THRESHOLD_MS) {
          cleaned[wallet] = data;
        }
      }
      return cleaned;
    } catch {
      return {};
    }
  });

  useEffect(() => {
    if (auth.apiKey && auth.wallet) {
      localStorage.setItem(STORAGE_KEY, auth.apiKey);
      localStorage.setItem('X-Wallet-Address', auth.wallet || '');
      localStorage.setItem('X-User-Email', auth.email || '');
      
      setWalletKeys((prev) => {
        const updated = { ...prev };
        updated[auth.wallet] = {
          apiKey: auth.apiKey,
          email: auth.email,
          lastUsed: Date.now(),
        };
        return updated;
      });
    } else {
      localStorage.removeItem(STORAGE_KEY);
      localStorage.removeItem('X-Wallet-Address');
      localStorage.removeItem('X-User-Email');
    }
  }, [auth]);

  useEffect(() => {
    localStorage.setItem(STORAGE_WALLET_MAP, JSON.stringify(walletKeys));
  }, [walletKeys]);

  const signIn = (apiKey, wallet, email) => {
    setAuth({ apiKey, wallet, email });
  };

  const signOut = () => {
    localStorage.removeItem(STORAGE_KEY);
    localStorage.removeItem('X-Wallet-Address');
    localStorage.removeItem('X-User-Email');
    setAuth({ apiKey: '', wallet: '', email: '' });
  };

  const getSavedWallets = () => {
    return Object.entries(walletKeys).map(([wallet, data]) => ({
      wallet,
      apiKey: data.apiKey,
      email: data.email,
      lastUsed: data.lastUsed,
    }));
  };

  const removeStaleKeys = () => {
    const now = Date.now();
    setWalletKeys((prev) => {
      const cleaned = {};
      for (const [wallet, data] of Object.entries(prev)) {
        if (now - data.lastUsed < STALE_THRESHOLD_MS) {
          cleaned[wallet] = data;
        }
      }
      return cleaned;
    });
  };

  return (
    <AuthContext.Provider value={{ auth, signIn, signOut, walletKeys, getSavedWallets, removeStaleKeys }}>
      {children}
    </AuthContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  return useContext(AuthContext);
}
