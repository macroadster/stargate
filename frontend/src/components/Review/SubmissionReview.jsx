import React, { useState, useEffect } from 'react';
import ReactMarkdown from 'react-markdown';
import { CheckCircle, XCircle, Clock, FileText, ExternalLink } from 'lucide-react';
import { API_BASE } from '../../apiBase';

const SubmissionReview = ({ submissionId, onApprove, onReject, onClose }) => {
  const [submission, setSubmission] = useState(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);

  useEffect(() => {
    fetchSubmission();
  }, [submissionId, fetchSubmission]);

  const fetchSubmission = async () => {
    try {
      const response = await fetch(`${API_BASE}/api/smart_contract/submissions/${submissionId}`);
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();
      setSubmission(data);
    } catch (error) {
      console.error('Failed to fetch submission:', error);
    } finally {
      setLoading(false);
    }
  };

  const handleApprove = async () => {
    setActionLoading(true);
    try {
      await onApprove(submissionId);
      onClose();
    } catch (error) {
      console.error('Approval failed:', error);
    } finally {
      setActionLoading(false);
    }
  };

  const handleReject = async () => {
    setActionLoading(true);
    try {
      await onReject(submissionId);
      onClose();
    } catch (error) {
      console.error('Rejection failed:', error);
    } finally {
      setActionLoading(false);
    }
  };

  if (loading) return (
    <div className="flex items-center justify-center p-8">
      <Clock className="w-6 h-6 animate-spin" />
      <span className="ml-2">Loading submission...</span>
    </div>
  );

  if (!submission) return (
    <div className="text-center p-8 text-gray-500">
      Submission not found
    </div>
  );

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-900 rounded-xl shadow-xl max-w-4xl max-h-[90vh] overflow-y-auto m-4">
        {/* Header */}
        <div className="border-b border-gray-200 dark:border-gray-800 p-6">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-2xl font-bold">Submission Review</h2>
              <div className="flex items-center gap-4 mt-2 text-sm text-gray-600 dark:text-gray-400">
                <span>Task: {submission.task_id}</span>
                <span>Submitted by: {submission.deliverables?.submitted_by || 'Unknown'}</span>
                <span className={`px-2 py-1 rounded text-xs font-semibold ${
                  submission.status === 'pending_review' ? 'bg-yellow-100 text-yellow-800' :
                  submission.status === 'accepted' ? 'bg-green-100 text-green-800' :
                  'bg-red-100 text-red-800'
                }`}>
                  {submission.status?.replace('_', ' ') || 'Unknown'}
                </span>
              </div>
            </div>
            <button onClick={onClose} className="text-gray-500 hover:text-gray-700">
              <XCircle className="w-6 h-6" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="p-6 space-y-6">
          {/* Main Document */}
          {submission.deliverables?.document && (
            <section className="border border-gray-200 dark:border-gray-800 rounded-lg p-4">
              <h3 className="text-lg font-semibold mb-3 flex items-center gap-2">
                <FileText className="w-5 h-5" />
                Work Document
              </h3>
              <div className="prose prose-sm max-w-none dark:prose-invert">
                <ReactMarkdown>{submission.deliverables.document}</ReactMarkdown>
              </div>
            </section>
          )}

          {/* Technical Specifications */}
          {submission.deliverables?.technical_specifications && (
            <section className="border border-gray-200 dark:border-gray-800 rounded-lg p-4">
              <h3 className="text-lg font-semibold mb-3">Technical Specifications</h3>
              <pre className="bg-gray-100 dark:bg-gray-800 p-4 rounded text-sm overflow-x-auto">
                {JSON.stringify(submission.deliverables.technical_specifications, null, 2)}
              </pre>
            </section>
          )}

          {/* Completion Proof */}
          {submission.completion_proof && (
            <section className="border border-gray-200 dark:border-gray-800 rounded-lg p-4">
              <h3 className="text-lg font-semibold mb-3">Completion Proof</h3>
              <div className="space-y-3">
                <div>
                  <strong>Methodology:</strong>
                  <p className="text-gray-700 dark:text-gray-300 mt-1">
                    {submission.completion_proof.methodology}
                  </p>
                </div>
                <div>
                  <strong>Verification Status:</strong>
                  <span className="ml-2 px-2 py-1 rounded text-xs bg-blue-100 text-blue-800">
                    {submission.completion_proof.verification_status}
                  </span>
                </div>
                {submission.completion_proof.reference_documents && (
                  <div>
                    <strong>References:</strong>
                    <ul className="mt-2 space-y-1">
                      {submission.completion_proof.reference_documents.map((doc, index) => (
                        <li key={index} className="text-sm text-gray-600 dark:text-gray-400">
                          {doc}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </section>
          )}

          {/* Attachments */}
          {submission.deliverables?.attachments && (
            <section className="border border-gray-200 dark:border-gray-800 rounded-lg p-4">
              <h3 className="text-lg font-semibold mb-3">Attachments</h3>
              <div className="space-y-2">
                {submission.deliverables.attachments.map((attachment, index) => (
                  <div key={index} className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded">
                    <div className="flex items-center gap-2">
                      <FileText className="w-4 h-4" />
                      <span className="text-sm font-medium">{attachment.filename}</span>
                      <span className="text-xs text-gray-500">({attachment.size})</span>
                    </div>
                    <a 
                      href={attachment.url} 
                      download={attachment.filename}
                      className="text-blue-600 hover:text-blue-800 flex items-center gap-1"
                    >
                      <ExternalLink className="w-4 h-4" />
                      Download
                    </a>
                  </div>
                ))}
              </div>
            </section>
          )}
        </div>

        {/* Actions */}
        <div className="border-t border-gray-200 dark:border-gray-800 p-6">
          <div className="flex justify-end gap-3">
            <button 
              onClick={handleReject}
              disabled={actionLoading || submission.status !== 'pending_review'}
              className="px-4 py-2 rounded-lg border border-red-300 text-red-700 hover:bg-red-50 disabled:opacity-60"
            >
              Request Revisions
            </button>
            <button 
              onClick={handleApprove}
              disabled={actionLoading || submission.status !== 'pending_review'}
              className="px-4 py-2 rounded-lg bg-green-600 text-white hover:bg-green-700 disabled:opacity-60 flex items-center gap-2"
            >
              <CheckCircle className="w-4 h-4" />
              {actionLoading ? 'Processing...' : 'Approve Work'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default SubmissionReview;