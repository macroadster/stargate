import React from 'react';
import { render, screen, waitFor, act, fireEvent } from '@testing-library/react';
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
    // Pretend the sentinel is always near the viewport so post-load checks fire.
    Element.prototype.getBoundingClientRect = vi.fn(() => ({
      top: 100,
      bottom: 140,
      left: 0,
      right: 100,
      width: 100,
      height: 40,
    }));
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

  it('loads first page and appends next page when sentinel intersects after load', async () => {
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

    // After load, re-armed observer / rAF check should request page 2 when near viewport.
    await waitFor(() => {
      expect(apiFetch.mock.calls.length).toBeGreaterThanOrEqual(2);
    });

    const moreCall = apiFetch.mock.calls.find((c) =>
      String(c[0]).includes('cursor_date='),
    );
    expect(moreCall).toBeTruthy();
    expect(String(moreCall[0])).toContain('cursor_type=before');

    await waitFor(() => {
      expect(screen.getByText('open-older')).toBeInTheDocument();
    });
  });

  it('loads more when "Scroll for more" is clicked', async () => {
    // Keep sentinel "far" so auto load-more does not fire; only click should.
    Element.prototype.getBoundingClientRect = vi.fn(() => ({
      top: 5000,
      bottom: 5040,
      left: 0,
      right: 100,
      width: 100,
      height: 40,
    }));

    apiFetch
      .mockResolvedValueOnce(
        pageResponse(
          Array.from({ length: 20 }, (_, i) => `open-${i}`),
          { hasMore: true, nextCursor: '2026-07-12T12:00:00Z' },
        ),
      )
      .mockResolvedValueOnce(
        pageResponse(['open-via-click'], { hasMore: false, nextCursor: '' }),
      );

    render(<OpenContractsView setSelectedInscription={vi.fn()} refreshKey={0} />);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /scroll for more/i })).toBeInTheDocument();
    });

    // Only first page so far
    expect(apiFetch).toHaveBeenCalledTimes(1);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /scroll for more/i }));
    });

    await waitFor(() => {
      expect(screen.getByText('open-via-click')).toBeInTheDocument();
    });
  });

  it('shows empty state when there are no open contracts', async () => {
    apiFetch.mockResolvedValueOnce(pageResponse([], { hasMore: false }));

    render(<OpenContractsView setSelectedInscription={vi.fn()} refreshKey={0} />);

    await waitFor(() => {
      expect(screen.getByText(/No pending transactions/i)).toBeInTheDocument();
    });
  });

  it('preserves API newest-first order', async () => {
    Element.prototype.getBoundingClientRect = vi.fn(() => ({
      top: 5000,
      bottom: 5040,
      left: 0,
      right: 100,
      width: 100,
      height: 40,
    }));
    const now = Math.floor(Date.now() / 1000);
    apiFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        success: true,
        data: {
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
    Element.prototype.getBoundingClientRect = vi.fn(() => ({
      top: 5000,
      bottom: 5040,
      left: 0,
      right: 100,
      width: 100,
      height: 40,
    }));

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
      expect(screen.getByRole('button', { name: /scroll for more/i })).toBeInTheDocument();
    });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /scroll for more/i }));
    });

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
