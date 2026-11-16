const read = (proof, snake, pascal) => {
  const v = proof?.[snake];
  return v !== undefined && v !== null && v !== '' ? v : proof?.[pascal];
};

/**
 * fundingProofStatus normalizes a merkle proof's confirmation status
 * ("provisional" | "confirmed") to lowercase, or "" when unset.
 */
export const fundingProofStatus = (proof) =>
  String(read(proof, 'confirmation_status', 'ConfirmationStatus') || '')
    .trim()
    .toLowerCase();

/** readProofField reads a merkle proof field accepting snake_case or PascalCase. */
export const readProofField = read;
