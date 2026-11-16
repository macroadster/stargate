import React from 'react';
import CopyButton from './CopyButton';
import { fundingProofStatus, readProofField } from './fundingProofStatus';

export const FundingProofBadge = ({ proof }) => {
  const status = fundingProofStatus(proof);
  if (!status) return null;
  const confirmed = status === 'confirmed';
  return (
    <span
      className={`badge ${confirmed ? 'badge-success' : 'badge-warning'}`}
      title={`Funding proof ${status}`}
      data-testid="funding-proof-badge"
    >
      {confirmed ? 'Confirmed' : 'Pending'}
    </span>
  );
};

const shorten = (value, head = 12, tail = 6) => {
  const s = String(value || '');
  if (s.length <= head + tail + 1) return s;
  return `${s.slice(0, head)}…${s.slice(-tail)}`;
};

/**
 * FundingProofSummary renders the on-chain funding proof for a task/contract:
 * confirmation status, funding txid, block height, funded sats, and (when
 * known) the confirmation timestamp and sweep disposition.
 */
const FundingProofSummary = ({ proof }) => {
  if (!proof) return null;
  const status = fundingProofStatus(proof) || 'unknown';
  const confirmed = status === 'confirmed';
  const txid = String(readProofField(proof, 'tx_id', 'TxID') || '');
  const height = Number(readProofField(proof, 'block_height', 'BlockHeight') || 0) || 0;
  const funded = Number(readProofField(proof, 'funded_amount_sats', 'FundedAmountSats') || 0) || 0;
  const fundingAddress = String(readProofField(proof, 'funding_address', 'FundingAddress') || '');
  const sweep = String(readProofField(proof, 'sweep_status', 'SweepStatus') || '').trim().toLowerCase();
  const confirmedAtRaw = readProofField(proof, 'confirmed_at', 'ConfirmedAt');
  const confirmedAt = confirmedAtRaw ? new Date(confirmedAtRaw) : null;
  const confirmedAtValid = confirmedAt && !Number.isNaN(confirmedAt.getTime());

  return (
    <div
      className={`modal-blockchain-card ${confirmed ? 'green' : 'purple'}`}
      data-testid="funding-proof-summary"
    >
      <div className="space-y-3">
        <div className="modal-blockchain-row">
          <span className="modal-blockchain-label">Confirmation</span>
          <span
            className={`badge ${confirmed ? 'badge-success' : 'badge-warning'}`}
            data-testid="proof-confirmation-status"
          >
            {status}
          </span>
        </div>
        {txid && (
          <div className="modal-blockchain-row">
            <span className="modal-blockchain-label">Funding TX</span>
            <div className="flex items-center gap-2">
              <span className="modal-blockchain-mono" title={txid}>
                {shorten(txid)}
              </span>
              <CopyButton text={txid} />
            </div>
          </div>
        )}
        <div className="modal-blockchain-row">
          <span className="modal-blockchain-label">Block Height</span>
          <span className="modal-blockchain-value font-semibold">
            {height > 0 ? (
              <a
                href={`/block/${height}`}
                className="text-primary hover:underline cursor-pointer"
              >
                {height}
              </a>
            ) : (
              'TBD'
            )}
          </span>
        </div>
        <div className="modal-blockchain-row">
          <span className="modal-blockchain-label">Funded</span>
          <span className="modal-blockchain-value font-semibold">{funded.toLocaleString()} sats</span>
        </div>
        {fundingAddress && (
          <div className="modal-blockchain-row">
            <span className="modal-blockchain-label">Funding Address</span>
            <span className="modal-blockchain-mono" title={fundingAddress}>
              {shorten(fundingAddress, 10, 6)}
            </span>
          </div>
        )}
        {confirmedAtValid && (
          <div className="modal-blockchain-row">
            <span className="modal-blockchain-label">Confirmed At</span>
            <span className="modal-blockchain-value font-semibold">{confirmedAt.toLocaleString()}</span>
          </div>
        )}
        {sweep && (
          <div className="modal-blockchain-row">
            <span className="modal-blockchain-label">Sweep</span>
            <span className="modal-blockchain-value font-semibold">
              {sweep === 'direct' ? 'Not needed (donation paid in PSBT)' : sweep}
            </span>
          </div>
        )}
      </div>
    </div>
  );
};

export default FundingProofSummary;
