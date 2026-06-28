import React, { createContext, useContext, useEffect, useState } from 'react';
import { initDB, getAuthState, setAuthState, clearAuthState, getWalletKeysFromDB, updateWalletKeyInDB, removeWalletKeyFromDB, removeStaleKeysFromDB } from '../utils/db';
import { apiFetch } from '../utils/api';

const AuthContext = createContext(null);

const STALE_THRESHOLD_MS = 7 * 24 * 60 * 60 * 1000;
const API_BASE = import.meta.env.VITE_API_BASE || '';

export function AuthProvider({ children }) {
  const [isReady, setIsReady] = useState(false);
  const [auth, setAuth] = useState({ apiKey: '', wallet: '', email: '' });
  const [walletKeys, setWalletKeys] = useState({});

  useEffect(() => {
    async function setup() {
      try {
        await initDB();
        setAuth(getAuthState());
        setWalletKeys(getWalletKeysFromDB());
        setIsReady(true);
      } catch (err) {
        console.error("Auth initialization failed", err);
        // Fallback to empty state if DB fails
        setIsReady(true);
      }
    }
    setup();
  }, []);

  useEffect(() => {
    if (!isReady) return;

    if (auth.apiKey && auth.wallet) {
      setAuthState(auth.apiKey, auth.wallet, auth.email || '');
      updateWalletKeyInDB(auth.wallet, auth.apiKey, auth.email || '');
      setWalletKeys(getWalletKeysFromDB());
    } else {
      clearAuthState();
    }
  }, [auth, isReady]);

  const signIn = (apiKey, wallet, email) => {
    setAuth({ apiKey, wallet, email });
  };

  const signOut = async () => {
    try {
      // Also call backend to clear httpOnly cookie
      await apiFetch('/api/auth/logout', { method: 'POST' });
    } catch (e) {
      console.error("Failed to call logout endpoint", e);
    }
    clearAuthState();
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
    removeStaleKeysFromDB(STALE_THRESHOLD_MS);
    setWalletKeys(getWalletKeysFromDB());
  };

  const deleteWalletKey = (wallet) => {
    removeWalletKeyFromDB(wallet);
    setWalletKeys(getWalletKeysFromDB());
  };

  if (!isReady) {
    return null; // Or a loading spinner
  }

  return (
    <AuthContext.Provider value={{ auth, signIn, signOut, walletKeys, getSavedWallets, removeStaleKeys, deleteWalletKey }}>
      {children}
    </AuthContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  return useContext(AuthContext);
}
