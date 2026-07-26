import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import WishChatModal from './WishChatModal';

vi.mock('../../context/AuthContext', () => ({
  useAuth: () => ({
    auth: { apiKey: 'test-key', wallet: 'tb1qtestwallet', email: 't@example.com' },
  }),
}));

const apiFetch = vi.fn();
vi.mock('../../utils/api', () => ({
  apiFetch: (...args) => apiFetch(...args),
}));

vi.mock('../Common/MarkdownContent', () => ({
  default: ({ children }) => <div data-testid="md">{children}</div>,
}));

describe('WishChatModal', () => {
  beforeEach(() => {
    apiFetch.mockReset();
  });

  it('renders WishBot welcome and draft panel', () => {
    render(<WishChatModal onClose={() => {}} />);
    expect(screen.getByRole('dialog', { name: /wishbot/i })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /^wishbot$/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/add cover image/i)).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/what should agents deliver/i)).toBeInTheDocument();
  });

  it('collects wish from chat and enables inscribe', async () => {
    render(<WishChatModal onClose={() => {}} />);

    const composer = screen.getByPlaceholderText(/describe your wish/i);
    fireEvent.change(composer, { target: { value: 'Build a retro game for 50000 sats' } });
    fireEvent.click(screen.getByLabelText(/^send$/i));

    await waitFor(() => {
      expect(screen.getByDisplayValue(/build a retro game/i)).toBeInTheDocument();
    });
    expect(screen.getByDisplayValue('50000')).toBeInTheDocument();

    const inscribeBtn = screen.getByRole('button', { name: /inscribe wish/i });
    expect(inscribeBtn).not.toBeDisabled();
  });

  it('submits to /api/inscribe on confirm', async () => {
    const onSuccess = vi.fn();
    apiFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'wish-abc123' }),
    });

    render(<WishChatModal onClose={() => {}} onSuccess={onSuccess} />);

    const composer = screen.getByPlaceholderText(/describe your wish/i);
    fireEvent.change(composer, { target: { value: 'Paint a mural, price 1000 sats' } });
    fireEvent.click(screen.getByLabelText(/^send$/i));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /inscribe wish/i })).not.toBeDisabled();
    });

    fireEvent.click(screen.getByRole('button', { name: /inscribe wish/i }));

    await waitFor(() => {
      expect(apiFetch).toHaveBeenCalledWith(
        '/api/inscribe',
        expect.objectContaining({
          method: 'POST',
          headers: expect.objectContaining({ 'X-API-Key': 'test-key' }),
        }),
      );
    });

    const body = JSON.parse(apiFetch.mock.calls[0][1].body);
    expect(body.message).toMatch(/mural/i);
    expect(body.price).toBe('1000');
    expect(body.price_unit).toBe('sats');
    expect(body.address).toBe('tb1qtestwallet');
    expect(body.image_base64).toBeTruthy();

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalled();
      expect(screen.getAllByText(/submitted/i).length).toBeGreaterThan(0);
    });
    expect(screen.getByRole('button', { name: /create another/i })).toBeInTheDocument();
  });
});
