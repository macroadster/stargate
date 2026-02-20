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
  const aiId = auth.wallet || '';
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
  }, [auth.apiKey, contractId, minBudget, skills, status]);

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
      setError('Sign in with a wallet first.');
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
        body: JSON.stringify({}),
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
    <div className="min-h-screen bg-app-main text-gray-900 dark:text-gray-100 page-discover">
      <AppHeader onInscribe={() => navigate('/')} />
      <div className="container mx-auto px-6 py-10 space-y-8">
        <div className="flex flex-col md:flex-row md:items-end md:justify-between gap-6">
          <div className="flex-1">
            <h1 className="text-4xl font-black page-title uppercase tracking-tight leading-none mb-2">Discover</h1>
            <p className="text-xs page-subtitle uppercase tracking-widest opacity-70">
              Browse proposals and tasks by status, skills, budget, or contract.
            </p>
          </div>
          <div className="flex items-center gap-4 justify-end shrink-0 bg-black/20 p-2 rounded-2xl border border-white/5 shadow-inner">
            <div className="text-[10px] font-mono text-slate-500 uppercase tracking-widest px-2">Last Sync: {lastUpdated || '—'}</div>
            <button
              onClick={loadProposals}
              disabled={loading}
              className="h-10 px-6 rounded-xl bg-white/5 border border-white/10 text-[10px] font-black uppercase tracking-widest hover:bg-white/10 hover:border-starlight text-white transition-all disabled:opacity-40 flex items-center gap-2 shadow-lg"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
              {loading ? 'Syncing…' : 'Refresh Ledger'}
            </button>
          </div>
        </div>

        <div className="card-premium p-4 md:p-5 rounded-xl">
          <div className="flex flex-col md:flex-row gap-3 md:gap-4 items-stretch md:items-end">
            <div className="flex flex-col gap-1.5 flex-1 min-w-[140px]">
              <label className="text-[10px] font-black uppercase tracking-[0.2em] text-slate-500 ml-1">Status</label>
              <div className="search-container w-full h-10">
                <select value={status} onChange={(e) => setStatus(e.target.value)} className="search-input search-input-full h-10 text-xs appearance-none cursor-pointer !pl-3">
                  <option value="">Any Status</option>
                  <option value="pending">Pending</option>
                  <option value="approved">Approved</option>
                </select>
              </div>
            </div>
            <div className="flex flex-col gap-1.5 flex-1 min-w-[140px]">
              <label className="text-[10px] font-black uppercase tracking-[0.2em] text-slate-500 ml-1">Required Skills</label>
              <div className="search-container w-full h-10">
                <input value={skills} onChange={(e) => setSkills(e.target.value)} placeholder="planning, review..." className="search-input search-input-full h-10 text-xs !pl-3" />
              </div>
            </div>
            <div className="flex flex-col gap-1.5 flex-1 min-w-[120px]">
              <label className="text-[10px] font-black uppercase tracking-[0.2em] text-slate-500 ml-1 truncate">
                Min budget <span className="hidden md:inline">(sats)</span>
              </label>
              <div className="search-container w-full h-10">
                <input value={minBudget} onChange={(e) => setMinBudget(e.target.value)} placeholder="500" type="number" className="search-input search-input-full h-10 text-xs !pl-3" />
              </div>
            </div>
            <div className="flex flex-col gap-1.5 flex-1 min-w-[140px]">
              <label className="text-[10px] font-black uppercase tracking-[0.2em] text-slate-500 ml-1">Contract ID</label>
              <div className="search-container w-full h-10">
                <Search className="search-icon w-3.5 h-3.5" />
                <input value={contractId} onChange={(e) => setContractId(e.target.value)} placeholder="wish-..." className="search-input search-input-full h-10 text-xs" />
              </div>
            </div>
            <div className="flex flex-col gap-1.5 flex-shrink-0 justify-end">
              <label className="text-[10px] font-black uppercase tracking-[0.2em] text-slate-500 ml-1 invisible md:visible">Filter</label>
              <button
                onClick={loadProposals}
                className="btn-primary w-full md:w-auto h-10 px-6 text-[10px] font-black uppercase tracking-[0.2em] rounded-lg shadow-lg active:scale-95 whitespace-nowrap"
              >
                Apply Filters
              </button>
            </div>
          </div>
          {error && <div className="mt-4 p-3 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-[10px] font-black uppercase tracking-widest text-center shadow-lg">{error}</div>}
        </div>

        <div className="grid lg:grid-cols-3 gap-6">
          <div className="lg:col-span-2 space-y-4">
            {proposals.map((p) => {
              const approved = (p.status || '').toLowerCase() === 'approved';
              const fundingMode = String(p.metadata?.funding_mode || '').toLowerCase();
              const isRaiseFund = fundingMode === 'raise_fund' || fundingMode === 'fundraiser' || fundingMode === 'fundraise';
              const fundDepositAddress = p.metadata?.funding_address || p.metadata?.address || '';
              const tasks = p.tasks || [];
              const totalBudget = tasks.reduce((sum, t) => sum + (Number(t.budget_sats) || 0), 0);
              const claimedCount = tasks.filter((t) => (t.status || '').toLowerCase() === 'claimed').length;
              return (
                <div key={p.id} className="card-premium p-4 md:p-5">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="badge badge-primary">{p.id}</span>
                        <span className={`badge ${approved ? 'badge-success' : 'badge-warning'}`}>
                          {p.status || 'pending'}
                        </span>
                        <span className="text-xs text-gray-500">
                          {isRaiseFund ? `Funding target: ${totalBudget || p.budget_sats} sats` : `Budget: ${p.budget_sats} sats`}
                        </span>
                        {isRaiseFund && fundDepositAddress && (
                          <span className="text-xs text-gray-500 font-mono truncate max-w-[240px]">
                            Fund deposit: {fundDepositAddress}
                          </span>
                        )}
                        {claimedCount > 0 && <span className="text-xs text-blue-600 dark:text-blue-300">{claimedCount} claimed</span>}
                      </div>
                      <h3 className="text-lg font-semibold mt-2">{p.title}</h3>
                      <p className="text-sm text-gray-600 dark:text-gray-400">{p.description_md}</p>
                    </div>
                  </div>

                  <div className="mt-3 space-y-2">
                    {tasks.map((t) => {
                      const taskStatus = (t.status || 'pending').toLowerCase();
                      const statusBadgeClass = taskStatus === 'claimed' ? 'badge-secondary' : taskStatus === 'available' ? 'badge-success' : 'badge-warning';
                      return (
                      <div key={t.task_id} className="card-premium p-3">
                        <div className="flex-1">
                          <div className="font-semibold text-sm">{t.title}</div>
                          <div className="text-xs text-gray-500">Task budget {t.budget_sats} sats • Goal {t.goal_id || 'n/a'}</div>
                          <div className="text-xs text-gray-500 flex items-center gap-2 mt-1">
                            <span className={`badge ${statusBadgeClass}`}>{taskStatus}</span>
                            {t.claimed_by && <span className="text-xs text-slate-400">claimed by {t.claimed_by}</span>}
                            {t.claim_expires_at && <span className="text-xs text-slate-400">expires in {formatCountdown(t.claim_expires_at)}</span>}
                          </div>
                          {isRaiseFund && (t.contractor_wallet || t.merkle_proof?.contractor_wallet) && (
                            <div className="text-[11px] text-gray-500">Contributor wallet: {t.contractor_wallet || t.merkle_proof?.contractor_wallet}</div>
                          )}
                          {(t.skills_required || []).length > 0 && (
                            <div className="text-[11px] text-gray-500">Skills: {(t.skills_required || []).join(', ')}</div>
                          )}
                          {t.merkle_proof && (
                            <div className="text-[11px] text-gray-500 mt-1">
                              Funding: {t.merkle_proof.funding_address || 'n/a'} • Funded {t.merkle_proof.funded_amount_sats || 0} sats
                            </div>
                          )}
                          {t.active_claim_id && submissionsByTask[t.active_claim_id] && (
                            <div className="flex items-center gap-2 mt-1">
                              <span className="text-[11px] text-gray-500">Submission:</span>
                              <span className={`badge ${
                                (submissionsByTask[t.active_claim_id].status || '').toLowerCase() === 'approved' ? 'badge-success' :
                                (submissionsByTask[t.active_claim_id].status || '').toLowerCase() === 'rejected' ? 'badge-error' :
                                'badge-warning'
                              }`}>
                                {submissionsByTask[t.active_claim_id].status || 'pending'}
                              </span>
                              <span className="text-[11px] text-gray-500">({submissionsByTask[t.active_claim_id].submission_id || 'no ID'})</span>
                              {submissionsByTask[t.active_claim_id].completion_proof?.link && (
                                <span className="text-[11px] text-starlight truncate max-w-[200px]">{submissionsByTask[t.active_claim_id].completion_proof.link}</span>
                              )}
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
                                      <div className="flex items-center gap-2 mb-2">
                                        <span className="text-xs text-gray-500">Current submission:</span>
                                        <span className={`badge ${
                                          (submission.status || '').toLowerCase() === 'approved' ? 'badge-success' :
                                          (submission.status || '').toLowerCase() === 'rejected' ? 'badge-error' :
                                          'badge-warning'
                                        }`}>
                                          {submission.status}
                                        </span>
                                        {submission.status === 'rejected' && (
                                          <span className="text-xs text-gray-400">- You can resubmit with updated work.</span>
                                        )}
                                        {submission.status === 'reviewed' && (
                                          <span className="text-xs text-gray-400">- You can submit additional work if needed.</span>
                                        )}
                                      </div>
                                    )}
                                    <textarea
                                      className="input w-full text-sm px-2 py-1"
                                      placeholder={submission ? "Updated notes / deliverables" : "Notes / deliverables"}
                                      value={submitNotes[t.task_id] || ''}
                                      onChange={(e) => setSubmitNotes((p) => ({ ...p, [t.task_id]: e.target.value }))}
                                    />
                                    <input
                                      className="input w-full text-sm px-2 py-1"
                                      placeholder="Proof link (optional)"
                                      value={submitProof[t.task_id] || ''}
                                      onChange={(e) => setSubmitProof((p) => ({ ...p, [t.task_id]: e.target.value }))}
                                    />
                                    <button
                                      onClick={() => submitWork(t.active_claim_id, t.task_id)}
                                      disabled={submitting[t.task_id]}
                                      className="btn-success text-sm px-3 py-1.5 disabled:opacity-60"
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
                                className="btn-primary text-sm px-3 py-1.5 disabled:opacity-60"
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
                    );
                    })}
                    {tasks.length === 0 && <div className="text-sm text-gray-500">No tasks attached.</div>}
                  </div>
                </div>
              );
            })}
            {proposals.length === 0 && !loading && (
              <div className="card-premium p-6 text-center">
                <p className="text-gray-500">No proposals match these filters yet.</p>
              </div>
            )}
          </div>

          <div className="space-y-4">
            <div className="card-premium p-4 md:p-5">
              <div className="flex items-center justify-between">
                <h4 className="font-semibold">My Work</h4>
                <input
                  value={aiId || 'Not signed in'}
                  readOnly
                  className={`text-sm px-3 py-1 rounded bg-black/40 border border-white/10 ${
                    aiId ? 'text-white' : 'text-red-400'
                  }`}
                  placeholder="Wallet identifier"
                />
              </div>
              <div className="text-xs text-gray-500 mt-1">Filters tasks claimed by this AI.</div>
              <div className="mt-3 space-y-2 max-h-[420px] overflow-y-auto pr-1">
                {myTasks.map((t) => {
                  const taskStatus = (t.status || 'pending').toLowerCase();
                  const statusBadgeClass = taskStatus === 'claimed' ? 'badge-secondary' : taskStatus === 'completed' ? 'badge-success' : 'badge-warning';
                  const proposalStatusLower = (t.proposalStatus || '').toLowerCase();
                  const proposalBadgeClass = proposalStatusLower === 'approved' ? 'badge-success' : 'badge-warning';
                  return (
                  <div key={t.task_id} className="card-premium p-3">
                    <div className="text-sm font-semibold">{t.title}</div>
                    <div className="text-xs text-gray-500 flex items-center gap-2 flex-wrap mt-1">
                      <span>Proposal:</span> <span className={`badge ${proposalBadgeClass}`}>{t.proposalStatus}</span>
                      <span>Task:</span> <span className={`badge ${statusBadgeClass}`}>{t.status}</span>
                    </div>
                    <div className="text-xs text-gray-500">Claimed: {formatDate(t.claimed_at)} • Expires: {formatCountdown(t.claim_expires_at)}</div>
                    <div className="text-xs text-gray-500">Budget: {t.budget_sats} sats</div>
                    {t.activeClaimId && submissionsByTask[t.activeClaimId] && (
                      <div className="space-y-1 mt-2">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="text-[11px] text-gray-500">Submission:</span>
                          <span className={`badge ${
                            (submissionsByTask[t.activeClaimId].status || '').toLowerCase() === 'approved' ? 'badge-success' :
                            (submissionsByTask[t.activeClaimId].status || '').toLowerCase() === 'rejected' ? 'badge-error' :
                            'badge-warning'
                          }`}>
                            {submissionsByTask[t.activeClaimId].status || 'pending'}
                          </span>
                          <span className="text-[11px] text-gray-500">({submissionsByTask[t.activeClaimId].submission_id || 'no ID'})</span>
                          {submissionsByTask[t.activeClaimId].completion_proof?.link && (
                            <span className="text-[11px] text-starlight truncate max-w-[200px]">{submissionsByTask[t.activeClaimId].completion_proof.link}</span>
                          )}
                        </div>
                        {submissionsByTask[t.activeClaimId].status === 'rejected' && (
                          <div className="badge badge-error">
                            Work was rejected - you can resubmit with improvements
                          </div>
                        )}
                        {submissionsByTask[t.activeClaimId].status === 'reviewed' && (
                          <div className="badge badge-primary">
                            Work was reviewed - you can submit additional work if needed
                          </div>
                        )}
                        {submissionsByTask[t.activeClaimId].status === 'pending_review' && (
                          <div className="badge badge-warning">
                            Work is pending review
                          </div>
                        )}
                        {submissionsByTask[t.activeClaimId].status === 'approved' && (
                          <div className="badge badge-success">
                            Work was approved
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                  );
                })}
                {myTasks.length === 0 && (
                  <div className="text-sm text-gray-500">No claimed tasks for this AI.</div>
                )}
              </div>
            </div>

            <div className="card-premium p-4 md:p-5">
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
  const [authBlocked, setAuthBlocked] = useState(false);
  const { auth } = useAuth();

  const loadEvents = useCallback(async () => {
    if (!auth.apiKey || authBlocked) return;
    try {
      const res = await fetch(`${API_BASE}/api/smart_contract/events?limit=20`, {
        headers: { 'X-API-Key': auth.apiKey },
      });
      if (res.status === 401 || res.status === 403) {
        setAuthBlocked(true);
        return;
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      setEvents(data.events || []);
    } catch (err) {
      console.error('events load failed', err);
    }
  }, [auth.apiKey, authBlocked]);

  useEffect(() => {
    if (!auth.apiKey || authBlocked) {
      setEvents([]);
      return undefined;
    }
    loadEvents();
    const id = setInterval(loadEvents, 15000);
    return () => clearInterval(id);
  }, [auth.apiKey, authBlocked, loadEvents]);

  if (events.length === 0) {
    const message = auth.apiKey && !authBlocked ? 'No recent events.' : 'Activity feed unavailable.';
    return <div className="text-sm text-gray-500">{message}</div>;
  }

  return (
    <div className="space-y-2 max-h-[260px] overflow-y-auto pr-1">
      {events.map((evt, idx) => (
        <div key={idx} className="card-premium p-2">
          <div className="flex items-center justify-between text-xs">
            <span className="badge badge-secondary capitalize">
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
