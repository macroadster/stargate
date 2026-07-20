import React from 'react';
import { render, screen, waitFor, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import OpenContractsView from './OpenContractsView';

vi.mock('../../utils/api', () => ({
  apiFetch: vi.fn(),
}));

vi.mock('../Inscription/InscriptionCard', () => ({
  default: ({ inscription }) => (
    <div data-testid="inscription-card">{inscription.id}</div>
  ),
}));

import { apiFetch } from '../../utils/api';

function pageResponse(ids, { hasMore = false, nextCursor = '' } = {}) {
  return {
    ok: true,
    json: async () => ({
      success: true,
      data: {
        transactions: ids.map((id, index) => ({
          id,
          status: 'pending',
          text: `wish ${id}`,
          // newest first from API
          timestamp: 1_700_000_000 - index,
        })),
        contracts: ids.map((id) => ({ id, status: 'pending' })),
        has_more: hasMore,
        next_cursor_date: nextCursor,
      },
    }),
  };
}

describe('OpenContractsView infinite scroll', () => {
  let observerCallback;

  beforeEach(() => {
    vi.clearAllMocks();
    observerCallback = null;
    global.IntersectionObserver = class {
      constructor(cb) {
        observerCallback = cb;
      }
      observe() {}
      disconnect() {}
      unobserve() {}
    };
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('loads first page and appends next page when sentinel intersects', async () => {
    apiFetch
      .mockResolvedValueOnce(
        pageResponse(
          Array.from({ length: 20 }, (_, i) => `open-${i}`),
          { hasMore: true, nextCursor: '2026-07-12T12:00:00Z' },
        ),
      )
      .mockResolvedValueOnce(
        pageResponse(['open-older'], {
          hasMore: false,
          nextCursor: '',
        }),
      );

    render(<OpenContractsView setSelectedInscription={vi.fn()} refreshKey={0} />);

    await waitFor(() => {
      expect(screen.getAllByTestId('inscription-card')).toHaveLength(20);
    });

    expect(apiFetch).toHaveBeenCalledTimes(1);
    const firstUrl = String(apiFetch.mock.calls[0][0]);
    expect(firstUrl).toContain('open=true');
    expect(firstUrl).toContain('limit=20');
    expect(firstUrl).not.toContain('cursor_date=');

    await act(async () => {
      observerCallback?.([{ isIntersecting: true }]);
    });

    await waitFor(() => {
      expect(apiFetch).toHaveBeenCalledTimes(2);
    });

    const secondUrl = String(apiFetch.mock.calls[1][0]);
    expect(secondUrl).toContain('cursor_date=2026-07-12T12%3A00%3A00Z');
    expect(secondUrl).toContain('cursor_type=before');

    await waitFor(() => {
      expect(screen.getByText('open-older')).toBeInTheDocument();
      expect(screen.getAllByTestId('inscription-card')).toHaveLength(21);
    });
  });

  it('shows empty state when there are no open contracts', async () => {
    apiFetch.mockResolvedValueOnce(pageResponse([], { hasMore: false }));

    render(<OpenContractsView setSelectedInscription={vi.fn()} refreshKey={0} />);

    await waitFor(() => {
      expect(screen.getByText(/No pending transactions/i)).toBeInTheDocument();
    });
  });

  it('preserves API newest-first order without client re-sort jumps', async () => {
    const now = Math.floor(Date.now() / 1000);
    apiFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        success: true,
        data: {
          // Server already returns newest first
          transactions: [
            { id: 'newer', status: 'pending', text: 'new', timestamp: now },
            { id: 'older', status: 'pending', text: 'old', timestamp: now - 1000 },
          ],
          has_more: false,
          next_cursor_date: '',
        },
      }),
    });

    render(<OpenContractsView setSelectedInscription={vi.fn()} refreshKey={0} />);

    await waitFor(() => {
      const cards = screen.getAllByTestId('inscription-card');
      expect(cards).toHaveLength(2);
      expect(cards[0]).toHaveTextContent('newer');
      expect(cards[1]).toHaveTextContent('older');
    });
  });

  it('does not clear the list while a second page is loading', async () => {
    let resolveSecond;
    apiFetch
      .mockResolvedValueOnce(
        pageResponse(
          Array.from({ length: 20 }, (_, i) => `open-${i}`),
          { hasMore: true, nextCursor: '2026-07-12T12:00:00Z' },
        ),
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveSecond = resolve;
          }),
      );

    render(<OpenContractsView setSelectedInscription={vi.fn()} refreshKey={0} />);

    await waitFor(() => {
      expect(screen.getAllByTestId('inscription-card')).toHaveLength(20);
    });

    await act(async () => {
      observerCallback?.([{ isIntersecting: true }]);
    });

    // While page 2 is in flight, page 1 cards must remain mounted (no jump-to-top wipe).
    expect(screen.getAllByTestId('inscription-card')).toHaveLength(20);

    await act(async () => {
      resolveSecond(
        pageResponse(['open-older'], { hasMore: false, nextCursor: '' }),
      );
    });

    await waitFor(() => {
      expect(screen.getAllByTestId('inscription-card')).toHaveLength(21);
    });
  });
});
