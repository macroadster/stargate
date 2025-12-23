import React, { useLayoutEffect, useRef, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import { CheckCircle, XCircle, Clock, ExternalLink, Filter, ChevronDown, ChevronUp, Eye, FileText, Code, Columns, List } from 'lucide-react';
import toast from 'react-hot-toast';
import { API_BASE } from '../../apiBase';
import CopyButton from '../Common/CopyButton';
import { useAuth } from '../../context/AuthContext';

const DeliverablesReview = ({ proposalItems, submissions, submissionsList, onRefresh, isContractLocked = false }) => {
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

  // Handle both array and object formats for submissions
  const submissionsObj = React.useMemo(
    () => ((submissions && !Array.isArray(submissions) && typeof submissions === 'object') ? submissions : {}),
    [submissions],
  );
  const submissionsArray = React.useMemo(() => {
    try {
      if (Array.isArray(submissionsList)) {
        return submissionsList;
      }
      if (Array.isArray(submissions)) {
        return submissions;
      }
      if (submissionsObj) {
        return Object.keys(submissionsObj).map((key) => submissionsObj[key]);
      }
      return [];
    } catch (e) {
      console.error('Error processing submissions:', e, submissions);
      return [];
    }
  }, [submissionsList, submissions, submissionsObj]);
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

  const allDeliverables = React.useMemo(
    () =>
      proposalItems
        .flatMap((proposal) => {
          const tasks = Array.isArray(proposal.tasks) && proposal.tasks.length > 0
            ? proposal.tasks
            : (Array.isArray(proposal.metadata?.suggested_tasks) ? proposal.metadata.suggested_tasks : []);

          return tasks.map((task) => {
            // Find submission by task_id or claim_id
            const submission = submissionsObj[task.task_id] || submissionsObj[task.active_claim_id] || null;

            return {
              ...task,
              proposal,
              submission,
              submissionKey: submission?.submission_id, // Use actual submission_id for API calls
              proposalId: proposal.id,
              proposalTitle: proposal.title,
            };
          });
        })
        .filter((item) => item.submission),
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

  const filterByKeys = (entries, allowed) => Object.keys(entries).reduce((acc, key) => {
    if (allowed.has(key)) {
      acc[key] = entries[key];
    }
    return acc;
  }, {});

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

  const submissionsByTask = submissionsArray.reduce((acc, submission) => {
    const taskId = submission?.task_id;
    if (!taskId) return acc;
    if (!acc[taskId]) acc[taskId] = [];
    acc[taskId].push(submission);
    return acc;
  }, {});

  const comparisonGroups = proposalItems.flatMap((proposal) => {
    const tasks = Array.isArray(proposal.tasks) && proposal.tasks.length > 0
      ? proposal.tasks
      : (Array.isArray(proposal.metadata?.suggested_tasks) ? proposal.metadata.suggested_tasks : []);

    return tasks.map((task) => {
      const groupSubmissions = submissionsByTask[task.task_id] || [];
      return {
        ...task,
        proposal,
        proposalId: proposal.id,
        proposalTitle: proposal.title,
        submissions: groupSubmissions,
      };
    });
  }).filter((group) => group.submissions.length > 0);

  const toggleTaskExpansion = (taskId) => {
    setExpandedTasks(prev => ({
      ...prev,
      [taskId]: !prev[taskId]
    }));
  };

  const performReviewRequest = async (submissionId, action) => {
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

  const renderMarkdown = (content) => {
    const safeContent = content || '';
    const hasMarkdown = /(^|\n)(#{1,6}\s|[-*]\s|\d+\.\s|```)/.test(safeContent);

    if (!hasMarkdown) {
      return (
        <pre className="text-sm text-gray-800 dark:text-gray-100 whitespace-pre-wrap">
          {safeContent}
        </pre>
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

  const getSubmissionTimestamp = (submission) => {
    const raw = submission?.submitted_at || submission?.created_at;
    if (!raw) return 0;
    const parsed = Date.parse(raw);
    return Number.isNaN(parsed) ? 0 : parsed;
  };

  const countWords = (value) => {
    if (!value || typeof value !== 'string') return 0;
    return value.trim().split(/\s+/).filter(Boolean).length;
  };

  const getSubmissionNotes = (submission) => (
    submission?.deliverables?.notes
      || submission?.deliverables?.document
      || submission?.deliverables?.rework_notes
      || ''
  );

  const getNotesPreview = (value, limit = 320) => {
    if (!value) return '';
    if (value.length <= limit) return value;
    return `${value.slice(0, limit)}…`;
  };

  const getSubmissionId = (submission, fallback) => (
    submission?.submission_id || submission?.id || fallback
  );

  const formatTimestamp = (value) => (
    value ? new Date(value).toLocaleString() : 'Unknown time'
  );

  const sortSubmissions = (submissions) => {
    return [...submissions].sort((a, b) => {
      if (sortBy === 'most_words' || sortBy === 'fewest_words') {
        const aWords = countWords(getSubmissionNotes(a));
        const bWords = countWords(getSubmissionNotes(b));
        return sortBy === 'most_words' ? bWords - aWords : aWords - bWords;
      }
      const aTime = getSubmissionTimestamp(a);
      const bTime = getSubmissionTimestamp(b);
      return sortBy === 'oldest' ? aTime - bTime : bTime - aTime;
    });
  };

  const exportComparison = () => {
    const payload = comparisonGroups
      .map((group) => {
        const filteredSubmissions = group.submissions.filter((submission) => {
          if (filterStatus === 'all') return true;
          return (submission.status || '').toLowerCase() === filterStatus;
        });
        if (filteredSubmissions.length === 0) return null;
        const sorted = sortSubmissions(filteredSubmissions);
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

  if (allDeliverables.length === 0 && comparisonGroups.length === 0) {
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
        
        <div className="flex items-center gap-2 flex-wrap">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setViewMode('list')}
              className={`px-3 py-1.5 text-xs rounded border flex items-center gap-1 ${
                viewMode === 'list'
                  ? 'bg-gray-900 text-white border-gray-900'
                  : 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200'
              }`}
            >
              <List className="w-3 h-3" />
              List
            </button>
            <button
              type="button"
              onClick={() => setViewMode('compare')}
              className={`px-3 py-1.5 text-xs rounded border flex items-center gap-1 ${
                viewMode === 'compare'
                  ? 'bg-gray-900 text-white border-gray-900'
                  : 'bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200'
              }`}
            >
              <Columns className="w-3 h-3" />
              Compare
            </button>
          </div>
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
          {viewMode === 'compare' && (
            <>
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value)}
                className="text-sm rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-2 py-1"
              >
                <option value="newest">Newest first</option>
                <option value="oldest">Oldest first</option>
                <option value="most_words">Most words</option>
                <option value="fewest_words">Fewest words</option>
              </select>
              <button
                type="button"
                onClick={exportComparison}
                className="px-3 py-1.5 text-xs rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700"
              >
                Export JSON
              </button>
            </>
          )}
        </div>
      </div>

      <div className="text-sm text-gray-600 dark:text-gray-400">
        {viewMode === 'list'
          ? `Showing ${filteredDeliverables.length} of ${allDeliverables.length} deliverables`
          : `Showing ${comparisonGroups.length} tasks with submissions`}
      </div>

      {viewMode === 'list' && (
        <div className="flex items-center justify-between gap-3 flex-wrap text-xs text-gray-600 dark:text-gray-400">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={allSelected}
              onChange={(e) => toggleSelectAll(e.target.checked, selectableDeliverables)}
              className="h-4 w-4 rounded border-gray-300 text-emerald-600 focus:ring-emerald-500 dark:border-gray-600"
            />
            Select all
          </label>
          <div className="flex items-center gap-2 flex-wrap">
            <span>{selectedCount} selected</span>
            <button
              type="button"
              onClick={() => bulkReview('approve', filteredDeliverables)}
              disabled={selectedCount === 0 || reviewingId === 'bulk' || isContractLocked}
              className="px-3 py-1.5 text-xs rounded bg-green-600 hover:bg-green-500 text-white disabled:opacity-60"
            >
              Bulk approve
            </button>
            <button
              type="button"
              onClick={() => bulkReview('reject', filteredDeliverables)}
              disabled={selectedCount === 0 || reviewingId === 'bulk' || isContractLocked}
              className="px-3 py-1.5 text-xs rounded bg-red-600 hover:bg-red-500 text-white disabled:opacity-60"
            >
              Bulk reject
            </button>
          </div>
        </div>
      )}

      {viewMode === 'compare' ? (
        <div className="space-y-4" ref={rootRef}>
          {comparisonGroups.map((group) => {
            const filteredSubmissions = group.submissions.filter((submission) => {
              if (filterStatus === 'all') return true;
              return (submission.status || '').toLowerCase() === filterStatus;
            });
            if (filteredSubmissions.length === 0) return null;

            const sortedSubmissions = sortSubmissions(filteredSubmissions);

            return (
              <div key={group.task_id} className="border border-gray-200 dark:border-gray-700 rounded-lg bg-white dark:bg-gray-900 overflow-hidden">
                <div className="p-4 border-b border-gray-200 dark:border-gray-700">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-xs text-gray-500 dark:text-gray-400">Task {group.task_id}</div>
                      <h5 className="text-base font-semibold text-black dark:text-white">{group.title}</h5>
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">{group.proposalTitle}</p>
                    </div>
                    <span className="text-xs text-gray-500 dark:text-gray-400">Proposal: {group.proposalId}</span>
                  </div>
                </div>
                <div className="overflow-x-auto">
                  <div className="flex gap-4 p-4 min-w-full">
                    {sortedSubmissions.map((submission, index) => {
                      const submissionId = submission.submission_id || submission.id || `${group.task_id}-${index}`;
                      const notes = getSubmissionNotes(submission);
                      const wordCount = countWords(notes);
                      const preview = getNotesPreview(notes);
                      const isExpanded = !!expandedSubmissions[submissionId];
                      const status = submission.status || 'pending';
                      return (
                        <div key={submissionId} className="min-w-[360px] max-w-[520px] flex-shrink-0 border border-gray-200 dark:border-gray-700 rounded-lg p-3 bg-gray-50 dark:bg-gray-900">
                          <div className="flex items-start justify-between gap-2">
                            <div>
                              <div className="flex items-center gap-2">
                                {getStatusIcon(status)}
                                <span className={`text-xs px-2 py-0.5 rounded border ${getStatusColor(status)}`}>
                                  {status}
                                </span>
                                <span className="text-[11px] text-gray-500 dark:text-gray-400">Attempt {index + 1}</span>
                              </div>
                              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                {submission.submitted_at ? new Date(submission.submitted_at).toLocaleString() : 'Unknown time'}
                              </div>
                            </div>
                            <div className="text-xs text-gray-500 dark:text-gray-400 text-right">
                              {submission.submission_id || submission.id}
                            </div>
                          </div>

                          <div className="grid grid-cols-2 gap-2 text-xs text-gray-600 dark:text-gray-300 mt-3">
                            <div>Words: {wordCount}</div>
                            <div>Proof: {submission.completion_proof?.link ? 'Yes' : 'No'}</div>
                            <div className="col-span-2">Submitted by: {submission?.deliverables?.submitted_by || 'Unknown'}</div>
                          </div>

                          {submission?.rejection_reason && (
                            <div className="mt-3 bg-red-50 dark:bg-red-900/40 border border-red-200 dark:border-red-700 rounded p-2 text-xs text-red-700 dark:text-red-200 whitespace-pre-wrap">
                              {submission.rejection_reason}
                            </div>
                          )}

                          <div className="mt-3">
                            <div className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-1">Notes</div>
                            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded p-2 max-h-64 overflow-y-auto overflow-x-auto max-w-full">
                              {isExpanded ? renderMarkdown(notes || 'No notes provided.') : (
                                <pre className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap overflow-x-auto max-w-full">
                                  {preview || 'No notes provided.'}
                                </pre>
                              )}
                            </div>
                            {notes && notes.length > 0 && (
                              <button
                                type="button"
                                onClick={() => setExpandedSubmissions(prev => ({ ...prev, [submissionId]: !prev[submissionId] }))}
                                className="mt-2 text-xs text-blue-600 dark:text-blue-400 hover:underline"
                              >
                                {isExpanded ? 'Show preview' : 'Show full notes'}
                              </button>
                            )}
                          </div>

                          <div className="mt-3 space-y-2">
                            <textarea
                              className="w-full rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-xs px-2 py-1"
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
                                className="px-2 py-1 bg-purple-600 hover:bg-purple-500 text-white rounded text-xs disabled:opacity-60 flex items-center gap-1"
                              >
                                <Eye className="w-3 h-3" />
                                {reviewingId === submissionId ? 'Processing…' : 'Review'}
                              </button>
                              <button
                                onClick={() => reviewDeliverable(submissionId, 'approve')}
                                disabled={reviewingId === submissionId || isContractLocked}
                                className="px-2 py-1 bg-green-600 hover:bg-green-500 text-white rounded text-xs disabled:opacity-60 flex items-center gap-1"
                              >
                                <CheckCircle className="w-3 h-3" />
                                {reviewingId === submissionId ? 'Processing…' : 'Approve'}
                              </button>
                              <button
                                onClick={() => reviewDeliverable(submissionId, 'reject')}
                                disabled={reviewingId === submissionId || isContractLocked}
                                className="px-2 py-1 bg-red-600 hover:bg-red-500 text-white rounded text-xs disabled:opacity-60 flex items-center gap-1"
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
            <div key={deliverable.task_id} className="border border-gray-200 dark:border-gray-700 rounded-lg bg-white dark:bg-gray-900 overflow-hidden">
              <div className="p-4">
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
                        className="h-4 w-4 rounded border-gray-300 text-emerald-600 focus:ring-emerald-500 dark:border-gray-600"
                      />
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

                  <div className="flex items-center gap-2">
                    {(() => {
                      const status = (deliverable.submission?.status || '').toLowerCase();
                      const finalStatuses = ['approved', 'rejected', 'reviewed'];
                      const showQuickActions = !finalStatuses.includes(status);
                      if (!showQuickActions) return null;
                      return (
                        <div className="flex items-center gap-1">
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'approve')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="px-2 py-1 bg-green-600 hover:bg-green-500 text-white rounded text-xs disabled:opacity-60"
                          >
                            Approve
                          </button>
                          <button
                            onClick={() => reviewDeliverable(deliverable.submissionKey, 'reject')}
                            disabled={reviewingId === deliverable.submissionKey || isContractLocked}
                            className="px-2 py-1 bg-red-600 hover:bg-red-500 text-white rounded text-xs disabled:opacity-60"
                          >
                            Reject
                          </button>
                        </div>
                      );
                    })()}
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
                </div>

                {(() => {
                  const notes = getSubmissionNotes(deliverable.submission || {});
                  const preview = getNotesPreview(notes, 200);
                  const wordCount = countWords(notes);
                  const status = (deliverable.submission?.status || '').toLowerCase();
                  const statusText = status || 'pending';
                  return (
                    <div className="mt-3 bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg p-3">
                      <div className="flex items-center justify-between text-xs text-gray-600 dark:text-gray-400 mb-2">
                        <span>Words: {wordCount}</span>
                        <span>Status: {statusText}</span>
                      </div>
                      <pre className="text-xs text-gray-700 dark:text-gray-300 whitespace-pre-wrap overflow-x-auto max-w-full">
                        {preview || 'No notes provided.'}
                      </pre>
                    </div>
                  );
                })()}

                {expandedTasks[deliverable.task_id] && (
                  <div className="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700 space-y-4">
                  <div>
                    <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Deliverable Details</h6>
                    <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3 space-y-2">
                      {deliverable.submission?.submitted_at && (
                        <div className="flex items-start justify-between gap-2">
                          <span className="text-sm text-gray-600 dark:text-gray-400">Submitted At:</span>
                          <span className="text-sm text-black dark:text-white">
                            {new Date(deliverable.submission.submitted_at).toLocaleString()}
                          </span>
                        </div>
                      )}
                      <div className="flex items-start justify-between gap-2">
                        <span className="text-sm text-gray-600 dark:text-gray-400">Task ID:</span>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-mono text-black dark:text-white">{deliverable.task_id}</span>
                          <CopyButton text={deliverable.task_id} />
                        </div>
                      </div>
                      {(deliverable.submission?.claim_id || deliverable.active_claim_id) && (
                        <div className="flex items-start justify-between gap-2">
                          <span className="text-sm text-gray-600 dark:text-gray-400">Claim ID:</span>
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-mono text-black dark:text-white">
                              {deliverable.submission?.claim_id || deliverable.active_claim_id}
                            </span>
                            <CopyButton text={deliverable.submission?.claim_id || deliverable.active_claim_id} />
                          </div>
                        </div>
                      )}
                      <div className="flex items-start justify-between gap-2">
                        <span className="text-sm text-gray-600 dark:text-gray-400">Submission ID:</span>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-mono text-black dark:text-white">
                            {deliverable.submission?.submission_id || deliverable.submission?.id || deliverable.active_claim_id}
                          </span>
                          <CopyButton text={deliverable.submission?.submission_id || deliverable.submission?.id || deliverable.active_claim_id} />
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
                      <div className="bg-blue-50 dark:bg-blue-900/40 border border-blue-200 dark:border-blue-700 rounded-lg p-3 max-h-[60vh] overflow-y-auto overflow-x-auto max-w-full">
                        {renderMarkdown(deliverable.submission.deliverables.notes)}
                      </div>
                    </div>
                  )}

                  {(deliverable.submission?.rejection_reason || deliverable.submission?.rejection_type) && (
                    <div>
                      <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Rejection Feedback</h6>
                      <div className="bg-red-50 dark:bg-red-900/40 border border-red-200 dark:border-red-700 rounded-lg p-3 space-y-2">
                        {deliverable.submission?.rejection_type && (
                          <div className="text-xs font-semibold uppercase tracking-wide text-red-700 dark:text-red-200">
                            {deliverable.submission.rejection_type}
                          </div>
                        )}
                        {deliverable.submission?.rejection_reason && (
                          <div className="text-sm text-red-900 dark:text-red-100 whitespace-pre-wrap">
                            {deliverable.submission.rejection_reason}
                          </div>
                        )}
                        {deliverable.submission?.rejected_at && (
                          <div className="text-xs text-red-700 dark:text-red-200">
                            Rejected at {new Date(deliverable.submission.rejected_at).toLocaleString()}
                          </div>
                        )}
                      </div>
                    </div>
                  )}

                  {deliverable.submission?.deliverables?.document && (
                    <div>
                      <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Submission Document</h6>
                      <div className="bg-slate-50 dark:bg-slate-900/40 border border-slate-200 dark:border-slate-700 rounded-lg p-3 max-h-[60vh] overflow-y-auto overflow-x-auto max-w-full">
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
                        <h6 className="text-sm font-semibold text-black dark:text-white mb-2">Submission Timeline</h6>
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
                              <div key={submissionId} className="border border-gray-200 dark:border-gray-700 rounded-lg p-3 bg-gray-50 dark:bg-gray-900">
                                <div className="flex items-start justify-between gap-2">
                                  <div>
                                    <div className="flex items-center gap-2">
                                      {getStatusIcon(status)}
                                      <span className={`text-xs px-2 py-0.5 rounded border ${getStatusColor(status)}`}>
                                        {status}
                                      </span>
                                      <span className="text-[11px] text-gray-500 dark:text-gray-400">Attempt {index + 1}</span>
                                    </div>
                                    <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                      {formatTimestamp(submission.submitted_at || submission.created_at)}
                                    </div>
                                  </div>
                                  <div className="text-xs text-gray-500 dark:text-gray-400 text-right">
                                    {submissionId}
                                  </div>
                                </div>

                                <div className="mt-2 grid grid-cols-2 gap-2 text-xs text-gray-600 dark:text-gray-300">
                                  <div>Words: {words}</div>
                                  <div>Claim: {submission.claim_id || '—'}</div>
                                </div>

                                {submission.rejection_reason && (
                                  <div className="mt-2 text-xs text-red-700 dark:text-red-200 bg-red-50 dark:bg-red-900/40 border border-red-200 dark:border-red-700 rounded p-2 whitespace-pre-wrap">
                                    {submission.rejection_reason}
                                  </div>
                                )}

                                {index > 0 && (
                                  <button
                                    type="button"
                                    onClick={() => setExpandedDiffs(prev => ({ ...prev, [diffKey]: !prev[diffKey] }))}
                                    className="mt-2 text-xs text-blue-600 dark:text-blue-400 hover:underline"
                                  >
                                    {showDiff ? 'Hide comparison' : 'Compare with previous'}
                                  </button>
                                )}

                                {showDiff && (
                                  <div className="mt-2 grid md:grid-cols-2 gap-3">
                                    <div>
                                      <div className="text-[11px] text-gray-500 dark:text-gray-400 mb-1">
                                        Previous ({prevWords} words)
                                      </div>
                                      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded p-2 max-h-48 overflow-y-auto overflow-x-auto max-w-full">
                                        {renderMarkdown(prevNotes || 'No notes provided.')}
                                      </div>
                                    </div>
                                    <div>
                                      <div className="text-[11px] text-gray-500 dark:text-gray-400 mb-1">
                                        Current ({words} words)
                                      </div>
                                      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded p-2 max-h-48 overflow-y-auto overflow-x-auto max-w-full">
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
      )}

    </div>
  );
};

export default DeliverablesReview;
