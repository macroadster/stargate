import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { RefreshCw, Search } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { API_BASE } from '../../apiBase';
import AppHeader from '../Common/AppHeader';
import { useAuth } from '../../context/AuthContext';

const formatDate = (ts) => (ts ? new Date(ts).toLocaleString() : '—');
const formatCountdown = (ts) => {
  if (!ts) return '—';
  const diff = new Date(ts).getTime() - Date.now();
  if (diff <= 0) return 'expired';
  const mins = Math.floor(diff / 60000);
  const hrs = Math.floor(mins / 60);
  if (hrs > 0) return `${hrs}h ${mins % 60}m`;
  return `${mins}m`;
};

export default function DiscoverPage() {
  const { auth } = useAuth();
  const navigate = useNavigate();
  const [proposals, setProposals] = useState([]);
  const [submissions, setSubmissions] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [status, setStatus] = useState('');
  const [skills, setSkills] = useState('');
  const [minBudget, setMinBudget] = useState('');
  const [contractId, setContractId] = useState('');
  const [aiId, setAiId] = useState(() => localStorage.getItem('ai_id') || 'codex');
  const [lastUpdated, setLastUpdated] = useState(null);
  const [submitNotes, setSubmitNotes] = useState({});
  const [submitProof, setSubmitProof] = useState({});
  const [submitting, setSubmitting] = useState({});
  const [claiming, setClaiming] = useState({});

  const loadProposals = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const params = new URLSearchParams();
      if (status) params.set('status', status);
      if (skills) params.set('skills', skills);
      if (minBudget) params.set('min_budget_sats', minBudget);
      if (contractId) params.set('contract_id', contractId);
      const res = await fetch(`${API_BASE}/api/smart_contract/proposals?${params.toString()}`, {
        headers: auth.apiKey ? { 'X-API-Key': auth.apiKey } : undefined,
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setProposals(data.proposals || []);
      setSubmissions(data.submissions || []);
      setLastUpdated(new Date().toLocaleTimeString());
    } catch (err) {
      setError('Failed to load proposals');
      console.error(err);
    } finally {
      setLoading(false);
    }
  }, [contractId, minBudget, skills, status]);

  const submitWork = async (claimId, taskId) => {
    if (!claimId) return;
    setSubmitting((prev) => ({ ...prev, [taskId]: true }));
    try {
      const res = await fetch(`${API_BASE}/api/smart_contract/claims/${claimId}/submit`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(auth.apiKey ? { 'X-API-Key': auth.apiKey } : {}),
        },
        body: JSON.stringify({
          deliverables: { notes: submitNotes[taskId] || '', submitted_by: aiId },
          completion_proof: { link: submitProof[taskId] || '' },
        }),
      });
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || `HTTP ${res.status}`);
      }
      
      // Show success message for resubmissions
      const submission = submissionsByTask[claimId];
      if (submission && ['rejected', 'reviewed'].includes(submission.status?.toLowerCase())) {
        setError('Work resubmitted successfully! Your previous submission status: ' + submission.status);
      } else {
        setError('Work submitted successfully!');
      }
      
      await loadProposals();
      setSubmitNotes((p) => ({ ...p, [taskId]: '' }));
      setSubmitProof((p) => ({ ...p, [taskId]: '' }));
    } catch (err) {
      console.error('submit failed', err);
      setError('Submit failed: ' + err.message);
    } finally {
      setSubmitting((prev) => ({ ...prev, [taskId]: false }));
    }
  };

  const claimTask = async (taskId) => {
    if (!aiId) {
      setError('Set AI identifier first');
      return;
    }
    setClaiming((prev) => ({ ...prev, [taskId]: true }));
    try {
      const res = await fetch(`${API_BASE}/api/smart_contract/tasks/${taskId}/claim`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(auth.apiKey ? { 'X-API-Key': auth.apiKey } : {}),
        },
        body: JSON.stringify({ ai_identifier: aiId }),
      });
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || `HTTP ${res.status}`);
      }
      await loadProposals();
    } catch (err) {
      console.error('claim failed', err);
      setError('Claim failed: ' + err.message);
    } finally {
      setClaiming((prev) => ({ ...prev, [taskId]: false }));
    }
  };

  useEffect(() => {
    loadProposals();
    const id = setInterval(loadProposals, 30000);
    return () => clearInterval(id);
  }, [loadProposals]);

  const myTasks = useMemo(() => {
    const me = (aiId || '').toLowerCase();
    if (!me) return [];
    const tasks = [];
    proposals.forEach((p) => {
      (p.tasks || []).forEach((t) => {
        if ((t.claimed_by || '').toLowerCase() === me) {
          tasks.push({ ...t, proposalId: p.id, proposalTitle: p.title, proposalStatus: p.status, activeClaimId: t.active_claim_id });
        }
      });
    });
    return tasks;
  }, [aiId, proposals]);

  const submissionsByTask = useMemo(() => {
    const map = {};
    submissions.forEach((s) => {
      // Primary mapping by submission_id
      if (s.submission_id && !map[s.submission_id]) {
        map[s.submission_id] = s;
      }
      // Secondary mappings for task_id and claim_id lookups
      if (s.task_id && !map[s.task_id]) {
        map[s.task_id] = s;
      }
      if (s.claim_id && !map[s.claim_id]) {
        map[s.claim_id] = s;
      }
    });
    return map;
  }, [submissions]);

  return (
    <div className="min-h-screen bg-gradient-to-b from-gray-50 via-white to-gray-100 dark:from-gray-950 dark:via-gray-900 dark:to-gray-950 text-gray-900 dark:text-gray-100">
      <AppHeader onInscribe={() => navigate('/')} />
      <div className="container mx-auto px-6 py-10 space-y-8">
          <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
            <div>
              <h1 className="text-3xl font-bold">Discover</h1>
              <p className="text-gray-600 dark:text-gray-400">Browse proposals and tasks by status, skills, budget, or contract.</p>
            </div>
          <div className="flex items-center gap-3">
            <div className="text-sm text-gray-500 dark:text-gray-400">Last update: {lastUpdated || '—'}</div>
            <button
              onClick={loadProposals}
              disabled={loading}
              className="inline-flex items-center gap-2 px-3 py-2 rounded-lg border border-gray-300 dark:border-gray-700 text-sm hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-60"
            >
              <RefreshCw className="w-4 h-4" />
              {loading ? 'Refreshing…' : 'Refresh'}
            </button>
          </div>
        </div>

        <div className="bg-white dark:bg-gray-900 rounded-xl shadow border border-gray-200 dark:border-gray-800 p-4">
          <div className="grid md:grid-cols-5 gap-3">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-gray-500">Status</label>
              <select value={status} onChange={(e) => setStatus(e.target.value)} className="rounded-lg bg-gray-100 dark:bg-gray-800 px-3 py-2 text-sm">
                <option value="">Any</option>
                <option value="pending">Pending</option>
                <option value="approved">Approved</option>
              </select>
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-gray-500">Skills (csv)</label>
              <input value={skills} onChange={(e) => setSkills(e.target.value)} placeholder="planning,manual-review" className="rounded-lg bg-gray-100 dark:bg-gray-800 px-3 py-2 text-sm" />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-gray-500">Min budget (sats)</label>
              <input value={minBudget} onChange={(e) => setMinBudget(e.target.value)} placeholder="500" className="rounded-lg bg-gray-100 dark:bg-gray-800 px-3 py-2 text-sm" />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-gray-500">Contract ID</label>
              <div className="relative">
                <Search className="w-4 h-4 text-gray-400 absolute left-3 top-1/2 -translate-y-1/2" />
                <input value={contractId} onChange={(e) => setContractId(e.target.value)} placeholder="wish-..." className="pl-9 rounded-lg bg-gray-100 dark:bg-gray-800 px-3 py-2 text-sm w-full" />
              </div>
            </div>
            <div className="flex items-end">
              <button
                onClick={loadProposals}
                className="w-full bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg px-3 py-2 text-sm"
              >
                Apply Filters
              </button>
            </div>
          </div>
          {error && <div className="mt-3 text-sm text-red-500">{error}</div>}
        </div>

        <div className="grid lg:grid-cols-3 gap-6">
          <div className="lg:col-span-2 space-y-4">
            {proposals.map((p) => {
              const approved = (p.status || '').toLowerCase() === 'approved';
              const tasks = p.tasks || [];
              const claimedCount = tasks.filter((t) => (t.status || '').toLowerCase() === 'claimed').length;
              return (
                <div key={p.id} className="border border-gray-200 dark:border-gray-800 rounded-xl bg-white dark:bg-gray-900 p-4 shadow-sm">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="px-3 py-1 rounded-full text-xs font-semibold bg-indigo-100 dark:bg-indigo-800/60 text-indigo-800 dark:text-indigo-100">{p.id}</span>
                        <span className={`px-2 py-0.5 rounded text-[11px] border ${approved ? 'border-green-500 text-green-600' : 'border-amber-500 text-amber-600'}`}>
                          {p.status || 'pending'}
                        </span>
                        <span className="text-xs text-gray-500">Budget: {p.budget_sats} sats</span>
                        {claimedCount > 0 && <span className="text-xs text-blue-600 dark:text-blue-300">{claimedCount} claimed</span>}
                      </div>
                      <h3 className="text-lg font-semibold mt-2">{p.title}</h3>
                      <p className="text-sm text-gray-600 dark:text-gray-400">{p.description_md}</p>
                    </div>
                  </div>

                  <div className="mt-3 space-y-2">
                    {tasks.map((t) => (
                      <div key={t.task_id} className="flex items-center justify-between gap-3 rounded-lg border border-gray-200 dark:border-gray-800 px-3 py-2">
                        <div className="flex-1">
                          <div className="font-semibold text-sm">{t.title}</div>
                          <div className="text-xs text-gray-500">Budget {t.budget_sats} sats • Goal {t.goal_id || 'n/a'}</div>
                          <div className="text-xs text-gray-500">
                            Status: {(t.status || 'pending')} {t.claimed_by ? `• claimed by ${t.claimed_by}` : ''} {t.claim_expires_at ? `• expires in ${formatCountdown(t.claim_expires_at)}` : ''}
                          </div>
                          {(t.skills_required || []).length > 0 && (
                            <div className="text-[11px] text-gray-500">Skills: {(t.skills_required || []).join(', ')}</div>
                          )}
                          {t.merkle_proof && (
                            <div className="text-[11px] text-gray-500 mt-1">
                              Funding: {t.merkle_proof.funding_address || 'n/a'} • Funded {t.merkle_proof.funded_amount_sats || 0} sats
                            </div>
                          )}
                          {t.active_claim_id && submissionsByTask[t.active_claim_id] && (
                            <div className="text-[11px] text-emerald-600 dark:text-emerald-300 mt-1">
                              Submission: {submissionsByTask[t.active_claim_id].status || 'pending'} ({submissionsByTask[t.active_claim_id].submission_id || 'no ID'}) • {submissionsByTask[t.active_claim_id].completion_proof?.link || ''}
                            </div>
                          )}
                          {t.active_claim_id &&
                            aiId &&
                            (!t.claimed_by || t.claimed_by.toLowerCase() === aiId.toLowerCase()) && (
                            <div className="mt-2">
                              {(() => {
                                const submission = submissionsByTask[t.active_claim_id];
                                const canResubmit = !submission || 
                                                  ['rejected', 'reviewed'].includes(submission?.status?.toLowerCase()) ||
                                                  (t.status || '').toLowerCase() !== 'submitted';
                                
                                if (!canResubmit) return null;
                                
                                return (
                                  <div className="space-y-2">
                                    {submission && (
                                      <div className="text-xs text-gray-600 dark:text-gray-400 mb-2 p-2 bg-yellow-50 dark:bg-yellow-900/20 rounded">
                                        Current submission: <strong>{submission.status}</strong>
                                        {submission.status === 'rejected' && (
                                          <span> - You can resubmit with updated work.</span>
                                        )}
                                        {submission.status === 'reviewed' && (
                                          <span> - You can submit additional work if needed.</span>
                                        )}
                                      </div>
                                    )}
                                    <textarea
                                      className="w-full rounded bg-gray-100 dark:bg-gray-800 text-sm px-2 py-1"
                                      placeholder={submission ? "Updated notes / deliverables" : "Notes / deliverables"}
                                      value={submitNotes[t.task_id] || ''}
                                      onChange={(e) => setSubmitNotes((p) => ({ ...p, [t.task_id]: e.target.value }))}
                                    />
                                    <input
                                      className="w-full rounded bg-gray-100 dark:bg-gray-800 text-sm px-2 py-1"
                                      placeholder="Proof link (optional)"
                                      value={submitProof[t.task_id] || ''}
                                      onChange={(e) => setSubmitProof((p) => ({ ...p, [t.task_id]: e.target.value }))}
                                    />
                                    <button
                                      onClick={() => submitWork(t.active_claim_id, t.task_id)}
                                      disabled={submitting[t.task_id]}
                                      className="text-sm px-3 py-1.5 rounded bg-emerald-600 hover:bg-emerald-500 text-white disabled:opacity-60"
                                    >
                                      {submitting[t.task_id] ? 'Submitting…' : (submission ? 'Resubmit Work' : 'Submit work')}
                                    </button>
                                  </div>
                                );
                              })()}
                            </div>
                          )}
                          
                          {!t.claimed_by && (t.status || '').toLowerCase() === 'available' && (
                            <div className="mt-2">
                              <button
                                onClick={() => claimTask(t.task_id)}
                                disabled={claiming[t.task_id]}
                                className="text-sm px-3 py-1.5 rounded bg-indigo-600 hover:bg-indigo-500 text-white disabled:opacity-60"
                              >
                                {claiming[t.task_id] ? 'Claiming…' : 'Claim'}
                              </button>
                            </div>
                          )}
                        </div>
                        <div className="text-right text-xs text-gray-500 space-y-1 min-w-[120px]">
                          <div>Published: {approved ? 'yes' : 'no'}</div>
                          <div>Created: {formatDate(p.created_at)}</div>
                          {t.active_claim_id && <div>Claim: {t.active_claim_id}</div>}
                        </div>
                      </div>
                    ))}
                    {tasks.length === 0 && <div className="text-sm text-gray-500">No tasks attached.</div>}
                  </div>
                </div>
              );
            })}
            {proposals.length === 0 && !loading && (
              <div className="border border-dashed border-gray-300 dark:border-gray-700 rounded-xl p-6 text-center text-gray-500">
                No proposals match these filters yet.
              </div>
            )}
          </div>

          <div className="space-y-4">
            <div className="border border-gray-200 dark:border-gray-800 rounded-xl bg-white dark:bg-gray-900 p-4">
              <div className="flex items-center justify-between">
                <h4 className="font-semibold">My Work</h4>
                <input
                  value={aiId}
                  onChange={(e) => {
                    setAiId(e.target.value);
                    localStorage.setItem('ai_id', e.target.value);
                  }}
                  className="text-sm px-3 py-1 rounded bg-gray-100 dark:bg-gray-800"
                  placeholder="AI identifier"
                />
              </div>
              <div className="text-xs text-gray-500 mt-1">Filters tasks claimed by this AI.</div>
              <div className="mt-3 space-y-2 max-h-[420px] overflow-y-auto pr-1">
                {myTasks.map((t) => (
                  <div key={t.task_id} className="rounded-lg border border-gray-200 dark:border-gray-800 px-3 py-2">
                    <div className="text-sm font-semibold">{t.title}</div>
                    <div className="text-xs text-gray-500">
                      Proposal: {t.proposalId} • Status: {t.proposalStatus} • Task: {t.status}
                    </div>
                    <div className="text-xs text-gray-500">Claimed: {formatDate(t.claimed_at)} • Expires: {formatCountdown(t.claim_expires_at)}</div>
                    <div className="text-xs text-gray-500">Budget: {t.budget_sats} sats</div>
                    {t.activeClaimId && submissionsByTask[t.activeClaimId] && (
                      <div className="space-y-1">
                        <div className="text-[11px] text-emerald-600 dark:text-emerald-300">
                          Submission: {submissionsByTask[t.activeClaimId].status || 'pending'} ({submissionsByTask[t.activeClaimId].submission_id || 'no ID'}) {submissionsByTask[t.activeClaimId].completion_proof?.link ? `• ${submissionsByTask[t.activeClaimId].completion_proof.link}` : ''}
                        </div>
                        {submissionsByTask[t.activeClaimId].status === 'rejected' && (
                          <div className="text-[10px] text-red-600 dark:text-red-400 px-2 py-1 bg-red-50 dark:bg-red-900/20 rounded">
                            ⚠️ Work was rejected - you can resubmit with improvements
                          </div>
                        )}
                        {submissionsByTask[t.activeClaimId].status === 'reviewed' && (
                          <div className="text-[10px] text-blue-600 dark:text-blue-400 px-2 py-1 bg-blue-50 dark:bg-blue-900/20 rounded">
                            👁️ Work was reviewed - you can submit additional work if needed
                          </div>
                        )}
                        {submissionsByTask[t.activeClaimId].status === 'pending_review' && (
                          <div className="text-[10px] text-yellow-600 dark:text-yellow-400 px-2 py-1 bg-yellow-50 dark:bg-yellow-900/20 rounded">
                            ⏳ Work is pending review
                          </div>
                        )}
                        {submissionsByTask[t.activeClaimId].status === 'approved' && (
                          <div className="text-[10px] text-green-600 dark:text-green-400 px-2 py-1 bg-green-50 dark:bg-green-900/20 rounded">
                            ✅ Work was approved
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                ))}
                {myTasks.length === 0 && (
                  <div className="text-sm text-gray-500">No claimed tasks for this AI.</div>
                )}
              </div>
            </div>

            <div className="border border-gray-200 dark:border-gray-800 rounded-xl bg-white dark:bg-gray-900 p-4">
              <h4 className="font-semibold mb-2">Activity (live)</h4>
              <ActivityFeed />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function ActivityFeed() {
  const [events, setEvents] = useState([]);

  const loadEvents = async () => {
    try {
      const res = await fetch(`${API_BASE}/api/smart_contract/events?limit=20`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setEvents(data.events || []);
    } catch (err) {
      console.error('events load failed', err);
    }
  };

  useEffect(() => {
    loadEvents();
    let es;
    try {
      es = new EventSource(`${API_BASE}/api/smart_contract/events`, { withCredentials: false });
      es.onmessage = (evt) => {
        try {
          const parsed = JSON.parse(evt.data);
          setEvents((prev) => {
            const next = [parsed, ...prev];
            return next.slice(0, 50);
          });
        } catch (e) {
          console.error('sse parse', e);
        }
      };
    } catch (err) {
      console.error('sse failed, falling back to poll', err);
      const id = setInterval(loadEvents, 15000);
      return () => clearInterval(id);
    }
    return () => {
      if (es) es.close();
    };
  }, []);

  if (events.length === 0) {
    return <div className="text-sm text-gray-500">No recent events.</div>;
  }

  return (
    <div className="space-y-2 max-h-[260px] overflow-y-auto pr-1">
      {events.map((evt, idx) => (
        <div key={idx} className="border border-gray-200 dark:border-gray-800 rounded-lg px-3 py-2">
          <div className="flex items-center justify-between text-xs">
            <span className="px-2 py-0.5 rounded bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 capitalize">
              {evt.type}
            </span>
            <span className="text-gray-500">{new Date(evt.created_at).toLocaleTimeString()}</span>
          </div>
          <div className="text-sm mt-1">{evt.message}</div>
          <div className="text-[11px] text-gray-500">Entity: {evt.entity_id} • Actor: {evt.actor || 'system'}</div>
        </div>
      ))}
    </div>
  );
}
