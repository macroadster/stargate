import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import FundingProofSummary, { FundingProofBadge } from './FundingProofSummary';
import { fundingProofStatus } from './fundingProofStatus';

vi.mock('./CopyButton', () => ({
  default: () => null,
}));

const confirmedProof = {
  tx_id: 'abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd12',
  block_height: 842100,
  funded_amount_sats: 42000,
  funding_address: 'tb1qfund0ngaddress000000000000000000000000',
  confirmation_status: 'confirmed',
  confirmed_at: '2026-11-16T12:00:00Z',
  sweep_status: 'direct',
};

const provisionalProof = {
  tx_id: 'beef5678beef5678beef5678beef5678beef5678beef5678beef5678beef56',
  block_height: 842090,
  funded_amount_sats: 1000,
  confirmation_status: 'provisional',
};

describe('fundingProofStatus', () => {
  it('normalizes case and whitespace', () => {
    expect(fundingProofStatus({ confirmation_status: ' Confirmed ' })).toBe('confirmed');
    expect(fundingProofStatus({ confirmation_status: 'PROVISIONAL' })).toBe('provisional');
    expect(fundingProofStatus({})).toBe('');
    expect(fundingProofStatus(null)).toBe('');
  });
});

describe('FundingProofBadge', () => {
  it('renders Confirmed for a confirmed proof', () => {
    render(<FundingProofBadge proof={confirmedProof} />);
    expect(screen.getByTestId('funding-proof-badge').textContent).toBe('Confirmed');
  });

  it('renders Pending for a provisional proof', () => {
    render(<FundingProofBadge proof={provisionalProof} />);
    expect(screen.getByTestId('funding-proof-badge').textContent).toBe('Pending');
  });

  it('renders nothing without a status', () => {
    const { container } = render(<FundingProofBadge proof={{}} />);
    expect(container.firstChild).toBeNull();
  });
});

describe('FundingProofSummary', () => {
  it('renders the confirmed proof details', () => {
    render(<FundingProofSummary proof={confirmedProof} />);
    expect(screen.getByTestId('proof-confirmation-status').textContent).toBe('confirmed');
    expect(screen.getByText('842100')).toBeTruthy();
    expect(screen.getByText('42,000 sats')).toBeTruthy();
    expect(screen.getByText('Not needed (donation paid in PSBT)')).toBeTruthy();
  });

  it('renders provisional proofs without confirmed-only rows', () => {
    render(<FundingProofSummary proof={provisionalProof} />);
    expect(screen.getByTestId('proof-confirmation-status').textContent).toBe('provisional');
    expect(screen.getByText('842090')).toBeTruthy();
    expect(screen.queryByText(/Confirmed At/)).toBeNull();
  });

  it('renders TBD when the block height is unknown', () => {
    render(<FundingProofSummary proof={{ ...provisionalProof, block_height: 0 }} />);
    expect(screen.getByText('TBD')).toBeTruthy();
  });

  it('renders nothing for a null proof', () => {
    const { container } = render(<FundingProofSummary proof={null} />);
    expect(container.firstChild).toBeNull();
  });
});
