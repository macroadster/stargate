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
  shouldShowInscription,
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
    expect(isConfirmedContract({ status: 'confirmed' })).toBe(true);
    expect(isConfirmedContract({ metadata: { confirmed_txid: 'abc' } })).toBe(true);
    expect(isConfirmedContract({ metadata: {} })).toBe(false);
  });

  it('parses stego manifest lines', () => {
    const m = parseStegoManifest('schema_version: 1\npayload_cid: xyz');
    expect(m.payload_cid).toBe('xyz');
  });
});

describe('shouldShowInscription', () => {
  const textInscription = { mime_type: 'text/plain', file_name: 'note.txt', text: 'hello' };
  const imageInscription = { mime_type: 'image/png', file_name: 'pic.png', image_url: '/content/abc' };
  const contractImage = {
    mime_type: 'image/png',
    file_name: 'stego.png',
    image_url: '/api/block-image/100/stego.png',
    metadata: { contract_id: 'wish-1', is_stego: true },
    contract_id: 'wish-1',
  };
  const stegoWithMessage = {
    mime_type: 'image/jpeg',
    file_name: 'hidden.jpg',
    image_url: '/content/def',
    metadata: { extracted_message: 'secret', is_stego: true },
  };

  it('hides text and regular images by default, keeps contracts', () => {
    expect(shouldShowInscription(textInscription)).toBe(false);
    expect(shouldShowInscription(imageInscription)).toBe(false);
    expect(shouldShowInscription(contractImage)).toBe(true);
    expect(shouldShowInscription(stegoWithMessage)).toBe(true);
  });

  it('shows regular images when hideImages is off', () => {
    const opts = { hideText: true, hideImages: false };
    expect(shouldShowInscription(textInscription, opts)).toBe(false);
    expect(shouldShowInscription(imageInscription, opts)).toBe(true);
    expect(shouldShowInscription(contractImage, opts)).toBe(true);
  });

  it('shows text when hideText is off', () => {
    const opts = { hideText: false, hideImages: true };
    expect(shouldShowInscription(textInscription, opts)).toBe(true);
    expect(shouldShowInscription(imageInscription, opts)).toBe(false);
    expect(shouldShowInscription(contractImage, opts)).toBe(true);
  });

  it('shows everything when both hides are off', () => {
    const opts = { hideText: false, hideImages: false };
    expect(shouldShowInscription(textInscription, opts)).toBe(true);
    expect(shouldShowInscription(imageInscription, opts)).toBe(true);
    expect(shouldShowInscription(contractImage, opts)).toBe(true);
  });
});
