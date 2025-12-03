import React, { useState, useEffect, useRef } from 'react';
import { Search, Moon, Sun, X, Check, Copy } from 'lucide-react';
import { Routes, Route, useParams, useNavigate, useLocation } from 'react-router-dom';

import BlockCard from './components/Block/BlockCard';
import InscriptionCard from './components/Inscription/InscriptionCard';
import PendingTransactionsView from './components/Block/PendingTransactionsView';
import InscribeModal from './components/Inscription/InscribeModal';
import InscriptionModal from './components/Inscription/InscriptionModal';

import { useBlocks } from './hooks/useBlocks';
import { useInscriptions } from './hooks/useInscriptions';
import { API_BASE } from './apiBase';

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
  const location = useLocation();
  const [showInscribeModal, setShowInscribeModal] = useState(false);
  const [selectedInscription, setSelectedInscription] = useState(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [isDarkMode, setIsDarkMode] = useState(true);
  const [searchResults, setSearchResults] = useState(null);
  const [copiedText, setCopiedText] = useState('');
  const sentinelRef = useRef(null);
  const [hideBrc20, setHideBrc20] = useState(true);

  const {
    blocks,
    selectedBlock,
    isUserNavigating,
    handleBlockSelect: originalHandleBlockSelect,
    setSelectedBlock,
    setIsUserNavigating,
    loadMoreBlocks
  } = useBlocks();

  const handleBlockSelect = (block) => {
    originalHandleBlockSelect(block);
    navigate(`/block/${block.height}`);
  };

  const {
    inscriptions,
    hasMoreImages,
    loadMoreInscriptions
  } = useInscriptions(selectedBlock);

  const isPendingRoute = location.pathname === '/pending';

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
    if (height && blocks.length > 0) {
      const targetHeight = parseInt(height);
      const block = blocks.find(b => b.height === targetHeight);
      if (block && block.height !== selectedBlock?.height) {
        setSelectedBlock(block);
        setIsUserNavigating(true);
      } else if (!block) {
        // Block not in recent blocks, trigger scan and fetch it
        fetch(`${API_BASE}/api/data/scan`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            block_height: parseInt(height),
            force_scan: true
          })
        })
        .then(scanResponse => scanResponse.json())
        .then(scanData => {
          console.log('Block scanned successfully:', scanData);
          // Create block object from scanned data
          const fetchedBlock = {
            height: targetHeight,
            hash: scanData.block_data?.block_hash || scanData.block_data?.block_hash,
            timestamp: scanData.block_data?.timestamp || scanData.block_data?.timestamp,
            tx_count: scanData.block_data?.inscriptions?.length || 0,
            isFuture: false
          };
          setSelectedBlock(fetchedBlock);
          setIsUserNavigating(true);
        })
        .catch(error => {
          console.error('Error scanning block:', error);
          // Try regular fetch as fallback
          fetch(`${API_BASE}/api/data/block/${height}`)
            .then(response => response.json())
            .then(data => {
              const fetchedBlock = {
                height: targetHeight,
                hash: data.block_hash,
                timestamp: data.timestamp,
                tx_count: data.tx_count || 0,
                isFuture: false
              };
              setSelectedBlock(fetchedBlock);
              setIsUserNavigating(true);
            })
            .catch(fetchError => {
              console.error('Error fetching block:', fetchError);
              navigate('/');
            });
        });
      }
    }
  }, [height, blocks, selectedBlock, setSelectedBlock, setIsUserNavigating, navigate]);

  useEffect(() => {
    if (!isPendingRoute || !blocks.length) return;

    const pendingBlock = blocks.find((b) => b.isFuture);
    if (pendingBlock && selectedBlock?.height !== pendingBlock.height) {
      setSelectedBlock(pendingBlock);
      setIsUserNavigating(true);
    }
  }, [blocks, isPendingRoute, selectedBlock, setSelectedBlock, setIsUserNavigating]);

  useEffect(() => {
    if (isDarkMode) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
    localStorage.setItem('theme', isDarkMode ? 'dark' : 'light');
  }, [isDarkMode]);

  // Stop auto-scrolling; only user-driven navigation
  useEffect(() => {
    if (isUserNavigating) {
      setIsUserNavigating(false);
    }
  }, [selectedBlock, isUserNavigating, setIsUserNavigating]);

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

  const copyToClipboard = async (text) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedText(text);
      setTimeout(() => setCopiedText(''), 2000);
    } catch (error) {
      console.error('Failed to copy:', error);
    }
  };

  const renderInlineSearch = () => {
    if (searchResults === null) return null;

    const inscriptionResults = searchResults?.inscriptions || [];
    const blockResults = searchResults?.blocks || [];
    
    const hasResults = (inscriptionResults && inscriptionResults.length > 0) || (blockResults && blockResults.length > 0);
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
            <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Inscriptions</div>
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
      <header className="bg-gray-100 dark:bg-gray-900 border-b border-gray-300 dark:border-gray-800">
        <div className="container mx-auto px-6 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-8">
              <div className="flex items-center gap-3">
                <div className="flex items-center justify-center w-8 h-8 bg-gradient-to-br from-indigo-500 to-purple-600 rounded-lg">
                  <span className="text-white text-lg">✦</span>
                </div>
                <h1 className="text-2xl font-bold">Starlight</h1>
              </div>
              
              <nav className="flex gap-6 text-sm">
                <button onClick={() => setShowInscribeModal(true)} className="text-indigo-600 dark:text-indigo-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer">Inscribe</button>
                <button
                  onClick={() => navigate('/')}
                  className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer"
                >
                  Blocks
                </button>
                <button
                  onClick={() => navigate('/pending')}
                  className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer"
                >
                  Contracts
                </button>
                <button
                  onClick={() => setHideBrc20(!hideBrc20)}
                  className={`text-sm px-3 py-1 rounded-full border ${hideBrc20 ? 'border-indigo-500 text-indigo-600 dark:text-indigo-300' : 'border-gray-400 text-gray-600 dark:text-gray-300'} bg-transparent cursor-pointer`}
                  title="Toggle BRC-20 visibility"
                >
                  {hideBrc20 ? 'Hide BRC-20' : 'Show BRC-20'}
                </button>
              </nav>
            </div>
            
            <div className="flex items-center gap-4">
              <div className="relative">
                <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 w-4 h-4 text-gray-400 dark:text-gray-500" />
                <input
                  type="text"
                  placeholder="Search inscriptions..."
                  value={searchQuery}
                  onChange={(e) => {
                    setSearchQuery(e.target.value);
                    handleSearch(e.target.value);
                  }}
                  className="bg-gray-200 dark:bg-gray-800 text-black dark:text-white pl-10 pr-10 py-2 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 w-64"
                />
                {searchQuery && (
                  <button
                    onClick={clearSearch}
                    className="absolute right-3 top-1/2 transform -translate-y-1/2 text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300"
                  >
                    <X className="w-4 h-4" />
                  </button>
                )}
                {renderInlineSearch()}
              </div>
              <button className="bg-indigo-600 hover:bg-indigo-700 text-white px-4 py-2 rounded-lg text-sm font-semibold">
                Connect
              </button>
              <button onClick={toggleTheme} className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white">
                {isDarkMode ? <Moon className="w-5 h-5" /> : <Sun className="w-5 h-5" />}
              </button>
            </div>
          </div>
        </div>
      </header>

      <div className="bg-gray-100 dark:bg-gray-900 border-b border-gray-300 dark:border-gray-800 relative">
        <div className="container mx-auto px-6 py-8">
          <div className="relative pt-6">
                        <div
              id="block-scroll"
              className="flex gap-4 overflow-x-auto whitespace-nowrap pb-4 px-12"
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
                  Inscriptions
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
                        Submitted {new Date(tx.timestamp * 1000).toLocaleString()}
                      </div>
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
             (!searchResults.blocks || searchResults.blocks.length === 0) && (
              <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                No results found for "{searchQuery}"
              </div>
            )}
          </div>
        ) : selectedBlock && (
          <>
            {console.log('Rendering block:', selectedBlock.height, 'isFuture:', selectedBlock.isFuture, 'inscriptions:', inscriptions.length)}
            <div className="mb-8">
              <h2 className="text-4xl font-bold mb-4 text-black dark:text-white">Block {selectedBlock.height}</h2>
              <div className="flex items-center gap-4 text-sm">
                <div className="flex items-center gap-2">
                  <span className="text-gray-600 dark:text-gray-400">{selectedBlock.hash}</span>
                  {copiedText === selectedBlock.hash ? (
                    <Check className="w-4 h-4 text-green-600 dark:text-green-400" />
                  ) : (
                    <Copy
                      className="w-4 h-4 text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white cursor-pointer"
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
              <PendingTransactionsView setSelectedInscription={setSelectedInscription} />
            ) : (
              <div className="mb-4">
                <div className="mb-4">
                  <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-indigo-500 pb-2 inline-block">
                    Smart Contracts
                  </h3>
                </div>

                 <div className="grid grid-cols-5 gap-4">
                   {console.log('Rendering inscriptions grid:', inscriptions.length)}
                   {filteredInscriptions.map((inscription, idx) => (
                     <InscriptionCard
                       key={idx}
                       inscription={inscription}
                       onClick={setSelectedInscription}
                     />
                   ))}
                   {hasMoreImages && (
                     <div ref={sentinelRef} className="col-span-5 flex justify-center py-4">
                       <div className="text-gray-500 dark:text-gray-400">Loading more...</div>
                     </div>
                   )}
                 </div>
              </div>
            )}
          </>
        )}
       </div>

       {showInscribeModal && (
         <InscribeModal
           onClose={() => setShowInscribeModal(false)}
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
             <div className="text-gray-400 text-sm">
               💡 Are you a builder? Try our API!
             </div>
           </div>
         </div>
       </footer>

    </div>
  );
}

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<MainContent />} />
      <Route path="/block/:height" element={<MainContent />} />
      <Route path="/pending" element={<MainContent />} />
    </Routes>
  );
}
