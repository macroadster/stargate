import { shouldShowProposalAction } from './InscriptionModal';

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
