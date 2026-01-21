import React, { useLayoutEffect, useState, useEffect, useMemo, useCallback } from 'react';
import { X } from 'lucide-react';
import toast from 'react-hot-toast';
import CopyButton from '../Common/CopyButton';
import ConfidenceIndicator from '../Common/ConfidenceIndicator';
import SafeQrCodeCanvas from '../Common/SafeQrCodeCanvas';
import DeliverablesReview from '../Review/DeliverablesReview';
import { API_BASE } from '../../apiBase';
import { useAuth } from '../../context/AuthContext';

// QR version 40-L max byte capacity (base64 uses byte mode).
const QR_BYTE_LIMIT = 2953;

const InscriptionModal = ({ inscription, onClose }) => {
  const { auth } = useAuth();
  const [activeTab, setActiveTab] = useState('overview');
  const [monoContent, setMonoContent] = useState(true);
  const [network, setNetwork] = useState(
    inscription?.metadata?.network ||
      inscription?.network ||
      (inscription?.contract_type?.toLowerCase().includes('testnet') ? 'testnet' : 'testnet4')
  );
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

  const guessNetworkFromAddress = (addr) => {
    const a = (addr || '').trim().toLowerCase();
    if (!a) return '';
    if (a.startsWith('tb1') || a.startsWith('m') || a.startsWith('n') || a.startsWith('2')) return 'testnet4';
    if (a.startsWith('bc1') || a.startsWith('1') || a.startsWith('3')) return 'mainnet';
    return '';
  };

  // Fetch network info (prefer wallet-derived guess, fallback to metadata/testnet)
  useEffect(() => {
    const walletGuess =
      guessNetworkFromAddress(auth.wallet) ||
      guessNetworkFromAddress(inscription?.metadata?.funding_address) ||
      guessNetworkFromAddress(inscription?.metadata?.address) ||
      guessNetworkFromAddress(inscription?.metadata?.contractor_wallet);
    const metaNetwork =
      inscription?.metadata?.network ||
      inscription?.network ||
      (inscription?.contract_type?.toLowerCase().includes('testnet') ? 'testnet4' : '');
    const localNetwork = walletGuess || metaNetwork || 'testnet4';
    setNetwork(localNetwork);

    const fetchNetwork = async () => {
      try {
        const response = await fetch(`${API_BASE}/bitcoin/v1/health`);
        if (response.ok) {
          const data = await response.json();
          if (!walletGuess) {
            setNetwork(data.network || localNetwork || 'testnet4');
          }
        }
      } catch (error) {
        console.error('Failed to fetch network info:', error);
      }
    };
    fetchNetwork();
  }, [inscription, auth.wallet]);

  useEffect(() => {
    setShowPsbtQr(false);
  }, [psbtResult]);
  const inscriptionMessageRaw = inscription.text || inscription.metadata?.embedded_message || inscription.metadata?.extracted_message || '';
  const inscriptionAddressRaw = inscription.address ?? inscription.metadata?.address ?? '';
  const contractCandidates = useMemo(() => {
    const rawIds = [
      inscription.contract_id,
      inscription.id,
      inscription.metadata?.contract_id,
      inscription.metadata?.visible_pixel_hash,
      inscription.metadata?.ingestion_id,
    ].filter(Boolean);
    const expanded = new Set();
    rawIds.forEach((id) => {
      expanded.add(id);
      // Consider prefixed/unprefixed wish-* variants so proposals line up with inscription IDs.
      if (id.startsWith('wish-')) {
        expanded.add(id.replace(/^wish-/, ''));
      } else {
        expanded.add(`wish-${id}`);
      }
    });
    return Array.from(expanded);
  }, [
    inscription.contract_id,
    inscription.id,
    inscription.metadata?.contract_id,
    inscription.metadata?.visible_pixel_hash,
    inscription.metadata?.ingestion_id,
  ]);
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

  const stegoManifest = useMemo(() => {
    const raw = scanMessage || inscription?.metadata?.extracted_message || '';
    if (!raw) return null;
    const manifest = {};
    raw.split('\n').forEach((line) => {
      const idx = line.indexOf(':');
      if (idx < 0) return;
      const key = line.slice(0, idx).trim();
      const value = line.slice(idx + 1).trim();
      if (key) {
        manifest[key] = value;
      }
    });
    return Object.keys(manifest).length > 0 ? manifest : null;
  }, [inscription?.metadata?.extracted_message, scanMessage]);

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
    return inscription?.metadata?.extracted_message || '';
  }, [stegoPayload, inscription?.metadata?.extracted_message]);

  const inscriptionMessage = parsedPayload?.message || inscriptionMessageRaw;
  const inscriptionAddress = parsedPayload?.address ?? inscriptionAddressRaw;
  const fundingMode = (
    inscription.metadata?.funding_mode ||
    parsedPayload?.funding_mode ||
    approvedProposal?.metadata?.funding_mode ||
    ''
  ).toLowerCase();
  const looksLikeRaiseFund = (value) => {
    const normalized = (value || '').toLowerCase();
    return (
      normalized.includes('fund raising') ||
      normalized.includes('fundraising') ||
      normalized.includes('raise fund') ||
      normalized.includes('fundraise')
    );
  };
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
  const isPlaceholderAddress = React.useCallback((value) => {
    const cleaned = (value || '').trim().toLowerCase();
    if (!cleaned) return true;
    return cleaned.includes('...') || cleaned.includes('pending') || cleaned === 'bc1p-simulated-funding-address';
  }, []);
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
    const picked = cleaned.find((value) => value && !isPlaceholderAddress(value));
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
    isPlaceholderAddress,
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
  const confidenceValue = Number(inscription.metadata?.confidence || 0);
  const confidencePercent = Math.round(confidenceValue * 100);
  const confirmationStatus = (inscription.metadata?.confirmation_status || inscription.confirmation_status || '').toLowerCase();
  const metadataStatus = (inscription.metadata?.status || '').toLowerCase();
  const scanStatus = (inscription.scan_result?.confirmation_status || inscription.scan_result?.status || '').toLowerCase();
  const isConfirmedStatus = (value) => (value || '').toLowerCase() === 'confirmed';
  const isConfirmedContract = Boolean(
    inscription.metadata?.confirmed_txid ||
      inscription.metadata?.confirmed_height ||
      inscription.confirmed_txid ||
      inscription.confirmed_height ||
      inscription.metadata?.confirmed === true ||
      inscription.confirmed === true ||
      isConfirmedStatus(confirmationStatus) ||
      isConfirmedStatus(metadataStatus) ||
      isConfirmedStatus(scanStatus) ||
      isConfirmedStatus(inscription.status)
  );
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
  
  const isActuallyImageFile =
    inscription.mime_type?.includes('image') &&
    !inscription.image_url?.endsWith('.txt') &&
    (inscription.image_url || inscription.thumbnail);
  const modalImageSource = isActuallyImageFile ? (inscription.thumbnail || inscription.image_url) : null;
  const scanImageSource = modalImageSource || inscription.image_url || inscription.thumbnail || '';
  const mime = (inscription.mime_type || '').toLowerCase();
  const isHtmlContent = mime.includes('text/html') || mime.includes('application/xhtml');
  const isSvgContent = mime === 'image/svg+xml' || (mime.includes('svg') && mime.includes('xml'));
  const sandboxSrc = inscription.image_url || inscription.thumbnail;
  const inlineDoc = (isHtmlContent || isSvgContent) ? (inscription.text || '') : '';
  const fetchWithTimeout = React.useCallback(async (url, options = {}, timeoutMs = 6000) => {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const headers = {
        ...(options.headers || {}),
        ...(auth.apiKey ? { 'X-API-Key': auth.apiKey } : {}),
      };
      return await fetch(url, { ...options, headers, signal: controller.signal });
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
        const form = new FormData();
        form.append('image', blob, 'stego.png');
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
    if (!auth.apiKey || authBlocked || !contractCandidates.length) return;
    lastFetchedKeyRef.current = contractKey;
    if (showLoading) {
      setIsLoadingProposals(true);
    }
    setProposalError('');
    try {
      const res = await fetchWithTimeout(`${API_BASE}/api/smart_contract/proposals`, {}, 6000);
      if (res.status === 401 || res.status === 403) {
        setAuthBlocked(true);
        setProposalItems([]);
        setSubmissions({});
        return;
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      let items = (data?.proposals || []).filter((p) => {
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
      const submissionList = Array.isArray(data?.submissions)
        ? data.submissions
        : Object.values(data?.submissions || {});
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
    auth.apiKey,
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
    if (!auth.apiKey || authBlocked || !contractCandidates.length) return;
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
        const list = Object.values(uniqueById).sort((a, b) => getSubmissionTimestamp(b) - getSubmissionTimestamp(a));
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
  }, [auth.apiKey, authBlocked, contractCandidates, fetchWithTimeout]);

  useEffect(() => {
    if (!auth.apiKey || authBlocked) {
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

  const getSubmissionTimestamp = (submission) => {
    const raw = submission?.submitted_at || submission?.created_at;
    if (!raw) return 0;
    const parsed = Date.parse(raw);
    return Number.isNaN(parsed) ? 0 : parsed;
  };

  const getLatestSubmissionByTask = (taskId) => {
    if (!taskId || submissionsList.length === 0) return null;
    let latest = null;
    submissionsList.forEach((submission) => {
      if (submission?.task_id !== taskId) return;
      if (!latest) {
        latest = submission;
        return;
      }
      if (getSubmissionTimestamp(submission) > getSubmissionTimestamp(latest)) {
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
        split_psbt: isRaiseFund ? true : undefined,
      };
      const res = await fetch(`${API_BASE}/api/smart_contract/contracts/${contractId}/psbt`, {
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
      !isPlaceholderAddress(fundingWallet) &&
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

  const markdownContent = `# Steganographic Smart Contract Analysis

## Contract Identity
- **Contract ID**: \`${inscription.contract_id || inscription.id}\`
- **Block Height**: ${inscription.block_height || inscription.genesis_block_height || 'Unknown'}
- **Transaction ID**: \`${inscription.metadata?.transaction_id || 'Not available'}\`
- **Deployment Date**: ${inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toLocaleDateString() : 'Unknown'}

## Technical Architecture
- **Contract Type**: ${inscription.contract_type || inscription.contractType || 'Steganographic'}
- **Protocol Layer**: ${inscription.protocol || 'BRC-20'}
- **Data Capability**: ${inscription.capability || 'Data Storage & Concealment'}
- **MIME Type**: ${inscription.mime_type || 'Unknown'}

## Steganographic Specifications
- **Detection Method**: ${inscription.metadata?.detection_method || 'AI-Powered Analysis'}
- **Steganography Type**: ${inscription.metadata?.stego_type || 'Unknown'}
- **Confidence Level**: ${inscription.metadata?.confidence ? Math.round(inscription.metadata.confidence * 100) + '%' : 'N/A'}
- **Probability Score**: ${inscription.metadata?.stego_probability ? Math.round(inscription.metadata.stego_probability * 100) + '%' : 'N/A'}

## Media Properties
- **Image Format**: ${inscription.metadata?.image_format || 'Unknown'}
- **File Size**: ${inscription.metadata?.image_size ? (inscription.metadata.image_size / 1024).toFixed(2) + ' KB' : 'Unknown'}
- **Image Index**: ${inscription.metadata?.image_index || 'Unknown'}
- **Encoding Method**: ${inscription.metadata?.stego_type || 'Analysis Required'}

## Extracted Intelligence
${inscription.metadata?.extracted_message ? `\`\`\`\n${inscription.metadata.extracted_message}\n\`\`\`` : 'No hidden message detected'}

## Blockchain Integration
- **Block Hash**: \`${inscription.metadata?.block_hash || 'Unknown'}\`
- **Network**: Bitcoin Mainnet
- **Consensus**: Proof of Work
- **Timestamp**: ${inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toISOString() : 'Unknown'}

---

*Analysis performed by Steganography Detection System*`;

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg lg:max-w-7xl w-full h-full lg:w-[95vw] lg:mx-4 lg:min-h-[80vh] lg:max-h-[85vh] overflow-hidden flex flex-col shadow-2xl">
        <div className="sticky top-0 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 p-4 lg:p-6 flex-shrink-0">
          <div className="flex justify-between items-center">
            <h2 className="text-lg lg:text-xl font-bold text-black dark:text-white">Smart Contract Details</h2>
            <button onClick={onClose} className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200">
              <X className="w-6 h-6" />
            </button>
          </div>
        </div>

        <div
          className="p-4 flex-1 overflow-y-auto overflow-x-auto"
          data-deliverables-scroll
          ref={scrollContainerRef}
        >
            <div className="flex flex-col items-center lg:flex-row lg:items-start gap-6 mb-6">
              <div className="flex-shrink-0">
                {modalImageSource ? (
                  <div className="relative">
                    <img
                      src={modalImageSource}
                      alt={inscription.file_name || inscription.id}
                      className="w-48 h-48 object-cover rounded-lg border-2 border-gray-300 dark:border-gray-700"
                    />
                    {confidencePercent > 0 && (
                      <div className="absolute top-2 right-2 bg-green-500 text-white text-xs px-2 py-1 rounded-md font-bold">
                        {confidencePercent}%
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="w-48 h-48 bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 rounded-lg flex items-center justify-center border-2 border-gray-300 dark:border-gray-700">
                    <div className="text-6xl text-center">
                      {inscription.contract_type === 'Steganographic Contract' ? '🎨' :
                       inscription.mime_type?.includes('text') ? '📄' :
                       inscription.mime_type?.includes('image') ? '🖼️' : '📦'}
                    </div>
                  </div>
                )}
              </div>

              <div className="flex-1 w-full">
              <div className="border-b border-gray-200 dark:border-gray-700 mb-6">
                <div className="flex gap-6 overflow-x-auto no-scrollbar">
{[
  { id: 'overview', label: 'Details', icon: '📋' },
  { id: 'content', label: 'Content', icon: '📄' },
  { id: 'proposals', label: 'Proposals', icon: '🗂️' },
  { id: 'deliverables', label: 'Deliverables', icon: '✅' },
  { id: 'blockchain', label: 'Blockchain', icon: '⛓️' }
].map((tab) => (
                    <button
                      key={tab.id}
                      onClick={() => setActiveTab(tab.id)}
                      className={`px-4 py-2 font-medium text-sm border-b-2 transition-colors flex items-center gap-2 whitespace-nowrap ${
                        activeTab === tab.id
                          ? 'border-indigo-500 text-indigo-600 dark:text-indigo-400'
                          : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
                      }`}
                    >
                      <span>{tab.icon}</span>
                      {tab.label}
                    </button>
                  ))}
                </div>
              </div>


              {activeTab === 'overview' && (
                <div className="space-y-6">
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-purple-500 rounded-full"></span>
                      Identity
                    </h4>
                    <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-4 space-y-3">
                      <div className="space-y-2">
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex items-start gap-2">
                            <span className="text-gray-600 dark:text-gray-400 text-sm whitespace-nowrap">Transaction ID:</span>
                            <span className="text-black dark:text-white font-mono text-xs break-all leading-tight">{inscription.metadata?.transaction_id || inscription.id}</span>
                          </div>
                          <CopyButton text={inscription.metadata?.transaction_id || inscription.id} />
                        </div>
                      </div>
                      <div className="flex items-center gap-4">
                        <div className="flex items-center gap-2">
                          <span className="text-gray-600 dark:text-gray-400 text-sm">Block Height:</span>
                          <span className="text-black dark:text-white font-semibold">{inscription.block_height || inscription.genesis_block_height || 'Unknown'}</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <span className="text-gray-600 dark:text-gray-400 text-sm">Type:</span>
                          <span className="text-black dark:text-white font-semibold">{inscription.mime_type?.split('/')[1]?.toUpperCase() || 'UNKNOWN'}</span>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                      File Information
                    </h4>
                    <div className="grid grid-cols-2 gap-4">
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3 overflow-hidden">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">File Name</div>
                        <div className="text-black dark:text-white font-semibold break-words">{inscription.file_name || 'N/A'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">File Size</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.size_bytes ? `${(inscription.size_bytes / 1024).toFixed(2)} KB` : 'Unknown'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Content Type</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.mime_type || 'Unknown'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Contract Type</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.contract_type || 'Standard'}</div>
                      </div>
                    </div>
                  </div>

                  {inscription.metadata?.is_stego && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-yellow-500 rounded-full"></span>
                        Steganographic Analysis
                      </h4>
                      <div className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4">
                        <div className="grid grid-cols-2 gap-4">
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Detection Status</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">Steganography Detected</div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Confidence Level</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">
                              {confidencePercent > 0 ? `${confidencePercent}%` : 'N/A'}
                            </div>
                          </div>
                          {inscription.metadata.stego_type && (
                            <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                              <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Method</div>
                              <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.stego_type.toUpperCase()}</div>
                            </div>
                          )}
                          {inscription.metadata.extracted_message && (
                            <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                              <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Hidden Message</div>
                              <div className="text-yellow-900 dark:text-yellow-100 font-semibold">Available</div>
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
                  <div className="flex items-center justify-between gap-2 flex-wrap">
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white">Proposals for this contract</h4>
                      <p className="text-sm text-gray-500 dark:text-gray-400">Tasks and budgets attached to this smart contract.</p>
                    </div>
                    <button
                      onClick={() => loadProposals({ showLoading: true })}
                      disabled={isLoadingProposals}
                      className="flex items-center gap-2 px-3 py-1.5 text-sm rounded-full border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-60"
                    >
                      <span className="text-xs">↻</span>
                      {isLoadingProposals ? 'Refreshing…' : 'Refresh'}
                    </button>
                  </div>
                  {proposalError && <div className="text-sm text-red-500">{proposalError}</div>}
                  {isLoadingProposals ? (
                    <div className="text-sm text-gray-500 dark:text-gray-400">Loading proposals...</div>
                  ) : proposalItems.length === 0 ? (
                    <div className="text-sm text-gray-500 dark:text-gray-400">No proposals found for this contract.</div>
                  ) : (
                    proposalItems.map((p) => {
                      const tasks = Array.isArray(p.tasks) && p.tasks.length > 0
                        ? p.tasks
                        : (Array.isArray(p.metadata?.suggested_tasks) ? p.metadata.suggested_tasks : []);
                      const totalTaskBudget = tasks.reduce((sum, t) => sum + (t.budget_sats || 0), 0);
                      // Gracefully show human text when description_md/title are JSON blobs
                      const prettyTitle = (() => {
                        if (typeof p.title === 'string') {
                          try {
                            const o = JSON.parse(p.title);
                            if (o?.message) return o.message;
                          } catch {
                            // Ignore invalid JSON payloads.
                          }
                        }
                        return p.title;
                      })();
                      const prettyDesc = (() => {
                        if (typeof p.description_md === 'string') {
                          try {
                            const o = JSON.parse(p.description_md);
                            if (o?.message) return o.message;
                          } catch {
                            // Ignore invalid JSON payloads.
                          }
                        }
                        return p.description_md;
                      })();
                      return (
                        <div key={p.id} className="border border-gray-200 dark:border-gray-700 rounded-lg p-3 bg-gray-50 dark:bg-gray-900/60">
                          <div className="flex justify-between items-center mb-1">
                            <h5 className="text-base font-semibold text-black dark:text-white">{prettyTitle}</h5>
                            <span className={`text-xs px-2 py-0.5 rounded border ${
                              (p.status || '').toLowerCase() === 'approved'
                                ? 'bg-emerald-100 dark:bg-emerald-900/40 border-emerald-400 text-emerald-700 dark:text-emerald-200'
                                : 'bg-gray-200 dark:bg-gray-700 border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200'
                            }`}>
                              {p.status || 'pending'}
                            </span>
                          </div>
                          <div className="text-xs text-gray-500 dark:text-gray-400 mb-2 break-all">ID: {p.id}</div>
                          <div className="text-sm text-gray-800 dark:text-gray-200 whitespace-pre-line mb-3">
                            {prettyDesc}
                          </div>
                          <div className="grid grid-cols-2 gap-2 text-sm text-gray-600 dark:text-gray-300 mb-3">
                            <div>Budget: {p.budget_sats || totalTaskBudget || 0} sats</div>
                            <div>Tasks: {tasks.length}</div>
                            {p.visible_pixel_hash && (
                              <div className="col-span-2 break-all text-xs text-gray-500 dark:text-gray-400">
                                Evidence hash: {p.visible_pixel_hash}
                              </div>
                            )}
                          </div>
                          {tasks.length > 0 && (
                            <div className="bg-gray-100 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded p-2">
                              <div className="text-xs font-semibold text-gray-600 dark:text-gray-300 mb-1">Tasks (with status)</div>
                              <ul className="text-sm text-gray-700 dark:text-gray-200 list-disc pl-4 space-y-1">
                                {tasks.map((t) => {
                                  const submission = getLatestSubmissionByTask(t.task_id)
                                    || submissions[t.task_id]
                                    || (t.active_claim_id ? submissions[t.active_claim_id] : null);
                                  const status = (submission?.status || t.status || 'pending').toLowerCase();
                                  const statusClasses = status === 'approved'
                                    ? 'bg-emerald-100 dark:bg-emerald-900/40 border-emerald-400 text-emerald-700 dark:text-emerald-200'
                                    : status === 'available'
                                      ? 'bg-blue-100 dark:bg-blue-900/40 border-blue-400 text-blue-700 dark:text-blue-200'
                                      : 'bg-amber-100 dark:bg-amber-900/40 border-amber-400 text-amber-800 dark:text-amber-200';
                                  return (
                                    <li key={t.task_id || t.title} className="flex items-center gap-2 flex-wrap">
                                      <span className="font-semibold">{t.title}</span>
                                      {t.budget_sats ? <span className="text-gray-600 dark:text-gray-300">— {t.budget_sats} sats</span> : null}
                                      <span className={`inline-flex items-center px-2 py-0.5 rounded text-[11px] border ${statusClasses}`}>
                                        {status}
                                      </span>
                                      {t.contractor_wallet && (
                                        <span className="text-[11px] text-gray-500 dark:text-gray-400 font-mono">
                                          • payout: {t.contractor_wallet}
                                        </span>
                                      )}
                                      {Array.isArray(t.skills || t.skills_required) && (t.skills || t.skills_required).length > 0 && (
                                        <span className="text-[11px] text-gray-500 dark:text-gray-400">
                                          • {(t.skills || t.skills_required).join(', ')}
                                        </span>
                                      )}
                                      {submission && (
                                        <span className="text-[11px] text-emerald-600 dark:text-emerald-300">
                                          • submission: {submission.status || 'pending'} {submission.completion_proof?.link ? `(${submission.completion_proof.link})` : ''}
                                        </span>
                                      )}
                                    </li>
                                  );
                                })}
                              </ul>
                            </div>
                          )}
                          <div className="flex justify-end mt-3">
                            {(() => {
                              const statusLower = (p.status || '').toLowerCase();
                              const isFinal = ['approved', 'published'].includes(statusLower);
                              if (isFinal) {
                                return (
                                  <span className="text-xs text-emerald-600 dark:text-emerald-300 font-semibold">
                                    Approved
                                  </span>
                                );
                              }
                              return (
                                <button
                                  onClick={() => approveProposal(p.id, false)}
                                  disabled={approvingId === p.id}
                                  className="px-3 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded text-sm disabled:opacity-60"
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
                    <div className="text-sm text-gray-500 dark:text-gray-400">
                      Payout tools are unavailable for this session.
                    </div>
                  ) : !approvedProposal ? (
                    <div className="text-sm text-gray-500 dark:text-gray-400">Approve a proposal to unlock deliverables.</div>
                  ) : deliverableTasks.length === 0 ? (
                    <div className="text-sm text-gray-500 dark:text-gray-400">No deliverables available yet.</div>
                  ) : (
                  <div className="border border-gray-200 dark:border-gray-800 rounded-lg p-4 bg-white dark:bg-gray-900">
                    <div className="flex items-start justify-between gap-3 flex-wrap">
                      <div>
                        <h4 className="text-base font-semibold text-black dark:text-white">Publish & Build PSBT</h4>
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
                        className="px-3 py-1.5 rounded bg-emerald-600 hover:bg-emerald-500 text-white text-sm disabled:opacity-60"
                        title={(() => {
                          if (!auth.wallet) return 'Sign in with funding API key first';
                          const fundingWallet =
                            selectedTask?.merkle_proof?.funding_address ||
                            inscription.metadata?.funding_address ||
                            '';
                          if (
                            fundingWallet &&
                            !isPlaceholderAddress(fundingWallet) &&
                            normalizeAddress(fundingWallet) !== normalizeAddress(auth.wallet)
                          ) {
                            return 'Funding wallet does not match signed-in wallet';
                          }
                          return '';
                        })()}
                      >
                        {psbtLoading ? 'Building…' : 'Publish & Build'}
                      </button>
                    </div>
                    {!auth.wallet && (
                      <div className="text-xs text-amber-600 dark:text-amber-400 mt-2">
                        Sign in with the funder API key (payer wallet) to build the PSBT.
                      </div>
                    )}
                    {psbtTasks.length === 0 && allTasks.length === 0 ? (
                      <div className="text-sm text-gray-500 dark:text-gray-400 mt-3">No deliverables available yet.</div>
                    ) : (
                      <div className="grid md:grid-cols-2 gap-3 text-sm mt-3">
                        <div className="space-y-1">
                          <label className="text-xs text-gray-500">
                            {isRaiseFund ? 'Funding target (sum of task budgets)' : 'Budget (sum of proposal tasks)'}
                          </label>
                          <div className="h-10 px-3 py-2 rounded bg-gray-100 dark:bg-gray-800 font-mono text-xs flex items-center">
                            {approvedBudgetsTotal || selectedTask?.budget_sats || 'n/a'} sats
                          </div>
                        </div>
                        <div className="space-y-2">
                          <label className="block text-xs text-gray-500">
                            {isRaiseFund ? 'Fund deposit address' : 'Payer address'}
                          </label>
                          <input
                            className="w-full h-10 rounded bg-gray-100 dark:bg-gray-800 px-3 py-2 font-mono text-xs text-gray-600 dark:text-gray-300"
                            placeholder={isRaiseFund ? 'Contract creator address' : 'Funding address'}
                            value={
                              isRaiseFund
                                ? resolvedFundraiserWallet || ''
                                : auth.wallet || fundDepositAddress || ''
                            }
                            readOnly
                          />
                        </div>
                        <div className="md:col-span-2 grid sm:grid-cols-2 gap-2">
                          <label className="flex items-start gap-3 text-xs text-gray-600 dark:text-gray-300 bg-gray-100 dark:bg-gray-800 rounded-md px-3 py-2">
                            <input
                              type="checkbox"
                              className="mt-0.5 h-4 w-4 rounded border-gray-300 text-emerald-600 focus:ring-emerald-500 dark:border-gray-600"
                              checked={psbtForm.includeDonation}
                              onChange={(e) => setPsbtForm((p) => ({ ...p, includeDonation: e.target.checked }))}
                            />
                            <span>Donate to Starlight Project to keep lights on</span>
                          </label>
                        </div>
                        <div className="space-y-2 md:col-span-2">
                          <label className="block text-xs text-gray-500">Fee rate (sat/vB)</label>
                          <input
                            className="w-full h-10 rounded bg-gray-100 dark:bg-gray-800 px-3 py-2"
                            type="number"
                            min="1"
                            step="1"
                            value={psbtForm.feeRate}
                            onChange={(e) => setPsbtForm((p) => ({ ...p, feeRate: e.target.value }))}
                          />
                        </div>
                        <div className="space-y-1 md:col-span-2">
                          <div className="text-xs text-gray-500">
                            {isRaiseFund
                              ? 'Contributor summary by contractor wallet'
                              : 'Payout summary by contractor wallet'}
                          </div>
                          <div className="rounded bg-gray-100 dark:bg-gray-800 p-3 space-y-2 min-h-[96px]">
                            {payoutSummaries.map((item) => (
                              <div key={item.wallet} className="flex items-center justify-between text-xs font-mono text-gray-700 dark:text-gray-300">
                                <span className="truncate">{item.wallet}</span>
                                <span>{item.total} sats</span>
                              </div>
                            ))}
                          </div>
                        </div>
                        <div className="space-y-2 md:col-span-2">
                          <label className="block text-xs text-gray-500">Contract ID</label>
                          <div className="w-full rounded bg-gray-100 dark:bg-gray-800 px-3 py-3 min-h-[48px] font-mono text-xs text-gray-500 dark:text-gray-400 flex items-center">
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
                      if (!isRaiseFund && !payoutWallet && payoutList.length === 0) {
                        return <div className="text-xs text-amber-600 dark:text-amber-400 mt-2">Payout wallet missing.</div>;
                      }
                      if (!isRaiseFund && payerAddress && payoutList.length === 0 && payoutWallet === payerAddress) {
                        return (
                          <div className="text-xs text-amber-600 dark:text-amber-400 mt-2">
                            Payout matches payer wallet—confirm contractor address.
                          </div>
                        );
                      }
                      return null;
                    })()}
                    {psbtError && <div className="text-sm text-red-500 mt-2">{psbtError}</div>}
                    {psbtResult && (() => {
                      const splitPsbts = Array.isArray(psbtResult.psbts) ? psbtResult.psbts : [];
                      if (splitPsbts.length > 0) {
                        return (
                          <div className="mt-3 space-y-3 bg-gray-50 dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700 rounded-lg p-3">
                            <div className="text-xs text-gray-600 dark:text-gray-300">
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
                                <div key={`${payerAddress}-${index}`} className="rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 p-3 space-y-2">
                                  <div className="text-[11px] text-gray-600 dark:text-gray-300 font-mono break-all">
                                    Contributor: {payerAddress}
                                  </div>
                                  <div className="text-xs text-gray-600 dark:text-gray-300 grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 font-mono">
                                    <div>Inputs selected</div>
                                    <div className="text-right tabular-nums">{entry.selected_sats} sats</div>
                                    <div>Funding target</div>
                                    <div className="text-right tabular-nums">{entry.budget_sats} sats</div>
                                    {entry.commitment_sats && psbtForm.includeDonation ? (
                                      <>
                                        <div>Donation</div>
                                        <div className="text-right tabular-nums">{entry.commitment_sats} sats</div>
                                      </>
                                    ) : null}
                                    <div>Fee</div>
                                    <div className="text-right tabular-nums">{entry.fee_sats} sats</div>
                                    <div>Change</div>
                                    <div className="text-right tabular-nums">{entry.change_sats} sats</div>
                                  </div>
                                  <textarea
                                    className="w-full rounded bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 font-mono text-xs p-2"
                                    rows={3}
                                    readOnly
                                    value={psbtValue}
                                  />
                                  <div className="flex gap-2 text-[11px] text-gray-600 dark:text-gray-300 flex-wrap">
                                    <button
                                      onClick={() => copyToClipboard(psbtValue, `hex-${index}`)}
                                      className="px-2 py-1 rounded border border-gray-300 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700"
                                    >
                                      {copiedPsbt === `hex-${index}` ? 'Copied hex' : 'Copy hex'}
                                    </button>
                                    {psbtBase64 && (
                                      <button
                                        onClick={() => copyToClipboard(psbtBase64, `b64-${index}`)}
                                        className="px-2 py-1 rounded border border-gray-300 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700"
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
                      const payoutAmounts = Array.isArray(psbtResult.payout_amounts)
                        ? psbtResult.payout_amounts
                        : [];
                      const payerDisplay = effectiveRaiseFund
                        ? (payerAddresses.length > 0 ? payerAddresses.join(', ') : payerAddress)
                        : payerAddress;
                      return (
                        <div className="mt-3 space-y-2 bg-gray-50 dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700 rounded-lg p-3">
                          <div className="flex gap-2 text-[11px] text-gray-600 dark:text-gray-300 flex-wrap">
                            <span>{effectiveRaiseFund ? 'Contributors' : 'Payer'}: {payerDisplay}</span>
                            <span>Network: {psbtResult.network_params || 'testnet4'}</span>
                          </div>
                          <div className="text-xs text-gray-600 dark:text-gray-300 break-all">
                            {effectiveRaiseFund ? 'Fund deposit script' : 'Payout script'}: {psbtResult.payout_script}
                          </div>
                          <div className="text-xs text-gray-600 dark:text-gray-300 grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 font-mono">
                            <div>{effectiveRaiseFund ? 'Inputs selected' : 'Selected'}</div>
                            <div className="text-right tabular-nums">{psbtResult.selected_sats} sats</div>
                            <div>{effectiveRaiseFund ? 'Funding target' : 'Price'}</div>
                            <div className="text-right tabular-nums">{budgetSats} sats</div>
                            {psbtResult.commitment_sats && psbtForm.includeDonation ? (
                              <>
                                <div>Donation</div>
                                <div className="text-right tabular-nums">{psbtResult.commitment_sats} sats</div>
                              </>
                            ) : null}
                            <div>Fee</div>
                            <div className="text-right tabular-nums">{psbtResult.fee_sats} sats</div>
                            <div>Change</div>
                            <div className="text-right tabular-nums">{psbtResult.change_sats} sats</div>
                          </div>
                          {effectiveRaiseFund && payoutAmounts.length > 1 && (
                            <div className="rounded bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 p-2 text-[11px] text-gray-600 dark:text-gray-300 space-y-1">
                              <div className="font-semibold text-gray-700 dark:text-gray-200">Funding outputs (per task)</div>
                              {payoutAmounts.map((amount, index) => (
                                <div key={`${amount}-${index}`} className="flex items-center justify-between font-mono">
                                  <span>Output {index + 1}</span>
                                  <span>{amount} sats</span>
                                </div>
                              ))}
                            </div>
                          )}
                          {effectiveRaiseFund && changeAddresses.length > 0 && (
                            <div className="rounded bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 p-2 text-[11px] text-gray-600 dark:text-gray-300 space-y-1">
                              <div className="font-semibold text-gray-700 dark:text-gray-200">Change outputs (by contributor)</div>
                              {changeAddresses.map((addr, index) => (
                                <div key={`${addr}-${index}`} className="flex items-center justify-between gap-2 font-mono">
                                  <span className="truncate">{addr}</span>
                                  <span>{changeAmounts[index] ?? 0} sats</span>
                                </div>
                              ))}
                            </div>
                          )}
                          <textarea
                            className="w-full rounded bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 font-mono text-xs p-2"
                            rows={3}
                            readOnly
                            value={psbtValue}
                          />
                          <div className="flex gap-2 text-[11px] text-gray-600 dark:text-gray-300 flex-wrap">
                            <button
                              onClick={() => copyToClipboard(psbtValue, 'hex')}
                              className="px-2 py-1 rounded border border-gray-300 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700"
                            >
                              {copiedPsbt === 'hex' ? 'Copied hex' : 'Copy hex'}
                            </button>
                            {psbtBase64 && (
                              <>
                                <button
                                  onClick={() => copyToClipboard(psbtBase64, 'b64')}
                                  className="px-2 py-1 rounded border border-gray-300 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700"
                                >
                                  {copiedPsbt === 'b64' ? 'Copied base64' : 'Copy base64'}
                                </button>
                                <button
                                  onClick={() => setShowPsbtQr((prev) => !prev)}
                                  className="px-2 py-1 rounded border border-gray-300 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-700"
                                >
                                  {showPsbtQr ? 'Hide QR' : 'Show QR'}
                                </button>
                              </>
                            )}
                          </div>
                          {showPsbtQr && psbtBase64 && (
                            <div className="flex justify-center py-2">
                              <div className="bg-white p-2 rounded">
                                {psbtQrTooLarge ? (
                                  <div className="text-xs text-amber-600 text-center">
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
                      <div className="border border-gray-200 dark:border-gray-800 rounded-lg p-4 bg-white dark:bg-gray-900 space-y-4">
                        <div className="flex items-center justify-between gap-2 flex-wrap">
                          <div>
                            <h4 className="text-base font-semibold text-black dark:text-white">Task Status Dashboard</h4>
                            <p className="text-xs text-gray-500 dark:text-gray-400">Snapshot of task progress and review queue.</p>
                          </div>
                          <div className="flex items-center gap-2 flex-wrap">
                            <select
                              value={dashboardFilter}
                              onChange={(e) => setDashboardFilter(e.target.value)}
                              className="text-xs rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-2 py-1"
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
                              className="text-xs rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 px-2 py-1"
                            >
                              <option value="status">Sort by status</option>
                              <option value="title">Sort by title</option>
                              <option value="budget">Sort by budget</option>
                            </select>
                            <button
                              type="button"
                              onClick={exportStatusReport}
                              className="px-3 py-1.5 text-xs rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700"
                            >
                              Export JSON
                            </button>
                          </div>
                        </div>

                        <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-3 text-xs">
                          <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-gray-500 dark:text-gray-400">Total tasks</div>
                            <div className="text-lg font-semibold text-black dark:text-white">{counts.total || 0}</div>
                          </div>
                          <div className="bg-emerald-50 dark:bg-emerald-900/40 rounded-lg p-3">
                            <div className="text-emerald-700 dark:text-emerald-200">Approved</div>
                            <div className="text-lg font-semibold text-emerald-800 dark:text-emerald-100">{counts.approved || 0}</div>
                          </div>
                          <div className="bg-amber-50 dark:bg-amber-900/40 rounded-lg p-3">
                            <div className="text-amber-700 dark:text-amber-200">Pending review</div>
                            <div className="text-lg font-semibold text-amber-800 dark:text-amber-100">{pendingReviewCount}</div>
                          </div>
                          <div className="bg-blue-50 dark:bg-blue-900/40 rounded-lg p-3">
                            <div className="text-blue-700 dark:text-blue-200">Claimed</div>
                            <div className="text-lg font-semibold text-blue-800 dark:text-blue-100">{counts.claimed || 0}</div>
                          </div>
                        </div>

                        <div>
                          <div className="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400 mb-2">
                            <span>Progress</span>
                            <span>{progress}% complete</span>
                          </div>
                          <div className="w-full h-2 rounded-full bg-gray-200 dark:bg-gray-800 overflow-hidden">
                            <div
                              className="h-full bg-emerald-500"
                              style={{ width: `${progress}%` }}
                            />
                          </div>
                        </div>

                        {pendingReviewCount > 0 && (
                          <div className="text-xs text-amber-800 dark:text-amber-200 bg-amber-50 dark:bg-amber-900/40 border border-amber-200 dark:border-amber-700 rounded p-2">
                            {pendingReviewCount} submission{pendingReviewCount === 1 ? '' : 's'} waiting for review.
                          </div>
                        )}

                        <div className="border border-gray-200 dark:border-gray-800 rounded-lg overflow-hidden">
                          <div className="grid grid-cols-4 gap-2 bg-gray-50 dark:bg-gray-900 text-xs text-gray-500 dark:text-gray-400 px-3 py-2">
                            <span>Task</span>
                            <span>Status</span>
                            <span>Budget</span>
                            <span>Last submission</span>
                          </div>
                          <div className="divide-y divide-gray-200 dark:divide-gray-800 text-xs">
                            {sorted.map((row) => (
                              <div key={row.task.task_id} className="grid grid-cols-4 gap-2 px-3 py-2">
                                <span className="text-gray-800 dark:text-gray-200 font-semibold truncate">{row.task.title}</span>
                                <span className="text-gray-600 dark:text-gray-300">{row.displayStatus}</span>
                                <span className="text-gray-600 dark:text-gray-300">{row.task.budget_sats || 0} sats</span>
                                <span className="text-gray-500 dark:text-gray-400">
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
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-indigo-500 rounded-full"></span>
                        Sandboxed Preview
                      </h4>
                      <div className="rounded-lg border border-indigo-200 dark:border-indigo-700 overflow-hidden bg-gray-50 dark:bg-gray-900">
                        <iframe
                          title="inscription-sandbox"
                          src={sandboxSrc || undefined}
                          srcDoc={sandboxSrc ? undefined : inlineDoc}
                          sandbox=""
                          referrerPolicy="no-referrer"
                          className="w-full min-h-[420px] bg-white"
                        />
                      </div>
                      <div className="text-xs text-gray-600 dark:text-gray-400 mt-2">
                        Rendered in an isolated sandbox (scripts/DOM access blocked).
                      </div>
                    </div>
                  )}

                  {textContent && !(isHtmlContent || isSvgContent) && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                        Text Content
                      </h4>
                      <div className="bg-blue-50 dark:bg-blue-900 border border-blue-200 dark:border-blue-700 rounded-lg p-4">
                        <div className="flex items-center justify-between mb-2">
                          <div className="flex items-start gap-2">
                            <span className="text-blue-600 dark:text-blue-400 text-sm">📄</span>
                            <span className="text-blue-800 dark:text-blue-200 text-sm font-medium">Inscription Text Data</span>
                          </div>
                          <CopyButton text={textContent} />
                        </div>
                        <div className="flex items-center gap-3 mb-3">
                          <label className="flex items-center gap-2 text-sm text-blue-800 dark:text-blue-200">
                            <input
                              type="checkbox"
                              checked={monoContent}
                              onChange={() => setMonoContent(!monoContent)}
                              className="form-checkbox h-4 w-4 text-blue-600"
                            />
                            Monospace
                          </label>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded p-4 max-h-96 min-h-[200px] overflow-y-auto w-full">
                          <pre className={`${monoContent ? 'font-mono text-sm' : 'font-sans text-sm'} text-blue-900 dark:text-blue-100 leading-relaxed whitespace-pre-wrap break-words max-w-full`}>
                            {textContent}
                          </pre>
                        </div>
                        <div className="mt-4 pt-4 border-t border-blue-200 dark:border-blue-700">
                          <div className="grid grid-cols-3 gap-4 w-full">
                            <div className="text-center">
                              <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">
                                {textContent.length}
                              </div>
                              <div className="text-sm text-blue-700 dark:text-blue-300">Characters</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">
                                {textContent.split(' ').filter(word => word.length > 0).length}
                              </div>
                              <div className="text-sm text-blue-700 dark:text-blue-300">Words</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">
                                {textContent.split('\n').length}
                              </div>
                              <div className="text-sm text-blue-700 dark:text-blue-300">Lines</div>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {(inscription.metadata?.extracted_message || scanLoading || scanMessage || stegoPayloadLoading || stegoPayload || stegoPayloadError || scanError) && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                        Hidden Message
                      </h4>
                      <div className="bg-green-50 dark:bg-green-900 border border-green-200 dark:border-green-700 rounded-lg p-4 space-y-4">
                        <div className="flex items-center justify-between">
                          <div className="flex items-start gap-2">
                            <span className="text-green-600 dark:text-green-400 text-sm">🔓</span>
                            <span className="text-green-800 dark:text-green-200 text-sm font-medium">Extracted Hidden Data</span>
                          </div>
                          <CopyButton text={hiddenMessageText} />
                        </div>

                        {scanLoading && (
                          <div className="text-sm text-green-700 dark:text-green-300">Scanning stego image…</div>
                        )}
                        {!scanLoading && scanError && (
                          <div className="text-sm text-amber-700 dark:text-amber-300">Scan unavailable: {scanError}</div>
                        )}
                        {stegoPayloadLoading && (
                          <div className="text-sm text-green-700 dark:text-green-300">Loading stego payload…</div>
                        )}
                        {!stegoPayloadLoading && stegoPayloadError && (
                          <div className="text-sm text-amber-700 dark:text-amber-300">Payload unavailable: {stegoPayloadError}</div>
                        )}

                        {stegoProposal ? (
                          <div className="space-y-3">
                            <div className="bg-white dark:bg-gray-800 rounded p-4">
                              <div className="text-xs text-green-700 dark:text-green-300 uppercase tracking-wide mb-2">Proposal</div>
                              <div className="text-lg font-semibold text-green-900 dark:text-green-100">{stegoProposal.title || 'Untitled'}</div>
                              {stegoProposal.description_md && (
                                <div className="text-sm text-green-800 dark:text-green-200 mt-2 whitespace-pre-wrap">
                                  {stegoProposal.description_md}
                                </div>
                              )}
                              <div className="mt-3 flex flex-wrap gap-4 text-xs text-green-700 dark:text-green-300">
                                <span>Budget: {stegoProposal.budget_sats || 0} sats</span>
                                <span>Visible Hash: {stegoProposal.visible_pixel_hash || '—'}</span>
                                {stegoProposalStatus && <span>Status: {stegoProposalStatus}</span>}
                              </div>
                            </div>

                            <div className="bg-white dark:bg-gray-800 rounded p-4">
                              <div className="text-xs text-green-700 dark:text-green-300 uppercase tracking-wide mb-2">Tasks</div>
                              {stegoTasks.length > 0 ? (
                                <div className="divide-y divide-gray-200 dark:divide-gray-700 text-sm">
                                  {stegoTasks.map((task) => (
                                    <div key={task.task_id || task.title} className="py-2 flex flex-col gap-1">
                                      <div className="flex items-center justify-between">
                                        <span className="text-green-900 dark:text-green-100 font-semibold">{task.title || 'Untitled task'}</span>
                                        <span className="text-green-700 dark:text-green-300 text-xs">
                                          {task.budget_sats || 0} sats
                                        </span>
                                      </div>
                                      {task.description && (
                                        <div className="text-green-800 dark:text-green-200 text-xs whitespace-pre-wrap">{task.description}</div>
                                      )}
                                      <div className="text-xs text-green-700 dark:text-green-300 flex flex-wrap gap-3">
                                        {task.task_id && <span>ID: {task.task_id}</span>}
                                        <span>Status: {stegoTaskStatusMap.get(task.task_id) || 'unknown'}</span>
                                      </div>
                                    </div>
                                  ))}
                                </div>
                              ) : (
                                <div className="text-sm text-green-700 dark:text-green-300">No tasks found in payload.</div>
                              )}
                            </div>
                          </div>
                        ) : (
                          <div className="bg-white dark:bg-gray-800 rounded p-4 max-h-96 min-h-[200px] overflow-y-auto w-full">
                            <pre className="text-green-900 dark:text-green-100 font-mono text-sm leading-relaxed whitespace-pre-wrap break-words max-w-full">
                              {hiddenMessageText}
                            </pre>
                          </div>
                        )}
                      </div>
                    </div>
                  )}

                  {!textContent && !inscription.metadata?.extracted_message && (
                    <div className="text-center py-12">
                      <div className="text-6xl mb-4">📦</div>
                      <div className="text-gray-600 dark:text-gray-400 font-semibold">No Text Content Available</div>
                      <div className="text-gray-500 dark:text-gray-500 text-sm mt-2">
                        This inscription contains binary data or media content that cannot be displayed as text.
                      </div>
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'technical' && (
                <div className="space-y-6">
                  {inscription.metadata?.extracted_message ? (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                        Extracted Hidden Message
                      </h4>
                      <div className="bg-gradient-to-br from-green-50 to-green-100 dark:from-green-900 dark:to-green-800 border border-green-200 dark:border-green-700 rounded-lg p-6">
                        <div className="flex items-start gap-3 mb-4">
                          <div className="w-8 h-8 bg-green-500 rounded-full flex items-center justify-center flex-shrink-0">
                            <span className="text-white text-lg">🔓</span>
                          </div>
                          <div>
                            <div className="text-green-900 dark:text-green-100 font-semibold text-lg">Successfully Decoded Message</div>
                            <div className="text-green-700 dark:text-green-300 text-sm">Hidden data extracted from steganographic carrier</div>
                          </div>
                        </div>
                        
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 border border-green-300 dark:border-green-600">
                          <div className="flex items-center justify-between mb-2">
                            <div className="text-green-800 dark:text-green-200 text-xs font-mono uppercase tracking-wider">Hidden Content:</div>
                            <CopyButton text={hiddenMessageText} />
                          </div>
                           <div className="bg-gray-50 dark:bg-gray-900 rounded p-3 max-h-64 overflow-y-auto">
                             <pre className="text-green-900 dark:text-green-100 font-mono text-sm leading-relaxed whitespace-pre-wrap break-words max-w-full">
                               {hiddenMessageText}
                             </pre>
                           </div>
                        </div>

                        <div className="mt-4 pt-4 border-t border-green-200 dark:border-green-700">
                          <div className="grid grid-cols-2 gap-4">
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {hiddenMessageText.length}
                              </div>
                              <div className="text-sm text-green-700 dark:text-green-300">Characters</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {hiddenMessageText.split(' ').length}
                              </div>
                              <div className="text-sm text-green-700 dark:text-green-300">Words</div>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  ) : (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-gray-500 rounded-full"></span>
                        Hidden Message Analysis
                      </h4>
                      <div className="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg p-6">
                        <div className="text-center">
                          <div className="text-6xl mb-4">🔍</div>
                          <div className="text-gray-600 dark:text-gray-400 font-semibold">No Hidden Message Detected</div>
                          <div className="text-gray-500 dark:text-gray-500 text-sm mt-2">
                            This contract may not contain extractable hidden data, or the message may be encoded using a different method.
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {inscription.metadata && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                        Message Analysis Details
                      </h4>
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-blue-50 dark:bg-blue-900 border border-blue-200 dark:border-blue-700 rounded-lg p-4">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">Encoding Method</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">
                            {inscription.metadata.stego_type?.includes('lsb') ? 'Least Significant Bit (LSB)' : 
                             inscription.metadata.stego_type?.includes('alpha') ? 'Alpha Channel' : 'Unknown'}
                          </div>
                          <div className="text-blue-600 dark:text-blue-400 text-xs mt-2">
                            {inscription.metadata.stego_type?.includes('lsb') ? 'Data hidden in image pixel values' : 
                             inscription.metadata.stego_type?.includes('alpha') ? 'Data hidden in transparency channel' : 'Unknown encoding method'}
                          </div>
                        </div>
                        <div className="bg-purple-50 dark:bg-purple-900 border border-purple-200 dark:border-purple-700 rounded-lg p-4">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Detection Confidence</div>
                          <ConfidenceIndicator confidence={inscription.metadata.confidence} />
                           <div className="text-purple-600 dark:text-purple-400 text-xs">
                             {inscription.metadata?.confidence ? 
                              (inscription.metadata.confidence >= 0.9 ? 'High confidence detection' :
                               inscription.metadata.confidence >= 0.7 ? 'Medium confidence detection' : 'Low confidence detection') :
                              'Analysis required for confidence assessment'}
                           </div>
                        </div>
                      </div>
                    </div>
                  )}

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-orange-500 rounded-full"></span>
                      Technical Architecture
                    </h4>
                    <div className="bg-gray-100 dark:bg-gray-900 rounded-lg p-4 max-h-64 overflow-y-auto">
                      <pre className="text-gray-700 dark:text-gray-300 text-sm whitespace-pre-wrap font-mono leading-relaxed max-w-full break-words">
                        {markdownContent}
                      </pre>
                    </div>
                  </div>
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

              {activeTab === 'blockchain' && (
                <div className="space-y-6">
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-purple-500 rounded-full"></span>
                      Transaction Information
                    </h4>
                    <div className="bg-purple-50 dark:bg-purple-900 border border-purple-200 dark:border-purple-700 rounded-lg p-4">
                      <div className="space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-purple-700 dark:text-purple-300 text-sm">Transaction ID</span>
                          <div className="flex items-center gap-2">
                            <span className="text-purple-900 dark:text-purple-100 font-mono text-xs">
                              {inscription.id?.slice(0, 12)}...
                            </span>
                            <CopyButton text={inscription.id || ''} />
                          </div>
                        </div>
                        <div className="flex items-center justify-between">
                          <span className="text-purple-700 dark:text-purple-300 text-sm">Block Height</span>
                          <span className="text-purple-900 dark:text-purple-100 font-semibold">
                            {inscription.block_height || inscription.genesis_block_height || 'Unknown'}
                          </span>
                        </div>
                         <div className="flex items-center justify-between">
                           <span className="text-purple-700 dark:text-purple-300 text-sm">Network</span>
                           <span className="text-purple-900 dark:text-purple-100 font-semibold">Bitcoin {network.charAt(0).toUpperCase() + network.slice(1)}</span>
                         </div>
                      </div>
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                      File Details
                    </h4>
                    <div className="bg-blue-50 dark:bg-blue-900 border border-blue-200 dark:border-blue-700 rounded-lg p-4">
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">File Name</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold break-all">{inscription.file_name || 'N/A'}</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">File Size</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">{inscription.size_bytes ? `${(inscription.size_bytes / 1024).toFixed(2)} KB` : 'Unknown'}</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">Content Type</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">{inscription.mime_type || 'Unknown'}</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">Contract Type</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">{inscription.contract_type || 'Standard'}</div>
                        </div>
                      </div>
                    </div>
                  </div>

                  {inscription.metadata?.scanned_at && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                        Analysis Information
                      </h4>
                      <div className="bg-green-50 dark:bg-green-900 border border-green-200 dark:border-green-700 rounded-lg p-4">
                        <div className="grid grid-cols-2 gap-4">
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-green-700 dark:text-green-300 text-xs mb-1">Scan Status</div>
                            <div className="text-green-900 dark:text-green-100 font-semibold">
                              {inscription.metadata.is_stego ? 'Steganography Detected' : 'No Hidden Data'}
                            </div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-green-700 dark:text-green-300 text-xs mb-1">Last Scanned</div>
                            <div className="text-green-900 dark:text-green-100 font-semibold">
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
