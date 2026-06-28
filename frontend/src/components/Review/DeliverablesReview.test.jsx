import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
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
});
