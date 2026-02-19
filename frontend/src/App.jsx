import React, { useState, useEffect, useRef } from 'react';
import { Check, Copy, Github, Linkedin } from 'lucide-react';
import { Routes, Route, useParams, useNavigate } from 'react-router-dom';


import BlockCard from './components/Block/BlockCard';
import InscriptionCard from './components/Inscription/InscriptionCard';
import OpenContractsView from './components/Block/OpenContractsView';
import InscribeModal from './components/Inscription/InscribeModal';
import InscriptionModal from './components/Inscription/InscriptionModal';
import DiscoverPage from './components/Discover/DiscoverPage';
import AuthPage from './pages/AuthPage';
import ContractsPage from './pages/ContractsPage';
import McpDocsPage from './pages/McpDocsPage';
import DocsPage from './pages/DocsPage';
import AppHeader from './components/Common/AppHeader';
import { AuthProvider, useAuth } from './context/AuthContext';
import { ThemeProvider } from './context/ThemeContext';

import { useBlocks } from './hooks/useBlocks';
import { useInscriptions } from './hooks/useInscriptions';
import { API_BASE, CONTENT_BASE } from './apiBase';

import { useHorizontalScroll } from './hooks/useHorizontalScroll';

const formatTimeAgo = (timestamp) => {
  const now = Date.now();
  const blockTime = timestamp * 1000;
  const diffMs = now - blockTime;
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffHours / 24);
  
  if (diffDays > 0) {
    return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`;
  } else if (diffHours > 0) {
    return `${diffHours} hour${diffHours > 1 ? 's' : ''} ago`;
  } else if (diffMs > 60000) {
    return `${Math.floor(diffMs / 60000)} minute${Math.floor(diffMs / 60000) > 1 ? 's' : ''} ago`;
  } else {
    return 'Just now';
  }
};

function MainContent() {
  const { height, wishId, contractId, proposalId } = useParams();
  const navigate = useNavigate();
  const { auth } = useAuth();
  const [showInscribeModal, setShowInscribeModal] = useState(false);
  const [selectedInscription, setSelectedInscription] = useState(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState(null);
  const [copiedText, setCopiedText] = useState('');
  const sentinelRef = useRef(null);
  const [hideBrc20, setHideBrc20] = useState(true);
  const [pendingRefreshKey, setPendingRefreshKey] = useState(0);
  const { elRef: scrollRef, isDragging } = useHorizontalScroll();
  const prevBlocksLengthRef = useRef(0);

  const {
    blocks,
    selectedBlock,
    isUserNavigating,
    handleBlockSelect: originalHandleBlockSelect,
    setSelectedBlock,
    setIsUserNavigating,
    setManualHeight,
    loadMoreBlocks,
    refreshBlocks
  } = useBlocks();

  useEffect(() => {
    const newBlock = blocks.find(b => b.isNew);
    if (newBlock && scrollRef.current && blocks.length > prevBlocksLengthRef.current) {
      scrollRef.current.scrollTo({
        left: 0,
        behavior: 'smooth'
      });
    }
    prevBlocksLengthRef.current = blocks.length;
  }, [blocks, scrollRef]);

  const handleBlockSelect = (block) => {
    if (isDragging) return; // Prevent click if a drag occurred
    originalHandleBlockSelect(block);
    navigate(`/block/${block.height}`);
  };

  const {
    inscriptions,
    hasMoreImages,
    loadMoreInscriptions,
    isLoading: isLoadingInscriptions,
    error: inscriptionsError
  } = useInscriptions(selectedBlock);



  const filteredInscriptions = inscriptions.filter((inscription) => {
    if (!hideBrc20) return true;
    const text = inscription.text || '';
    const name = inscription.file_name || '';
    const isBrc = text.toLowerCase().includes('brc-20') || text.toLowerCase().includes('brc20') || name.toLowerCase().includes('brc-20') || name.toLowerCase().includes('brc20');
    return !isBrc;
  });

  // Track if we're navigating via URL to prevent loops
  const urlNavigatedHeightRef = useRef(null);
  const lastUrlNavTimeRef = useRef(0);

  // Reset URL navigation ref when leaving block route
  useEffect(() => {
    if (!height) {
      urlNavigatedHeightRef.current = null;
    }
  }, [height]);

  // Handle URL navigation to specific block
  useEffect(() => {
    if (height && !searchResults && urlNavigatedHeightRef.current !== height) {
      const blockHeight = parseInt(height, 10);
      if (!isNaN(blockHeight)) {
        urlNavigatedHeightRef.current = height;
        lastUrlNavTimeRef.current = Date.now();
        // Tell useBlocks which block we want - it will handle placeholder/selection
        setManualHeight(blockHeight);
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [height, searchResults]);

  // Handle deep linking for wishes, contracts, and proposals
  useEffect(() => {
    const deepLinkId = wishId || contractId || proposalId;
    if (!deepLinkId) return;

    const fetchDeepLink = async () => {
      try {
        let searchId = deepLinkId;
        const headers = {};
        if (auth.apiKey) {
          headers['X-API-Key'] = auth.apiKey;
        }
        
        let proposalData = null;

        // If it's a proposal ID, we might need to find the associated contract first
        if (proposalId) {
          try {
            const propRes = await fetch(`${API_BASE}/api/smart_contract/proposals/${proposalId}`, { headers });
            if (propRes.ok) {
              proposalData = await propRes.json();
              searchId = proposalData.visible_pixel_hash || 
                         proposalData.metadata?.visible_pixel_hash || 
                         proposalData.metadata?.contract_id || 
                         proposalData.id;
            }
          } catch (e) {
            console.error("Failed to fetch proposal details", e);
          }
        }

        const response = await fetch(`${API_BASE}/api/search?q=${encodeURIComponent(searchId)}`, { headers });
        const data = await response.json();
        const payload = data?.data || data;
        
        // Prioritize contracts, then inscriptions.
        const contract = payload?.contracts?.[0];
        const inscription = payload?.inscriptions?.[0];
        
        if (contract) {
             const metadata = contract.metadata || {};
             const visiblePixelHash = contract.visible_pixel_hash || metadata.visible_pixel_hash || '';
             const contractId = contract.contract_id || contract.contractId || contract.id;
             
             // Determine image URL based on status
             let imageUrl = '';
             const status = (contract.status || metadata.confirmation_status || '').toLowerCase();
             const confirmedTxid = metadata.confirmed_txid || metadata.tx_id || '';
             
             if (status === 'confirmed' && confirmedTxid) {
               imageUrl = `${CONTENT_BASE}/content/${confirmedTxid}`;
             } else if (visiblePixelHash) {
               imageUrl = `${CONTENT_BASE}/uploads/${visiblePixelHash}`;
             } else if (contract.stego_image_url || contract.stegoImageUrl) {
               const rawUrl = contract.stego_image_url || contract.stegoImageUrl || '';
               imageUrl = rawUrl && !rawUrl.startsWith('http') ? `${CONTENT_BASE}${rawUrl}` : rawUrl;
             }
             
             const wishText = metadata.wish_text || metadata.embedded_message || metadata.message || '';
             const item = {
               id: contractId,
               contract_type: contract.contract_type || 'Smart Contract',
               metadata: {
                 ...metadata,
                 visible_pixel_hash: visiblePixelHash,
                 is_stego: true,
               },
               image_url: imageUrl,
               mime_type: metadata.content_type || 'image/png',
               text: wishText,
               genesis_block_height: contract.block_height || 0,
               block_height: contract.block_height || 0,
               status: status || 'pending',
             };
             setSelectedInscription(item);
        } else if (inscription) {
            const item = {
                id: inscription.id,
                contractType: 'Custom Contract',
                capability: 'Data Storage',
                protocol: 'BRC-20',
                apiEndpoints: 0,
                interactions: 0,
                reputation: 'N/A',
                isActive: inscription.status === 'confirmed',
                number: parseInt(inscription.id?.split('_')[1]) || 0,
                address: 'bc1q...',
                genesis_block_height: inscription.blockHeight || 0,
                mime_type: 'text/plain',
                text: inscription.text || '',
                metadata: inscription.metadata || {},
            };
             setSelectedInscription(item);
        } else if (proposalData) {
            // Fallback: If search failed but we have proposal data, create a synthetic item
            const meta = proposalData.metadata || {};
            const stegoId = proposalData.visible_pixel_hash || meta.visible_pixel_hash || meta.contract_id;
            
            // Determine image source:
            // 1. If ID looks like an inscription ID (has 'i'), it's definitely content.
            // 2. If status is pending/approved, it's likely in uploads (pre-chain).
            // 3. Otherwise (published/confirmed), assume content.
            const isInscriptionId = stegoId && stegoId.includes('i');
            const isPending = ['pending', 'approved'].includes((proposalData.status || '').toLowerCase());
            const imageBase = (isInscriptionId || !isPending) ? 'content' : 'uploads';

            const item = {
                id: stegoId || proposalData.id,
                contract_type: 'Steganographic Contract', // Ensure it looks like a contract
                metadata: {
                    ...meta,
                    visible_pixel_hash: proposalData.visible_pixel_hash,
                    contract_id: meta.contract_id,
                    wish_text: proposalData.description_md,
                    is_stego: true, // Hint to modal to show stego analysis sections
                    stego_type: 'alpha' // Default assumption if missing
                },
                image_url: stegoId ? `${CONTENT_BASE}/${imageBase}/${stegoId}` : null,
                mime_type: stegoId ? 'image/png' : 'application/json',
                text: proposalData.description_md || proposalData.title || '',
                status: proposalData.status,
                genesis_block_height: 0 // Unknown without search
            };
            setSelectedInscription(item);
        } else if (wishId || contractId) {
            // Fallback: Try direct inscription fetch
            try {
                const directRes = await fetch(`${API_BASE}/api/data/inscriptions/${wishId || contractId}`);
                if (directRes.ok) {
                    const directData = await directRes.json();
                    const ins = directData.inscription || directData;
                    if (ins) {
                         const item = {
                            id: ins.tx_id || ins.id,
                            contractType: ins.is_stego ? 'Smart Contract' : 'Inscription',
                            mime_type: ins.content_type,
                            text: ins.content,
                            metadata: ins.metadata || {},
                            image_url: `${CONTENT_BASE}/content/${ins.tx_id || ins.id}`,
                            genesis_block_height: ins.genesis_height || 0
                        };
                        setSelectedInscription(item);
                    }
                }
            } catch (e) {
                console.error("Direct fetch failed", e);
            }
        }
      } catch (error) {
        console.error("Deep link fetch failed", error);
      }
    };
    
    fetchDeepLink();
  }, [wishId, contractId, proposalId, auth.apiKey]);

  useEffect(() => {
    const targetHeight = height !== undefined ? parseInt(height, 10) : null;
    // Only hydrate selection from the route when we don't already have one.
    if (!selectedBlock && targetHeight !== null && !Number.isNaN(targetHeight) && blocks.length > 0) {
      const block = blocks.find(b => b.height === targetHeight);
      if (block) {
        setSelectedBlock(block);
        setIsUserNavigating(true);
      }

    }
  }, [height, blocks, selectedBlock, setSelectedBlock, setIsUserNavigating, navigate]);

  useEffect(() => {
    // If we are deep-linking to an item, do not auto-select the pending block
    if (wishId || contractId || proposalId) return;

    if (!blocks.length) return;

    // Auto-select pending block on initial load (both root and /pending routes)
    // Only do this once when nothing is selected yet
    const pendingBlock = blocks.find((b) => b.isFuture);
    if (pendingBlock && !selectedBlock) {
      setSelectedBlock(pendingBlock);
      setIsUserNavigating(true);
    }
  }, [blocks, selectedBlock, setSelectedBlock, setIsUserNavigating, wishId, contractId, proposalId]);

  // If selection and route diverge (race between click and router), force the route to the selected block.
  // But strictly AVOID this if we are on a deep-link route or just did URL-driven navigation.
  useEffect(() => {
    if (wishId || contractId || proposalId) return;
    // Skip if we just did URL-driven navigation (within last 500ms)
    if (Date.now() - lastUrlNavTimeRef.current < 500) return;

    if (!selectedBlock) return;
    const currentHeight = height !== undefined ? parseInt(height, 10) : null;
    if (currentHeight !== selectedBlock.height) {
      navigate(`/block/${selectedBlock.height}`, { replace: true });
      // Clear the user navigation flag once the route is synced
      setIsUserNavigating(false);
    }
  }, [selectedBlock, height, navigate, setIsUserNavigating, wishId, contractId, proposalId]);

  useEffect(() => {
    if (!hasMoreImages || !sentinelRef.current) return;

    const sentinel = sentinelRef.current;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          loadMoreInscriptions();
        }
      },
      { threshold: 1.0 }
    );

    observer.observe(sentinel);

    return () => {
      observer.unobserve(sentinel);
    };
  }, [hasMoreImages, loadMoreInscriptions]);



  // Stop auto-scrolling; only user-driven navigation
  useEffect(() => {
    if (isUserNavigating) {
      setIsUserNavigating(false);
    }
  }, [selectedBlock, isUserNavigating, setIsUserNavigating]);

  const handleSearch = async (query) => {
    if (query.trim() === '') {
      setSearchResults(null);
      return;
    }

    try {
      const response = await fetch(`${API_BASE}/api/search?q=${encodeURIComponent(query)}`);
      const data = await response.json();
      const payload = data?.data || data;
      setSearchResults(payload);
    } catch (error) {
      console.error('Search error:', error);
      setSearchResults({ inscriptions: [], blocks: [] });
    }
  };

  const clearSearch = () => {
    setSearchQuery('');
    setSearchResults(null);
  };

  const copyToClipboard = async (text) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedText(text);
      setTimeout(() => setCopiedText(''), 2000);
    } catch (error) {
      console.error('Failed to copy:', error);
    }
  };

  const handleInscribeSuccess = () => {
    refreshBlocks();
    setPendingRefreshKey((prev) => prev + 1);
  };



  const renderInlineSearch = () => {
    if (searchResults === null) return null;

    const inscriptionResults = searchResults?.inscriptions || [];
    const transactionResults = searchResults?.transactions || [];
    const blockResults = searchResults?.blocks || [];
    const contractResults = searchResults?.contracts || [];
    const proposalResults = searchResults?.proposals || [];
    
    const hasResults =
      (inscriptionResults && inscriptionResults.length > 0) ||
      (transactionResults && transactionResults.length > 0) ||
      (blockResults && blockResults.length > 0) ||
      (contractResults && contractResults.length > 0) ||
      (proposalResults && proposalResults.length > 0);
    if (!hasResults) return null;

    const onSelectBlock = (block) => {
      navigate(`/block/${block.block_height || block.height}`);
      clearSearch();
    };

    const onSelectInscription = (tx) => {
      if (tx.block_height) {
        navigate(`/block/${tx.block_height}`);
      }
      clearSearch();
    };

    const onSelectTransaction = (tx) => {
      if (tx.block_height) {
        navigate(`/block/${tx.block_height}`);
      }
      clearSearch();
    };

    const onSelectContract = (contract) => {
      navigate(`/contract/${contract.contract_id || contract.id}`);
      clearSearch();
    };

    const onSelectProposal = (proposal) => {
      navigate(`/proposal/${proposal.proposal_id || proposal.id}`);
      clearSearch();
    };

    return (
      <div className="absolute mt-2 w-96 max-h-96 overflow-y-auto dropdown-menu rounded-lg shadow-lg z-50">
        {inscriptionResults.length > 0 && (
          <div className="p-3 border-b border-white/10">
            <div className="text-xs uppercase tracking-wide text-secondary mb-2">Inscriptions</div>
            <div className="space-y-2">
              {inscriptionResults.slice(0, 5).map((tx, idx) => (
                <button
                  key={`ins-${idx}`}
                  onClick={() => onSelectInscription(tx)}
                  className="w-full text-left p-2 rounded bg-white/5 hover:bg-white/10 transition-colors"
                >
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="px-2 py-0.5 rounded bg-yellow-600 text-white text-[11px]">Inscription</span>
                    <span className="text-secondary">{tx.status || 'confirmed'}</span>
                  </div>
                  <div className="text-primary font-mono text-xs break-all">{tx.id}</div>
                  {tx.text && <div className="text-secondary text-xs truncate mt-1">{tx.text}</div>}
                </button>
              ))}
            </div>
          </div>
        )}
        {transactionResults.length > 0 && (
          <div className="p-3 border-b border-white/10">
            <div className="text-xs uppercase tracking-wide text-secondary mb-2">Transactions</div>
            <div className="space-y-2">
              {transactionResults.slice(0, 5).map((tx, idx) => (
                <button
                  key={`tx-${idx}`}
                  onClick={() => onSelectTransaction(tx)}
                  className="w-full text-left p-2 rounded bg-white/5 hover:bg-white/10 transition-colors"
                >
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="px-2 py-0.5 rounded bg-blue-600 text-white text-[11px]">Transaction</span>
                    <span className="text-secondary">{tx.status || 'confirmed'}</span>
                  </div>
                  <div className="text-primary font-mono text-xs break-all">{tx.id}</div>
                  {tx.text && <div className="text-secondary text-xs truncate mt-1">{tx.text}</div>}
                </button>
              ))}
            </div>
          </div>
        )}
        {contractResults.length > 0 && (
          <div className="p-3 border-b border-white/10">
            <div className="text-xs uppercase tracking-wide text-secondary mb-2">Contracts</div>
            <div className="space-y-2">
              {contractResults.slice(0, 5).map((contract, idx) => (
                <button
                  key={`ctr-${idx}`}
                  onClick={() => onSelectContract(contract)}
                  className="w-full text-left p-2 rounded bg-white/5 hover:bg-white/10 transition-colors"
                >
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="px-2 py-0.5 rounded bg-emerald-600 text-white text-[11px]">Contract</span>
                    <span className="text-emerald-500 font-bold">{contract.status || 'open'}</span>
                  </div>
                  <div className="text-primary font-mono text-xs break-all">
                    {contract.contract_id || contract.id}
                  </div>
                  {contract.title && <div className="text-secondary text-xs truncate mt-1">{contract.title}</div>}
                </button>
              ))}
            </div>
          </div>
        )}
        {proposalResults.length > 0 && (
          <div className="p-3 border-b border-white/10">
            <div className="text-xs uppercase tracking-wide text-secondary mb-2">Proposals</div>
            <div className="space-y-2">
              {proposalResults.slice(0, 5).map((proposal, idx) => (
                <button
                  key={`prop-${idx}`}
                  onClick={() => onSelectProposal(proposal)}
                  className="w-full text-left p-2 rounded bg-white/5 hover:bg-white/10 transition-colors"
                >
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="px-2 py-0.5 rounded bg-purple-600 text-white text-[11px]">Proposal</span>
                    <span className="text-purple-400 font-bold">{proposal.status || 'pending'}</span>
                  </div>
                  <div className="text-primary font-mono text-xs break-all">
                    {proposal.proposal_id || proposal.id}
                  </div>
                  {proposal.title && <div className="text-secondary text-xs truncate mt-1">{proposal.title}</div>}
                </button>
              ))}
            </div>
          </div>
        )}
        {blockResults.length > 0 && (
          <div className="p-3">
            <div className="text-xs uppercase tracking-wide text-secondary mb-2">Blocks</div>
            <div className="space-y-2">
              {blockResults.slice(0, 5).map((b, idx) => (
                <button
                  key={`blk-${idx}`}
                  onClick={() => onSelectBlock(b)}
                  className="w-full text-left p-2 rounded bg-white/5 hover:bg-white/10 transition-colors"
                >
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="px-2 py-0.5 rounded bg-primary text-white text-[11px]">Block</span>
                    <span className="text-secondary">{b.tx_count || 0} tx</span>
                  </div>
                  <div className="text-primary font-mono text-xs break-all">
                    #{b.block_height || b.height} • {b.id}
                  </div>
                  {b.timestamp && (
                    <div className="text-secondary text-[11px] mt-1">
                      {new Date(b.timestamp * 1000).toLocaleString()}
                    </div>
                  )}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="min-h-screen bg-app-main text-primary">
      <AppHeader
        onInscribe={() => setShowInscribeModal(true)}
        showSearch
        searchQuery={searchQuery}
        onSearchChange={(value) => {
          setSearchQuery(value);
          handleSearch(value);
        }}
        onClearSearch={clearSearch}
        renderInlineSearch={renderInlineSearch}
        showBrcToggle
        hideBrc20={hideBrc20}
        onToggleBrc20={() => setHideBrc20(!hideBrc20)}
      />

      <div className="pt-0">
        <div
          id="block-scroll"
          ref={scrollRef}
          className="flex gap-4 overflow-x-auto whitespace-nowrap py-6 px-6 border-b border-white/5"
          style={{ scrollbarWidth: 'none', msOverflowStyle: 'none' }}
          onScroll={(e) => {
            const el = e.currentTarget;
            if (el.scrollLeft + el.clientWidth >= el.scrollWidth - 50) {
              loadMoreBlocks();
            }
          }}
        >
          {blocks.map((block, index) => (
            <BlockCard
              key={`${block.height}-${index}`}
              block={block}
              onClick={handleBlockSelect}
              isSelected={selectedBlock?.height === block.height}
            />
          ))}
        </div>

        <div className="w-full px-4 sm:px-6 lg:px-8 xl:px-12 py-8">
          {searchResults !== null ? (
            <div className="mb-8">
              <h2 className="text-4xl font-bold mb-4 text-primary">Search Results</h2>

              {searchResults.inscriptions && searchResults.inscriptions.length > 0 && (
                <div className="mb-8">
                  <h3 className="text-primary text-lg font-semibold border-b-2 border-warning pb-2 inline-block mb-4">
                    Inscriptions
                  </h3>
                  <div className="space-y-3">
                    {searchResults.inscriptions.map((tx, idx) => (
                      <div
                        key={idx}
                        className="bg-white/5 border border-white/10 rounded-lg p-4 cursor-pointer hover:bg-white/10 transition-colors"
                        onClick={() => {
                          if (tx.block_height) {
                            navigate(`/block/${tx.block_height}`);
                          }
                          clearSearch();
                        }}
                      >
                        <div className="flex justify-between items-start mb-3">
                          <div className="flex items-center gap-3">
                            <div className="px-3 py-1 rounded text-xs font-semibold bg-yellow-600 text-white">
                              Inscription
                            </div>
                            <div className="text-primary font-mono text-sm">
                              {tx.id}
                            </div>
                          </div>
                          <div className="px-2 py-1 rounded text-xs font-semibold bg-white/5 text-secondary border border-white/10">
                            {tx.status || 'confirmed'}
                          </div>
                        </div>

                        <div className="grid grid-cols-2 gap-4 text-sm">
                          <div>
                            <div className="text-secondary mb-1">Block Height</div>
                            <div className="text-primary">{tx.block_height || '—'}</div>
                          </div>
                          <div>
                            <div className="text-secondary mb-1">Text Length</div>
                            <div className="text-primary">{tx.text?.length || 0} chars</div>
                          </div>
                        </div>

                        <div className="mt-3 text-xs text-secondary">
                          {tx.timestamp
                            ? `Submitted ${new Date(tx.timestamp * 1000).toLocaleString()}`
                            : 'Submitted —'}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {searchResults.transactions && searchResults.transactions.length > 0 && (
                <div className="mb-8">
                  <h3 className="text-primary text-lg font-semibold border-b-2 border-blue-500 pb-2 inline-block mb-4">
                    Transactions
                  </h3>
                  <div className="space-y-3">
                    {searchResults.transactions.map((tx, idx) => (
                      <div
                        key={idx}
                        className="bg-white/5 border border-white/10 rounded-lg p-4 cursor-pointer hover:bg-white/10 transition-colors"
                        onClick={() => {
                          if (tx.block_height) {
                            navigate(`/block/${tx.block_height}`);
                          }
                          clearSearch();
                        }}
                      >
                        <div className="flex justify-between items-start mb-3">
                          <div className="flex items-center gap-3">
                            <div className="px-3 py-1 rounded text-xs font-semibold bg-blue-600 text-white">
                              Transaction
                            </div>
                            <div className="text-primary font-mono text-sm">
                              {tx.id}
                            </div>
                          </div>
                          <div className="px-2 py-1 rounded text-xs font-semibold bg-white/5 text-secondary border border-white/10">
                            {tx.status || 'confirmed'}
                          </div>
                        </div>

                        <div className="grid grid-cols-2 gap-4 text-sm">
                          <div>
                            <div className="text-secondary mb-1">Block Height</div>
                            <div className="text-primary">{tx.block_height || '—'}</div>
                          </div>
                          <div>
                            <div className="text-secondary mb-1">Type</div>
                            <div className="text-primary">{tx.type || 'transaction'}</div>
                          </div>
                        </div>

                        {tx.text && (
                          <div className="mt-3 text-xs text-secondary truncate">
                            {tx.text}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {searchResults.contracts && searchResults.contracts.length > 0 && (
                <div className="mb-8">
                  <h3 className="text-secondary text-lg font-semibold border-b-2 border-success pb-2 inline-block mb-4">
                    Contracts
                  </h3>
                  <div className="space-y-3">
                    {searchResults.contracts.map((contract, idx) => (
                      <div
                        key={`contract-${idx}`}
                        className="bg-white/5 border border-white/10 rounded-lg p-4 cursor-pointer hover:bg-white/10 transition-colors"
                        onClick={() => {
                          navigate(`/contract/${contract.contract_id || contract.id}`);
                          clearSearch();
                        }}
                      >
                        <div className="flex justify-between items-start mb-3">
                          <div className="flex items-center gap-3">
                            <div className="px-3 py-1 rounded text-xs font-semibold bg-emerald-600 text-white">
                              Contract
                            </div>
                            <div className="text-primary font-mono text-sm">
                              {contract.contract_id || contract.id}
                            </div>
                          </div>
                          <div className="px-2 py-1 rounded text-xs font-semibold bg-white/5 text-secondary border border-white/10">
                            {contract.status || 'open'}
                          </div>
                        </div>

                        <div className="grid grid-cols-2 gap-4 text-sm">
                          <div>
                            <div className="text-secondary mb-1">Block</div>
                            <div className="text-primary">
                              {contract.block_height || '—'}
                            </div>
                          </div>
                          <div>
                            <div className="text-secondary mb-1">Budget</div>
                            <div className="text-primary font-mono truncate">
                              {contract.budget_sats ? `${contract.budget_sats} sats` : '—'}
                            </div>
                          </div>
                        </div>

                        {contract.title && (
                          <div className="mt-3 text-xs text-secondary truncate">
                            {contract.title}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {searchResults.proposals && searchResults.proposals.length > 0 && (
                <div className="mb-8">
                  <h3 className="text-purple-400 text-lg font-semibold border-b-2 border-purple-500 pb-2 inline-block mb-4">
                    Proposals
                  </h3>
                  <div className="space-y-3">
                    {searchResults.proposals.map((proposal, idx) => (
                      <div
                        key={`proposal-${idx}`}
                        className="bg-white/5 border border-white/10 rounded-lg p-4 cursor-pointer hover:bg-white/10 transition-colors"
                        onClick={() => {
                          navigate(`/proposal/${proposal.proposal_id || proposal.id}`);
                          clearSearch();
                        }}
                      >
                        <div className="flex justify-between items-start mb-3">
                          <div className="flex items-center gap-3">
                            <div className="px-3 py-1 rounded text-xs font-semibold bg-purple-600 text-white">
                              Proposal
                            </div>
                            <div className="text-primary font-mono text-sm">
                              {proposal.proposal_id || proposal.id}
                            </div>
                          </div>
                          <div className="px-2 py-1 rounded text-xs font-semibold bg-white/5 text-purple-400 border border-white/10">
                            {proposal.status || 'pending'}
                          </div>
                        </div>

                        <div className="grid grid-cols-2 gap-4 text-sm">
                          <div>
                            <div className="text-secondary mb-1">Title</div>
                            <div className="text-primary truncate">{proposal.title || '—'}</div>
                          </div>
                          <div>
                            <div className="text-secondary mb-1">Budget</div>
                            <div className="text-primary font-mono truncate">
                              {proposal.budget_sats ? `${proposal.budget_sats} sats` : '—'}
                            </div>
                          </div>
                        </div>

                        {proposal.visible_pixel_hash && (
                          <div className="mt-3 text-xs text-secondary truncate">
                            Pixel Hash: {proposal.visible_pixel_hash}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {searchResults.blocks && searchResults.blocks.length > 0 && (
                <div className="mb-8">
                  <h3 className="text-primary text-lg font-semibold border-b-2 border-indigo-500 pb-2 inline-block mb-4">
                    Blocks
                  </h3>
                  <div className="space-y-3">
                    {searchResults.blocks.map((b, idx) => (
                      <div
                        key={`block-${idx}`}
                        className="bg-white/5 border border-white/10 rounded-lg p-4 cursor-pointer hover:bg-white/10 transition-colors"
                        onClick={() => {
                          navigate(`/block/${b.block_height || b.height}`);
                          clearSearch();
                        }}
                      >
                        <div className="flex justify-between items-start mb-3">
                          <div className="flex items-center gap-3">
                            <div className="px-3 py-1 rounded text-xs font-semibold bg-indigo-600 text-white">
                              Block
                            </div>
                            <div className="text-primary font-mono text-sm">
                              #{b.block_height || b.height}
                            </div>
                          </div>
                          <div className="px-2 py-1 rounded text-xs font-semibold bg-white/5 text-secondary border border-white/10">
                            {b.tx_count || 0} tx
                          </div>
                        </div>

                        <div className="text-xs text-secondary font-mono truncate">
                          {b.id}
                        </div>

                        {b.timestamp && (
                          <div className="mt-3 text-xs text-secondary">
                            {new Date(b.timestamp * 1000).toLocaleString()}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {(!searchResults.inscriptions || searchResults.inscriptions.length === 0) &&
               (!searchResults.transactions || searchResults.transactions.length === 0) &&
               (!searchResults.contracts || searchResults.contracts.length === 0) &&
               (!searchResults.proposals || searchResults.proposals.length === 0) &&
               (!searchResults.blocks || searchResults.blocks.length === 0) && (
                <div className="text-center py-8 text-secondary">
                  No results found for "{searchQuery}"
                </div>
              )}
            </div>
          ) : selectedBlock && (
            <>
              <div className="mb-8">
                <h2 className="text-4xl font-bold mb-4 text-primary">Block {selectedBlock.height}</h2>
                <div className="flex items-center gap-4 text-sm">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="text-secondary truncate max-w-[200px] sm:max-w-none">{selectedBlock.hash}</span>
                    {copiedText === selectedBlock.hash ? (
                      <Check className="w-4 h-4 text-success flex-shrink-0" />
                    ) : (
                      <Copy
                        className="w-4 h-4 text-secondary hover:text-primary cursor-pointer flex-shrink-0"
                        onClick={() => copyToClipboard(selectedBlock.hash)}
                      />
                    )}
                  </div>
                </div>
                <div className="text-secondary text-sm mt-2">
                  {new Date(selectedBlock.timestamp * 1000).toLocaleString()} ({formatTimeAgo(selectedBlock.timestamp)})
                </div>
              </div>

              {selectedBlock.isFuture ? (
                <OpenContractsView
                  setSelectedInscription={setSelectedInscription}
                  refreshKey={pendingRefreshKey}
                />
              ) : (
                <div className="mb-4">
                  <div className="mb-6">
                    <h3 className="text-primary text-lg font-bold border-b-2 border-indigo-500 pb-2 inline-block">
                      Smart Contracts
                    </h3>
                  </div>
                  {inscriptionsError && (
                    <div className="mb-4 rounded-lg border border-error bg-error/10 text-error px-4 py-3 text-sm">
                      {inscriptionsError}
                    </div>
                  )}

                   {filteredInscriptions.length === 0 && isLoadingInscriptions && (
                     <div className="starlight-gallery">
                       {Array.from({ length: 6 }).map((_, i) => (
                         <div key={i} className="aspect-square rounded-2xl bg-white/5 animate-pulse" />
                       ))}
                     </div>
                   )}

                   {filteredInscriptions.length === 0 && !isLoadingInscriptions && (
                      <div className="text-secondary py-6">
                        No inscriptions found for this block.
                      </div>
                    )}

                     {filteredInscriptions.length > 0 && (
                       <div className="starlight-gallery">
                         {filteredInscriptions.map((inscription, idx) => (
                           <div key={idx} className="gallery-item">
                             <InscriptionCard
                               inscription={inscription}
                               onClick={setSelectedInscription}
                             />
                           </div>
                         ))}
                          {hasMoreImages && (
                            <div ref={sentinelRef} className="flex justify-center py-8 col-span-full">
                              <div className="spinner border-2" />
                            </div>
                          )}
                       </div>
                     )}
                </div>
              )}
            </>
          )}
        </div>

        {/* Intelligent Footer: only shows when reached end of content */}
        {!hasMoreImages && blocks.length > 0 && (
          <footer className="nav-glass h-16 flex flex-row items-center border-t border-white/5 relative z-0 shrink-0 mt-auto">
            <div className="container mx-auto px-6 h-full flex flex-row items-center justify-between gap-12">
              <div className="flex flex-row items-center gap-3">
                <div className="flex items-center justify-center w-7 h-7 bg-starlight rounded-lg glow-blue">
                  <span className="text-white text-[10px] font-extrabold">✦</span>
                </div>
                 <span className="text-lg font-bold text-gradient-starlight">Starlight</span>
              </div>

              <div className="flex flex-row items-center gap-8">
                <a
                  href="https://github.com/macroadster"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="nav-link p-0 text-secondary hover:text-primary"
                  title="GitHub"
                >
                  <Github className="w-5 h-5" />
                </a>
                <a
                  href="https://x.com/howssatoshi"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="nav-link p-0 text-muted hover:text-primary"
                  title="X (Twitter)"
                >
                  <span className="text-xl font-bold leading-none">𝕏</span>
                </a>
                <a
                  href="https://www.linkedin.com/in/eric-yang-182a377/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="nav-link p-0 text-muted hover:text-primary"
                  title="LinkedIn"
                >
                  <Linkedin className="w-5 h-5" />
                </a>
              </div>

                <a href="/mcp/docs" className="nav-link text-muted text-sm hover:text-primary whitespace-nowrap hidden md:block">
                  💡 Are you a builder? Try our API!
                </a>
             </div>
           </footer>
         )}
       </div>

      {showInscribeModal && (
        <InscribeModal
          onClose={() => setShowInscribeModal(false)}
          onSuccess={handleInscribeSuccess}
        />
      )}

      {selectedInscription && (
        <InscriptionModal
          inscription={selectedInscription}
          onClose={() => setSelectedInscription(null)}
          initialTab={proposalId ? 'proposals' : 'content'}
        />
      )}
    </div>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <ThemeProvider>
        <Routes>
          <Route path="/" element={<MainContent />} />
          <Route path="/pending" element={<MainContent />} />
          <Route path="/contracts" element={<ContractsPage />} />
          <Route path="/discover" element={<DiscoverPage />} />
          <Route path="/auth" element={<AuthPage />} />
          <Route path="/mcp/docs" element={<McpDocsPage />} />
          <Route path="/docs/*" element={<DocsPage />} />
          
          {/* Dynamic routes */}
          <Route path="/block/:height" element={<MainContent />} />
          <Route path="/wish/:wishId" element={<MainContent />} />
          <Route path="/contract/:contractId" element={<MainContent />} />
          <Route path="/proposal/:proposalId" element={<MainContent />} />

          {/* Fallback */}
          <Route path="*" element={<MainContent />} />
        </Routes>
      </ThemeProvider>
    </AuthProvider>
  );
}
