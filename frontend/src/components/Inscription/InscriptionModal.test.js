import { shouldShowProposalAction } from './inscriptionUtils';

describe('shouldShowProposalAction', () => {
  it('hides for rejected proposals', () => {
    expect(shouldShowProposalAction('rejected')).toBe(false);
  });

  it('hides for published proposals', () => {
    expect(shouldShowProposalAction('published')).toBe(false);
  });

  it('shows for pending proposals', () => {
    expect(shouldShowProposalAction('pending')).toBe(true);
  });

  it('shows for approved proposals', () => {
    expect(shouldShowProposalAction('approved')).toBe(true);
  });
});

import {
  looksLikeRaiseFund,
  expandContractCandidates,
  isPlaceholderAddress,
  isConfirmedContract,
  parseStegoManifest,
} from './inscriptionUtils';

describe('inscriptionUtils', () => {
  it('detects raise fund wording', () => {
    expect(looksLikeRaiseFund('Please raise fund')).toBe(true);
    expect(looksLikeRaiseFund('plain')).toBe(false);
  });

  it('expands wish prefixes on contract candidates', () => {
    const ids = expandContractCandidates({ id: 'abc', metadata: {} });
    expect(ids).toContain('abc');
    expect(ids).toContain('wish-abc');
  });

  it('flags placeholder addresses', () => {
    expect(isPlaceholderAddress('')).toBe(true);
    expect(isPlaceholderAddress('bc1qreal')).toBe(false);
  });

  it('detects confirmed contracts', () => {
    expect(isConfirmedContract({ metadata: { confirmation_status: 'confirmed' } })).toBe(true);
    expect(isConfirmedContract({ metadata: {} })).toBe(false);
  });

  it('parses stego manifest lines', () => {
    const m = parseStegoManifest('schema_version: 1\npayload_cid: xyz');
    expect(m.payload_cid).toBe('xyz');
  });
});
