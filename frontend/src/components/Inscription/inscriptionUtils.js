// Shared helpers for Inscription modal / cards

/** QR version 40-L max byte capacity (base64 uses byte mode). */
export const QR_BYTE_LIMIT = 2953;

export const guessNetworkFromAddress = (addr) => {
  const a = (addr || '').trim().toLowerCase();
  if (!a) return '';
  if (a.startsWith('tb1') || a.startsWith('m') || a.startsWith('n') || a.startsWith('2')) return 'testnet4';
  if (a.startsWith('bc1') || a.startsWith('1') || a.startsWith('3')) return 'mainnet';
  return '';
};

export const shouldShowProposalAction = (status) => {
  const normalized = (status || '').toLowerCase();
  return !(normalized === 'rejected' || normalized === 'published');
};
