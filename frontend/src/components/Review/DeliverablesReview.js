import React, { useState } from 'react';
import { CheckCircle, XCircle, Clock, ExternalLink, Filter, ChevronDown, ChevronUp, Eye, FileText, Code } from 'lucide-react';
import toast from 'react-hot-toast';
import { API_BASE } from '../../apiBase';
import CopyButton from '../Common/CopyButton';
import { useAuth } from '../../context/AuthContext';

const DeliverablesReview = ({ proposalItems, submissions, onRefresh, isContractLocked = false }) => {
  const { auth } = useAuth();
  // Add key to force re-render when submissions change
  const submissionsKey = JSON.stringify(submissions);
  
  // Reset state when submissions prop changes
  React.useEffect(() => {
    setReviewNotes({});
    setExpandedTasks({});
    setProofContent({});
    setLoadingProof({});
    setReviewingId('');
  }, [submissionsKey]);
  const [filterStatus, setFilterStatus] = useState('all');
  const [expandedTasks, setExpandedTasks] = useState({});
  const [reviewingId, setReviewingId] = useState('');
  const [reviewNotes, setReviewNotes] = useState({});
  const [proofContent, setProofContent] = useState({});
  const [loadingProof, setLoadingProof] = useState({});


  // Handle both array and object formats for submissions
  const submissionsObj = (submissions && typeof submissions === 'object') ? submissions : {};
  let submissionsArray;
  try {
    submissionsArray = Array.isArray(submissions) ? submissions : (submissionsObj ? Object.keys(submissionsObj).map(key => submissionsObj[key]) : []);
  } catch (e) {
    console.error('Error processing submissions:', e, submissions);
    submissionsArray = [];
  }
  const submissionsMap = {};
  
  // Create submission map by submission_id for API calls
  // Create submission map by submission_id for API calls
  submissionsArray.forEach(submission => {
    if (submission.submission_id) {
      submissionsMap[submission.submission_id] = submission;
    }
    // Also map by task_id and claim_id for lookup
    if (submission.task_id) {
      submissionsMap[submission.task_id] = submission;
    }
    if (submission.claim_id) {
      submissionsMap[submission.claim_id] = submission;
    }
  });

  const allDeliverables = proposalItems.flatMap(proposal => {
    const tasks = Array.isArray(proposal.tasks) && proposal.tasks.length > 0
      ? proposal.tasks
      : (Array.isArray(proposal.metadata?.suggested_tasks) ? proposal.metadata.suggested_tasks : []);
    
    return tasks.map(task => {
      // Find submission by task_id or claim_id
      const submission = submissionsObj[task.task_id] || submissionsObj[task.active_claim_id] || null;

      const result = {
        ...task,
        proposal,
        submission,
        submissionKey: submission?.submission_id, // Use actual submission_id for API calls
        proposalId: proposal.id,
        proposalTitle: proposal.title
      };

      return result;
    });
  }).filter(item => item.submission);

  const filteredDeliverables = allDeliverables.filter(deliverable => {
    if (filterStatus === 'all') return true;
    return (deliverable.submission?.status || '').toLowerCase() === filterStatus;
  });

  const toggleTaskExpansion = (taskId) => {
    setExpandedTasks(prev => ({
      ...prev,
      [taskId]: !prev[taskId]
    }));
  };

  const reviewDeliverable = async (submissionId, action) => {
    setReviewingId(submissionId);
    try {
      const reviewUrl = `${API_BASE}/api/smart_contract/submissions/${submissionId}/review`;
      const res = await fetch(reviewUrl, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(auth.apiKey ? { 'X-API-Key': auth.apiKey } : {}),
        },
        body: JSON.stringify({
          action,
          notes: reviewNotes[submissionId] || ''
        }),
      });
      
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || `HTTP ${res.status}`);
      }
      
      toast.success(`Deliverable ${action}d successfully`);
      setReviewNotes(prev => ({ ...prev, [submissionId]: '' }));
      if (onRefresh) onRefresh();
    } catch (err) {
      console.error('Review failed', err);
      toast.error(`Review failed: ${err.message}`);
    } finally {
      setReviewingId('');
    }
  };

  const fetchProofContent = async (proofUrl, submissionId) => {
    if (!proofUrl || proofContent[submissionId]) return;
    
    setLoadingProof(prev => ({ ...prev, [submissionId]: true }));
    try {
      const response = await fetch(proofUrl);
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      
      const contentType = response.headers.get('content-type') || '';
      let content = '';
      
      if (contentType.includes('application/json')) {
        const data = await response.json();
        content = JSON.stringify(data, null, 2);
      } else if (contentType.includes('text/') || contentType.includes('application/json')) {
        content = await response.text();
      } else if (contentType.includes('image/')) {
        content = `data:${contentType};base64,${await response.text()}`;
      } else {
        content = await response.text();
      }
      
      setProofContent(prev => ({ ...prev, [submissionId]: { content, contentType } }));
    } catch (err) {
      console.error('Failed to fetch proof content:', err);
      setProofContent(prev => ({ 
        ...prev, 
        [submissionId]: { 
          content: `Error loading proof: ${err.message}`, 
          contentType: 'error',
          error: true 
        } 
      }));
    } finally {
      setLoadingProof(prev => ({ ...prev, [submissionId]: false }));
    }
  };

  const getStatusIcon = (status) => {
    const normalized = (status || '').toLowerCase();
    switch (normalized) {
      case 'approved':
        return <CheckCircle className="w-4 h-4 text-green-500" />;
      case 'rejected':
        return <XCircle className="w-4 h-4 text-red-500" />;
      case 'reviewed':
        return <Eye className="w-4 h-4 text-purple-500" />;
      case 'submitted':
        return <Clock className="w-4 h-4 text-blue-500" />;
      default:
        return <Clock className="w-4 h-4 text-gray-400" />;
    }
  };

  const getStatusColor = (status) => {
    const normalized = (status || '').toLowerCase();
    switch (normalized) {
      case 'approved':
        return 'bg-green-100 dark:bg-green-900/40 border-green-400 text-green-700 dark:text-green-200';
      case 'rejected':
        return 'bg-red-100 dark:bg-red-900/40 border-red-400 text-red-700 dark:text-red-200';
      case 'reviewed':
        return 'bg-purple-100 dark:bg-purple-900/40 border-purple-400 text-purple-700 dark:text-purple-200';
      case 'submitted':
        return 'bg-blue-100 dark:bg-blue-900/40 border-blue-400 text-blue-700 dark:text-blue-200';
      default:
        return 'bg-gray-100 dark:bg-gray-900/40 border-gray-400 text-gray-700 dark:text-gray-200';
    }
  };

  const renderProofContent = (proofUrl, submissionId) => {
    const proof = proofContent[submissionId];
    const loading = loadingProof[submissionId];
    
    if (!proof && !loading) {
      return (
        <button
          onClick={() => fetchProofContent(proofUrl, submissionId)}
          className="text-sm text-blue-600 dark:text-blue-400 hover:underline flex items-center gap-1"
        >
          <Eye className="w-4 h-4" />
          Load Proof Content
        </button>
      );
    }
    
    if (loading) {
      return <div className="text-sm text-gray-500">Loading proof content...</div>;
    }
    
    if (proof?.error) {
      return (
        <div className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 p-2 rounded">
          {proof.content}
        </div>
      );
    }
    
    const { content, contentType } = proof;
    
    if (contentType?.includes('image/')) {
      return (
        <div className="space-y-2">
          <img 
            src={content} 
            alt="Completion proof" 
            className="max-w-full h-auto rounded border border-gray-300 dark:border-gray-600"
            style={{ maxHeight: '300px' }}
          />
          <div className="text-xs text-gray-500">Image proof</div>
        </div>
      );
    }
    
    if (contentType?.includes('application/json') || contentType?.includes('text/')) {
      return (
        <div className="space-y-2">
          <div className="flex items-center gap-2 text-xs text-gray-500">
            {contentType?.includes('json') ? <Code className="w-3 h-3" /> : <FileText className="w-3 h-3" />}
            {contentType || 'Text content'}
          </div>
          <div className="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded p-3 max-h-64 overflow-y-auto">
            <pre className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap font-mono">
              {content}
            </pre>
          </div>
        </div>
      );
    }
    
    return (
      <div className="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded p-3">
        <pre className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap break-words">
          {content}
        </pre>
      </div>
    );
  };

  if (allDeliverables.length === 0) {
    return (
      <div className="text-center py-8">
        <div className="text-4xl mb-4">📋</div>
        <div className="text-gray-600 dark:text-gray-400 font-semibold">No Deliverables Found</div>
        <div className="text-gray-500 dark:text-gray-500 text-sm mt-2">
          No task submissions have been made yet for these proposals.
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-4 flex-wrap">
        <div>
          <h4 className="text-lg font-semibold text-black dark:text-white">Deliverables Review</h4>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Review and approve task submissions for this contract
          </p>
        </div>
        
        <div className="flex items-center gap-2">

          <Filter className="w-4 h-4 text-gray-500" />
          <select
            value={filterStatus}
            onChange={(e) => setFilterStatus(e.target.value)}
            className="text-sm rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-2 py-1"
          >
            <option value="all">All Status</option>
            <option value="submitted">Submitted</option>
            <option value="reviewed">Reviewed</option>
            <option value="approved">Approved</option>
            <option value="rejected">Rejected</option>
          </select>
        </div>
      </div>

      <div className="text-sm text-gray-600 dark:text-gray-400">
        Showing {filteredDeliverables.length} of {allDeliverables.length} deliverables
      </div>

      <div className="space-y-3">
        {filteredDeliverables.map((deliverable) => (
          <div key={deliverable.task_id} className="border border-gray-200 dark:border-gray-700 rounded-lg bg-white dark:bg-gray-900 overflow-hidden">
            <div className="p-4">
              <div className="flex items-start justify-between gap-4">
                <div className="flex-1">
                  <div className="flex items-center gap-2 mb-2">
                    {getStatusIcon(deliverable.submission?.status)}
                    <span className={`text-xs px-2 py-0.5 rounded border ${getStatusColor(deliverable.submission?.status)}`}>
                      {deliverable.submission?.status || 'pending'}
                    </span>
                    <span className="text-xs text-gray-500 dark:text-gray-400">
                      Proposal: {deliverable.proposalId}
                    </span>
                  </div>
                  
                  <h5 className="font-semibold text-black dark:text-white mb-1">
                    {deliverable.title}
                  </h5>
                  <p className="text-sm text-gray-600 dark:text-gray-400 mb-2">
                    {deliverable.proposalTitle}
                  </p>
                  
                  <div className="flex items-center gap-4 text-xs text-gray-500 dark:text-gray-400">
                    <span>Budget: {deliverable.budget_sats} sats</span>
                    <span>Submitted by: {deliverable.submission?.deliverables?.submitted_by || 'Unknown'}</span>
                    {deliverable.submission?.submitted_at && (
                      <span>Submitted: {new Date(deliverable.submission.submitted_at).toLocaleDateString()}</span>
                    )}
                  </div>
                </div>

                <button
                  onClick={() => toggleTaskExpansion(deliverable.task_id)}
                  className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                >
                  {expandedTasks[deliverable.task_id] ? (
                    <ChevronUp className="w-5 h-5" />
                  ) : (
                    <ChevronDown className="w-5 h-5" />
                  )}
                </button>
              </div>

              {expandedTasks[deliverable.task_id] && (
                <div className="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700 space-y-4">
                  <div>
                    <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Deliverable Details</h6>
                    <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3 space-y-2">
                      <div className="flex items-start justify-between gap-2">
                        <span className="text-sm text-gray-600 dark:text-gray-400">Task ID:</span>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-mono text-black dark:text-white">{deliverable.task_id}</span>
                          <CopyButton text={deliverable.task_id} />
                        </div>
                      </div>
                      <div className="flex items-start justify-between gap-2">
                        <span className="text-sm text-gray-600 dark:text-gray-400">Submission ID:</span>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-mono text-black dark:text-white">{deliverable.submission?.id || deliverable.active_claim_id}</span>
                          <CopyButton text={deliverable.submission?.id || deliverable.active_claim_id} />
                        </div>
                      </div>
                      {deliverable.skills_required && deliverable.skills_required.length > 0 && (
                        <div className="flex items-start justify-between gap-2">
                          <span className="text-sm text-gray-600 dark:text-gray-400">Required Skills:</span>
                          <span className="text-sm text-black dark:text-white">
                            {deliverable.skills_required.join(', ')}
                          </span>
                        </div>
                      )}
                    </div>
                  </div>

                  {deliverable.submission?.deliverables?.notes && (
                    <div>
                      <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Submission Notes</h6>
                      <div className="bg-blue-50 dark:bg-blue-900/40 border border-blue-200 dark:border-blue-700 rounded-lg p-3">
                        <p className="text-sm text-blue-900 dark:text-blue-100 whitespace-pre-wrap">
                          {deliverable.submission.deliverables.notes}
                        </p>
                      </div>
                    </div>
                  )}

                  {/* Debug section - can be removed in production */}
                  {process.env.NODE_ENV === 'development' && (
                    <div>
                      <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Debug: Submission Data</h6>
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
                      <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Completion Proof</h6>
                      <div className="bg-green-50 dark:bg-green-900/40 border border-green-200 dark:border-green-700 rounded-lg p-3 space-y-3">
                        {deliverable.submission.completion_proof.link && (
                          <div className="flex items-center justify-between gap-2">
                            <div className="flex items-center gap-2">
                              <ExternalLink className="w-4 h-4 text-green-600 dark:text-green-400" />
                              <a
                                href={deliverable.submission.completion_proof.link}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-sm text-green-900 dark:text-green-100 hover:underline break-all flex-1"
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
                        <div className="border-t border-green-200 dark:border-green-700 pt-3">
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
                      <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Review Actions</h6>
                      <div className="space-y-3">
                        <textarea
                          className="w-full rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-sm px-3 py-2"
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
                            className="px-3 py-2 bg-purple-600 hover:bg-purple-500 text-white rounded text-sm disabled:opacity-60 flex items-center gap-2"
                          >
                            <Eye className="w-4 h-4" />
                            {reviewingId === deliverable.submissionKey ? 'Processing…' : 'Mark as Reviewed'}
                          </button>
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'approve')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="px-3 py-2 bg-green-600 hover:bg-green-500 text-white rounded text-sm disabled:opacity-60 flex items-center gap-2"
                          >
                            <CheckCircle className="w-4 h-4" />
                            {reviewingId === deliverable.submissionKey ? 'Processing…' : 'Approve'}
                          </button>
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'reject')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="px-3 py-2 bg-red-600 hover:bg-red-500 text-white rounded text-sm disabled:opacity-60 flex items-center gap-2"
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


    </div>
  );
};

export default DeliverablesReview;
