import React, { useLayoutEffect, useRef, useState } from 'react';
import { CheckCircle, XCircle, Clock, Eye, FileText, Code } from 'lucide-react';
import toast from 'react-hot-toast';
import { apiFetch } from '../../utils/api';
import { useAuth } from '../../context/AuthContext';
import {
  filterByKeys,
  normalizeSubmissions,
  buildAllDeliverables,
  buildComparisonGroups,
  groupSubmissionsByTask,
} from './deliverablesUtils';

/** State and review actions for DeliverablesReview (rules out of JSX). */
export function useDeliverablesReview({ proposalItems, submissions, submissionsList, onRefresh }) {
  const { auth } = useAuth();
  const [filterStatus, setFilterStatus] = useState('all');
  const [expandedTasks, setExpandedTasks] = useState({});
  const [reviewingId, setReviewingId] = useState('');
  const [reviewNotes, setReviewNotes] = useState({});
  const [proofContent, setProofContent] = useState({});
  const [loadingProof, setLoadingProof] = useState({});
  const [viewMode, setViewMode] = useState('list');
  const [sortBy, setSortBy] = useState('newest');
  const [expandedSubmissions, setExpandedSubmissions] = useState({});
  const [expandedDiffs, setExpandedDiffs] = useState({});
  const [selectedSubmissions, setSelectedSubmissions] = useState({});
  const rootRef = useRef(null);
  const scrollTopRef = useRef(0);


  const submissionsKey = JSON.stringify(submissionsList || submissions);
  const { submissionsObj, submissionsArray, submissionsMap } = React.useMemo(
    () => normalizeSubmissions(submissions, submissionsList),
    [submissions, submissionsList],
  );
  const allDeliverables = React.useMemo(
    () => buildAllDeliverables(proposalItems, submissionsObj),
    [proposalItems, submissionsObj],
  );

  const submissionIds = React.useMemo(
    () => submissionsArray.map((submission) => submission?.submission_id).filter(Boolean),
    [submissionsArray],
  );
  const submissionIdsKey = React.useMemo(() => submissionIds.join('|'), [submissionIds]);
  const deliverablesKey = React.useMemo(
    () =>
      allDeliverables
        .map((deliverable) => deliverable.task_id)
        .filter(Boolean)
        .sort()
        .join('|'),
    [allDeliverables],
  );


  React.useEffect(() => {
    const allowedSubmissionIds = new Set(submissionIds);
    const allowedTaskIds = new Set(allDeliverables.map((deliverable) => deliverable.task_id).filter(Boolean));

    setReviewNotes((prev) => filterByKeys(prev, allowedSubmissionIds));
    setExpandedSubmissions((prev) => filterByKeys(prev, allowedSubmissionIds));
    setExpandedDiffs((prev) => filterByKeys(prev, allowedSubmissionIds));
    setSelectedSubmissions((prev) => filterByKeys(prev, allowedSubmissionIds));
    setProofContent((prev) => filterByKeys(prev, allowedSubmissionIds));
    setLoadingProof((prev) => filterByKeys(prev, allowedSubmissionIds));
    setExpandedTasks((prev) => filterByKeys(prev, allowedTaskIds));
  }, [submissionIdsKey, deliverablesKey, submissionIds, allDeliverables]);

  const filteredDeliverables = allDeliverables.filter(deliverable => {
    if (filterStatus === 'all') return true;
    return (deliverable.submission?.status || '').toLowerCase() === filterStatus;
  });
  const selectableDeliverables = filteredDeliverables.filter((deliverable) => deliverable.submissionKey);
  const selectedCount = Object.values(selectedSubmissions).filter(Boolean).length;
  const allSelected = selectableDeliverables.length > 0
    && selectableDeliverables.every((deliverable) => selectedSubmissions[deliverable.submissionKey]);

  const submissionsByTask = React.useMemo(
    () => groupSubmissionsByTask(submissionsArray),
    [submissionsArray],
  );
  const comparisonGroups = React.useMemo(
    () => buildComparisonGroups(proposalItems, submissionsByTask),
    [proposalItems, submissionsByTask],
  );

  const toggleTaskExpansion = (taskId) => {
    setExpandedTasks(prev => ({
      ...prev,
      [taskId]: !prev[taskId]
    }));
  };

  const performReviewRequest = async (submissionId, action) => {
    const res = await apiFetch(`/api/smart_contract/submissions/${submissionId}/review`, {
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
  };

  const reviewDeliverable = async (submissionId, action) => {
    setReviewingId(submissionId);
    try {
      await performReviewRequest(submissionId, action);
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

  const toggleSelectSubmission = (submissionId) => {
    setSelectedSubmissions((prev) => ({
      ...prev,
      [submissionId]: !prev[submissionId],
    }));
  };

  const toggleSelectAll = (checked, deliverables) => {
    if (!checked) {
      setSelectedSubmissions({});
      return;
    }
    const next = {};
    deliverables.forEach((deliverable) => {
      if (deliverable.submissionKey) {
        next[deliverable.submissionKey] = true;
      }
    });
    setSelectedSubmissions(next);
  };

  const bulkReview = async (action, deliverables) => {
    const targetIds = deliverables
      .map((deliverable) => deliverable.submissionKey)
      .filter((id) => id && selectedSubmissions[id]);

    if (targetIds.length === 0) {
      toast.error('Select at least one submission to review.');
      return;
    }

    setReviewingId('bulk');
    try {
      for (const submissionId of targetIds) {
        await performReviewRequest(submissionId, action);
      }
      setReviewNotes((prev) => {
        const next = { ...prev };
        targetIds.forEach((id) => {
          delete next[id];
        });
        return next;
      });
      setSelectedSubmissions({});
      if (onRefresh) onRefresh();
      toast.success(`Bulk ${action} completed`);
    } catch (err) {
      toast.error(`Bulk ${action} failed: ${err.message}`);
    } finally {
      setReviewingId('');
    }
  };

  const fetchProofContent = async (proofUrl, submissionId) => {
    if (!proofUrl || proofContent[submissionId]) return;
    
    setLoadingProof(prev => ({ ...prev, [submissionId]: true }));
    try {
      const response = await apiFetch(proofUrl);
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
        return <CheckCircle className="w-4 h-4" style={{ color: '#10b981' }} />;
      case 'rejected':
        return <XCircle className="w-4 h-4" style={{ color: '#ef4444' }} />;
      case 'reviewed':
        return <Eye className="w-4 h-4" style={{ color: '#a855f7' }} />;
      case 'submitted':
        return <Clock className="w-4 h-4" style={{ color: '#3b82f6' }} />;
      default:
        return <Clock className="w-4 h-4" style={{ color: '#9ca3af' }} />;
    }
  };

  const getStatusColor = (status) => {
    const normalized = (status || '').toLowerCase();
    switch (normalized) {
      case 'approved':
        return 'deliverables-status-badge approved';
      case 'rejected':
        return 'deliverables-status-badge rejected';
      case 'reviewed':
        return 'deliverables-status-badge reviewed';
      case 'submitted':
        return 'deliverables-status-badge submitted';
      default:
        return 'deliverables-status-badge pending';
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

  const renderMarkdown = (content) => {
    const safeContent = content || '';
    const hasMarkdown = /(^|\n)(#{1,6}\s|[-*]\s|\d+\.\s|```)/.test(safeContent);

    if (!hasMarkdown) {
      return (
        <div className="text-sm text-gray-800 dark:text-gray-100 whitespace-pre-wrap break-words">
          {safeContent}
        </div>
      );
    }

    return (
      <div className="text-sm text-gray-800 dark:text-gray-100 space-y-3 min-w-0 overflow-x-auto max-w-full">
        <ReactMarkdown
          components={{
            h1: ({ ...props }) => <h1 className="text-xl font-semibold" {...props} />,
            h2: ({ ...props }) => <h2 className="text-lg font-semibold" {...props} />,
            h3: ({ ...props }) => <h3 className="text-base font-semibold" {...props} />,
            p: ({ ...props }) => <p className="leading-relaxed break-words" {...props} />,
            ul: ({ ...props }) => <ul className="list-disc pl-5 space-y-1 break-words" {...props} />,
            ol: ({ ...props }) => <ol className="list-decimal pl-5 space-y-1 break-words" {...props} />,
            code: ({ inline, ...props }) => (
              inline
                ? <code className="px-1 py-0.5 rounded bg-gray-200/70 dark:bg-gray-700/70 font-mono text-xs break-words" {...props} />
                : <code className="block font-mono text-xs whitespace-pre" {...props} />
            ),
            pre: ({ ...props }) => (
              <pre className="bg-gray-900 text-gray-100 rounded p-3 overflow-x-auto text-xs max-w-full" {...props} />
            ),
            a: ({ ...props }) => <a className="text-blue-600 dark:text-blue-300 underline break-all" {...props} />,
          }}
        >
          {safeContent}
        </ReactMarkdown>
      </div>
    );
  };

  useLayoutEffect(() => {
    const node = rootRef.current;
    if (!node) return undefined;
    const scrollEl = node.closest('[data-deliverables-scroll]');
    if (scrollEl) {
      scrollEl.scrollTop = scrollTopRef.current;
    }
    return () => {
      if (!node) return;
      const currentScrollEl = node.closest('[data-deliverables-scroll]');
      if (currentScrollEl) {
        scrollTopRef.current = currentScrollEl.scrollTop;
      }
    };
  }, [submissionsKey]);





  const exportComparison = () => {
    const payload = comparisonGroups
      .map((group) => {
        const filteredSubmissions = group.submissions.filter((submission) => {
          if (filterStatus === 'all') return true;
          return (submission.status || '').toLowerCase() === filterStatus;
        });
        if (filteredSubmissions.length === 0) return null;
        const sorted = sortSubmissions(filteredSubmissions, sortBy);
        return {
          task_id: group.task_id,
          title: group.title,
          proposal_id: group.proposalId,
          proposal_title: group.proposalTitle,
          submissions: sorted,
        };
      })
      .filter(Boolean);

    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `submission-comparison-${new Date().toISOString().slice(0, 10)}.json`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };

  return {
    auth,
    filterStatus, setFilterStatus,
    expandedTasks, setExpandedTasks,
    reviewingId, reviewNotes, setReviewNotes,
    proofContent, loadingProof,
    viewMode, setViewMode,
    sortBy, setSortBy,
    expandedSubmissions, setExpandedSubmissions,
    expandedDiffs, setExpandedDiffs,
    selectedSubmissions,
    rootRef, scrollTopRef,
    submissionsKey, submissionsObj, submissionsArray, submissionsMap,
    allDeliverables, filteredDeliverables, selectableDeliverables,
    selectedCount, allSelected, submissionsByTask, comparisonGroups,
    toggleTaskExpansion, reviewDeliverable, toggleSelectSubmission,
    toggleSelectAll, bulkReview, fetchProofContent, exportComparison,
    getStatusIcon, getStatusColor, renderProofContent,
  };
}
