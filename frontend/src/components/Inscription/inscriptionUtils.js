export const shouldShowProposalAction = (status) => {
  const normalized = (status || '').toLowerCase();
  return !(normalized === 'rejected' || normalized === 'published');
};
