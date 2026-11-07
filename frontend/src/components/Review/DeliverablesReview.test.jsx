import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { vi } from 'vitest';
import DeliverablesReview from './DeliverablesReview';
import { AuthProvider } from '../../context/AuthContext';

// Mock sql.js to prevent WASM file loading attempts in test (jsdom has no /sql-wasm.wasm)
vi.mock('sql.js', () => {
  const mockDatabase = {
    run: vi.fn(),
    export: vi.fn(() => new Uint8Array(0)),
    exec: vi.fn(() => []),
  };
  return {
    default: vi.fn().mockResolvedValue({
      Database: vi.fn().mockImplementation(function () {
        return mockDatabase;
      }),
    }),
  };
});

// Mock API_BASE
jest.mock('../../apiBase', () => ({
  API_BASE: 'http://localhost:3001'
}));

// Mock toast
jest.mock('react-hot-toast', () => ({
  success: jest.fn(),
  error: jest.fn()
}));

jest.mock('react-markdown', () => {
  return function MockMarkdown({ children }) {
    return <div>{children}</div>;
  };
});

// Mock CopyButton
jest.mock('../Common/CopyButton', () => {
  return function MockCopyButton({ text }) {
    return <button data-testid="copy-button">{text}</button>;
  };
});

// Mock fetch
global.fetch = jest.fn();

describe('DeliverablesReview', () => {
  test('renders no deliverables message when empty', async () => {
    render(
      <AuthProvider>
        <DeliverablesReview
          proposalItems={[]}
          submissions={{}}
          onRefresh={jest.fn()}
        />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByText('No Deliverables Found')).toBeInTheDocument();
    });
  });

  test('collapsed task hides notes; expand shows full submission notes', async () => {
    const submission = {
      submission_id: 'sub-1',
      task_id: 'task-1',
      claim_id: 'claim-1',
      status: 'pending',
      submitted_at: '2026-01-01T00:00:00Z',
      deliverables: {
        notes: '# Work complete\n\n- item one\n- item two',
        document: '## Document\n\nDetails here.',
      },
    };

    render(
      <AuthProvider>
        <DeliverablesReview
          proposalItems={[
            {
              id: 'prop-1',
              title: 'Test Proposal',
              tasks: [
                {
                  task_id: 'task-1',
                  title: 'Implement feature',
                  active_claim_id: 'claim-1',
                },
              ],
            },
          ]}
          submissions={{ 'task-1': submission }}
          onRefresh={jest.fn()}
        />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByText('Implement feature')).toBeInTheDocument();
    });

    // Collapsed header should surface task ID (not proposal ID) as high-level detail
    expect(screen.getByText(/Task:\s*task-1/)).toBeInTheDocument();
    expect(screen.queryByText(/Proposal:\s*prop-1/)).not.toBeInTheDocument();

    // Collapsed: no notes preview box / submission notes section
    expect(screen.queryByText('Submission Notes')).not.toBeInTheDocument();
    expect(document.querySelector('.deliverables-notes-preview')).toBeNull();
    expect(screen.queryByText(/Work complete/)).not.toBeInTheDocument();

    const expandBtn = document.querySelector('.deliverables-expand-btn');
    expect(expandBtn).toBeTruthy();
    fireEvent.click(expandBtn);

    await waitFor(() => {
      expect(screen.getByText('Submission Notes')).toBeInTheDocument();
      expect(screen.getByText('Submission Document')).toBeInTheDocument();
      expect(screen.getAllByText(/Work complete/).length).toBeGreaterThan(0);
      expect(screen.getAllByText(/Details here/).length).toBeGreaterThan(0);
    });
  });

  test('collapsed task shows latest attempt status, not first attempt', async () => {
    const first = {
      submission_id: 'sub-1',
      task_id: 'task-1',
      claim_id: 'claim-1',
      status: 'rejected',
      submitted_at: '2026-01-01T00:00:00Z',
      created_at: '2026-01-01T00:00:00Z',
      deliverables: { notes: 'first attempt notes' },
    };
    const latest = {
      submission_id: 'sub-2',
      task_id: 'task-1',
      claim_id: 'claim-2',
      status: 'approved',
      submitted_at: '2026-02-01T00:00:00Z',
      created_at: '2026-02-01T00:00:00Z',
      deliverables: { notes: 'latest attempt notes' },
    };

    // Oldest-first order is the revealing case: a plain map overwrite
    // (first -> second) would keep latest, but a newest-first overwrite
    // or an object map keyed by task_id would keep 'rejected'.
    // Pass the stale task_id map (first attempt) alongside the full
    // list so the collapsed header must resolve by timestamp.
    render(
      <AuthProvider>
        <DeliverablesReview
          proposalItems={[
            {
              id: 'prop-1',
              title: 'Test Proposal',
              tasks: [
                {
                  task_id: 'task-1',
                  title: 'Implement feature',
                  active_claim_id: 'claim-2',
                },
              ],
            },
          ]}
          submissions={{ 'task-1': first, 'sub-1': first, 'sub-2': latest }}
          submissionsList={[first, latest]}
          onRefresh={jest.fn()}
        />
      </AuthProvider>
    );

    await waitFor(() => {
      expect(screen.getByText('Implement feature')).toBeInTheDocument();
    });

    // Collapsed header must reflect the latest (approved) attempt.
    expect(screen.getByText('approved')).toBeInTheDocument();
    expect(screen.queryByText('rejected')).not.toBeInTheDocument();
    // Collapsed must not leak the first attempt's notes.
    expect(screen.queryByText(/first attempt notes/)).not.toBeInTheDocument();

    const expandBtn = document.querySelector('.deliverables-expand-btn');
    fireEvent.click(expandBtn);

    await waitFor(() => {
      expect(screen.getByText('Submission Timeline')).toBeInTheDocument();
      expect(screen.getByText('Attempt 1')).toBeInTheDocument();
      expect(screen.getByText('Attempt 2')).toBeInTheDocument();
      expect(screen.getAllByText('rejected').length).toBeGreaterThan(0);
    });
  });
});
