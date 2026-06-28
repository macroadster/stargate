import React, { useLayoutEffect, useState, useEffect, useMemo, useCallback } from 'react';
import toast from 'react-hot-toast';
import { API_BASE } from '../../apiBase';
import { apiFetch } from '../../utils/api';
import { useAuth } from '../../context/AuthContext';
import { useInscriptionNetwork } from './useInscriptionNetwork';
import {
  looksLikeRaiseFund,
  expandContractCandidates,
  isPlaceholderAddress,
  isConfirmedContract as checkConfirmedContract,
  resolveModalImage,
  parseStegoManifest,
  submissionTimestamp,
} from './inscriptionUtils';

/**
 * All non-JSX state, effects, and actions for InscriptionModal.
 */
export function useInscriptionModalState(inscription, initialTab = 'content') {
  const { auth } = useAuth();
  const [network, setNetwork] = useInscriptionNetwork(inscription, auth.wallet);
  const [activeTab, setActiveTab] = useState(initialTab);
  const [monoContent, setMonoContent] = useState(true);
  const [proposalItems, setProposalItems] = useState([]);
  const [isLoadingProposals, setIsLoadingProposals] = useState(false);
  const [proposalError, setProposalError] = useState('');
  const [approvingId, setApprovingId] = useState('');
  const [submissions, setSubmissions] = useState({});
  const [submissionsList, setSubmissionsList] = useState([]);
  const submissionsKeyRef = React.useRef('');
  const proposalsKeyRef = React.useRef('');
  const [dashboardFilter, setDashboardFilter] = useState('all');
  const [dashboardSort, setDashboardSort] = useState('status');
  const [psbtForm, setPsbtForm] = useState({
    contractorWallet: '',
    fundraiserWallet: '',
    pixelHash: '',
    budgetSats: '',
    feeRate: '1',
    changeAddress: '',
    contractId: '',
    taskId: '',
    includeDonation: true,
  });
  const [psbtResult, setPsbtResult] = useState(null);
  const [psbtError, setPsbtError] = useState('');
  const [psbtLoading, setPsbtLoading] = useState(false);
  const [authBlocked, setAuthBlocked] = useState(false);
  const [copiedPsbt, setCopiedPsbt] = useState('');
  const [showPsbtQr, setShowPsbtQr] = useState(false);
  const [stegoPayload, setStegoPayload] = useState(null);
  const [stegoPayloadLoading, setStegoPayloadLoading] = useState(false);
  const [stegoPayloadError, setStegoPayloadError] = useState('');
  const [scanMessage, setScanMessage] = useState('');
  const [scanLoading, setScanLoading] = useState(false);
  const [scanError, setScanError] = useState('');
  const [scanAttempted, setScanAttempted] = useState(false);
  const lastFetchedKeyRef = React.useRef('');
  const hasFetchedRef = React.useRef(false);
  const refreshIntervalRef = React.useRef(null);
  const scrollContainerRef = React.useRef(null);
  const deliverablesScrollRef = React.useRef(0);
  const [reworkRequests, setReworkRequests] = useState([]);
  const [isLoadingRework, setIsLoadingRework] = useState(false);
  const [showReworkForm, setShowReworkForm] = useState(false);
  const [reworkNotes, setReworkNotes] = useState('');
  const [isSubmittingRework, setIsSubmittingRework] = useState(false);



  useEffect(() => {
    setShowPsbtQr(false);
  }, [psbtResult]);
  const inscriptionMessageRaw = inscription.text || inscription.metadata?.embedded_message || inscription.metadata?.extracted_message || '';
  const inscriptionAddressRaw = inscription.address ?? inscription.metadata?.address ?? '';
  const contractCandidates = useMemo(
    () => expandContractCandidates(inscription),
    [
      inscription.contract_id,
      inscription.id,
      inscription.metadata?.contract_id,
      inscription.metadata?.visible_pixel_hash,
      inscription.metadata?.ingestion_id,
    ],
  );
  const contractKey = useMemo(() => contractCandidates.join('|'), [contractCandidates]);
  const allTasks = useMemo(() => {
    const collected = [];
    proposalItems.forEach((p) => {
      const tasks = Array.isArray(p.tasks) ? p.tasks : [];
      tasks.forEach((t) =>
        collected.push({
          ...t,
          proposalId: p.id,
          visible_pixel_hash: p.visible_pixel_hash || t.visible_pixel_hash,
          contractor_wallet: t.contractor_wallet || t.merkle_proof?.contractor_wallet || p.metadata?.contractor_wallet,
        }),
      );
    });
    return collected;
  }, [proposalItems]);
  const approvedProposal = useMemo(
    () =>
      proposalItems.find((p) => ['approved', 'published'].includes((p.status || '').toLowerCase())) || null,
    [proposalItems],
  );
  const approvedContractId = useMemo(
    () =>
      approvedProposal?.visible_pixel_hash ||
      approvedProposal?.metadata?.contract_id ||
      approvedProposal?.metadata?.visible_pixel_hash ||
      '',
    [approvedProposal],
  );
  const primaryContractId = useMemo(
    () =>
      psbtForm.contractId ||
      approvedContractId ||
      contractCandidates[0] ||
      inscription.contract_id ||
      inscription.id,
    [psbtForm.contractId, approvedContractId, contractCandidates, inscription.contract_id, inscription.id],
  );
  const psbtTasks = useMemo(() => {
    if (!approvedProposal) return allTasks;
    return allTasks.filter((t) => t.proposalId === approvedProposal.id);
  }, [allTasks, approvedProposal]);
  const deliverableTasks = useMemo(() => {
    if (!approvedProposal) return [];
    const byProposal = psbtTasks.filter((t) => t.proposalId === approvedProposal.id);
    return byProposal.length > 0 ? byProposal : [];
  }, [psbtTasks, approvedProposal]);
  const approvedBudgetsTotal = useMemo(() => {
    const tasks = psbtTasks.length > 0 ? psbtTasks : allTasks;
    // Prefer summing all tasks attached to the approved proposal; fall back to any tasks if none.
    const list = tasks.length > 0 ? tasks : allTasks;
    if (list.length === 0) return 0;
    return list.reduce((sum, t) => sum + (Number(t.budget_sats) || 0), 0);
  }, [psbtTasks, allTasks]);
  const payoutSummaries = useMemo(() => {
    const tasks = deliverableTasks.length > 0 ? deliverableTasks : psbtTasks;
    if (!tasks.length) return [];
    const totals = new Map();
    tasks.forEach((t) => {
      const wallet = (t.contractor_wallet || t.merkle_proof?.contractor_wallet || '').trim() || 'Unknown wallet';
      const existing = totals.get(wallet) || 0;
      totals.set(wallet, existing + (Number(t.budget_sats) || 0));
    });
    return Array.from(totals.entries()).map(([wallet, total]) => ({ wallet, total }));
  }, [deliverableTasks, psbtTasks]);

  let parsedPayload = null;
  if (typeof inscriptionMessageRaw === 'string') {
    try {
      const maybe = JSON.parse(inscriptionMessageRaw);
      if (maybe && typeof maybe === 'object') {
        parsedPayload = maybe;
      }
    } catch {
      // not json
    }
  }

  const stegoManifest = useMemo(
    () => parseStegoManifest(scanMessage || inscription?.metadata?.extracted_message || ''),
    [inscription?.metadata?.extracted_message, scanMessage],
  );

  const stegoPayloadCid = useMemo(() => {
    if (!scanAttempted) return '';
    return stegoManifest?.payload_cid || '';
  }, [scanAttempted, stegoManifest]);

  const stegoProposal = useMemo(() => {
    if (!stegoPayload?.proposal) return null;
    return stegoPayload.proposal;
  }, [stegoPayload]);

  const stegoTasks = useMemo(() => {
    return Array.isArray(stegoPayload?.tasks) ? stegoPayload.tasks : [];
  }, [stegoPayload]);

  const stegoProposalStatus = useMemo(() => {
    if (!stegoProposal) return '';
    const id = stegoProposal.id;
    const match = proposalItems.find(
      (p) =>
        p.id === id ||
        p.visible_pixel_hash === id ||
        p.metadata?.visible_pixel_hash === id ||
        p.metadata?.contract_id === id,
    );
    return match?.status || '';
  }, [proposalItems, stegoProposal]);

  const stegoTaskStatusMap = useMemo(() => {
    const map = new Map();
    allTasks.forEach((t) => {
      if (t.task_id) {
        map.set(t.task_id, t.status || '');
      }
    });
    return map;
  }, [allTasks]);

  const hiddenMessageText = useMemo(() => {
    if (stegoPayload) {
      return JSON.stringify(stegoPayload, null, 2);
    }
    return scanMessage || inscription?.metadata?.extracted_message || '';
  }, [stegoPayload, scanMessage, inscription?.metadata?.extracted_message]);

  const inscriptionMessage = parsedPayload?.message || inscriptionMessageRaw;
  const inscriptionAddress = parsedPayload?.address ?? inscriptionAddressRaw;
  const fundingMode = (
    inscription.metadata?.funding_mode ||
    parsedPayload?.funding_mode ||
    approvedProposal?.metadata?.funding_mode ||
    ''
  ).toLowerCase();
  const inferredRaiseFund =
    looksLikeRaiseFund(inscriptionMessage) ||
    looksLikeRaiseFund(approvedProposal?.title) ||
    looksLikeRaiseFund(approvedProposal?.description_md);
  const isRaiseFund =
    fundingMode === 'raise_fund' ||
    fundingMode === 'fundraiser' ||
    fundingMode === 'fundraise' ||
    inferredRaiseFund;
  const selectedTask = useMemo(() => {
    const sourceTasks = psbtTasks.length > 0 ? psbtTasks : allTasks;
    if (psbtForm.taskId) return sourceTasks.find((t) => t.task_id === psbtForm.taskId) || sourceTasks[0];
    const withFunding = sourceTasks.find((t) => t?.merkle_proof?.funding_address);
    return withFunding || sourceTasks[0];
  }, [psbtForm.taskId, psbtTasks, allTasks]);
  const resolvePsbtContractId = useCallback(() => {
    const candidate =
      psbtForm.contractId ||
      approvedContractId ||
      inscription.metadata?.visible_pixel_hash ||
      selectedTask?.merkle_proof?.visible_pixel_hash ||
      selectedTask?.contract_id ||
      primaryContractId;
    if (!candidate) return '';
    if (candidate.startsWith('proposal-')) {
      if (approvedContractId) return approvedContractId;
      if (inscription.metadata?.visible_pixel_hash) return inscription.metadata.visible_pixel_hash;
      if (selectedTask?.merkle_proof?.visible_pixel_hash) return selectedTask.merkle_proof.visible_pixel_hash;
    }
    return candidate;
  }, [
    psbtForm.contractId,
    approvedContractId,
    inscription.metadata?.visible_pixel_hash,
    selectedTask?.merkle_proof?.visible_pixel_hash,
    selectedTask?.contract_id,
    primaryContractId,
  ]);
  const isPlaceholderAddressCb = isPlaceholderAddress;
  const fundDepositAddress = useMemo(() => {
    const addr = (value) => (value || '').trim();
    const candidates = isRaiseFund
      ? [
          approvedProposal?.metadata?.funding_address,
          approvedProposal?.metadata?.payout_address,
          approvedProposal?.metadata?.fundraiser_wallet,
          inscription.metadata?.funding_address,
          inscription.metadata?.payout_address,
          inscription.metadata?.fundraiser_wallet,
          inscription.metadata?.payer_address,
          selectedTask?.merkle_proof?.funding_address,
          inscriptionAddress,
        ]
      : [
          approvedProposal?.metadata?.funding_address,
          inscription.metadata?.funding_address,
          selectedTask?.merkle_proof?.funding_address,
    ];
    const cleaned = candidates.map(addr);
    const picked = cleaned.find((value) => value && !isPlaceholderAddressCb(value));
    return picked || '';
  }, [
    isRaiseFund,
    approvedProposal?.metadata?.funding_address,
    approvedProposal?.metadata?.payout_address,
    approvedProposal?.metadata?.fundraiser_wallet,
    inscription.metadata?.funding_address,
    inscription.metadata?.payout_address,
    inscription.metadata?.fundraiser_wallet,
    inscription.metadata?.payer_address,
    selectedTask?.merkle_proof?.funding_address,
    inscriptionAddress,
    isPlaceholderAddressCb,
  ]);
  const resolvedContractorWallet = useMemo(
    () =>
      psbtForm.contractorWallet ||
      selectedTask?.contractor_wallet ||
      inscription.metadata?.contractor_wallet ||
      '',
    [psbtForm.contractorWallet, selectedTask?.contractor_wallet, inscription.metadata?.contractor_wallet],
  );
  const resolvedFundraiserWallet = useMemo(() => {
    const explicit =
      psbtForm.fundraiserWallet ||
      approvedProposal?.metadata?.fundraiser_wallet ||
      approvedProposal?.metadata?.payout_address ||
      inscription.metadata?.fundraiser_wallet ||
      inscription.metadata?.payout_address ||
      '';
    return explicit || fundDepositAddress || '';
  }, [
    psbtForm.fundraiserWallet,
    approvedProposal?.metadata?.fundraiser_wallet,
    approvedProposal?.metadata?.payout_address,
    inscription.metadata?.fundraiser_wallet,
    inscription.metadata?.payout_address,
    fundDepositAddress,
  ]);
  const textContent = inscriptionMessage || '';
  const pixelHash = inscription.metadata?.visible_pixel_hash || 
                   selectedTask?.merkle_proof?.visible_pixel_hash || 
                   '';
  const confidenceValue = Number(inscription.metadata?.confidence || 0);
  const confidencePercent = Math.round(confidenceValue * 100);
  const isConfirmedContract = checkConfirmedContract(inscription);
  const isFundingConfirmed = useMemo(() => {
    const tasks = allTasks.length > 0 ? allTasks : psbtTasks;
    return tasks.some(
      (task) =>
        (task?.merkle_proof?.confirmation_status || task?.confirmation_status || '').toLowerCase() ===
        'confirmed'
    );
  }, [allTasks, psbtTasks]);
  const hasFundingTxId = Boolean(inscription.metadata?.funding_txid);
  const isContractLocked = isConfirmedContract || isFundingConfirmed || hasFundingTxId;
  const normalizeAddress = (value) => (value || '').trim().toLowerCase();
  
  const {
    modalImageSource,
    scanImageSource,
    isHtmlContent,
    isSvgContent,
    sandboxSrc,
    inlineDoc,
  } = resolveModalImage(inscription);
  const fetchWithTimeout = React.useCallback(async (url, options = {}, timeoutMs = 6000) => {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const headers = {
        ...(options.headers || {}),
        ...(auth.apiKey ? { 'X-API-Key': auth.apiKey } : {}),
      };
      return await apiFetch(url, { ...options, headers, signal: controller.signal });
    } finally {
      clearTimeout(timer);
    }
  }, [auth.apiKey]);

  useEffect(() => {
    let alive = true;
    const fetchPayload = async () => {
      if (!stegoPayloadCid) {
        setStegoPayload(null);
        setStegoPayloadError('');
        return;
      }
      setStegoPayloadLoading(true);
      setStegoPayloadError('');
      try {
        const res = await fetchWithTimeout(`${API_BASE}/api/smart_contract/stego/payload/${stegoPayloadCid}`, {}, 6000);
        if (!res.ok) {
          throw new Error(`payload fetch failed: ${res.status}`);
        }
        const data = await res.json();
        if (alive) {
          setStegoPayload(data);
        }
      } catch (err) {
        if (alive) {
          setStegoPayload(null);
          setStegoPayloadError(err.message || 'payload fetch failed');
        }
      } finally {
        if (alive) {
          setStegoPayloadLoading(false);
        }
      }
    };
    fetchPayload();
    return () => {
      alive = false;
    };
  }, [stegoPayloadCid, fetchWithTimeout]);

  useEffect(() => {
    let alive = true;
    const runScan = async () => {
      if (!scanImageSource) {
        setScanMessage('');
        setScanError('');
        setScanAttempted(true);
        return;
      }
      setScanLoading(true);
      setScanError('');
      try {
        const imageRes = await fetchWithTimeout(scanImageSource, {}, 10000);
        if (!imageRes.ok) {
          throw new Error(`image fetch failed: ${imageRes.status}`);
        }
        const blob = await imageRes.blob();
        
        // Align filename extension with actual MIME type
        let extension = 'png';
        if (blob.type === 'image/jpeg') extension = 'jpg';
        else if (blob.type === 'image/gif') extension = 'gif';
        else if (blob.type === 'image/webp') extension = 'webp';
        else if (blob.type === 'image/bmp') extension = 'bmp';
        
        const form = new FormData();
        form.append('image', blob, `stego.${extension}`);
        const scanRes = await fetchWithTimeout(`${API_BASE}/bitcoin/v1/extract`, { method: 'POST', body: form }, 15000);
        if (!scanRes.ok) {
          throw new Error(`scan failed: ${scanRes.status}`);
        }
        const data = await scanRes.json();
        const message = data?.extraction_result?.message || '';
        if (alive) {
          setScanMessage(message);
        }
      } catch (err) {
        if (alive) {
          setScanMessage('');
          setScanError(err.message || 'scan failed');
        }
      } finally {
        if (alive) {
          setScanLoading(false);
          setScanAttempted(true);
        }
      }
    };
    runScan();
    return () => {
      alive = false;
    };
  }, [scanImageSource, fetchWithTimeout]);

  const loadProposals = React.useCallback(async (options = {}) => {
    const { showLoading = false } = options;
    if (authBlocked || !contractCandidates.length) return;
    lastFetchedKeyRef.current = contractKey;
    if (showLoading) {
      setIsLoadingProposals(true);
    }
    setProposalError('');
    try {
      const results = await Promise.all(
        contractCandidates.map(async (contractId) => {
          try {
            const res = await fetchWithTimeout(
              `${API_BASE}/api/smart_contract/proposals?contract_id=${encodeURIComponent(contractId)}`,
              {},
              6000
            );
            if (res.status === 401 || res.status === 403) return { status: res.status };
            if (!res.ok) return { error: res.status };
            return await res.json();
          } catch (e) {
            return { error: e.message };
          }
        })
      );

      // Check for auth blocks
      if (results.some((r) => r.status === 401 || r.status === 403)) {
        setAuthBlocked(true);
        setProposalItems([]);
        setSubmissions({});
        return;
      }

      // Merge results
      const allProposals = [];
      const allSubmissionsList = [];

      results.forEach((data) => {
        if (data && !data.error && !data.status) {
          if (Array.isArray(data.proposals)) {
            allProposals.push(...data.proposals);
          }
          if (Array.isArray(data.submissions)) {
            allSubmissionsList.push(...data.submissions);
          } else if (data.submissions) {
            allSubmissionsList.push(...Object.values(data.submissions));
          }
        }
      });

      // Deduplicate proposals
      const uniqueProposals = new Map();
      allProposals.forEach((p) => {
        if (!uniqueProposals.has(p.id)) {
          uniqueProposals.set(p.id, p);
        }
      });

      let items = Array.from(uniqueProposals.values()).filter((p) => {
        const tasks = Array.isArray(p.tasks) ? p.tasks : [];
        const suggested = Array.isArray(p.metadata?.suggested_tasks) ? p.metadata.suggested_tasks : [];
        const hasMatchingTasks = [...tasks, ...suggested].some((t) => contractCandidates.includes(t.contract_id));
        const idMatch = contractCandidates.includes(p.id);
        const metaContract = p.metadata?.contract_id && contractCandidates.includes(p.metadata.contract_id);
        const ingestMatch = p.metadata?.ingestion_id && contractCandidates.includes(p.metadata.ingestion_id);
        return idMatch || hasMatchingTasks || metaContract || ingestMatch;
      });

      // Create comprehensive submission mapping with all IDs
      const submissionsByKey = {};
      const uniqueSubmissions = new Map();
      allSubmissionsList.forEach((s) => {
        if (!uniqueSubmissions.has(s.submission_id)) {
          uniqueSubmissions.set(s.submission_id, s);
        }
      });
      const submissionList = Array.from(uniqueSubmissions.values());

      const submissionTime = (submission) => {
        const raw = submission?.submitted_at || submission?.created_at;
        const parsed = Date.parse(raw || '');
        return Number.isNaN(parsed) ? 0 : parsed;
      };
      submissionList.sort((a, b) => submissionTime(b) - submissionTime(a));
      submissionList.forEach((s) => {
        // Map by submission_id (primary key for API calls)
        if (s.submission_id) {
          submissionsByKey[s.submission_id] = s;
        }
        // Also map by task_id and claim_id for lookup, but prioritize newest by created_at
        if (s.task_id) {
          const existing = submissionsByKey[s.task_id];
          if (!existing || submissionTime(s) > submissionTime(existing)) {
            submissionsByKey[s.task_id] = s;
          }
        }
        if (s.claim_id) {
          const existing = submissionsByKey[s.claim_id];
          if (!existing || submissionTime(s) > submissionTime(existing)) {
            submissionsByKey[s.claim_id] = s;
          }
        }
      });
      const nextSubmissionsKey = submissionList
        .map((s) => `${s.submission_id || ''}:${s.status || ''}:${s.task_id || ''}:${s.claim_id || ''}:${s.created_at || ''}:${s.rejected_at || ''}`)
        .join('|');
      if (nextSubmissionsKey !== submissionsKeyRef.current) {
        submissionsKeyRef.current = nextSubmissionsKey;
        setSubmissions(submissionsByKey);
        if (submissionList.length > 0) {
          setSubmissionsList(submissionList);
        }
      }
      // Sort approved first, then pending/others, preserving matches
      items = items.sort((a, b) => {
        const sa = (a.status || '').toLowerCase();
        const sb = (b.status || '').toLowerCase();
        if (sa === sb) return 0;
        if (sa === 'approved') return -1;
        if (sb === 'approved') return 1;
        return 0;
      });
      const derivedTasks = [];
      items.forEach((p) => {
        const tasks = Array.isArray(p.tasks) ? p.tasks : [];
        tasks.forEach((t) =>
          derivedTasks.push({
            ...t,
            proposalId: p.id,
            visible_pixel_hash: p.visible_pixel_hash || t.visible_pixel_hash,
            contractor_wallet: t.contractor_wallet || p.metadata?.contractor_wallet,
          }),
        );
      });
      const nextProposalsKey = items
        .map((p) => {
          const tasks = Array.isArray(p.tasks) ? p.tasks : (Array.isArray(p.metadata?.suggested_tasks) ? p.metadata.suggested_tasks : []);
          const taskKey = tasks.map((t) => `${t.task_id || ''}:${t.status || ''}:${t.active_claim_id || ''}:${t.claimed_by || ''}`).join(',');
          return `${p.id || ''}:${p.status || ''}:${taskKey}`;
        })
        .join('|');
      if (nextProposalsKey !== proposalsKeyRef.current) {
        proposalsKeyRef.current = nextProposalsKey;
        setProposalItems(items);
      }
      if (items.length > 0) {
        const approved = items.find((p) => ['approved', 'published'].includes((p.status || '').toLowerCase()));
        const preferredList = approved ? derivedTasks.filter((t) => t.proposalId === approved.id) : derivedTasks;
        const first = approved || items[0];
        const preferredHash = first.visible_pixel_hash || psbtForm.pixelHash || inscription.metadata?.visible_pixel_hash || '';
        const firstTaskWithFunding = preferredList.find((t) => t?.merkle_proof?.funding_address);
        const firstTask = firstTaskWithFunding || preferredList[0];
        const preferredContractId =
          firstTask?.contract_id ||
          approved?.visible_pixel_hash ||
          approved?.metadata?.contract_id ||
          approved?.metadata?.visible_pixel_hash ||
          first.visible_pixel_hash ||
          first.metadata?.contract_id ||
          primaryContractId;
        const defaultBudget = preferredList.length > 0
          ? preferredList.reduce((sum, t) => sum + (Number(t.budget_sats) || 0), 0)
          : firstTask?.budget_sats || first.budget_sats || '';
        setPsbtForm((prev) => {
          const prevContractId = prev.contractId;
          const shouldReplaceContractId = !prevContractId || prevContractId.startsWith('proposal-');
          return {
            ...prev,
            pixelHash: preferredHash,
            contractId: shouldReplaceContractId ? preferredContractId : prevContractId,
            budgetSats: prev.budgetSats || defaultBudget,
            taskId: prev.taskId || firstTask?.task_id || '',
            contractorWallet: prev.contractorWallet || firstTask?.contractor_wallet || inscription.metadata?.contractor_wallet || '',
            fundraiserWallet:
              prev.fundraiserWallet ||
              approved?.metadata?.fundraiser_wallet ||
              approved?.metadata?.payout_address ||
              inscription.metadata?.fundraiser_wallet ||
              inscription.metadata?.payout_address ||
              firstTask?.contractor_wallet ||
              fundDepositAddress ||
              '',
          };
        });
      }
      if (items.length > 0) {
        hasFetchedRef.current = true;
      }
    } catch (err) {
      console.error('Failed to load proposals', err);
      const reason = err.name === 'AbortError' ? 'timed out' : 'service unavailable';
      setProposalError(`Unable to load proposals for this contract (${reason}).`);
      setProposalItems([]);
    } finally {
      if (showLoading) {
        setIsLoadingProposals(false);
      }
    }
  }, [
    authBlocked,
    contractCandidates,
    contractKey,
    fetchWithTimeout,
    fundDepositAddress,
    inscription.metadata?.contractor_wallet,
    inscription.metadata?.fundraiser_wallet,
    inscription.metadata?.payout_address,
    inscription.metadata?.visible_pixel_hash,
    primaryContractId,
    psbtForm.pixelHash,
  ]);

  const loadSubmissions = React.useCallback(async () => {
    if (authBlocked || !contractCandidates.length) return;
    try {
      const submissionsPromises = contractCandidates.map(async (contractId) => {
        const res = await fetchWithTimeout(`${API_BASE}/api/smart_contract/submissions?contract_id=${contractId}`, {}, 6000);
        if (!res.ok) {
          console.error(`Failed to load submissions for contract ${contractId}:`, res.status);
          return { contractId, submissions: [] };
        }
        const data = await res.json();
        return { contractId, submissions: data?.submissions || [] };
      });

      const results = await Promise.all(submissionsPromises);
      
      const allSubmissions = {};
      const allSubmissionsList = [];
      results.forEach((result) => {
        const { submissions } = result;
        if (Array.isArray(submissions)) {
          submissions.forEach((submission) => {
            allSubmissionsList.push(submission);
            if (submission.submission_id) {
              allSubmissions[submission.submission_id] = submission;
            }
            if (submission.task_id) {
              allSubmissions[submission.task_id] = submission;
            }
            if (submission.claim_id) {
              allSubmissions[submission.claim_id] = submission;
            }
          });
        }
      });

      if (allSubmissionsList.length > 0) {
        const uniqueById = {};
        allSubmissionsList.forEach((submission) => {
          const key = submission.submission_id || `${submission.task_id}:${submission.created_at || ''}`;
          if (!uniqueById[key]) {
            uniqueById[key] = submission;
          }
        });
        const list = Object.values(uniqueById).sort((a, b) => submissionTimestamp(b) - submissionTimestamp(a));
        const nextSubmissionsKey = list
          .map((s) => `${s.submission_id || ''}:${s.status || ''}:${s.task_id || ''}:${s.claim_id || ''}:${s.created_at || ''}:${s.rejected_at || ''}`)
          .join('|');
        if (nextSubmissionsKey !== submissionsKeyRef.current) {
          submissionsKeyRef.current = nextSubmissionsKey;
          setSubmissions(allSubmissions);
          setSubmissionsList(list);
        }
      }
    } catch (err) {
      console.error('Failed to load submissions:', err);
    }
  }, [authBlocked, contractCandidates, fetchWithTimeout]);

  useEffect(() => {
    if (authBlocked) {
      setProposalItems([]);
      setSubmissions({});
      return undefined;
    }
    loadProposals({ showLoading: true });
    loadSubmissions();
    // Poll every 30s for live status updates.
    refreshIntervalRef.current = setInterval(() => {
      loadProposals();
      loadSubmissions();
    }, 30000);
    return () => {
      if (refreshIntervalRef.current) {
        clearInterval(refreshIntervalRef.current);
      }
    };
  }, [auth.apiKey, authBlocked, contractKey, contractCandidates, loadProposals, loadSubmissions]);

  useLayoutEffect(() => {
    if (activeTab !== 'deliverables') return undefined;
    const node = scrollContainerRef.current;
    if (!node) return undefined;
    node.scrollTop = deliverablesScrollRef.current;
    return () => {
      deliverablesScrollRef.current = node.scrollTop;
    };
  }, [activeTab, proposalItems, submissionsList]);

  useEffect(() => {
    if (activeTab !== 'rework') return;
    if (!auth.apiKey) {
      setReworkRequests([]);
      return;
    }
    const fetchReworkRequests = async () => {
      setIsLoadingRework(true);
      try {
        const res = await apiFetch(
          `/api/smart_contract/contracts/${inscription.id}/rework`,
          { headers: { 'X-API-Key': auth.apiKey } }
        );
        if (res.ok) {
          const data = await res.json();
          setReworkRequests(data.rework_requests || []);
        }
      } catch (err) {
        console.error('Failed to fetch rework requests:', err);
      } finally {
        setIsLoadingRework(false);
      }
    };
    fetchReworkRequests();
  }, [activeTab, auth.apiKey, inscription.id]);

  const getLatestSubmissionByTask = (taskId) => {
    if (!taskId || submissionsList.length === 0) return null;
    let latest = null;
    submissionsList.forEach((submission) => {
      if (submission?.task_id !== taskId) return;
      if (!latest) {
        latest = submission;
        return;
      }
      if (submissionTimestamp(submission) > submissionTimestamp(latest)) {
        latest = submission;
      }
    });
    return latest;
  };

  useEffect(() => {
    if (psbtForm.fundraiserWallet) {
      return;
    }
    if (fundDepositAddress) {
      setPsbtForm((prev) => ({ ...prev, fundraiserWallet: fundDepositAddress }));
    }
  }, [psbtForm.fundraiserWallet, fundDepositAddress]);

  const approveProposal = async (proposalId, isPublish = false) => {
    if (!proposalId) return;
    setApprovingId(proposalId);
    setProposalError('');
    try {
      const endpoint = isPublish ? 'publish' : 'approve';
      const res = await fetchWithTimeout(`${API_BASE}/api/smart_contract/proposals/${proposalId}/${endpoint}`, { method: 'POST' }, 6000);
      if (!res.ok) {
        const body = await res.text();
        let errorMessage = body || `HTTP ${res.status}`;
        try {
          const parsedBody = JSON.parse(body);
          const errorObj = parsedBody?.error;
          errorMessage = 
            parsedBody?.message || 
            (typeof errorObj === 'string' ? errorObj : errorObj?.message || errorObj?.error) || 
            body || 
            `HTTP ${res.status}`;
        } catch {
          // Use original body if it's not valid JSON
        }
        throw new Error(errorMessage);
      }
      await loadProposals({ showLoading: true });
      toast.success(isPublish ? 'Proposal published' : 'Proposal approved & published');
    } catch (err) {
      console.error('Failed to approve proposal', err);
      setProposalError(`Approve failed: ${err.message}`);
      toast.error('Approval failed');
    } finally {
      setApprovingId('');
    }
  };

  const copyToClipboard = async (text, key) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedPsbt(key);
      setTimeout(() => setCopiedPsbt(''), 1500);
    } catch (err) {
      console.error('copy failed', err);
    }
  };

  const generatePSBT = async () => {
    setPsbtError('');
    setPsbtResult(null);
    if (!auth.apiKey || !auth.wallet) {
      setPsbtError('Sign in with the funding API key (payer wallet) first.');
      return;
    }
    const contractId = resolvePsbtContractId();
    const payoutWallet = resolvedContractorWallet;
    const fundraiserWallet = resolvedFundraiserWallet;
    const fundingTasks = deliverableTasks.length > 0 ? deliverableTasks : psbtTasks;
    if (!isRaiseFund && !selectedTask) {
      setPsbtError('Select a task to build the PSBT.');
      return;
    }
    if (isRaiseFund && fundingTasks.length === 0) {
      setPsbtError('No tasks available to fund.');
      return;
    }
    const raiseFundTarget = fundingTasks.reduce((sum, t) => sum + (Number(t.budget_sats) || 0), 0);
    const targetBudget = isRaiseFund
      ? raiseFundTarget
      : Number(psbtForm.budgetSats || 0) || approvedBudgetsTotal || Number(selectedTask?.budget_sats || 0) || 0;
    const payouts = isRaiseFund
      ? []
      : payoutSummaries
          .filter((p) => p.wallet && p.wallet !== 'Unknown wallet')
          .map((p) => ({ address: p.wallet, amount_sats: Math.trunc(p.total) }));
    if (isRaiseFund && !fundraiserWallet) {
      setPsbtError('Add the fundraiser payout address first.');
      return;
    }
    if (!isRaiseFund && !payoutWallet && payouts.length === 0) {
      setPsbtError('No contractor wallet found for this task.');
      return;
    }
    if (!contractId) {
      setPsbtError('Missing contract id for PSBT build.');
      return;
    }
    setPsbtLoading(true);
    try {
      const feeRateParsed = psbtForm.feeRate === '' ? NaN : Number(psbtForm.feeRate);
      const feeRate = Number.isFinite(feeRateParsed) ? Math.max(1, feeRateParsed) : 1;
      const payload = {
        contractor_wallet: isRaiseFund ? undefined : payoutWallet,
        pixel_hash:
          selectedTask?.merkle_proof?.visible_pixel_hash ||
          psbtForm.pixelHash?.trim() ||
          inscription.metadata?.visible_pixel_hash ||
          undefined,
        use_pixel_hash: true,
        commitment_target: psbtForm.includeDonation ? 'donation' : 'funding',
        task_id: isRaiseFund ? undefined : selectedTask?.task_id,
        payouts: payouts.length > 0 ? payouts : undefined,
        budget_sats: targetBudget || undefined,
        fee_rate_sats_vb: feeRate,
        change_address: !isRaiseFund && psbtForm.changeAddress?.trim() ? psbtForm.changeAddress.trim() : undefined,
        split_psbt: isRaiseFund ? true : undefined,
      };
      const res = await apiFetch(`/api/smart_contract/contracts/${contractId}/psbt`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-API-Key': auth.apiKey,
        },
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      const payloadData = data?.data || data;
      if (!res.ok) {
        const errorObj = payloadData?.error || data?.error;
        const errorMessage = 
          data?.message || 
          payloadData?.message || 
          (typeof errorObj === 'string' ? errorObj : errorObj?.message || errorObj?.error) || 
          `HTTP ${res.status}`;
        throw new Error(errorMessage);
      }
      setPsbtResult(payloadData);
    } catch (err) {
      setPsbtError(err.message);
    } finally {
      setPsbtLoading(false);
    }
  };

  const publishAndBuild = async () => {
    setPsbtError('');
    setPsbtResult(null);
    const proposal = approvedProposal || proposalItems[0];
    if (!proposal) {
      setPsbtError('No proposal available to publish.');
      return;
    }
    const payoutWallet = resolvedContractorWallet;
    const fundraiserWallet = resolvedFundraiserWallet;
    const fundingWallet =
      selectedTask?.merkle_proof?.funding_address ||
      inscription.metadata?.funding_address ||
      '';
    if (
      fundingWallet &&
      auth.wallet &&
      !isPlaceholderAddressCb(fundingWallet) &&
      normalizeAddress(fundingWallet) !== normalizeAddress(auth.wallet)
    ) {
      setPsbtError('Funding wallet does not match signed-in wallet.');
      return;
    }
    if (isRaiseFund && !fundraiserWallet) {
      setPsbtError('Add the fundraiser payout address first.');
      return;
    }
    if (!isRaiseFund && !payoutWallet) {
      setPsbtError('Add the contractor payout wallet first.');
      return;
    }
    if (!auth.apiKey || !auth.wallet) {
      setPsbtError('Sign in with the funding API key (payer wallet) first.');
      return;
    }
    const status = (proposal.status || '').toLowerCase();
    try {
      if (!['approved', 'published'].includes(status)) {
        await approveProposal(proposal.id, status === 'approved');
        await loadProposals({ showLoading: true });
      }
      await generatePSBT();
    } catch (err) {
      setPsbtError(err.message);
    }
  };

  return {
    auth,
    network,
    setNetwork,
    activeTab,
    setActiveTab,
    monoContent,
    setMonoContent,
    proposalItems,
    isLoadingProposals,
    proposalError,
    approvingId,
    submissions,
    submissionsList,
    dashboardFilter,
    setDashboardFilter,
    dashboardSort,
    setDashboardSort,
    psbtForm,
    setPsbtForm,
    psbtResult,
    psbtError,
    psbtLoading,
    authBlocked,
    copiedPsbt,
    showPsbtQr,
    setShowPsbtQr,
    stegoPayload,
    stegoPayloadLoading,
    stegoPayloadError,
    scanMessage,
    scanLoading,
    scanError,
    scanAttempted,
    scrollContainerRef,
    reworkRequests,
    isLoadingRework,
    showReworkForm,
    setShowReworkForm,
    reworkNotes,
    setReworkNotes,
    isSubmittingRework,
    setIsSubmittingRework,
    inscriptionMessageRaw,
    inscriptionAddressRaw,
    contractCandidates,
    contractKey,
    allTasks,
    approvedProposal,
    approvedContractId,
    primaryContractId,
    psbtTasks,
    deliverableTasks,
    approvedBudgetsTotal,
    payoutSummaries,
    parsedPayload,
    stegoManifest,
    stegoPayloadCid,
    stegoProposal,
    stegoTasks,
    stegoProposalStatus,
    stegoTaskStatusMap,
    hiddenMessageText,
    inscriptionMessage,
    inscriptionAddress,
    fundingMode,
    isRaiseFund,
    selectedTask,
    resolvePsbtContractId,
    fundDepositAddress,
    resolvedContractorWallet,
    resolvedFundraiserWallet,
    textContent,
    pixelHash,
    confidenceValue,
    confidencePercent,
    isConfirmedContract,
    isFundingConfirmed,
    isContractLocked,
    normalizeAddress,
    modalImageSource,
    scanImageSource,
    isHtmlContent,
    isSvgContent,
    sandboxSrc,
    inlineDoc,
    loadProposals,
    loadSubmissions,
    getLatestSubmissionByTask,
    approveProposal,
    copyToClipboard,
    generatePSBT,
    publishAndBuild,
  };
}
