import React, { useState, useEffect } from 'react';
import { Search, Moon, Sun, ChevronLeft, ChevronRight, X, Check, Copy } from 'lucide-react';

import BlockCard from './components/Block/BlockCard';
import InscriptionCard from './components/Inscription/InscriptionCard';
import PendingTransactionsView from './components/Block/PendingTransactionsView';
import InscribeModal from './components/Inscription/InscribeModal';
import InscriptionModal from './components/Inscription/InscriptionModal';
import CopyButton from './components/Common/CopyButton';
import { useBlocks } from './hooks/useBlocks';
import { useInscriptions } from './hooks/useInscriptions';

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

export default function OrdiscanExplorer() {
  const [showInscribeModal, setShowInscribeModal] = useState(false);
  const [selectedInscription, setSelectedInscription] = useState(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [isDarkMode, setIsDarkMode] = useState(true);
  const [searchResults, setSearchResults] = useState(null);
  const [copiedText, setCopiedText] = useState('');

  const {
    blocks,
    selectedBlock,
    isUserNavigating,
    shouldAutoScroll,
    handleBlockSelect,
    setSelectedBlock,
    setIsUserNavigating
  } = useBlocks();

  const {
    inscriptions,
    hasMoreImages,
    totalImages,
    displayedCount,
    loadMoreInscriptions
  } = useInscriptions(selectedBlock);

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
    if (isDarkMode) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
    localStorage.setItem('theme', isDarkMode ? 'dark' : 'light');
  }, [isDarkMode]);

  useEffect(() => {
    if (selectedBlock) {
      if (!isUserNavigating && shouldAutoScroll) {
        setTimeout(() => {
          const blockElement = document.querySelector(`[data-block-id="${selectedBlock.height}"]`);
          if (blockElement) {
            blockElement.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
          }
        }, 100);
      }
      
      if (isUserNavigating) {
        setIsUserNavigating(false);
      }
    }
  }, [selectedBlock, isUserNavigating, shouldAutoScroll, setIsUserNavigating]);

  const toggleTheme = () => {
    setIsDarkMode(!isDarkMode);
  };

  const handleSearch = async (query) => {
    if (query.trim() === '') {
      setSearchResults(null);
      return;
    }

    try {
      const response = await fetch(`http://localhost:3001/api/search?q=${encodeURIComponent(query)}`);
      const data = await response.json();
      setSearchResults(data);
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

  const handleScroll = (direction) => {
    const container = document.getElementById('block-scroll');
    if (container) {
      const scrollAmount = 200;
      container.scrollBy({ left: direction === 'left' ? -scrollAmount : scrollAmount, behavior: 'smooth' });
    }
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
                <button className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer">Blocks</button>
                <button className="text-gray-600 dark:text-gray-400 hover:text-black dark:hover:text-white bg-transparent border-none cursor-pointer">Contracts</button>
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
            <button
              onClick={() => handleScroll('left')}
              className="absolute left-0 top-1/2 -translate-y-1/2 z-10 bg-gray-200 dark:bg-gray-800 hover:bg-gray-300 dark:hover:bg-gray-700 rounded-full p-2 text-black dark:text-white"
            >
              <ChevronLeft className="w-5 h-5" />
            </button>

            <div
              id="block-scroll"
              className="flex gap-4 overflow-x-auto pb-4 px-12 scrollbar-hide"
              style={{ scrollbarWidth: 'none', msOverflowStyle: 'none' }}
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

            <button
              onClick={() => handleScroll('right')}
              className="absolute right-0 top-1/2 -translate-y-1/2 z-10 bg-gray-200 dark:bg-gray-800 hover:bg-gray-300 dark:hover:bg-gray-700 rounded-full p-2 text-black dark:text-white"
            >
              <ChevronRight className="w-5 h-5" />
            </button>
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
                  {inscriptions.map((inscription, idx) => (
                    <InscriptionCard
                      key={idx}
                      inscription={inscription}
                      onClick={setSelectedInscription}
                    />
                  ))}
                </div>
                
                {selectedBlock && !selectedBlock.isFuture && hasMoreImages && inscriptions.length > 0 && (
                  <div className="text-center mt-6 mb-4">
                    <button
                      onClick={loadMoreInscriptions}
                      className="bg-indigo-600 hover:bg-indigo-700 text-white px-6 py-3 rounded-lg font-semibold transition-colors"
                    >
                      Load More Images
                    </button>
                    <div className="text-gray-500 dark:text-gray-400 text-sm mt-2">
                      Showing {displayedCount} of {totalImages} images
                    </div>
                  </div>
                )}
                
                {selectedBlock && !selectedBlock.isFuture && !hasMoreImages && inscriptions.length > 0 && (
                  <div className="text-center mt-6 mb-4 text-gray-500 dark:text-gray-400">
                    <div className="flex items-center justify-center gap-2">
                      <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                      </svg>
                      <span>Showing all {totalImages} images</span>
                    </div>
                  </div>
                )}
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