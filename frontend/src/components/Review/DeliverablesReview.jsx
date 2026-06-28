import React from 'react';
import ReactMarkdown from 'react-markdown';
import { CheckCircle, XCircle, Clock, ExternalLink, Filter, ChevronDown, ChevronUp, Eye, FileText, Code, Columns, List } from 'lucide-react';
import CopyButton from '../Common/CopyButton';
import {
  countWords,
  getSubmissionNotes,
  getNotesPreview,
  getSubmissionId,
  formatTimestamp,
  sortSubmissions,
} from './deliverablesUtils';
import { useDeliverablesReview } from './useDeliverablesReview';

const DeliverablesReview = ({ proposalItems, submissions, submissionsList, onRefresh, isContractLocked = false }) => {
  const d = useDeliverablesReview({ proposalItems, submissions, submissionsList, onRefresh });
  const {
    filterStatus, setFilterStatus, expandedTasks, reviewingId, reviewNotes, setReviewNotes,
    proofContent, loadingProof, viewMode, setViewMode, sortBy, setSortBy,
    expandedSubmissions, setExpandedSubmissions, expandedDiffs, setExpandedDiffs, selectedSubmissions,
    rootRef, scrollTopRef, submissionsArray, allDeliverables, filteredDeliverables, selectableDeliverables,
    selectedCount, allSelected, comparisonGroups,
    toggleTaskExpansion, reviewDeliverable, toggleSelectSubmission, toggleSelectAll, bulkReview, fetchProofContent,
    exportComparison, getStatusIcon, getStatusColor, renderProofContent,
  } = d;

  if (allDeliverables.length === 0 && comparisonGroups.length === 0) {
    return (
      <div className="deliverables-empty">
        <div className="deliverables-empty-icon">📋</div>
        <div className="deliverables-empty-title">No Deliverables Found</div>
        <div className="deliverables-empty-subtitle">
          No task submissions have been made yet for these proposals.
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="deliverables-header">
        <div>
          <h4 className="deliverables-header-title">Deliverables Review</h4>
          <p className="deliverables-header-subtitle">
            Review and approve task submissions for this contract
          </p>
        </div>
        
        <div className="deliverables-controls">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setViewMode('list')}
              className={`deliverables-view-btn ${viewMode === 'list' ? 'active' : ''}`}
            >
              <List className="w-3 h-3" />
              List
            </button>
            <button
              type="button"
              onClick={() => setViewMode('compare')}
              className={`deliverables-view-btn ${viewMode === 'compare' ? 'active' : ''}`}
            >
              <Columns className="w-3 h-3" />
              Compare
            </button>
          </div>
          <Filter className="w-4 h-4 text-gray-500" />
          <select
            value={filterStatus}
            onChange={(e) => setFilterStatus(e.target.value)}
            className="deliverables-select"
          >
            <option value="all">All Status</option>
            <option value="submitted">Submitted</option>
            <option value="reviewed">Reviewed</option>
            <option value="approved">Approved</option>
            <option value="rejected">Rejected</option>
          </select>
          {viewMode === 'compare' && (
            <>
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value)}
                className="deliverables-select"
              >
                <option value="newest">Newest first</option>
                <option value="oldest">Oldest first</option>
                <option value="most_words">Most words</option>
                <option value="fewest_words">Fewest words</option>
              </select>
              <button
                type="button"
                onClick={exportComparison}
                className="deliverables-export-btn"
              >
                Export JSON
              </button>
            </>
          )}
        </div>
      </div>

      <div className="deliverables-count">
        {viewMode === 'list'
          ? `Showing ${filteredDeliverables.length} of ${allDeliverables.length} deliverables`
          : `Showing ${comparisonGroups.length} tasks with submissions`}
      </div>

      {viewMode === 'list' && (
        <div className="deliverables-bulk-bar">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={allSelected}
              onChange={(e) => toggleSelectAll(e.target.checked, selectableDeliverables)}
              className="deliverables-checkbox"
            />
            Select all
          </label>
          <div className="flex items-center gap-2 flex-wrap">
            <span>{selectedCount} selected</span>
            <button
              type="button"
              onClick={() => bulkReview('approve', filteredDeliverables)}
              disabled={selectedCount === 0 || reviewingId === 'bulk' || isContractLocked}
              className="deliverables-bulk-btn approve"
            >
              Bulk approve
            </button>
            <button
              type="button"
              onClick={() => bulkReview('reject', filteredDeliverables)}
              disabled={selectedCount === 0 || reviewingId === 'bulk' || isContractLocked}
              className="deliverables-bulk-btn reject"
            >
              Bulk reject
            </button>
          </div>
        </div>
      )}

      {viewMode === 'compare' ? (
        <div className="deliverables-compare-view" ref={rootRef}>
          {comparisonGroups.map((group) => {
            const filteredSubmissions = group.submissions.filter((submission) => {
              if (filterStatus === 'all') return true;
              return (submission.status || '').toLowerCase() === filterStatus;
            });
            if (filteredSubmissions.length === 0) return null;

            const sortedSubmissions = sortSubmissions(filteredSubmissions, sortBy);

            return (
              <div key={group.task_id} className="deliverables-compare-group">
                <div className="deliverables-compare-header">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="deliverables-task-label">Task {group.task_id}</div>
                      <h5 className="deliverables-task-title">{group.title}</h5>
                      <p className="deliverables-task-meta mt-1">{group.proposalTitle}</p>
                    </div>
                    <span className="deliverables-task-label">Proposal: {group.proposalId}</span>
                  </div>
                </div>
                <div className="deliverables-compare-scroll">
                  <div className="deliverables-compare-cards">
                    {sortedSubmissions.map((submission, index) => {
                      const submissionId = submission.submission_id || submission.id || `${group.task_id}-${index}`;
                      const notes = getSubmissionNotes(submission);
                      const wordCount = countWords(notes);
                      const preview = getNotesPreview(notes);
                      const isExpanded = !!expandedSubmissions[submissionId];
                      const status = submission.status || 'pending';
                      return (
                        <div key={submissionId} className="deliverables-compare-card">
                          <div className="deliverables-compare-card-header">
                            <div>
                              <div className="flex items-center gap-2">
                                {getStatusIcon(status)}
                                <span className={`text-xs px-2 py-0.5 rounded border ${getStatusColor(status)}`}>
                                  {status}
                                </span>
                                <span className="deliverables-compare-attempt">Attempt {index + 1}</span>
                              </div>
                              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                {submission.submitted_at ? new Date(submission.submitted_at).toLocaleString() : 'Unknown time'}
                              </div>
                            </div>
                            <div className="deliverables-compare-id">
                              {submission.submission_id || submission.id}
                            </div>
                          </div>

                          <div className="deliverables-compare-stats">
                            <div>Words: {wordCount}</div>
                            <div>Proof: {submission.completion_proof?.link ? 'Yes' : 'No'}</div>
                            <div className="deliverables-compare-stats-full">Submitted by: {submission?.deliverables?.submitted_by || 'Unknown'}</div>
                          </div>

                          {submission?.rejection_reason && (
                            <div className="deliverables-compare-rejection">
                              {submission.rejection_reason}
                            </div>
                          )}

                          <div className="mt-3">
                            <div className="deliverables-compare-notes-label">Notes</div>
                            <div className="deliverables-compare-notes-box">
                              {isExpanded ? renderMarkdown(notes || 'No notes provided.') : (
                                <div className="deliverables-notes-preview">
                                  {preview || 'No notes provided.'}
                                </div>
                              )}
                            </div>
                            {notes && notes.length > 0 && (
                              <button
                                type="button"
                                onClick={() => setExpandedSubmissions(prev => ({ ...prev, [submissionId]: !prev[submissionId] }))}
                                className="deliverables-compare-notes-expand"
                              >
                                {isExpanded ? 'Show preview' : 'Show full notes'}
                              </button>
                            )}
                          </div>

                          <div className="mt-3 space-y-2">
                            <textarea
                              className="deliverables-textarea"
                              placeholder="Review notes (optional)"
                              rows={2}
                              value={reviewNotes[submissionId] || ''}
                              onChange={(e) => setReviewNotes(prev => ({
                                ...prev,
                                [submissionId]: e.target.value
                              }))}
                            />
                            <div className="flex gap-2 flex-wrap">
                              <button
                                onClick={() => reviewDeliverable(submissionId, 'review')}
                                disabled={reviewingId === submissionId || isContractLocked}
                                className="deliverables-action-btn review"
                              >
                                <Eye className="w-3 h-3" />
                                {reviewingId === submissionId ? 'Processing…' : 'Review'}
                              </button>
                              <button
                                onClick={() => reviewDeliverable(submissionId, 'approve')}
                                disabled={reviewingId === submissionId || isContractLocked}
                                className="deliverables-action-btn approve"
                              >
                                <CheckCircle className="w-3 h-3" />
                                {reviewingId === submissionId ? 'Processing…' : 'Approve'}
                              </button>
                              <button
                                onClick={() => reviewDeliverable(submissionId, 'reject')}
                                disabled={reviewingId === submissionId || isContractLocked}
                                className="deliverables-action-btn reject"
                              >
                                <XCircle className="w-3 h-3" />
                                {reviewingId === submissionId ? 'Processing…' : 'Reject'}
                              </button>
                            </div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        <div className="space-y-3" ref={rootRef}>
          {filteredDeliverables.map((deliverable) => (
            <div key={deliverable.task_id} className="deliverables-card">
              <div className="deliverables-card-header">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-2">
                      <input
                        type="checkbox"
                        checked={!!selectedSubmissions[deliverable.submissionKey]}
                        onChange={() => {
                          if (!deliverable.submissionKey) return;
                          toggleSelectSubmission(deliverable.submissionKey);
                        }}
                        disabled={!deliverable.submissionKey}
                        className="deliverables-checkbox"
                      />
                      {getStatusIcon(deliverable.submission?.status)}
                      <span className={`text-xs px-2 py-0.5 rounded border ${getStatusColor(deliverable.submission?.status)}`}>
                        {deliverable.submission?.status || 'pending'}
                      </span>
                      <span className="deliverables-task-label">
                        Proposal: {deliverable.proposalId}
                      </span>
                    </div>
                    
                    <h5 className="deliverables-task-title mb-1">
                      {deliverable.title}
                    </h5>
                    <p className="deliverables-task-meta mb-2">
                      {deliverable.proposalTitle}
                    </p>
                    
                    <div className="deliverables-task-meta">
                      <span>Budget: {deliverable.budget_sats} sats</span>
                      <span>Submitted by: {deliverable.submission?.deliverables?.submitted_by || 'Unknown'}</span>
                      {deliverable.submission?.submitted_at && (
                        <span>Submitted: {new Date(deliverable.submission.submitted_at).toLocaleDateString()}</span>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    {(() => {
                      const status = (deliverable.submission?.status || '').toLowerCase();
                      const finalStatuses = ['approved', 'rejected', 'reviewed'];
                      const showQuickActions = !finalStatuses.includes(status);
                      if (!showQuickActions) return null;
                      return (
                        <div className="flex items-center gap-2">
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'approve')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="deliverables-action-btn approve"
                          >
                            Approve
                          </button>
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'reject')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="deliverables-action-btn reject"
                          >
                            Reject
                          </button>
                        </div>
                      );
                    })()}
                    <button
                      onClick={() => toggleTaskExpansion(deliverable.task_id)}
                      className="deliverables-expand-btn"
                    >
                      {expandedTasks[deliverable.task_id] ? (
                        <ChevronUp className="w-5 h-5" />
                      ) : (
                        <ChevronDown className="w-5 h-5" />
                      )}
                    </button>
                  </div>
                </div>

                {(() => {
                  const notes = getSubmissionNotes(deliverable.submission || {});
                  const preview = getNotesPreview(notes, 200);
                  const wordCount = countWords(notes);
                  const status = (deliverable.submission?.status || '').toLowerCase();
                  const statusText = status || 'pending';
                  return (
                    <div className="deliverables-notes-box">
                      <div className="flex items-center justify-between text-xs text-gray-600 dark:text-gray-400 mb-2">
                        <span>Words: {wordCount}</span>
                        <span>Status: {statusText}</span>
                      </div>
                      <div className="deliverables-notes-preview">
                        {preview || 'No notes provided.'}
                      </div>
                    </div>
                  );
                })()}

                {expandedTasks[deliverable.task_id] && (
                    <div className="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700 space-y-4">
                  <div>
                    <h6 className="deliverables-section-title mb-2">Deliverable Details</h6>
                    <div className="deliverables-notes-box">
                      {deliverable.submission?.submitted_at && (
                        <div className="flex items-start justify-between gap-2">
                          <span className="text-sm text-gray-600 dark:text-gray-400">Submitted At:</span>
                          <span className="text-sm">
                            {new Date(deliverable.submission.submitted_at).toLocaleString()}
                          </span>
                        </div>
                      )}
                      <div className="flex items-start justify-between gap-2">
                        <span className="text-sm text-gray-600 dark:text-gray-400">Task ID:</span>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-mono">{deliverable.task_id}</span>
                          <CopyButton text={deliverable.task_id} />
                        </div>
                      </div>
                      {(deliverable.submission?.claim_id || deliverable.active_claim_id) && (
                        <div className="flex items-start justify-between gap-2">
                          <span className="text-sm text-gray-600 dark:text-gray-400">Claim ID:</span>
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-mono">
                              {deliverable.submission?.claim_id || deliverable.active_claim_id}
                            </span>
                            <CopyButton text={deliverable.submission?.claim_id || deliverable.active_claim_id} />
                          </div>
                        </div>
                      )}
                      <div className="flex items-start justify-between gap-2">
                        <span className="text-sm text-gray-600 dark:text-gray-400">Submission ID:</span>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-mono">
                            {deliverable.submission?.submission_id || deliverable.submission?.id || deliverable.active_claim_id}
                          </span>
                          <CopyButton text={deliverable.submission?.submission_id || deliverable.submission?.id || deliverable.active_claim_id} />
                        </div>
                      </div>
                      {deliverable.skills_required && deliverable.skills_required.length > 0 && (
                        <div className="flex items-start justify-between gap-2">
                          <span className="text-sm text-gray-600 dark:text-gray-400">Required Skills:</span>
                          <span className="text-sm">
                            {deliverable.skills_required.join(', ')}
                          </span>
                        </div>
                      )}
                    </div>
                  </div>

                  {deliverable.submission?.deliverables?.notes && (
                    <div>
                      <h6 className="deliverables-section-title mb-2">Submission Notes</h6>
                      <div className="deliverables-notes-box" style={{ maxHeight: '60vh', overflowY: 'auto' }}>
                        {renderMarkdown(deliverable.submission.deliverables.notes)}
                      </div>
                    </div>
                  )}

                  {(deliverable.submission?.rejection_reason || deliverable.submission?.rejection_type) && (
                    <div>
                      <h6 className="deliverables-section-title mb-2">Rejection Feedback</h6>
                      <div className="deliverables-compare-rejection">
                        {deliverable.submission?.rejection_type && (
                          <div className="text-xs font-semibold uppercase tracking-wide">
                            {deliverable.submission.rejection_type}
                          </div>
                        )}
                        {deliverable.submission?.rejection_reason && (
                          <div className="text-sm whitespace-pre-wrap">
                            {deliverable.submission.rejection_reason}
                          </div>
                        )}
                        {deliverable.submission?.rejected_at && (
                          <div className="text-xs">
                            Rejected at {new Date(deliverable.submission.rejected_at).toLocaleString()}
                          </div>
                        )}
                      </div>
                    </div>
                  )}

                  {deliverable.submission?.deliverables?.document && (
                    <div>
                      <h6 className="deliverables-section-title mb-2">Submission Document</h6>
                      <div className="deliverables-notes-box" style={{ maxHeight: '60vh', overflowY: 'auto' }}>
                        {renderMarkdown(deliverable.submission.deliverables.document)}
                      </div>
                    </div>
                  )}

                  {(() => {
                    const timeline = [...(submissionsByTask[deliverable.task_id] || [])]
                      .sort((a, b) => getSubmissionTimestamp(a) - getSubmissionTimestamp(b));
                    if (timeline.length === 0) return null;
                    return (
                      <div>
                        <h6 className="deliverables-section-title mb-2">Submission Timeline</h6>
                        <div className="space-y-3">
                          {timeline.map((submission, index) => {
                            const submissionId = getSubmissionId(submission, `${deliverable.task_id}-${index}`);
                            const status = submission.status || 'pending';
                            const notes = getSubmissionNotes(submission);
                            const words = countWords(notes);
                            const prevNotes = index > 0 ? getSubmissionNotes(timeline[index - 1]) : '';
                            const prevWords = countWords(prevNotes);
                            const diffKey = `${deliverable.task_id}-${submissionId}`;
                            const showDiff = !!expandedDiffs[diffKey];
                            return (
                              <div key={submissionId} className="deliverables-compare-card">
                                <div className="flex items-start justify-between gap-2">
                                  <div>
                                    <div className="flex items-center gap-2">
                                      {getStatusIcon(status)}
                                      <span className={`text-xs px-2 py-0.5 rounded border ${getStatusColor(status)}`}>
                                        {status}
                                      </span>
                                      <span className="deliverables-compare-attempt">Attempt {index + 1}</span>
                                    </div>
                                    <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                      {formatTimestamp(submission.submitted_at || submission.created_at)}
                                    </div>
                                  </div>
                                  <div className="deliverables-compare-id">
                                    {submissionId}
                                  </div>
                                </div>

                                <div className="deliverables-compare-stats">
                                  <div>Words: {words}</div>
                                  <div>Claim: {submission.claim_id || '—'}</div>
                                </div>

                                {submission.rejection_reason && (
                                  <div className="deliverables-compare-rejection">
                                    {submission.rejection_reason}
                                  </div>
                                )}

                                {index > 0 && (
                                  <button
                                    type="button"
                                    onClick={() => setExpandedDiffs(prev => ({ ...prev, [diffKey]: !prev[diffKey] }))}
                                    className="deliverables-compare-notes-expand"
                                  >
                                    {showDiff ? 'Hide comparison' : 'Compare with previous'}
                                  </button>
                                )}

                                {showDiff && (
                                  <div className="mt-2 grid md:grid-cols-2 gap-3">
                                    <div>
                                      <div className="deliverables-compare-attempt mb-1">
                                        Previous ({prevWords} words)
                                      </div>
                                      <div className="deliverables-compare-notes-box" style={{ maxHeight: '12rem' }}>
                                        {renderMarkdown(prevNotes || 'No notes provided.')}
                                      </div>
                                    </div>
                                    <div>
                                      <div className="deliverables-compare-attempt mb-1">
                                        Current ({words} words)
                                      </div>
                                      <div className="deliverables-compare-notes-box" style={{ maxHeight: '12rem' }}>
                                        {renderMarkdown(notes || 'No notes provided.')}
                                      </div>
                                    </div>
                                  </div>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      </div>
                    );
                  })()}

                  {/* Debug section - can be removed in production */}
                  {import.meta.env.DEV && (
                    <div>
                      <h6 className="deliverables-section-title mb-2">Debug: Submission Data</h6>
                      <div className="bg-gray-100 dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg p-3 space-y-2">
                        <div className="text-xs">
                          <strong>Current Status:</strong> {deliverable.submission?.status || 'undefined'}
                        </div>
                        <div className="text-xs">
                          <strong>Show Review Actions:</strong> {(() => {
                            const status = (deliverable.submission?.status || '').toLowerCase();
                            const finalStatuses = ['approved', 'rejected', 'reviewed'];
                            const showReviewActions = !finalStatuses.includes(status);
                            return showReviewActions ? 'YES' : 'NO';
                          })()}
                        </div>
                        <pre className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap">
                          {JSON.stringify(deliverable.submission, null, 2)}
                        </pre>
                      </div>
                    </div>
                  )}

                  {deliverable.submission?.completion_proof && (
                    <div>
                      <h6 className="deliverables-section-title mb-2">Completion Proof</h6>
                      <div className="deliverables-notes-box" style={{ background: 'rgba(34, 197, 94, 0.1)', borderColor: 'rgba(34, 197, 94, 0.2)' }}>
                        {deliverable.submission.completion_proof.link && (
                          <div className="flex items-center justify-between gap-2">
                            <div className="flex items-center gap-2">
                              <ExternalLink className="w-4 h-4" style={{ color: '#22c55e' }} />
                              <a
                                href={deliverable.submission.completion_proof.link}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-sm hover:underline break-all flex-1"
                                style={{ color: '#14532d' }}
                              >
                                {deliverable.submission.completion_proof.link}
                              </a>
                            </div>
                          </div>
                        )}
                        
                        {/* Show other completion proof data */}
                        <div className="space-y-2 text-sm">
                          {deliverable.submission.completion_proof.methodology && (
                            <div>
                              <span className="font-semibold">Methodology:</span>
                              <p className="text-gray-700 dark:text-gray-300 mt-1">
                                {deliverable.submission.completion_proof.methodology}
                              </p>
                            </div>
                          )}
                          
                          {deliverable.submission.completion_proof.verification_status && (
                            <div>
                              <span className="font-semibold">Verification Status:</span>
                              <span className="ml-2 text-gray-700 dark:text-gray-300">
                                {deliverable.submission.completion_proof.verification_status}
                              </span>
                            </div>
                          )}
                          
                          {deliverable.submission.completion_proof.reference_documents && (
                            <div>
                              <span className="font-semibold">Reference Documents:</span>
                              <ul className="mt-1 space-y-1">
                                {deliverable.submission.completion_proof.reference_documents.map((doc, index) => (
                                  <li key={index} className="text-gray-700 dark:text-gray-300 flex items-center gap-2">
                                    <ExternalLink className="w-3 h-3" />
                                    <a
                                      href={doc}
                                      target="_blank"
                                      rel="noopener noreferrer"
                                      className="hover:underline break-all"
                                    >
                                      {doc}
                                    </a>
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}
                          
                          {deliverable.submission.completion_proof.data && (
                            <div>
                              <span className="font-semibold">Proof Data:</span>
                              <div className="mt-1 bg-gray-100 dark:bg-gray-800 rounded p-2">
                                <pre className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap">
                                  {typeof deliverable.submission.completion_proof.data === 'string' 
                                    ? deliverable.submission.completion_proof.data
                                    : JSON.stringify(deliverable.submission.completion_proof.data, null, 2)
                                  }
                                </pre>
                              </div>
                            </div>
                          )}
                          
                          {!deliverable.submission.completion_proof.link && 
                           !deliverable.submission.completion_proof.methodology &&
                           !deliverable.submission.completion_proof.verification_status &&
                           !deliverable.submission.completion_proof.reference_documents &&
                           !deliverable.submission.completion_proof.data && (
                            <div className="text-gray-500 dark:text-gray-400 text-sm">
                              No completion proof details available
                            </div>
                          )}
                        </div>
                        
                        {deliverable.submission.completion_proof.link && (
                        <div className="border-t border-green-200 dark:border-green-700 pt-3 mt-3">
                          {renderProofContent(deliverable.submission.completion_proof.link, deliverable.submissionKey)}
                        </div>
                        )}
                      </div>
                    </div>
                  )}

                  {(() => {
                    const status = (deliverable.submission?.status || '').toLowerCase();
                    // Show review actions for any status that hasn't been finally decided
                    const finalStatuses = ['approved', 'rejected', 'reviewed'];
                    const showReviewActions = !finalStatuses.includes(status);
                    return showReviewActions;
                  })() && (
                    <div>
                      <h6 className="deliverables-section-title mb-2">Review Actions</h6>
                      <div className="space-y-3">
                        <textarea
                          className="deliverables-textarea"
                          placeholder="Review notes (optional)"
                          rows={3}
                          value={reviewNotes[deliverable.submissionKey] || ''}
                          onChange={(e) => setReviewNotes(prev => ({
                            ...prev,
                            [deliverable.submissionKey]: e.target.value
                          }))}
                        />
                        <div className="flex gap-2 flex-wrap">
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'review')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="deliverables-action-btn review"
                          >
                            <Eye className="w-4 h-4" />
                            {reviewingId === deliverable.submissionKey ? 'Processing…' : 'Mark as Reviewed'}
                          </button>
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'approve')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="deliverables-action-btn approve"
                          >
                            <CheckCircle className="w-4 h-4" />
                            {reviewingId === deliverable.submissionKey ? 'Processing…' : 'Approve'}
                          </button>
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'reject')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="deliverables-action-btn reject"
                          >
                            <XCircle className="w-4 h-4" />
                            {reviewingId === deliverable.submissionKey ? 'Processing…' : 'Reject'}
                          </button>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
      )}

    </div>
  );
};

export default DeliverablesReview;
