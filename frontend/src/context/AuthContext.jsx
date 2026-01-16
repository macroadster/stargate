import React, { createContext, useContext, useEffect, useState } from 'react';

const AuthContext = createContext(null);

const STORAGE_KEY = 'X-API-Key';
const STORAGE_KEYS_LIST = 'X-API-Key-List';

export function AuthProvider({ children }) {
  const [auth, setAuth] = useState(() => {
    const key = localStorage.getItem(STORAGE_KEY) || '';
    const wallet = localStorage.getItem('X-Wallet-Address') || '';
    const email = localStorage.getItem('X-User-Email') || '';
    return { apiKey: key, wallet, email };
  });

  const [savedKeys, setSavedKeys] = useState(() => {
    try {
      return JSON.parse(localStorage.getItem(STORAGE_KEYS_LIST) || '[]');
    } catch {
      return [];
    }
  });

  useEffect(() => {
    if (auth.apiKey) {
      localStorage.setItem(STORAGE_KEY, auth.apiKey);
      localStorage.setItem('X-Wallet-Address', auth.wallet || '');
      localStorage.setItem('X-User-Email', auth.email || '');
    }
  }, [auth]);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEYS_LIST, JSON.stringify(savedKeys.slice(0, 20)));
  }, [savedKeys]);

  const addKey = (entry) => {
    if (!entry?.apiKey) return;
    setSavedKeys((prev) => {
      const filtered = prev.filter((k) => k.apiKey !== entry.apiKey);
      return [{ ...entry }, ...filtered].slice(0, 20);
    });
  };

  const signIn = (apiKey, wallet, email) => {
    setAuth({ apiKey, wallet, email });
    addKey({ apiKey, wallet, email, label: wallet || email || apiKey.slice(-6) });
  };

  const signOut = () => {
    localStorage.removeItem(STORAGE_KEY);
    localStorage.removeItem('X-Wallet-Address');
    localStorage.removeItem('X-User-Email');
    setAuth({ apiKey: '', wallet: '', email: '' });
  };

  return (
    <AuthContext.Provider value={{ auth, signIn, signOut, savedKeys, addKey }}>
      {children}
    </AuthContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  return useContext(AuthContext);
}
