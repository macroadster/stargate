import React, { useState, useEffect, useRef } from 'react';
import { Check, Copy } from 'lucide-react';
import { Routes, Route, useParams, useNavigate } from 'react-router-dom';


import BlockCard from './components/Block/BlockCard';
import InscriptionCard from './components/Inscription/InscriptionCard';
import PendingTransactionsView from './components/Block/PendingTransactionsView';
import InscribeModal from './components/Inscription/InscribeModal';
import InscriptionModal from './components/Inscription/InscriptionModal';
import DiscoverPage from './components/Discover/DiscoverPage';
import AuthPage from './pages/AuthPage';
import ContractsPage from './pages/ContractsPage';
import McpDocsPage from './pages/McpDocsPage';
import AppHeader from './components/Common/AppHeader';
import { AuthProvider } from './context/AuthContext';

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
  const { height } = useParams();
  const navigate = useNavigate();
  const [showInscribeModal, setShowInscribeModal] = useState(false);
  const [selectedInscription, setSelectedInscription] = useState(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [isDarkMode, setIsDarkMode] = useState(true);
  const [searchResults, setSearchResults] = useState(null);
  const [copiedText, setCopiedText] = useState('');
  const sentinelRef = useRef(null);
  const [hideBrc20, setHideBrc20] = useState(true);
  const [pendingRefreshKey, setPendingRefreshKey] = useState(0);
  const { elRef: scrollRef, isDragging } = useHorizontalScroll();
  // MCP proposal management handled in inscription modal.

  const {
    blocks,
    selectedBlock,
    isUserNavigating,
    handleBlockSelect: originalHandleBlockSelect,
    setSelectedBlock,
    setIsUserNavigating,
    loadMoreBlocks,
    refreshBlocks
  } = useBlocks();

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

  useEffect(() => {
    const savedTheme = localStorage.getItem('theme');
    if (savedTheme) {
      setIsDarkMode(savedTheme === 'dark');
    } else {
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      setIsDarkMode(prefersDark);
    }
  }, []);

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

  // If selection and route diverge (race between click and router), force the route to the selected block.
  useEffect(() => {
    if (!selectedBlock) return;
    const currentHeight = height !== undefined ? parseInt(height, 10) : null;
    if (currentHeight !== selectedBlock.height) {
      navigate(`/block/${selectedBlock.height}`, { replace: true });
      // Clear the user navigation flag once the route is synced
      setIsUserNavigating(false);
    }
  }, [selectedBlock, height, navigate, setIsUserNavigating]);

  useEffect(() => {
    if (!blocks.length) return;

    // Auto-select pending block on initial load (both root and /pending routes)
    // Only do this once when nothing is selected yet
    const pendingBlock = blocks.find((b) => b.isFuture);
    if (pendingBlock && !selectedBlock) {
      setSelectedBlock(pendingBlock);
      setIsUserNavigating(true);
    }
  }, [blocks, selectedBlock, setSelectedBlock, setIsUserNavigating]);

  useEffect(() => {
    if (isDarkMode) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
    localStorage.setItem('theme', isDarkMode ? 'dark' : 'light');
  }, [isDarkMode]);

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



  const toggleTheme = () => {
    setIsDarkMode(!isDarkMode);
  };

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

  const selectContract = (contract) => {
    const rawUrl = contract.stego_image_url || contract.stegoImageUrl || '';
    const imageUrl = rawUrl && !rawUrl.startsWith('http') ? `${CONTENT_BASE}${rawUrl}` : rawUrl;
    const metadata = contract.metadata || {};
    const wishText = metadata.wish_text || metadata.embedded_message || metadata.message || '';
    const inscription = {
      id: contract.contract_id || contract.contractId || contract.id,
      contract_type: contract.contract_type || 'Smart Contract',
      metadata,
      image_url: imageUrl,
      mime_type: metadata.content_type || 'image/png',
      text: wishText,
      genesis_block_height: contract.block_height || 0,
      block_height: contract.block_height || 0,
      status: metadata.confirmation_status || 'open',
    };
    setSelectedInscription(inscription);
    clearSearch();
  };

  // Top-level proposals panel removed; handled in inscription modal.

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
    const blockResults = searchResults?.blocks || [];
    const contractResults = searchResults?.contracts || [];
    
    const hasResults =
      (inscriptionResults && inscriptionResults.length > 0) ||
      (blockResults && blockResults.length > 0) ||
      (contractResults && contractResults.length > 0);
    if (!hasResults) return null;

    const onSelectBlock = (block) => {
      setSelectedBlock({ ...block, hash: block.id?.toString() || block.hash });
      clearSearch();
    };

    const onSelectInscription = (tx) => {
      const inscription = {
        id: tx.id,
        contractType: 'Custom Contract',
        capability: 'Data Storage',
        protocol: 'BRC-20',
        apiEndpoints: 0,
        interactions: 0,
        reputation: 'N/A',
        isActive: tx.status === 'confirmed',
        number: parseInt(tx.id?.split('_')[1]) || 0,
        address: 'bc1q...',
        genesis_block_height: tx.blockHeight || 0,
        mime_type: 'text/plain',
        text: tx.text || '',
        metadata: tx.metadata || {},
      };
      setSelectedInscription(inscription);
      clearSearch();
    };

    return (
      <div className="absolute mt-2 w-96 max-h-96 overflow-y-auto bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50">
        {inscriptionResults.length > 0 && (
          <div className="p-3 border-b border-gray-200 dark:border-gray-800">
            <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Transactions</div>
            <div className="space-y-2">
              {inscriptionResults.slice(0, 5).map((tx, idx) => (
                <button
                  key={`ins-${idx}`}
                  onClick={() => onSelectInscription(tx)}
                  className="w-full text-left p-2 rounded bg-yellow-50 dark:bg-yellow-900/40 hover:bg-yellow-100 dark:hover:bg-yellow-800 transition-colors"
                >
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="px-2 py-0.5 rounded bg-yellow-600 text-white text-[11px]">Inscribe</span>
                    <span className="text-yellow-800 dark:text-yellow-200">{tx.status || 'pending'}</span>
                  </div>
                  <div className="text-yellow-900 dark:text-yellow-100 font-mono text-xs break-all">{tx.id}</div>
                  {tx.text && <div className="text-yellow-700 dark:text-yellow-300 text-xs truncate mt-1">{tx.text}</div>}
                </button>
              ))}
            </div>
          </div>
        )}
        {contractResults.length > 0 && (
          <div className="p-3 border-b border-gray-200 dark:border-gray-800">
            <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Open Contracts</div>
            <div className="space-y-2">
              {contractResults.slice(0, 5).map((contract, idx) => (
                <button
                  key={`ctr-${idx}`}
                  onClick={() => selectContract(contract)}
                  className="w-full text-left p-2 rounded bg-emerald-50 dark:bg-emerald-900/40 hover:bg-emerald-100 dark:hover:bg-emerald-800 transition-colors"
                >
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="px-2 py-0.5 rounded bg-emerald-600 text-white text-[11px]">Contract</span>
                    <span className="text-emerald-800 dark:text-emerald-200">Open</span>
                  </div>
                  <div className="text-emerald-900 dark:text-emerald-100 font-mono text-xs break-all">
                    {contract.contract_id || contract.contractId || contract.id}
                  </div>
                  {(contract.metadata?.embedded_message || contract.metadata?.message) && (
                    <div className="text-emerald-700 dark:text-emerald-300 text-xs truncate mt-1">
                      {contract.metadata?.embedded_message || contract.metadata?.message}
                    </div>
                  )}
                </button>
              ))}
            </div>
          </div>
        )}
        {blockResults.length > 0 && (
          <div className="p-3">
            <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Blocks</div>
            <div className="space-y-2">
              {blockResults.slice(0, 5).map((b, idx) => (
                <button
                  key={`blk-${idx}`}
                  onClick={() => onSelectBlock(b)}
                  className="w-full text-left p-2 rounded bg-indigo-50 dark:bg-indigo-900/40 hover:bg-indigo-100 dark:hover:bg-indigo-800 transition-colors"
                >
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="px-2 py-0.5 rounded bg-indigo-600 text-white text-[11px]">Block</span>
                    <span className="text-indigo-800 dark:text-indigo-200">{b.tx_count || 0} tx</span>
                  </div>
                  <div className="text-indigo-900 dark:text-indigo-100 font-mono text-xs break-all">
                    #{b.height} • {b.id || b.hash}
                  </div>
                  {b.timestamp && (
                    <div className="text-indigo-700 dark:text-indigo-300 text-[11px] mt-1">
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
    <div className="min-h-screen bg-white dark:bg-gray-950 text-black dark:text-white">
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
        showThemeToggle
        isDarkMode={isDarkMode}
        onToggleTheme={toggleTheme}
      />

      <div className="bg-gray-100 dark:bg-gray-900 border-b border-gray-300 dark:border-gray-800 relative">
        <div className="container mx-auto px-6 py-8">
          <div className="relative pt-6">
                        <div
              id="block-scroll"
              ref={scrollRef}
              className="flex gap-4 overflow-x-auto whitespace-nowrap pb-4 px-12 no-scrollbar"
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

          </div>
        </div>
      </div>

      <div className="container mx-auto px-6 py-8">
        {searchResults !== null ? (
          <div className="mb-8">
            <h2 className="text-4xl font-bold mb-4 text-black dark:text-white">Search Results</h2>

            {searchResults.inscriptions && searchResults.inscriptions.length > 0 && (
              <div className="mb-8">
                <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-yellow-500 pb-2 inline-block mb-4">
                  Transactions
                </h3>
                <div className="space-y-3">
                  {searchResults.inscriptions.map((tx, idx) => (
                    <div
                      key={idx}
                      className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4 cursor-pointer hover:bg-yellow-100 dark:hover:bg-yellow-800 transition-colors"
                      onClick={() => {
                         const inscription = {
                           id: tx.id,
                           contractType: 'Custom Contract',
                           capability: 'Data Storage',
                           protocol: 'BRC-20',
                           apiEndpoints: 0,
                           interactions: 0,
                           reputation: 'N/A',
                           isActive: tx.status === 'confirmed',
                           number: parseInt(tx.id.split('_')[1]) || 0,
                           address: 'bc1q...',
                           genesis_block_height: 923627,
                           mime_type: 'text/plain',
                         };
                        setSelectedInscription(inscription);
                        clearSearch();
                      }}
                    >
                      <div className="flex justify-between items-start mb-3">
                        <div className="flex items-center gap-3">
                          <div className="px-3 py-1 rounded text-xs font-semibold bg-yellow-600 text-white">
                            Inscribe
                          </div>
                          <div className="text-yellow-800 dark:text-yellow-200 font-mono text-sm">
                            {tx.id}
                          </div>
                        </div>
                        <div className="px-2 py-1 rounded text-xs font-semibold bg-yellow-100 dark:bg-yellow-800 text-yellow-800 dark:text-yellow-200">
                          {tx.status}
                        </div>
                      </div>

                      <div className="grid grid-cols-2 gap-4 text-sm">
                        <div>
                          <div className="text-yellow-700 dark:text-yellow-300 mb-1">Text Length</div>
                          <div className="text-yellow-900 dark:text-yellow-100">{tx.text?.length || 0} chars</div>
                        </div>
                        <div>
                          <div className="text-yellow-700 dark:text-yellow-300 mb-1">Price</div>
                          <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{tx.price} BTC</div>
                        </div>
                      </div>

                      <div className="mt-3 text-xs text-yellow-600 dark:text-yellow-400">
                        {tx.timestamp
                          ? `Submitted ${new Date(tx.timestamp * 1000).toLocaleString()}`
                          : 'Submitted —'}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {searchResults.contracts && searchResults.contracts.length > 0 && (
              <div className="mb-8">
                <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-emerald-500 pb-2 inline-block mb-4">
                  Open Contracts
                </h3>
                <div className="space-y-3">
                  {searchResults.contracts.map((contract, idx) => (
                    <div
                      key={`contract-${idx}`}
                      className="bg-emerald-50 dark:bg-emerald-900 border border-emerald-200 dark:border-emerald-700 rounded-lg p-4 cursor-pointer hover:bg-emerald-100 dark:hover:bg-emerald-800 transition-colors"
                      onClick={() => selectContract(contract)}
                    >
                      <div className="flex justify-between items-start mb-3">
                        <div className="flex items-center gap-3">
                          <div className="px-3 py-1 rounded text-xs font-semibold bg-emerald-600 text-white">
                            Contract
                          </div>
                          <div className="text-emerald-800 dark:text-emerald-200 font-mono text-sm">
                            {contract.contract_id || contract.contractId || contract.id}
                          </div>
                        </div>
                        <div className="px-2 py-1 rounded text-xs font-semibold bg-emerald-100 dark:bg-emerald-800 text-emerald-800 dark:text-emerald-200">
                          open
                        </div>
                      </div>

                      <div className="grid grid-cols-2 gap-4 text-sm">
                        <div>
                          <div className="text-emerald-700 dark:text-emerald-300 mb-1">Block</div>
                          <div className="text-emerald-900 dark:text-emerald-100">
                            {contract.block_height || '—'}
                          </div>
                        </div>
                        <div>
                          <div className="text-emerald-700 dark:text-emerald-300 mb-1">Pixel Hash</div>
                          <div className="text-emerald-900 dark:text-emerald-100 font-mono truncate">
                            {contract.visible_pixel_hash || contract.metadata?.visible_pixel_hash || '—'}
                          </div>
                        </div>
                      </div>

                      {(contract.metadata?.embedded_message || contract.metadata?.message) && (
                        <div className="mt-3 text-xs text-emerald-700 dark:text-emerald-300 truncate">
                          {contract.metadata?.embedded_message || contract.metadata?.message}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {searchResults.blocks && searchResults.blocks.length > 0 && (
              <div className="mb-8">
                <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-indigo-500 pb-2 inline-block mb-4">
                  Blocks
                </h3>
                <div className="space-y-3">
                  {searchResults.blocks.map((block, idx) => (
                    <div
                      key={idx}
                      className="bg-indigo-50 dark:bg-indigo-900 border border-indigo-200 dark:border-indigo-700 rounded-lg p-4 cursor-pointer hover:bg-indigo-100 dark:hover:bg-indigo-800 transition-colors"
                      onClick={() => {
                        setSelectedBlock({...block, hash: block.id.toString()});
                        clearSearch();
                      }}
                    >
                      <div className="flex justify-between items-start mb-3">
                        <div className="flex items-center gap-3">
                          <div className="px-3 py-1 rounded text-xs font-semibold bg-indigo-600 text-white">
                            Block
                          </div>
                          <div className="text-indigo-800 dark:text-indigo-200 font-mono text-sm">
                            #{block.id}
                          </div>
                        </div>
                        <div className="text-indigo-700 dark:text-indigo-300 text-sm">
                          {block.tx_count} transactions
                        </div>
                      </div>

                      <div className="text-xs text-indigo-600 dark:text-indigo-400">
                        Height: {block.height} • Timestamp: {new Date(block.timestamp * 1000).toLocaleString()}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {(!searchResults.inscriptions || searchResults.inscriptions.length === 0) &&
             (!searchResults.contracts || searchResults.contracts.length === 0) &&
             (!searchResults.blocks || searchResults.blocks.length === 0) && (
              <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                No results found for "{searchQuery}"
              </div>
            )}
          </div>
        ) : selectedBlock && (
          <>
            <div className="mb-8">
              <h2 className="text-4xl font-bold mb-4 text-black dark:text-white">Block {selectedBlock.height}</h2>
              <div className="flex items-center gap-4 text-sm">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="text-gray-600 dark:text-gray-400 truncate max-w-[200px] sm:max-w-none">{selectedBlock.hash}</span>
                  {copiedText === selectedBlock.hash ? (
                    <Check className="w-4 h-4 text-green-600 dark:text-green-400 flex-shrink-0" />
                  ) : (
                    <Copy
                      className="w-4 h-4 text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white cursor-pointer flex-shrink-0"
                      onClick={() => copyToClipboard(selectedBlock.hash)}
                    />
                  )}
                </div>
              </div>
              <div className="text-gray-600 dark:text-gray-400 text-sm mt-2">
                {new Date(selectedBlock.timestamp * 1000).toLocaleString()} ({formatTimeAgo(selectedBlock.timestamp)})
              </div>
            </div>

            {selectedBlock.isFuture ? (
              <PendingTransactionsView
                setSelectedInscription={setSelectedInscription}
                refreshKey={pendingRefreshKey}
              />
            ) : (
              <div className="mb-4">
                <div className="mb-4">
                  <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-indigo-500 pb-2 inline-block">
                    Smart Contracts
                  </h3>
                </div>
                {inscriptionsError && (
                  <div className="mb-4 rounded-lg border border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-900/40 dark:text-red-100 px-4 py-3 text-sm">
                    {inscriptionsError}
                  </div>
                )}

                 {filteredInscriptions.length === 0 && isLoadingInscriptions && (
                   <div className="columns-1 sm:columns-2 xl:columns-3 gap-6">
                     {Array.from({ length: 5 }).map((_, i) => (
                       <div key={i} className="h-48 rounded-lg bg-gray-200 dark:bg-gray-800 animate-pulse break-inside-avoid mb-6" />
                     ))}
                   </div>
                 )}

                 {filteredInscriptions.length === 0 && !isLoadingInscriptions && (
                   <div className="text-gray-500 dark:text-gray-400 py-6">
                     No inscriptions found for this block.
                   </div>
                 )}

                  {filteredInscriptions.length > 0 && (
                    <>
                      <div className="columns-1 sm:columns-2 xl:columns-3 gap-6">
                        {filteredInscriptions.map((inscription, idx) => (
                          <InscriptionCard
                            key={idx}
                            inscription={inscription}
                            onClick={setSelectedInscription}
                          />
                        ))}
                        {hasMoreImages && (
                          <div ref={sentinelRef} className="column-span-all flex justify-center py-4">
                            <div className="text-gray-500 dark:text-gray-400">Loading more...</div>
                          </div>
                        )}
                      </div>
                      {!hasMoreImages && (
                        <div className="column-span-all text-center text-gray-500 dark:text-gray-400 text-sm mt-4">
                          You&apos;ve reached the end of inscriptions for this block.
                        </div>
                      )}
                    </>
                  )}
              </div>
            )}
          </>
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
         />
       )}

       <footer className="bg-gray-100 dark:bg-gray-900 border-t border-gray-300 dark:border-gray-800 mt-12">
         <div className="container mx-auto px-6 py-6">
           <div className="flex items-center justify-between">
             <div className="flex items-center gap-2">
               <div className="flex items-center justify-center w-6 h-6 bg-gradient-to-br from-indigo-500 to-purple-600 rounded">
                 <span className="text-white text-sm">✦</span>
               </div>
               <span className="text-gray-400">Starlight</span>
             </div>
              <a href="/mcp/docs" className="text-gray-400 text-sm hover:text-gray-600 dark:hover:text-gray-200 transition-colors">
                💡 Are you a builder? Try our API!
              </a>
           </div>
         </div>
       </footer>

    </div>
  );
}

export default function App() {
  useEffect(() => {
    const savedTheme = localStorage.getItem('theme');
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    const isDark = savedTheme ? savedTheme === 'dark' : prefersDark;
    if (isDark) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }, []);

  return (
    <AuthProvider>
      <Routes>
        <Route path="/" element={<MainContent />} />
        <Route path="/block/:height" element={<MainContent />} />
        <Route path="/pending" element={<MainContent />} />
        <Route path="/contracts" element={<ContractsPage />} />
        <Route path="/discover" element={<DiscoverPage />} />
        <Route path="/auth" element={<AuthPage />} />
        <Route path="/mcp/docs" element={<McpDocsPage />} />
      </Routes>
    </AuthProvider>
  );
}
