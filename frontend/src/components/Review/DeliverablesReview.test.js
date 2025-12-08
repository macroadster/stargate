import React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import DeliverablesReview from './DeliverablesReview';

// Mock API_BASE
jest.mock('../../apiBase', () => ({
  API_BASE: 'http://localhost:3001'
}));

// Mock toast
jest.mock('react-hot-toast', () => ({
  success: jest.fn(),
  error: jest.fn()
}));

// Mock CopyButton
jest.mock('../Common/CopyButton', () => {
  return function MockCopyButton({ text }) {
    return <button data-testid="copy-button">{text}</button>;
  };
});

// Mock fetch
global.fetch = jest.fn();

describe('DeliverablesReview', () => {
  test('renders no deliverables message when empty', () => {
    render(
      <DeliverablesReview
        proposalItems={[]}
        submissions={{}}
        onRefresh={jest.fn()}
      />
    );

    expect(screen.getByText('No Deliverables Found')).toBeInTheDocument();
  });
});