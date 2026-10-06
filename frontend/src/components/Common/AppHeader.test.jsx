import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import AppHeader from './AppHeader';

const mockAuth = vi.hoisted(() => ({
  auth: { apiKey: '', wallet: '', email: '' },
  signOut: vi.fn(),
}));

vi.mock('../../context/AuthContext', () => ({
  useAuth: () => mockAuth,
}));

vi.mock('../../context/ThemeContext', () => ({
  useTheme: () => ({
    isDarkMode: false,
    useSystemTheme: false,
    setTheme: vi.fn(),
  }),
}));

const renderHeader = (props = {}) =>
  render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route path="/" element={<AppHeader showTextToggle hideText hideImages {...props} />} />
        <Route path="/auth" element={<div>Auth page</div>} />
      </Routes>
    </MemoryRouter>
  );

describe('AppHeader inscription filters', () => {
  beforeEach(() => {
    mockAuth.auth = { apiKey: '', wallet: '', email: '' };
  });

  it('shows Hide images checked by default next to Hide text', () => {
    renderHeader();
    fireEvent.click(screen.getByTitle('More options'));
    const hideText = screen.getAllByText('Hide text');
    const hideImages = screen.getAllByText('Hide images');
    expect(hideText.length).toBeGreaterThanOrEqual(1);
    expect(hideImages.length).toBeGreaterThanOrEqual(1);
    hideImages.forEach((label) => {
      expect(label.closest('button').querySelector('svg')).toBeTruthy();
    });
  });

  it('sends unsigned users to sign in instead of changing filters', () => {
    const onToggleImages = vi.fn();
    const onToggleText = vi.fn();
    renderHeader({ onToggleImages, onToggleText });
    fireEvent.click(screen.getByTitle('More options'));
    fireEvent.click(screen.getAllByText('Hide images')[0]);
    expect(onToggleImages).not.toHaveBeenCalled();
    expect(onToggleText).not.toHaveBeenCalled();
    expect(screen.getByText('Auth page')).toBeInTheDocument();
  });

  it('lets a signed-in user toggle Hide images', () => {
    mockAuth.auth = { apiKey: 'key', wallet: 'tb1qtest', email: '' };
    const onToggleImages = vi.fn();
    renderHeader({ onToggleImages });
    fireEvent.click(screen.getByTitle('More options'));
    fireEvent.click(screen.getAllByText('Hide images')[0]);
    expect(onToggleImages).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('Auth page')).not.toBeInTheDocument();
  });
});
