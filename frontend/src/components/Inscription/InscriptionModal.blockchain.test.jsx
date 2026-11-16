import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import InscriptionModal from './InscriptionModal';

const setActiveTab = vi.fn();

const confirmedProof = {
  tx_id: 'abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd12',
  block_height: 842100,
  funded_amount_sats: 42000,
  funding_address: 'tb1qfund0ngaddress000000000000000000000000',
  confirmation_status: 'confirmed',
  confirmed_at: '2026-11-16T12:00:00Z',
  sweep_status: 'direct',
};

const baseState = {
  auth: { apiKey: 'test-key', wallet: 'tb1qtest' },
  network: 'testnet4',
  setNetwork: vi.fn(),
  activeTab: 'blockchain',
  setActiveTab,
  monoContent: false,
  setMonoContent: vi.fn(),
  proposalItems: [],
  isLoadingProposals: false,
  proposalError: '',
  approvingId: '',
  submissions: {},
  submissionsList: [],
  dashboardFilter: 'all',
  setDashboardFilter: vi.fn(),
  dashboardSort: 'newest',
  setDashboardSort: vi.fn(),
  psbtForm: {},
  setPsbtForm: vi.fn(),
  psbtResult: null,
  psbtError: '',
  psbtLoading: false,
  authBlocked: false,
  copiedPsbt: '',
  showPsbtQr: false,
  setShowPsbtQr: vi.fn(),
  stegoPayload: null,
  stegoPayloadLoading: false,
  stegoPayloadError: '',
  scanMessage: '',
  scanLoading: false,
  scanError: '',
  scrollContainerRef: { current: null },
  reworkRequests: [],
  setReworkRequests: vi.fn(),
  isLoadingRework: false,
  reworkError: '',
  showReworkForm: false,
  setShowReworkForm: vi.fn(),
  reworkNotes: '',
  setReworkNotes: vi.fn(),
  isSubmittingRework: false,
  setIsSubmittingRework: vi.fn(),
  allTasks: [
    { task_id: 'task-1', title: 'Deliver the goods', status: 'claimed', merkle_proof: confirmedProof },
  ],
  approvedProposal: null,
  psbtTasks: [],
  deliverableTasks: [],
  approvedBudgetsTotal: 0,
  wishBudgetSats: 1000,
  remainingWishSats: 1000,
  payoutSummaries: [],
  stegoProposal: null,
  stegoTasks: [],
  stegoProposalStatus: '',
  stegoTaskStatusMap: {},
  hiddenMessageText: '',
  inscriptionMessage: '',
  inscriptionAddress: '',
  isRaiseFund: false,
  selectedTask: null,
  fundDepositAddress: '',
  resolvedContractorWallet: '',
  resolvedFundraiserWallet: '',
  textContent: '',
  pixelHash: '',
  confidencePercent: 0,
  isContractLocked: true,
  modalImageSource: null,
  isHtmlContent: false,
  isSvgContent: false,
  sandboxSrc: '',
  inlineDoc: '',
  loadProposals: vi.fn(),
  loadSubmissions: vi.fn(),
  getLatestSubmissionByTask: vi.fn(),
  approveProposal: vi.fn(),
  copyToClipboard: vi.fn(),
  generatePSBT: vi.fn(),
  publishAndBuild: vi.fn(),
};

vi.mock('./useInscriptionModalState', () => ({
  useInscriptionModalState: vi.fn(),
}));

vi.mock('../Common/CopyButton', () => ({
  default: () => null,
}));

vi.mock('../Common/MarkdownContent', () => ({
  default: ({ children }) => <div>{children}</div>,
}));

vi.mock('../Common/SafeQrCodeCanvas', () => ({
  default: () => null,
}));

vi.mock('../Review/DeliverablesReview', () => ({
  default: () => null,
}));

import { useInscriptionModalState } from './useInscriptionModalState';

const inscription = {
  id: 'contract-1',
  file_name: 'wish.png',
  tx_id: 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef00',
  block_height: 842100,
  metadata: {},
};

describe('InscriptionModal blockchain tab — funding proof', () => {
  beforeEach(() => {
    setActiveTab.mockReset();
  });

  it('shows the confirmed merkle proof for the contract', () => {
    useInscriptionModalState.mockReturnValue({ ...baseState });
    render(
      <MemoryRouter>
        <InscriptionModal inscription={inscription} onClose={() => {}} initialTab="blockchain" />
      </MemoryRouter>,
    );

    expect(screen.getByText('Funding Proof')).toBeTruthy();
    expect(screen.getByTestId('proof-confirmation-status').textContent).toBe('confirmed');
    expect(screen.getByText('42,000 sats')).toBeTruthy();
    // inscription block height + proof block height both render
    expect(screen.getAllByText('842100').length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText('Not needed (donation paid in PSBT)')).toBeTruthy();
  });

  it('hides the funding proof section when no task carries one', () => {
    useInscriptionModalState.mockReturnValue({ ...baseState, allTasks: [] });
    render(
      <MemoryRouter>
        <InscriptionModal inscription={inscription} onClose={() => {}} initialTab="blockchain" />
      </MemoryRouter>,
    );

    expect(screen.queryByText('Funding Proof')).toBeNull();
  });
});
