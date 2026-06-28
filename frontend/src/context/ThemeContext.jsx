/* eslint-disable react-refresh/only-export-components */
import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';

const ThemeContext = createContext(null);

const migrateTheme = () => {
  const saved = localStorage.getItem('theme');
  if (saved === 'system') {
    localStorage.setItem('theme', 'auto');
  }
};

export const ThemeProvider = ({ children }) => {
  migrateTheme();

  const getSystemTheme = useCallback(() => {
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  }, []);

  const [isDarkMode, setIsDarkMode] = useState(() => {
    const savedTheme = localStorage.getItem('theme');
    if (savedTheme && savedTheme !== 'auto') {
      return savedTheme === 'dark';
    }
    return getSystemTheme();
  });

  const [useSystemTheme, setUseSystemTheme] = useState(() => {
    const savedTheme = localStorage.getItem('theme');
    return !savedTheme || savedTheme === 'auto';
  });

  useEffect(() => {
    if (isDarkMode) {
      document.documentElement.classList.add('dark');
      document.documentElement.setAttribute('data-theme', 'dark');
    } else {
      document.documentElement.classList.remove('dark');
      document.documentElement.setAttribute('data-theme', 'light');
    }
  }, [isDarkMode]);

  useEffect(() => {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');

    const handleSystemThemeChange = (e) => {
      if (useSystemTheme) {
        setIsDarkMode(e.matches);
      }
    };

    mediaQuery.addEventListener('change', handleSystemThemeChange);
    return () => mediaQuery.removeEventListener('change', handleSystemThemeChange);
  }, [useSystemTheme]);

  const toggleTheme = useCallback(() => {
    setUseSystemTheme(false);
    setIsDarkMode(prev => {
      const newValue = !prev;
      localStorage.setItem('theme', newValue ? 'dark' : 'light');
      return newValue;
    });
  }, []);

  const setTheme = useCallback((theme) => {
    if (theme === 'auto') {
      setUseSystemTheme(true);
      setIsDarkMode(getSystemTheme());
      localStorage.setItem('theme', 'auto');
    } else {
      setUseSystemTheme(false);
      setIsDarkMode(theme === 'dark');
      localStorage.setItem('theme', theme);
    }
  }, [getSystemTheme]);

  return (
    <ThemeContext.Provider value={{ isDarkMode, toggleTheme, useSystemTheme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
};

export const useTheme = () => {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return context;
};
