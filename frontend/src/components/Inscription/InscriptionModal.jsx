import React from 'react';
import { X } from 'lucide-react';
import CopyButton from '../Common/CopyButton';
import SafeQrCodeCanvas from '../Common/SafeQrCodeCanvas';
import DeliverablesReview from '../Review/DeliverablesReview';
import { QR_BYTE_LIMIT, shouldShowProposalAction } from './inscriptionUtils';
import { useInscriptionModalState } from './useInscriptionModalState';

const InscriptionModal = ({ inscription, onClose, initialTab = 'content' }) => {
  const m = useInscriptionModalState(inscription, initialTab);
  const {
    auth, network, setNetwork, activeTab, setActiveTab, monoContent, setMonoContent,
    proposalItems, isLoadingProposals, proposalError, approvingId, submissions, submissionsList,
    dashboardFilter, setDashboardFilter, dashboardSort, setDashboardSort,
    psbtForm, setPsbtForm, psbtResult, psbtError, psbtLoading, authBlocked, copiedPsbt,
    showPsbtQr, setShowPsbtQr, stegoPayload, stegoPayloadLoading, stegoPayloadError,
    scanMessage, scanLoading, scanError, scrollContainerRef,
    reworkRequests, isLoadingRework, showReworkForm, setShowReworkForm, reworkNotes, setReworkNotes,
    isSubmittingRework, setIsSubmittingRework,
    allTasks, approvedProposal, psbtTasks, deliverableTasks, approvedBudgetsTotal, payoutSummaries,
    stegoProposal, stegoTasks, stegoProposalStatus, stegoTaskStatusMap, hiddenMessageText,
    inscriptionMessage, inscriptionAddress, isRaiseFund, selectedTask,
    fundDepositAddress, resolvedContractorWallet, resolvedFundraiserWallet, textContent, pixelHash,
    confidencePercent, isContractLocked, modalImageSource, isHtmlContent, isSvgContent, sandboxSrc, inlineDoc,
    loadProposals, loadSubmissions, getLatestSubmissionByTask, approveProposal, copyToClipboard,
    generatePSBT, publishAndBuild,
  } = m;

  return (
    <div className="modal-backdrop">
      <div className="modal-container">
        <div className="modal-header">
          <h2 className="text-lg lg:text-xl font-bold">Smart Contract Details</h2>
          <button onClick={onClose} className="btn-icon">
            <X className="w-6 h-6" />
          </button>
        </div>

        <div className="modal-content" data-deliverables-scroll ref={scrollContainerRef}>
          <div className="flex flex-col items-start lg:flex-row gap-6 mb-6">
            <div className="flex-shrink-0">
              {pixelHash ? (
                <a 
                  href={`/sandbox/${pixelHash}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block hover:opacity-80 transition-opacity cursor-pointer"
                  title="View details in separate page"
                >
                  {modalImageSource ? (
                    <div className="relative">
                      <img
                        src={modalImageSource}
                        alt={inscription.file_name || inscription.id}
                        className="modal-identity-image"
                      />
                      {confidencePercent > 0 && (
                        <div className="badge badge-success absolute top-2 right-2">
                          {confidencePercent}%
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="modal-identity-image modal-placeholder flex items-center justify-center">
                      <div className="text-6xl text-center">
                        {inscription.contract_type === 'Steganographic Contract' ? '🎨' :
                         (inscription.mime_type || '').toLowerCase().includes('text') ? '📄' :
                         (inscription.mime_type || '').toLowerCase().includes('image') ? '🖼️' : '📦'}
                      </div>
                    </div>
                  )}
                </a>
              ) : (
                modalImageSource ? (
                  <div className="relative">
                    <img
                      src={modalImageSource}
                      alt={inscription.file_name || inscription.id}
                      className="modal-identity-image"
                    />
                    {confidencePercent > 0 && (
                      <div className="badge badge-success absolute top-2 right-2">
                        {confidencePercent}%
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="modal-identity-image modal-placeholder flex items-center justify-center">
                    <div className="text-6xl text-center">
                      {inscription.contract_type === 'Steganographic Contract' ? '🎨' :
                       (inscription.mime_type || '').toLowerCase().includes('text') ? '📄' :
                       (inscription.mime_type || '').toLowerCase().includes('image') ? '🖼️' : '📦'}
                    </div>
                  </div>
                )
              )}
            </div>

            <div className="flex-1 w-full">
              <div className="mb-6">
                <div className="modal-tabs">
                  {[
                    { id: 'content', label: 'Details', icon: '📋' },
                    { id: 'proposals', label: 'Proposals', icon: '🗂️' },
                    { id: 'deliverables', label: 'Deliverables', icon: '✅' },
                    { id: 'rework', label: 'Rework', icon: '🔧' },
                    { id: 'blockchain', label: 'Blockchain', icon: '⛓️' }
                  ].map((tab) => (
                    <button
                      key={tab.id}
                      onClick={() => setActiveTab(tab.id)}
                      className={`modal-tab ${activeTab === tab.id ? 'active' : ''}`}
                    >
                      <span>{tab.icon}</span>
                      {tab.label}
                    </button>
                  ))}
                </div>
              </div>


              {activeTab === 'content' && (
                <div className="space-y-6">
                  {pixelHash && (
                    <div>
                      <h4 className="modal-section-title">
                        <span className="modal-section-dot green"></span>
                        Contract Details
                      </h4>
                      <div className="modal-text-box">
                        <div className="flex flex-col gap-3">
                          <div className="flex flex-col gap-1">
                            <div className="flex items-center justify-between">
                              <span className="modal-data-label">Contract ID</span>
                              <CopyButton text={inscription.contract_id || inscription.id} />
                            </div>
                            <span className="font-mono text-sm text-primary break-all">{inscription.contract_id || inscription.id}</span>
                          </div>
                          <div className="flex flex-col gap-1">
                            <div className="flex items-center justify-between">
                              <span className="modal-data-label">Visible Pixel Hash</span>
                              <CopyButton text={pixelHash} />
                            </div>
                            <span className="font-mono text-sm text-primary break-all">{pixelHash}</span>
                          </div>
                          {inscription.status && (
                            <div className="flex flex-col gap-1">
                              <span className="modal-data-label">Status</span>
                              <span className="text-primary">{inscription.status}</span>
                            </div>
                          )}
                          {inscription.block_height > 0 && (
                            <div className="flex flex-col gap-1">
                              <span className="modal-data-label">Block Height</span>
                              <a href={`/block/${inscription.block_height}`} className="text-primary hover:underline cursor-pointer">{inscription.block_height}</a>
                            </div>
                          )}
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'proposals' && (
                <div className="space-y-4">
                  <div className="modal-proposals-header">
                    <div>
                      <h4 className="modal-proposals-title">Proposals for this contract</h4>
                      <p className="modal-proposals-subtitle">Tasks and budgets attached to this smart contract.</p>
                    </div>
                    <button
                      onClick={() => loadProposals({ showLoading: true })}
                      disabled={isLoadingProposals}
                      className="modal-proposals-refresh"
                    >
                      <span className="text-xs">↻</span>
                      {isLoadingProposals ? 'Refreshing…' : 'Refresh'}
                    </button>
                  </div>
                  {proposalError && <div className="modal-data-label">{proposalError}</div>}
                  {isLoadingProposals ? (
                    <div className="modal-data-label">Loading proposals...</div>
                  ) : proposalItems.length === 0 ? (
                    <div className="modal-data-label">No proposals found for this contract.</div>
                  ) : (
                    proposalItems.map((p) => {
                      const tasks = Array.isArray(p.tasks) && p.tasks.length > 0
                        ? p.tasks
                        : (Array.isArray(p.metadata?.suggested_tasks) ? p.metadata.suggested_tasks : []);
                      const totalTaskBudget = tasks.reduce((sum, t) => sum + (t.budget_sats || 0), 0);
                      const prettyTitle = (() => {
                        if (typeof p.title === 'string') {
                          try {
                            const o = JSON.parse(p.title);
                            if (o?.message) return o.message;
                          } catch { /* not JSON */ }
                        }
                        return p.title;
                      })();
                      const prettyDesc = (() => {
                        if (typeof p.description_md === 'string') {
                          try {
                            const o = JSON.parse(p.description_md);
                            if (o?.message) return o.message;
                          } catch { /* not JSON */ }
                        }
                        return p.description_md;
                      })();
                      const isApproved = (p.status || '').toLowerCase() === 'approved';
                      return (
                        <div key={p.id} className="modal-proposal-card">
                          <div className="modal-proposal-header">
                            <h5 className="modal-proposal-title">{prettyTitle}</h5>
                            <span className={`modal-proposal-status ${isApproved ? 'approved' : 'default'}`}>
                              {p.status || 'pending'}
                            </span>
                          </div>
                          <div className="modal-proposal-id">ID: {p.id}</div>
                          <div className="modal-proposal-desc">
                            {prettyDesc}
                          </div>
                          <div className="modal-proposal-meta">
                            <div>Budget: {p.budget_sats || totalTaskBudget || 0} sats</div>
                            <div>Tasks: {tasks.length}</div>
                            {p.visible_pixel_hash && (
                              <div className="modal-proposal-hash">
                                Evidence hash: {p.visible_pixel_hash}
                              </div>
                            )}
                          </div>
                          {tasks.length > 0 && (
                            <div className="modal-tasks-box">
                              <div className="modal-tasks-title">Tasks (with status)</div>
                              <ul className="list-disc pl-4 space-y-1">
                                {tasks.map((t) => {
                                  const submission = getLatestSubmissionByTask(t.task_id)
                                    || submissions[t.task_id]
                                    || (t.active_claim_id ? submissions[t.active_claim_id] : null);
                                  const status = (submission?.status || t.status || 'pending').toLowerCase();
                                  const statusClass = status === 'approved' ? 'approved' : status === 'available' ? 'available' : 'pending';
                                  return (
                                    <li key={t.task_id || t.title} className="modal-task-item">
                                      <span className="font-semibold">{t.title}</span>
                                      {t.budget_sats ? <span>— {t.budget_sats} sats</span> : null}
                                      <span className={`modal-task-status ${statusClass}`}>
                                        {status}
                                      </span>
                                      {t.contractor_wallet && (
                                        <span className="modal-task-wallet">
                                          • payout: {t.contractor_wallet}
                                        </span>
                                      )}
                                      {Array.isArray(t.skills || t.skills_required) && (t.skills || t.skills_required).length > 0 && (
                                        <span className="modal-task-skills">
                                          • {(t.skills || t.skills_required).join(', ')}
                                        </span>
                                      )}
                                      {submission && (
                                        <span className="modal-task-submission">
                                          • submission: {submission.status || 'pending'} {submission.completion_proof?.link ? `(${submission.completion_proof.link})` : ''}
                                        </span>
                                      )}
                                    </li>
                                  );
                                })}
                              </ul>
                            </div>
                          )}
                          <div className="modal-proposal-actions">
                            {(() => {
                              const statusLower = (p.status || '').toLowerCase();
                              const isFinal = ['approved', 'published'].includes(statusLower);
                              if (isFinal) {
                                return (
                                  <span className="modal-approved-label">
                                    Approved
                                  </span>
                                );
                              }
                              return (
                                <button
                                  onClick={() => approveProposal(p.id, false)}
                                  disabled={approvingId === p.id}
                                  className="modal-btn-approve"
                                >
                                  {approvingId === p.id ? 'Processing…' : 'Approve'}
                                </button>
                              );
                            })()}
                          </div>
                        </div>
                      );
                    })
                  )}
                </div>
              )}

              {activeTab === 'deliverables' && (
                <div className="space-y-4">
                  {isContractLocked ? null : !auth.apiKey || authBlocked ? (
                    <div className="modal-data-label">
                      Payout tools are unavailable for this session.
                    </div>
                  ) : !approvedProposal ? (
                    <div className="modal-data-label">Approve a proposal to unlock deliverables.</div>
                  ) : deliverableTasks.length === 0 ? (
                    <div className="modal-data-label">No deliverables available yet.</div>
                  ) : (
                  <div className="modal-deliverables-card">
                    <div className="modal-deliverables-header">
                      <div>
                        <h4 className="modal-deliverables-title">Publish & Build PSBT</h4>
                      </div>
                      <button
                        onClick={publishAndBuild}
                        disabled={(() => {
                          const payoutWallet = resolvedContractorWallet;
                          const fundraiserWallet = resolvedFundraiserWallet;
                          const fundingWallet =
                            selectedTask?.merkle_proof?.funding_address ||
                            inscription.metadata?.funding_address ||
                            '';
                          const fundingMismatch =
                            fundingWallet &&
                            auth.wallet &&
                            !isPlaceholderAddress(fundingWallet) &&
                            normalizeAddress(fundingWallet) !== normalizeAddress(auth.wallet);
                          const payoutList = payoutSummaries
                            .filter((p) => p.wallet && p.wallet !== 'Unknown wallet')
                            .map((p) => p.wallet);
                          const noTasks = deliverableTasks.length === 0;
                          const missingPayout = isRaiseFund
                            ? !fundraiserWallet
                            : payoutList.length === 0 && !payoutWallet;
                          return psbtLoading || !auth.wallet || !approvedProposal || missingPayout || noTasks || fundingMismatch;
                        })()}
                        className="modal-deliverables-btn"
                      >
                        {psbtLoading ? 'Building…' : 'Publish & Build'}
                      </button>
                    </div>
                    {!auth.wallet && (
                      <div className="modal-deliverables-alert">
                        Sign in with the funder API key (payer wallet) to build the PSBT.
                      </div>
                    )}
                    {psbtTasks.length === 0 && allTasks.length === 0 ? (
                      <div className="modal-data-label mt-3">No deliverables available yet.</div>
                    ) : (
                      <div className="modal-deliverables-grid text-sm mt-4" style={{ gap: '1rem' }}>
                        <div className="space-y-1">
                          <label className="block modal-deliverables-label">
                            {isRaiseFund ? 'Funding target (sum of task budgets)' : 'Budget (sum of proposal tasks)'}
                          </label>
                          <div className="modal-deliverables-box" style={{ minHeight: '2.5rem', padding: '0.5rem 0.75rem', fontFamily: 'monospace', fontSize: '0.7rem' }}>
                            {approvedBudgetsTotal || selectedTask?.budget_sats || 'n/a'} sats
                          </div>
                        </div>
                        <div className="space-y-1">
                          <label className="block modal-deliverables-label">
                            {isRaiseFund ? 'Fund deposit address' : 'Payer address'}
                          </label>
                          <div
                            className="modal-deliverables-box"
                            style={{ minHeight: '2.5rem', padding: '0.5rem 0.75rem', fontFamily: 'monospace', fontSize: '0.7rem' }}
                          >
                            {isRaiseFund ? resolvedFundraiserWallet || 'n/a' : auth.wallet || fundDepositAddress || 'n/a'}
                          </div>
                        </div>
                        <div className="md:col-span-2">
                          <label className="flex items-start gap-3 modal-deliverables-checkbox-label" style={{ padding: '0.75rem' }}>
                            <input
                              type="checkbox"
                              className="modal-deliverables-checkbox"
                              checked={psbtForm.includeDonation}
                              onChange={(e) => setPsbtForm((p) => ({ ...p, includeDonation: e.target.checked }))}
                            />
                            <span style={{ marginTop: '0.125rem' }}>Donate to Starlight Project to keep lights on</span>
                          </label>
                        </div>
                        <div className="space-y-1 md:col-span-2">
                          <label className="block modal-deliverables-label">Fee rate (sat/vB)</label>
                          <input
                            className="modal-deliverables-input"
                            type="number"
                            min="1"
                            step="1"
                            value={psbtForm.feeRate}
                            onChange={(e) => setPsbtForm((p) => ({ ...p, feeRate: e.target.value }))}
                          />
                        </div>
                        {!isRaiseFund && (
                          <div className="space-y-1 md:col-span-2">
                            <label className="block modal-deliverables-label">Change address</label>
                            <input
                              className="modal-deliverables-input"
                              type="text"
                              placeholder={auth.wallet || 'Defaults to payer wallet'}
                              value={psbtForm.changeAddress}
                              onChange={(e) => setPsbtForm((p) => ({ ...p, changeAddress: e.target.value }))}
                              spellCheck={false}
                              autoCapitalize="off"
                              autoCorrect="off"
                            />
                            <div className="modal-deliverables-label">
                              Leave blank to send change back to the payer wallet.
                            </div>
                          </div>
                        )}
                        <div className="space-y-1 md:col-span-2">
                          <div className="modal-deliverables-label">
                            {isRaiseFund
                              ? 'Contributor summary by contractor wallet'
                              : 'Payout summary by contractor wallet'}
                          </div>
                          <div className="modal-deliverables-box" style={{ flexDirection: 'column', gap: '0.5rem' }}>
                            {payoutSummaries.map((item) => (
                              <div key={item.wallet} className="modal-deliverables-row">
                                <span>{item.wallet}</span>
                                <span>{item.total} sats</span>
                              </div>
                            ))}
                          </div>
                        </div>
                        <div className="space-y-2 md:col-span-2">
                          <label className="block modal-deliverables-label">Contract ID</label>
                          <div className="modal-deliverables-contract">
                            {psbtForm.contractId || primaryContractId || 'n/a'}
                          </div>
                        </div>
                      </div>
                    )}
                    {(() => {
                      const payoutWallet = resolvedContractorWallet;
                      const fundraiserWallet = resolvedFundraiserWallet;
                      const payoutList = payoutSummaries
                        .filter((p) => p.wallet && p.wallet !== 'Unknown wallet')
                        .map((p) => p.wallet);
                      const payerAddress = psbtResult?.payer_address || auth.wallet || '';
                      if (isRaiseFund && !fundraiserWallet) {
                        return <div className="text-xs text-amber-600 dark:text-amber-400 mt-2">Fundraiser address missing.</div>;
                      }
                      if (!isRaiseFund && payerAddress && payoutList.length === 0 && payoutWallet === payerAddress) {
                        return (
                          <div className="modal-deliverables-alert">
                            Payout matches payer wallet—confirm contractor address.
                          </div>
                        );
                      }
                      return null;
                    })()}
                    {psbtError && <div className="modal-data-label mt-2">{psbtError}</div>}
                    {psbtResult && (() => {
                      const splitPsbts = Array.isArray(psbtResult.psbts) ? psbtResult.psbts : [];
                      if (splitPsbts.length > 0) {
                        return (
                          <div className="modal-psbt-result">
                            <div className="modal-psbt-row">
                              Split PSBTs: {splitPsbts.length} contributors
                            </div>
                            {splitPsbts.map((entry, index) => {
                              const psbtValue =
                                entry.psbt_hex ||
                                entry.psbt ||
                                entry.psbt_base64 ||
                                entry.encodedBase64 ||
                                entry.EncodedBase64 ||
                                '';
                              const psbtBase64 = entry.psbt_base64 || entry.encodedBase64 || entry.EncodedBase64 || '';
                              const payerAddress = entry.payer_address || 'Unknown';
                              return (
                                <div key={`${payerAddress}-${index}`} className="modal-deliverables-card" style={{ padding: '0.75rem' }}>
                                  <div className="modal-psbt-row" style={{ fontSize: '0.688rem' }}>
                                    Contributor: {payerAddress}
                                  </div>
                                  <div className="modal-psbt-grid">
                                    <div>Inputs selected</div>
                                    <div style={{ textAlign: 'right' }}>{entry.selected_sats} sats</div>
                                    <div>Funding target</div>
                                    <div style={{ textAlign: 'right' }}>{entry.budget_sats} sats</div>
                                    {entry.commitment_sats && psbtForm.includeDonation ? (
                                      <>
                                        <div>Donation</div>
                                        <div style={{ textAlign: 'right' }}>{entry.commitment_sats} sats</div>
                                      </>
                                    ) : null}
                                    <div>Fee</div>
                                    <div style={{ textAlign: 'right' }}>{entry.fee_sats} sats</div>
                                    <div>Change</div>
                                    <div style={{ textAlign: 'right' }}>{entry.change_sats} sats</div>
                                  </div>
                                  <textarea
                                    className="modal-psbt-textarea"
                                    rows={3}
                                    readOnly
                                    value={psbtValue}
                                  />
                                  <div className="modal-psbt-actions">
                                    <button
                                      onClick={() => copyToClipboard(psbtValue, `hex-${index}`)}
                                      className="modal-psbt-copy-btn"
                                    >
                                      {copiedPsbt === `hex-${index}` ? 'Copied hex' : 'Copy hex'}
                                    </button>
                                    {psbtBase64 && (
                                      <button
                                        onClick={() => copyToClipboard(psbtBase64, `b64-${index}`)}
                                        className="modal-psbt-copy-btn"
                                      >
                                        {copiedPsbt === `b64-${index}` ? 'Copied base64' : 'Copy base64'}
                                      </button>
                                    )}
                                  </div>
                                </div>
                              );
                            })}
                          </div>
                        );
                      }
                      const psbtValue =
                        psbtResult.psbt_hex ||
                        psbtResult.psbt ||
                        psbtResult.psbt_base64 ||
                        psbtResult.encodedBase64 ||
                        psbtResult.EncodedBase64 ||
                        '';
                      const psbtBase64 = psbtResult.psbt_base64 || psbtResult.encodedBase64 || psbtResult.EncodedBase64 || '';
                      const psbtBase64Bytes = psbtBase64
                        ? typeof TextEncoder === 'undefined'
                          ? psbtBase64.length
                          : new TextEncoder().encode(psbtBase64).length
                        : 0;
                      const psbtQrTooLarge = psbtBase64Bytes > QR_BYTE_LIMIT;
                      const effectiveRaiseFund = isRaiseFund || psbtResult?.funding_mode === 'raise_fund';
                      const budgetSats = effectiveRaiseFund
                        ? (psbtResult.budget_sats || approvedBudgetsTotal || 0)
                        : (psbtResult.budget_sats || psbtResult.budget || Number(psbtForm.budgetSats || 0) || 0);
                      const payerAddress = psbtResult.payer_address || auth.wallet || 'Not signed in';
                      const payerAddresses = Array.isArray(psbtResult.payer_addresses)
                        ? psbtResult.payer_addresses
                        : [];
                      const changeAddresses = Array.isArray(psbtResult.change_addresses)
                        ? psbtResult.change_addresses
                        : [];
                      const changeAmounts = Array.isArray(psbtResult.change_amounts)
                        ? psbtResult.change_amounts
                        : [];
                      const effectiveChangeAddress = psbtResult.change_address || changeAddresses[0] || '';
                      const payoutAmounts = Array.isArray(psbtResult.payout_amounts)
                        ? psbtResult.payout_amounts
                        : [];
                      const payerDisplay = effectiveRaiseFund
                        ? (payerAddresses.length > 0 ? payerAddresses.join(', ') : payerAddress)
                        : payerAddress;
                      return (
                        <div className="modal-psbt-result">
                          <div className="modal-psbt-row">
                            <span>
                              {effectiveRaiseFund ? 'Contributors' : 'Payer'}:{' '}
                              <span style={payerDisplay === 'Not signed in' ? { color: '#ef4444' } : {}}>
                                {payerDisplay}
                              </span>
                            </span>
                            <span>Network: {psbtResult.network_params || 'testnet4'}</span>
                          </div>
                          <div className="modal-psbt-row">
                            {effectiveRaiseFund ? 'Fund deposit script' : 'Payout script'}: {psbtResult.payout_script}
                          </div>
                          <div className="modal-psbt-grid">
                            <div>{effectiveRaiseFund ? 'Inputs selected' : 'Selected'}</div>
                            <div style={{ textAlign: 'right' }}>{psbtResult.selected_sats} sats</div>
                            <div>{effectiveRaiseFund ? 'Funding target' : 'Price'}</div>
                            <div style={{ textAlign: 'right' }}>{budgetSats} sats</div>
                            {psbtResult.commitment_sats && psbtForm.includeDonation ? (
                              <>
                                <div>Donation</div>
                                <div style={{ textAlign: 'right' }}>{psbtResult.commitment_sats} sats</div>
                              </>
                            ) : null}
                            <div>Fee</div>
                            <div style={{ textAlign: 'right' }}>{psbtResult.fee_sats} sats</div>
                            <div>Change</div>
                            <div style={{ textAlign: 'right' }}>{psbtResult.change_sats} sats</div>
                          </div>
                          {!effectiveRaiseFund && effectiveChangeAddress && (
                            <div className="modal-deliverables-card" style={{ padding: '0.5rem', fontSize: '0.688rem' }}>
                              <div className="modal-deliverables-label" style={{ fontWeight: 600, marginBottom: '0.25rem' }}>Change address</div>
                              <div style={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>{effectiveChangeAddress}</div>
                            </div>
                          )}
                          {effectiveRaiseFund && payoutAmounts.length > 1 && (
                            <div className="modal-deliverables-card" style={{ padding: '0.5rem', fontSize: '0.688rem' }}>
                              <div className="modal-deliverables-label" style={{ fontWeight: 600, marginBottom: '0.25rem' }}>Funding outputs (per task)</div>
                              {payoutAmounts.map((amount, index) => (
                                <div key={`${amount}-${index}`} className="modal-deliverables-row">
                                  <span>Output {index + 1}</span>
                                  <span>{amount} sats</span>
                                </div>
                              ))}
                            </div>
                          )}
                          {effectiveRaiseFund && changeAddresses.length > 0 && (
                            <div className="modal-deliverables-card" style={{ padding: '0.5rem', fontSize: '0.688rem' }}>
                              <div className="modal-deliverables-label" style={{ fontWeight: 600, marginBottom: '0.25rem' }}>Change outputs (by contributor)</div>
                              {changeAddresses.map((addr, index) => (
                                <div key={`${addr}-${index}`} className="modal-deliverables-row">
                                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{addr}</span>
                                  <span>{changeAmounts[index] ?? 0} sats</span>
                                </div>
                              ))}
                            </div>
                          )}
                          <textarea
                            className="modal-psbt-textarea"
                            rows={3}
                            readOnly
                            value={psbtValue}
                          />
                          <div className="modal-psbt-actions">
                            <button
                              onClick={() => copyToClipboard(psbtValue, 'hex')}
                              className="modal-psbt-copy-btn"
                            >
                              {copiedPsbt === 'hex' ? 'Copied hex' : 'Copy hex'}
                            </button>
                            {psbtBase64 && (
                              <>
                                <button
                                  onClick={() => copyToClipboard(psbtBase64, 'b64')}
                                  className="modal-psbt-copy-btn"
                                >
                                  {copiedPsbt === 'b64' ? 'Copied base64' : 'Copy base64'}
                                </button>
                                <button
                                  onClick={() => setShowPsbtQr((prev) => !prev)}
                                  className="modal-psbt-copy-btn"
                                >
                                  {showPsbtQr ? 'Hide QR' : 'Show QR'}
                                </button>
                              </>
                            )}
                          </div>
                          {showPsbtQr && psbtBase64 && (
                            <div className="modal-qr-container">
                              <div className="bg-white">
                                {psbtQrTooLarge ? (
                                  <div className="modal-qr-warning">
                                    PSBT is too large for a single QR. Copy the base64 instead.
                                  </div>
                                ) : (
                                  <SafeQrCodeCanvas value={psbtBase64} size={180} level="L" includeMargin />
                                )}
                              </div>
                            </div>
                          )}
                        </div>
                      );
                    })()}
                  </div>

                  )}

                  {(() => {
                    if (deliverableTasks.length === 0) return null;
                    const rows = deliverableTasks.map((task) => {
                      const submission = getLatestSubmissionByTask(task.task_id);
                      const submissionStatus = (submission?.status || '').toLowerCase();
                      let displayStatus = (task.status || 'pending').toLowerCase();
                      if (submissionStatus) {
                        displayStatus = submissionStatus;
                      }
                      return {
                        task,
                        submission,
                        displayStatus,
                      };
                    });
                    const counts = rows.reduce((acc, row) => {
                      acc.total += 1;
                      acc[row.displayStatus] = (acc[row.displayStatus] || 0) + 1;
                      return acc;
                    }, { total: 0 });
                    const approvedCount = counts.approved || 0;
                    const pendingReviewCount = counts.pending_review || 0;
                    const progress = counts.total > 0 ? Math.round((approvedCount / counts.total) * 100) : 0;

                    const filtered = rows.filter((row) => {
                      if (dashboardFilter === 'all') return true;
                      return row.displayStatus === dashboardFilter;
                    });
                    const sorted = [...filtered].sort((a, b) => {
                      if (dashboardSort === 'title') {
                        return (a.task.title || '').localeCompare(b.task.title || '');
                      }
                      if (dashboardSort === 'budget') {
                        return (b.task.budget_sats || 0) - (a.task.budget_sats || 0);
                      }
                      return (a.displayStatus || '').localeCompare(b.displayStatus || '');
                    });

                    const exportStatusReport = () => {
                      const payload = rows.map((row) => ({
                        task_id: row.task.task_id,
                        title: row.task.title,
                        status: row.displayStatus,
                        budget_sats: row.task.budget_sats,
                        submission_id: row.submission?.submission_id,
                        submitted_at: row.submission?.submitted_at || row.submission?.created_at,
                        rejection_reason: row.submission?.rejection_reason,
                        rejection_type: row.submission?.rejection_type,
                      }));
                      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
                      const url = URL.createObjectURL(blob);
                      const link = document.createElement('a');
                      link.href = url;
                      link.download = `task-status-${new Date().toISOString().slice(0, 10)}.json`;
                      document.body.appendChild(link);
                      link.click();
                      link.remove();
                      URL.revokeObjectURL(url);
                    };

                    return (
                      <div className="modal-dashboard-card">
                        <div className="modal-dashboard-header">
                          <div>
                            <h4 className="modal-dashboard-title">Task Status Dashboard</h4>
                            <p className="modal-dashboard-subtitle">Snapshot of task progress and review queue.</p>
                          </div>
                          <div className="modal-dashboard-filters">
                            <select
                              value={dashboardFilter}
                              onChange={(e) => setDashboardFilter(e.target.value)}
                              className="modal-dashboard-select"
                            >
                              <option value="all">All statuses</option>
                              <option value="available">Available</option>
                              <option value="claimed">Claimed</option>
                              <option value="submitted">Submitted</option>
                              <option value="pending_review">Pending review</option>
                              <option value="reviewed">Reviewed</option>
                              <option value="approved">Approved</option>
                              <option value="rejected">Rejected</option>
                            </select>
                            <select
                              value={dashboardSort}
                              onChange={(e) => setDashboardSort(e.target.value)}
                              className="modal-dashboard-select"
                            >
                              <option value="status">Sort by status</option>
                              <option value="title">Sort by title</option>
                              <option value="budget">Sort by budget</option>
                            </select>
                            <button
                              type="button"
                              onClick={exportStatusReport}
                              className="modal-dashboard-export-btn"
                            >
                              Export JSON
                            </button>
                          </div>
                        </div>

                        <div className="modal-dashboard-stats">
                          <div className="modal-dashboard-stat total">
                            <div className="modal-dashboard-stat-label">Total tasks</div>
                            <div className="modal-dashboard-stat-value">{counts.total || 0}</div>
                          </div>
                          <div className="modal-dashboard-stat approved">
                            <div className="modal-dashboard-stat-label">Approved</div>
                            <div className="modal-dashboard-stat-value">{counts.approved || 0}</div>
                          </div>
                          <div className="modal-dashboard-stat pending">
                            <div className="modal-dashboard-stat-label">Pending review</div>
                            <div className="modal-dashboard-stat-value">{pendingReviewCount}</div>
                          </div>
                          <div className="modal-dashboard-stat claimed">
                            <div className="modal-dashboard-stat-label">Claimed</div>
                            <div className="modal-dashboard-stat-value">{counts.claimed || 0}</div>
                          </div>
                        </div>

                        <div>
                          <div className="modal-dashboard-progress">
                            <span>Progress</span>
                            <span>{progress}% complete</span>
                          </div>
                          <div className="modal-dashboard-progress-bar">
                            <div
                              className="modal-dashboard-progress-fill"
                              style={{ width: `${progress}%` }}
                            />
                          </div>
                        </div>

                        {pendingReviewCount > 0 && (
                          <div className="modal-dashboard-alert">
                            {pendingReviewCount} submission{pendingReviewCount === 1 ? '' : 's'} waiting for review.
                          </div>
                        )}

                        <div className="modal-dashboard-table">
                          <div className="modal-dashboard-table-header">
                            <span>Task</span>
                            <span>Status</span>
                            <span>Budget</span>
                            <span>Last submission</span>
                          </div>
                          <div className="modal-dashboard-table-body">
                            {sorted.map((row) => (
                              <div key={row.task.task_id} className="modal-dashboard-table-row">
                                <span className="modal-dashboard-table-cell title">{row.task.title}</span>
                                <span className="modal-dashboard-table-cell status">{row.displayStatus}</span>
                                <span className="modal-dashboard-table-cell budget">{row.task.budget_sats || 0} sats</span>
                                <span className="modal-dashboard-table-cell date">
                                  {row.submission?.submitted_at
                                    ? new Date(row.submission.submitted_at).toLocaleDateString()
                                    : row.submission?.created_at
                                      ? new Date(row.submission.created_at).toLocaleDateString()
                                      : '—'}
                                </span>
                              </div>
                            ))}
                          </div>
                        </div>
                      </div>
                    );
                  })()}

                  <DeliverablesReview
                    proposalItems={proposalItems}
                    submissions={submissions}
                    submissionsList={submissionsList}
                    onRefresh={() => loadProposals({ showLoading: true })}
                    isContractLocked={isContractLocked}
                  />
                </div>
              )}

              {activeTab === 'content' && (
                <div className="space-y-6">
                  {(isHtmlContent || isSvgContent) && (sandboxSrc || inlineDoc) && (
                    <div>
                      <h4 className="modal-section-title">
                        <span className="modal-section-dot indigo"></span>
                        Sandboxed Preview
                      </h4>
                      <div className="modal-sandbox-box">
                        <iframe
                          title="inscription-sandbox"
                          src={sandboxSrc || undefined}
                          srcDoc={sandboxSrc ? undefined : inlineDoc}
                          sandbox="allow-scripts"
                          referrerPolicy="no-referrer"
                          className="modal-sandbox-iframe"
                        />
                      </div>
                      <div className="modal-data-label mt-2">
                        Rendered in an isolated sandbox (scripts enabled, DOM access restricted).
                      </div>
                    </div>
                  )}

                  {textContent && !(isHtmlContent || isSvgContent) && (
                    <div>
                      <h4 className="modal-section-title">
                        <span className="modal-section-dot blue"></span>
                        Text Content
                      </h4>
                      <div className="modal-text-box">
                        <div className="flex items-center justify-between mb-2">
                          <div className="flex items-start gap-2">
                            <span className="modal-text-label">📄</span>
                            <span className="modal-text-label font-medium">Inscription Text Data</span>
                          </div>
                          <CopyButton text={textContent} />
                        </div>
                        <div className="flex items-center gap-3 mb-3">
                          <label className="flex items-center gap-2 modal-text-label">
                            <input
                              type="checkbox"
                              checked={monoContent}
                              onChange={() => setMonoContent(!monoContent)}
                              className="form-checkbox h-4 w-4 text-indigo-600"
                            />
                            Monospace
                          </label>
                        </div>
                        <div className="modal-text-content">
                          <pre className={monoContent ? 'font-mono' : 'font-sans'}>
                            {textContent}
                          </pre>
                        </div>
                        <div className="modal-stats">
                          <div className="modal-stats-grid">
                            <div className="modal-stats-item">
                              <div className="modal-stats-value">
                                {textContent.length}
                              </div>
                              <div className="modal-stats-label">Characters</div>
                            </div>
                            <div className="modal-stats-item">
                              <div className="modal-stats-value">
                                {textContent.split(' ').filter(word => word.length > 0).length}
                              </div>
                              <div className="modal-stats-label">Words</div>
                            </div>
                            <div className="modal-stats-item">
                              <div className="modal-stats-value">
                                {textContent.split('\n').length}
                              </div>
                              <div className="modal-stats-label">Lines</div>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {(inscription.metadata?.extracted_message || scanLoading || scanMessage || stegoPayloadLoading || stegoPayload || stegoPayloadError || scanError) && (
                    <div>
                      <h4 className="modal-section-title">
                        <span className="modal-section-dot green"></span>
                        Hidden Message
                      </h4>
                      <div className="modal-hidden-box">
                        <div className="flex items-center justify-between">
                          <div className="flex items-start gap-2">
                            <span className="modal-hidden-label">🔓</span>
                            <span className="modal-hidden-label font-medium">Extracted Hidden Data</span>
                          </div>
                          <CopyButton text={hiddenMessageText} />
                        </div>

                        {scanLoading && (
                          <div className="modal-hidden-value">Scanning stego image…</div>
                        )}
                        {!scanLoading && scanError && (
                          <div className="modal-warning-text">Scan unavailable: {scanError}</div>
                        )}
                        {stegoPayloadLoading && (
                          <div className="modal-hidden-value">Loading stego payload…</div>
                        )}
                        {!stegoPayloadLoading && stegoPayloadError && (
                          <div className="modal-warning-text">Payload unavailable: {stegoPayloadError}</div>
                        )}

                        {stegoProposal ? (
                          <div className="space-y-3">
                            <div className="modal-stego-card">
                              <div className="modal-stego-label">Proposal</div>
                              <div className="modal-stego-value text-lg font-semibold">{stegoProposal.title || 'Untitled'}</div>
                              {stegoProposal.description_md && (
                                <div className="modal-stego-value mt-2 whitespace-pre-wrap">
                                  {stegoProposal.description_md}
                                </div>
                              )}
                              <div className="modal-stego-label mt-3 flex flex-wrap gap-4">
                                <span>Budget: {stegoProposal.budget_sats || 0} sats</span>
                                <span>Visible Hash: {stegoProposal.visible_pixel_hash || '—'}</span>
                                {stegoProposalStatus && <span>Status: {stegoProposalStatus}</span>}
                              </div>
                            </div>

                            <div className="modal-stego-card">
                              <div className="modal-stego-label">Tasks</div>
                              {stegoTasks.length > 0 ? (
                                <div className="modal-stego-value divide-y">
                                  {stegoTasks.map((task) => (
                                    <div key={task.task_id || task.title} className="py-2 flex flex-col gap-1">
                                      <div className="flex items-center justify-between">
                                        <span className="modal-stego-value font-semibold">{task.title || 'Untitled task'}</span>
                                        <span className="modal-stego-label">
                                          {task.budget_sats || 0} sats
                                        </span>
                                      </div>
                                      {task.description && (
                                        <div className="modal-stego-label text-xs whitespace-pre-wrap">{task.description}</div>
                                      )}
                                      <div className="modal-stego-label text-xs flex flex-wrap gap-3">
                                        {task.task_id && <span>ID: {task.task_id}</span>}
                                        <span>Status: {stegoTaskStatusMap.get(task.task_id) || 'unknown'}</span>
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              ) : (
                                <div className="modal-stego-value">No tasks found in payload.</div>
                              )}
                            </div>
                          </div>
                          ) : (
                          <div className="modal-text-content">
                            <pre className="font-mono">
                              {hiddenMessageText}
                            </pre>
                          </div>
                        )}
                      </div>
                    </div>
                  )}

                  {!textContent && !inscription.metadata?.extracted_message && !pixelHash && (
                    <div className="text-center py-12">
                      <div className="text-6xl mb-4">📦</div>
                      <div className="modal-data-label font-semibold">No Text Content Available</div>
                      <div className="modal-data-label text-sm mt-2">
                        This inscription contains binary data or media content that cannot be displayed as text.
                      </div>
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'analysis' && (
                <div className="space-y-6">
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-yellow-500 rounded-full"></span>
                      Steganographic Analysis Report
                    </h4>
                    <div className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4">
                      <div className="flex items-center gap-2 mb-4">
                        <div className="w-3 h-3 bg-yellow-500 rounded-full animate-pulse"></div>
                        <span className="text-yellow-800 dark:text-yellow-200 font-medium">Analysis Complete - Hidden Data Detected</span>
                      </div>
                      <p className="text-yellow-700 dark:text-yellow-300 mb-4 leading-relaxed">
                        This smart contract contains embedded data patterns consistent with advanced steganographic techniques. 
                        Steganographic analysis has identified patterns within the carrier medium.
                      </p>
                      
                      {inscription.metadata && (
                        <div className="grid grid-cols-2 gap-4 mt-6">
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Detection Algorithm</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.detection_method || 'Analysis Required'}</div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Steganography Type</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.stego_type || 'Unknown'}</div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Carrier Format</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.image_format || 'Unknown'}</div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Data Payload</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.image_size || 'Unknown'} bytes</div>
                          </div>
                        </div>
                      )}
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-cyan-500 rounded-full"></span>
                      Analysis Timeline
                    </h4>
                    <div className="bg-cyan-50 dark:bg-cyan-900 border border-cyan-200 dark:border-cyan-700 rounded-lg p-4">
                      <div className="space-y-3">
                        <div className="flex items-center gap-3">
                          <div className="w-2 h-2 bg-cyan-500 rounded-full"></div>
                          <div className="flex-1">
                            <div className="text-cyan-900 dark:text-cyan-100 font-medium">Image Extraction</div>
                            <div className="text-cyan-700 dark:text-cyan-300 text-sm">Successfully extracted image from transaction witness data</div>
                          </div>
                        </div>
                        <div className="flex items-center gap-3">
                          <div className="w-2 h-2 bg-cyan-500 rounded-full"></div>
                          <div className="flex-1">
                            <div className="text-cyan-900 dark:text-cyan-100 font-medium">Pattern Analysis</div>
                            <div className="text-cyan-700 dark:text-cyan-300 text-sm">Applied steganographic analysis algorithms</div>
                          </div>
                        </div>
                        <div className="flex items-center gap-3">
                          <div className="w-2 h-2 bg-green-500 rounded-full"></div>
                          <div className="flex-1">
                            <div className="text-cyan-900 dark:text-cyan-100 font-medium">Message Extraction</div>
                            <div className="text-cyan-700 dark:text-cyan-300 text-sm">Successfully decoded hidden message from carrier</div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {activeTab === 'rework' && (
                <div className="space-y-4">
                  <div className="flex justify-between items-center">
                    <h4 className="modal-section-title">
                      <span className="modal-section-dot orange"></span>
                      Contract Rework Requests
                    </h4>
                    {auth.apiKey && (
                      <button
                        onClick={() => setShowReworkForm(true)}
                        className="modal-btn-reject"
                      >
                        + Request Rework
                      </button>
                    )}
                  </div>
                  
                  {showReworkForm && (
                    <div className="modal-form-card">
                      <div className="space-y-3">
                        <label className="modal-data-label">Feedback Notes</label>
                        <textarea
                          value={reworkNotes}
                          onChange={(e) => setReworkNotes(e.target.value)}
                          placeholder="Explain what needs to be reworked..."
                          className="modal-textarea"
                          rows={4}
                        />
                        <div className="flex gap-2">
                          <button
                            onClick={async () => {
                              if (!reworkNotes.trim()) {
                                toast.error('Please provide feedback notes');
                                return;
                              }
                              setIsSubmittingRework(true);
                              try {
                                const res = await apiFetch(
                                  `/api/smart_contract/contracts/${inscription.id}/rework`,
                                  {
                                    method: 'POST',
                                    headers: {
                                      'Content-Type': 'application/json',
                                      'X-API-Key': auth.apiKey,
                                    },
                                    body: JSON.stringify({ notes: reworkNotes }),
                                  }
                                );
                                if (!res.ok) {
                                  throw new Error('Failed to create rework request');
                                }
                                toast.success('Rework request submitted');
                                setReworkNotes('');
                                setShowReworkForm(false);
                                // Refresh rework requests
                                const reworkRes = await apiFetch(
                                  `/api/smart_contract/contracts/${inscription.id}/rework`,
                                  { headers: { 'X-API-Key': auth.apiKey } }
                                );
                                if (reworkRes.ok) {
                                  const data = await reworkRes.json();
                                  setReworkRequests(data.rework_requests || []);
                                }
                              } catch (err) {
                                toast.error(err.message);
                              } finally {
                                setIsSubmittingRework(false);
                              }
                            }}
                            disabled={isSubmittingRework}
                            className="modal-btn-approve"
                          >
                            {isSubmittingRework ? 'Submitting...' : 'Submit'}
                          </button>
                          <button
                            onClick={() => {
                              setShowReworkForm(false);
                              setReworkNotes('');
                            }}
                            className="modal-btn-cancel"
                          >
                            Cancel
                          </button>
                        </div>
                      </div>
                    </div>
                  )}

                  {isLoadingRework ? (
                    <div className="modal-loading">Loading rework requests...</div>
                  ) : reworkRequests.length === 0 ? (
                    <div className="modal-data-label">No rework requests yet.</div>
                  ) : (
                    <div className="space-y-3">
                      {reworkRequests.map((req) => (
                        <div key={req.request_id} className="modal-rework-card">
                          <div className="flex justify-between items-start">
                            <div>
                              <span className={`badge ${req.status === 'open' ? 'badge-warning' : 'badge-success'}`}>
                                {req.status}
                              </span>
                              <span className="modal-data-label ml-2">
                                {new Date(req.created_at).toLocaleString()}
                              </span>
                            </div>
                            <div className="flex items-center gap-2">
                              <div className="font-mono text-xs text-gray-500">
                                {req.requester?.slice(0, 8)}...{req.requester?.slice(-4)}
                              </div>
                              {req.status === 'open' && auth.apiKey && auth.wallet && req.requester && auth.wallet.toLowerCase() === req.requester.toLowerCase() && (
                                <button
                                  onClick={async () => {
                                    try {
                                      const res = await apiFetch(
                                        `/api/smart_contract/contracts/${inscription.id}/rework/${req.request_id}`,
                                        {
                                          method: 'PATCH',
                                          headers: { 'X-API-Key': auth.apiKey },
                                        }
                                      );
                                      if (res.ok) {
                                        // Refresh rework requests
                                        const reworkRes = await apiFetch(
                                          `/api/smart_contract/contracts/${inscription.id}/rework`,
                                          { headers: { 'X-API-Key': auth.apiKey } }
                                        );

                                        if (reworkRes.ok) {
                                          const data = await reworkRes.json();
                                          setReworkRequests(data.rework_requests || []);
                                        }
                                        toast.success('Rework request resolved');
                                      }
                                    } catch {
                                      toast.error('Failed to resolve rework request');
                                    }
                                  }}
                                  className="modal-btn-approve"
                                >
                                  Resolve
                                </button>
                              )}
                            </div>
                          </div>
                          <div className="modal-text-box mt-2">
                            {req.notes}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'blockchain' && (
                <div className="space-y-6">
                  <div>
                    <h4 className="modal-section-title">
                      <span className="modal-section-dot purple"></span>
                      Transaction Information
                    </h4>
                    <div className="modal-blockchain-card purple">
                      <div className="space-y-3">
                        <div className="modal-blockchain-row">
                          <span className="modal-blockchain-label">Transaction ID</span>
                          <div className="flex items-center gap-2">
                            <span className="modal-blockchain-mono">
                              {(inscription.tx_id || inscription.id) ? (inscription.tx_id || inscription.id).slice(0, 12) + '...' : 'TBD'}
                            </span>
                            <CopyButton text={inscription.tx_id || inscription.id || 'TBD'} />
                          </div>
                        </div>
                        <div className="modal-blockchain-row">
                          <span className="modal-blockchain-label">Block Height</span>
                          <span className="modal-blockchain-value font-semibold">
                            {(inscription.block_height || inscription.genesis_block_height) ? (
                              <a href={`/block/${inscription.block_height || inscription.genesis_block_height}`} className="text-primary hover:underline cursor-pointer">
                                {inscription.block_height || inscription.genesis_block_height}
                              </a>
                            ) : 'Unknown'}
                          </span>
                        </div>
                        <div className="modal-blockchain-row">
                          <span className="modal-blockchain-label">Network</span>
                          <span className="modal-blockchain-value font-semibold">Bitcoin {network.charAt(0).toUpperCase() + network.slice(1)}</span>
                        </div>
                      </div>
                    </div>
                  </div>

                  {inscription.metadata?.scanned_at && (
                    <div>
                      <h4 className="modal-section-title">
                        <span className="modal-section-dot green"></span>
                        Analysis Information
                      </h4>
                      <div className="modal-blockchain-card green">
                        <div className="modal-blockchain-grid">
                          <div className="modal-blockchain-grid-item">
                            <div className="modal-blockchain-grid-label">Scan Status</div>
                            <div className="modal-blockchain-grid-value">
                              {inscription.metadata.is_stego ? 'Steganography Detected' : 'No Hidden Data'}
                            </div>
                          </div>
                          <div className="modal-blockchain-grid-item">
                            <div className="modal-blockchain-grid-label">Last Scanned</div>
                            <div className="modal-blockchain-grid-value">
                              {new Date(inscription.metadata.scanned_at * 1000).toLocaleString()}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
               )}
             </div>
           </div>
         </div>
       </div>
     </div>
  );
};

export default InscriptionModal;
