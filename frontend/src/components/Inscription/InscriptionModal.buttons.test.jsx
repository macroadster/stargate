import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import InscriptionModal from './InscriptionModal';

const approveProposal = vi.fn();
const setShowReworkForm = vi.fn();
const setActiveTab = vi.fn();

const baseState = {
  auth: { apiKey: 'test-key', wallet: 'tb1qtest' },
  network: 'testnet4',
  setNetwork: vi.fn(),
  activeTab: 'proposals',
  setActiveTab,
  monoContent: false,
  setMonoContent: vi.fn(),
  proposalItems: [
    {
      id: 'prop-1',
      title: 'Pending proposal',
      status: 'pending',
      budget_sats: 1000,
    },
  ],
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
  showReworkForm: false,
  setShowReworkForm,
  reworkNotes: '',
  setReworkNotes: vi.fn(),
  isSubmittingRework: false,
  setIsSubmittingRework: vi.fn(),
  allTasks: [],
  approvedProposal: null,
  psbtTasks: [],
  deliverableTasks: [],
  approvedBudgetsTotal: 0,
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
  isContractLocked: false,
  modalImageSource: null,
  isHtmlContent: false,
  isSvgContent: false,
  sandboxSrc: '',
  inlineDoc: '',
  loadProposals: vi.fn(),
  loadSubmissions: vi.fn(),
  getLatestSubmissionByTask: vi.fn(),
  approveProposal,
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
  metadata: {},
};

describe('InscriptionModal confirmed-contract actions', () => {
  beforeEach(() => {
    approveProposal.mockReset();
    setShowReworkForm.mockReset();
    setActiveTab.mockReset();
  });

  it('keeps Approve enabled for unconfirmed contracts', () => {
    useInscriptionModalState.mockReturnValue({ ...baseState, isContractLocked: false });
    render(<InscriptionModal inscription={inscription} onClose={() => {}} initialTab="proposals" />);

    expect(screen.getByRole('button', { name: 'Approve' })).not.toBeDisabled();
  });

  it('keeps Request Rework enabled for unconfirmed contracts', () => {
    useInscriptionModalState.mockReturnValue({
      ...baseState,
      isContractLocked: false,
      activeTab: 'rework',
    });
    render(<InscriptionModal inscription={inscription} onClose={() => {}} initialTab="rework" />);

    expect(screen.getByRole('button', { name: /\+ Request Rework/ })).not.toBeDisabled();
  });

  it('disables Approve for a confirmed contract', () => {
    useInscriptionModalState.mockReturnValue({ ...baseState, isContractLocked: true });
    render(<InscriptionModal inscription={inscription} onClose={() => {}} initialTab="proposals" />);

    const approve = screen.getByRole('button', { name: 'Approve' });
    expect(approve).toBeDisabled();
    expect(approve).toHaveAttribute('title', 'Confirmed contracts cannot be approved');
  });

  it('disables Request Rework for a confirmed contract', () => {
    useInscriptionModalState.mockReturnValue({
      ...baseState,
      isContractLocked: true,
      activeTab: 'rework',
    });
    render(<InscriptionModal inscription={inscription} onClose={() => {}} initialTab="rework" />);

    const rework = screen.getByRole('button', { name: /\+ Request Rework/ });
    expect(rework).toBeDisabled();
    expect(rework).toHaveAttribute('title', 'Confirmed contracts cannot request rework');
  });
});
