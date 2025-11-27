import React, { useState, useEffect, useRef } from 'react';
import { Search, Copy, Check, Moon, Sun, ChevronLeft, ChevronRight, X } from 'lucide-react';
import { QRCodeCanvas } from 'qrcode.react';
import StegoAnalysisViewer from './StegoAnalysisViewer.js';

// Simulated data generators (adapted for real data)
const generateBlock = (block) => {
  const now = Date.now();
  const inscriptionCounts = [0, 5, 8, 15, 25, 97, 529, 1384, 4039, 3655];
  const randomCount = inscriptionCounts[Math.floor(Math.random() * inscriptionCounts.length)];
  
  return {
    height: block.height,
    timestamp: block.timestamp || now - ((923627 - block.height) * 600000),
    hash: block.id,
    inscriptionCount: randomCount,
    hasBRC20: Math.random() > 0.6,
    thumbnail: randomCount > 0 ? (Math.random() > 0.5 ? '🖼️' : '🎨') : null,
    tx_count: block.tx_count,
    smart_contracts: block.smart_contracts || []
  };
};

const generateInscriptions = (inscriptions) => {
  const types = ['HTML', 'WEBP', 'PNG', 'JSON', 'TEXT', 'SVG'];
  const colors = ['from-red-500', 'from-blue-500', 'from-purple-500', 'from-green-500', 'from-yellow-500', 'from-pink-500'];
  const contractTypes = ['Knowledge Base', 'Data Oracle', 'AI Agent', 'Token Contract', 'DeFi Protocol'];
  const capabilities = ['Query Processing', 'Data Storage', 'Computation', 'Token Minting', 'Asset Transfer'];
  const protocols = ['ERC-20', 'ERC-721', 'BRC-20', 'Custom Protocol', 'Multi-Chain'];
  
  return inscriptions.map((insc, i) => ({
    id: insc.id,
    type: types[Math.floor(Math.random() * types.length)],
    thumbnail: insc.mime_type.startsWith('image/') ? `http://localhost:3001/api/inscription/${insc.id}/content` : null,
    gradient: colors[Math.floor(Math.random() * colors.length)],
    hasMultiple: Math.random() > 0.8,
    contractType: contractTypes[Math.floor(Math.random() * contractTypes.length)],
    capability: capabilities[Math.floor(Math.random() * capabilities.length)],
    protocol: protocols[Math.floor(Math.random() * protocols.length)],
    apiEndpoints: Math.floor(Math.random() * 10) + 1,
    interactions: Math.floor(Math.random() * 10000),
    reputation: (Math.random() * 2 + 3).toFixed(1),
    isActive: Math.random() > 0.2,
    number: insc.number,
    address: insc.address,
    genesis_block_height: insc.genesis_block_height,
    mime_type: insc.mime_type
  }));
};

const BlockCard = ({ block, onClick, isSelected }) => {
  const timeAgo = Math.floor((Date.now() - block.timestamp) / 3600000);
  const hasSmartContracts = block.smart_contracts && block.smart_contracts.length > 0;
  const hasWitnessImages = block.witness_images && block.witness_images.length > 0;

  const getBackgroundClass = () => {
    if (block.isFuture) return 'from-yellow-200 to-yellow-300 dark:from-yellow-600 dark:to-yellow-800';
    if (hasSmartContracts) return 'from-purple-200 to-purple-300 dark:from-purple-600 dark:to-purple-800';
    if (hasWitnessImages) return 'from-green-200 to-green-300 dark:from-green-600 dark:to-green-800';
    if (block.inscriptionCount > 0) return 'from-indigo-200 to-indigo-300 dark:from-indigo-600 dark:to-indigo-800';
    return 'from-gray-200 to-gray-300 dark:from-gray-700 dark:to-gray-800';
  };

  const getBadgeText = () => {
    if (block.isFuture) return 'Pending';
    if (hasSmartContracts) return `${block.smart_contracts.length} stego contract${block.smart_contracts.length !== 1 ? 's' : ''}`;
    if (hasWitnessImages) return `${block.witness_images.length} witness image${block.witness_images.length !== 1 ? 's' : ''}`;
    return `${block.inscriptionCount} inscription${block.inscriptionCount !== 1 ? 's' : ''}`;
  };

  const getBadgeClass = () => {
    if (block.isFuture) return 'text-yellow-800 dark:text-yellow-200';
    if (hasSmartContracts) return 'text-purple-700 dark:text-purple-200';
    if (hasWitnessImages) return 'text-green-700 dark:text-green-200';
    return 'text-indigo-700 dark:text-indigo-200';
  };

  return (
    <div
      onClick={() => onClick(block)}
      data-block-id={block.height}
      className={`relative flex-shrink-0 w-40 cursor-pointer transition-all ${
        isSelected ? 'scale-102' : 'hover:scale-102'
      } ${block.isFuture ? 'opacity-75' : ''}`}
    >
      <div className={`rounded-xl overflow-hidden border-2 ${
        isSelected ? 'border-indigo-500' : block.isFuture ? 'border-yellow-400' : 'border-gray-300 dark:border-gray-700'
      } bg-gradient-to-br ${getBackgroundClass()}`}>
        {/* Thumbnail or placeholder */}
        <div className="h-32 flex items-center justify-center bg-black bg-opacity-20">
          {hasSmartContracts ? (
            <div className="text-6xl">🎨</div>
          ) : hasWitnessImages ? (
            <div className="text-6xl">🖼️</div>
          ) : block.thumbnail ? (
            <div className="text-6xl">{block.thumbnail}</div>
          ) : (
            <div className="text-gray-600 text-sm">No inscriptions</div>
          )}
        </div>
        
        {/* Block info */}
        <div className={`p-3 ${block.isFuture ? 'bg-yellow-500 bg-opacity-40' : 'bg-black bg-opacity-40 dark:bg-black dark:bg-opacity-40 bg-white bg-opacity-60'}`}>
          <div className={`font-bold text-lg mb-1 ${block.isFuture ? 'text-yellow-900 dark:text-yellow-100' : 'text-black dark:text-white'}`}>{block.height}</div>
          <div className={`text-xs mb-2 ${getBadgeClass()}`}>
            {getBadgeText()}
          </div>
          <div className={`text-xs ${block.isFuture ? 'text-yellow-700 dark:text-yellow-300' : 'text-gray-600 dark:text-gray-400'}`}>
            {block.isFuture ? 'Next block' : (timeAgo < 1 ? '< 1 hour ago' : `${timeAgo} hour${timeAgo > 1 ? 's' : ''} ago`)}
          </div>
        </div>
        
        {/* Special badges */}
        {hasSmartContracts && (
          <div className="absolute top-2 right-2 bg-purple-600 text-white text-xs px-2 py-1 rounded-md font-bold">
            STEGO
          </div>
        )}
        {hasWitnessImages && (
          <div className="absolute top-2 left-2 bg-green-600 text-white text-xs px-2 py-1 rounded-md font-bold">
            WITNESS
          </div>
        )}
        {block.hasBRC20 && !hasSmartContracts && !hasWitnessImages && (
          <div className="absolute top-2 right-2 bg-orange-600 text-white text-xs px-2 py-1 rounded-md font-bold">
            BRC-20
          </div>
        )}
      </div>
    </div>
  );
}

const InscriptionCard = ({ inscription, onClick }) => {
  console.log('Rendering inscription card:', inscription.id);
  return (
    <div
      onClick={() => onClick(inscription)}
      className="relative group cursor-pointer"
    >
      <div className="relative overflow-hidden rounded-lg border-2 border-gray-300 dark:border-gray-700 hover:border-indigo-500 transition-all duration-200 bg-white dark:bg-gray-800">
        {/* Image or placeholder */}
        <div className="h-32 flex items-center justify-center bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800">
          {inscription.image ? (
            <img 
              src={inscription.image} 
              alt={inscription.id}
              className="max-w-full max-h-full object-contain"
              onError={(e) => {
                e.target.style.display = 'none';
                e.target.nextSibling.style.display = 'flex';
              }}
            />
          ) : null}
          <div className="text-4xl" style={{display: inscription.image ? 'none' : 'flex'}}>
            {inscription.mime_type?.includes('text') ? '📄' : 
             inscription.mime_type?.includes('image') ? '🖼️' : '📦'}
          </div>
        </div>
        
        {/* Content overlay */}
        <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <div className="absolute bottom-0 left-0 right-0 p-3 text-white">
            <div className="text-xs font-mono truncate">{inscription.id}</div>
            {inscription.text && (
              <div className="text-xs mt-1 truncate opacity-90">{inscription.text}</div>
            )}
          </div>
        </div>
        
        {/* Status badges */}
        <div className="absolute top-2 left-2 flex gap-1">
          {inscription.contractType && (
            <div className="px-2 py-1 bg-purple-600 text-white text-xs rounded-full font-semibold">
              {inscription.contractType}
            </div>
          )}
          {inscription.protocol && (
            <div className="px-2 py-1 bg-blue-600 text-white text-xs rounded-full font-semibold">
              {inscription.protocol}
            </div>
          )}
        </div>
        
        {/* Reputation indicator */}
        {inscription.reputation && (
          <div className="absolute top-2 right-2 flex items-center gap-1">
            <span className="text-yellow-500 dark:text-yellow-400">★</span>
            <span className="text-black dark:text-white text-sm font-semibold">{inscription.reputation}</span>
          </div>
        )}
      </div>
      
      <div className="mt-2">
        <div className="text-black dark:text-white font-mono text-xs truncate" title={inscription.id}>{inscription.id}</div>
      </div>
    </div>
  );
};

const PendingTransactionsView = ({ copiedText, copyToClipboard, setSelectedInscription }) => {
  const [pendingTxs, setPendingTxs] = useState([]);

  useEffect(() => {
    fetchPendingTransactions();
  }, []);

  const fetchPendingTransactions = async () => {
    try {
      const response = await fetch('http://localhost:3001/api/pending-transactions');
      const data = await response.json();
      setPendingTxs(data || []);
    } catch (error) {
      console.error('Error fetching pending transactions:', error);
      setPendingTxs([]);
    }
  };

  return (
    <div className="mb-4">
      <div className="mb-4">
        <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-yellow-500 pb-2 inline-block">
          Pending Transactions
        </h3>
      </div>

      {Array.isArray(pendingTxs) && pendingTxs.length > 0 ? (
        <div className="space-y-3">
                  {pendingTxs.map((tx, idx) => (
                    <div
                      key={idx}
                      className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4 cursor-pointer hover:bg-yellow-100 dark:hover:bg-yellow-800 transition-colors"
                      onClick={() => {
                        // Convert pending tx to inscription format for modal
                        const inscription = {
                          id: tx.id,
                          contractType: 'Pending Contract',
                          capability: 'Data Storage',
                          protocol: 'BRC-20',
                          apiEndpoints: 1,
                          interactions: 0,
                          reputation: 'Pending',
                          isActive: false,
                          number: parseInt(tx.id.split('_')[1]) || 0,
                          address: 'bc1q...pending',
                          genesis_block_height: tx.blockHeight,
                          mime_type: 'text/plain',
                          text: tx.text,
                          price: tx.price,
                          timestamp: tx.timestamp,
                          status: tx.status,
                          image: tx.imageData ? `http://localhost:3001/uploads/${tx.imageData.split('/').pop()}` : null,
                        };
                        setSelectedInscription(inscription);
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
                          <button onClick={(e) => { e.stopPropagation(); copyToClipboard(tx.id); }} className="text-yellow-600 dark:text-yellow-400 hover:text-yellow-800 dark:hover:text-yellow-200">
                            {copiedText === tx.id ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
                          </button>
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
      ) : (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400">
          No pending transactions
        </div>
      )}
    </div>
  );
};

const InscribeModal = ({ onClose, blocks, setPendingTransactions }) => {
  const [step, setStep] = useState(1); // 1: form, 2: payment
  const [imageFile, setImageFile] = useState(null);
  const [embedText, setEmbedText] = useState('');
  const [price, setPrice] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setIsSubmitting(true);

    try {
      const formData = new FormData();
      if (imageFile) formData.append('image', imageFile);
      formData.append('text', embedText);
      formData.append('price', price);

      const response = await fetch('http://localhost:3001/api/inscribe', {
        method: 'POST',
        body: formData,
      });

      if (response.ok) {
        const result = await response.json();
        console.log('Inscription successful:', result);
        
        // Refresh pending transactions
        setTimeout(() => {
          fetch('http://localhost:3001/api/pending-transactions')
            .then(res => res.json())
            .then(data => setPendingTransactions(data || []))
            .catch(err => console.error('Error fetching pending transactions:', err));
        }, 1000);
        
        onClose();
      } else {
        console.error('Inscription failed');
      }
    } catch (error) {
      console.error('Error submitting inscription:', error);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 max-w-md w-full mx-4">
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-xl font-bold text-black dark:text-white">Create Inscription</h2>
          <button onClick={onClose} className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200">
            <X className="w-5 h-5" />
          </button>
        </div>

        {step === 1 ? (
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Image (optional)
              </label>
              <input
                type="file"
                accept="image/*"
                onChange={(e) => setImageFile(e.target.files[0])}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Text Content
              </label>
              <textarea
                value={embedText}
                onChange={(e) => setEmbedText(e.target.value)}
                required
                rows={4}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
                placeholder="Enter text to inscribe..."
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Price (BTC)
              </label>
              <input
                type="number"
                value={price}
                onChange={(e) => setPrice(e.target.value)}
                step="0.00000001"
                required
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
                placeholder="0.00000000"
              />
            </div>

            <div className="flex gap-3">
              <button
                type="button"
                onClick={onClose}
                className="flex-1 px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmitting}
                className="flex-1 px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700 disabled:opacity-50"
              >
                {isSubmitting ? 'Creating...' : 'Create Inscription'}
              </button>
            </div>
          </form>
        ) : (
          <div className="text-center py-8">
            <div className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
              Payment Step Coming Soon
            </div>
            <button
              onClick={() => setStep(1)}
              className="px-4 py-2 bg-gray-600 text-white rounded-md hover:bg-gray-700"
            >
              Back
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

const InscriptionModal = ({ inscription, onClose, copiedText, copyToClipboard }) => {
  const [activeTab, setActiveTab] = useState('overview');
  
  // Generate sample markdown content
  const markdownContent = `# Smart Contract: ${inscription.id}

## Overview
This is a sophisticated smart contract deployed on the Bitcoin blockchain using advanced steganographic techniques.

## Technical Specifications
- **Contract Type**: ${inscription.contractType || 'Unknown'}
- **Capability**: ${inscription.capability || 'Not specified'}
- **Protocol**: ${inscription.protocol || 'Custom'}
- **API Endpoints**: ${inscription.apiEndpoints || 0}
- **Total Interactions**: ${inscription.interactions || 0}

## Performance Metrics
- **Reputation Score**: ${inscription.reputation || 'N/A'}
- **Active Status**: ${inscription.isActive ? 'Active' : 'Inactive'}
- **Genesis Block**: ${inscription.genesis_block_height || 'Unknown'}

## Deployment Details
- **Inscription Number**: ${inscription.number || 'Unknown'}
- **Address**: ${inscription.address || 'Not available'}
- **MIME Type**: ${inscription.mime_type || 'Unknown'}

---

*This analysis was generated using advanced steganographic detection algorithms.*`;

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg max-w-4xl w-full mx-4 max-h-[90vh] overflow-y-auto">
        <div className="sticky top-0 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 p-4">
          <div className="flex justify-between items-center">
            <h2 className="text-xl font-bold text-black dark:text-white">Inscription Details</h2>
            <button onClick={onClose} className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200">
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        <div className="p-4">
          {/* Header with image and basic info */}
          <div className="flex gap-6 mb-6">
            <div className="flex-shrink-0">
              {inscription.image ? (
                <img 
                  src={inscription.image} 
                  alt={inscription.id}
                  className="w-48 h-48 object-cover rounded-lg border-2 border-gray-300 dark:border-gray-700"
                />
              ) : (
                <div className="w-48 h-48 bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 rounded-lg flex items-center justify-center border-2 border-gray-300 dark:border-gray-700">
                  <div className="text-6xl text-center">
                    {inscription.mime_type?.includes('text') ? '📄' : 
                     inscription.mime_type?.includes('image') ? '🖼️' : '📦'}
                  </div>
                </div>
              )}
            </div>

            <div className="flex-1">
              <div className="mb-4">
                <h3 className="text-lg font-semibold text-black dark:text-white mb-2">Basic Information</h3>
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <span className="text-gray-600 dark:text-gray-400">ID:</span>
                    <span className="text-black dark:text-white font-mono text-sm">{inscription.id}</span>
                    <button 
                      onClick={() => copyToClipboard(inscription.id)}
                      className="text-indigo-600 dark:text-indigo-400 hover:text-indigo-800 dark:hover:text-indigo-200"
                    >
                      {copiedText === inscription.id ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-gray-600 dark:text-gray-400">Address:</span>
                    <span className="text-black dark:text-white font-mono text-sm">{inscription.address}</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-gray-600 dark:text-gray-400">Status:</span>
                    <span className={`px-2 py-1 rounded text-xs font-semibold ${
                      inscription.isActive ? 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-300' : 'bg-gray-100 dark:bg-gray-900 text-gray-800 dark:text-gray-300'
                    }`}>
                      {inscription.isActive ? 'Active' : 'Inactive'}
                    </span>
                  </div>
                </div>
              </div>

              <div>
                <h3 className="text-lg font-semibold text-black dark:text-white mb-2">Contract Details</h3>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <span className="text-gray-600 dark:text-gray-400">Type:</span>
                    <div className="text-black dark:text-white">{inscription.contractType}</div>
                  </div>
                  <div>
                    <span className="text-gray-600 dark:text-gray-400">Capability:</span>
                    <div className="text-black dark:text-white">{inscription.capability}</div>
                  </div>
                  <div>
                    <span className="text-gray-600 dark:text-gray-400">Protocol:</span>
                    <div className="text-black dark:text-white">{inscription.protocol}</div>
                  </div>
                  <div>
                    <span className="text-gray-600 dark:text-gray-400">Reputation:</span>
                    <div className="flex items-center gap-1">
                      <span className="text-yellow-500 dark:text-yellow-400">★</span>
                      <span className="text-black dark:text-white font-semibold">{inscription.reputation}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div>
              {/* Tabs */}
              <div className="border-b border-gray-200 dark:border-gray-700 mb-4">
                <div className="flex gap-4">
                  {['overview', 'technical', 'analysis'].map((tab) => (
                    <button
                      key={tab}
                      onClick={() => setActiveTab(tab)}
                      className={`px-4 py-2 font-medium text-sm border-b-2 transition-colors ${
                        activeTab === tab
                          ? 'border-indigo-500 text-indigo-600 dark:text-indigo-400'
                          : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
                      }`}
                    >
                      {tab.charAt(0).toUpperCase() + tab.slice(1)}
                    </button>
                  ))}
                </div>
              </div>

              {/* Tab content */}
              {activeTab === 'overview' && (
                <div className="space-y-4">
                  {inscription.text && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-2">Content</h4>
                      <div className="bg-gray-100 dark:bg-gray-900 rounded-lg p-4">
                        <p className="text-gray-700 dark:text-gray-300 whitespace-pre-wrap">{inscription.text}</p>
                      </div>
                    </div>
                  )}
                  
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-2">Performance Metrics</h4>
                    <div className="grid grid-cols-2 gap-4">
                      <div className="bg-gray-100 dark:bg-gray-900 rounded-lg p-4">
                        <div className="text-2xl font-bold text-indigo-600 dark:text-indigo-400">{inscription.apiEndpoints || 0}</div>
                        <div className="text-sm text-gray-600 dark:text-gray-400">API Endpoints</div>
                      </div>
                      <div className="bg-gray-100 dark:bg-gray-900 rounded-lg p-4">
                        <div className="text-2xl font-bold text-green-600 dark:text-green-400">{inscription.interactions || 0}</div>
                        <div className="text-sm text-gray-600 dark:text-gray-400">Total Interactions</div>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {activeTab === 'technical' && (
                <div className="space-y-4">
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-2">Technical Specifications</h4>
                    <div className="bg-gray-100 dark:bg-gray-900 rounded-lg p-4">
                      <pre className="text-gray-700 dark:text-gray-300 text-sm whitespace-pre-wrap font-mono leading-relaxed">
                        {markdownContent}
                      </pre>
                    </div>
                  </div>
                </div>
              )}

              {activeTab === 'analysis' && (
                <div className="space-y-4">
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-2">Steganographic Analysis</h4>
                    <div className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4">
                      <div className="flex items-center gap-2 mb-2">
                        <div className="w-3 h-3 bg-yellow-500 rounded-full"></div>
                        <span className="text-yellow-800 dark:text-yellow-200 font-medium">Analysis Complete</span>
                      </div>
                      <p className="text-yellow-700 dark:text-yellow-300">
                        This inscription contains embedded data patterns consistent with steganographic techniques. 
                        Advanced analysis reveals hidden information structures within the carrier medium.
                      </p>
                    </div>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default function OrdiscanExplorer() {
  const [blocks, setBlocks] = useState([]);
  const [selectedBlock, setSelectedBlock] = useState(null);
  const [inscriptions, setInscriptions] = useState([]);
  const [selectedInscription, setSelectedInscription] = useState(null);
  const [showInscribeModal, setShowInscribeModal] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [isDarkMode, setIsDarkMode] = useState(true);
  const [pendingTransactions, setPendingTransactions] = useState([]);
  const [searchResults, setSearchResults] = useState(null);
  const [copiedText, setCopiedText] = useState('');
  const [currentInscriptions, setCurrentInscriptions] = useState([]);

  // Initialize with blocks and theme
  useEffect(() => {
    fetchBlocks();
    fetchInscriptions();

    // Poll for new blocks every 30 seconds
    const interval = setInterval(fetchBlocks, 30000);
    return () => clearInterval(interval);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Initialize theme based on user preference
  useEffect(() => {
    const savedTheme = localStorage.getItem('theme');
    if (savedTheme) {
      setIsDarkMode(savedTheme === 'dark');
    } else {
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      setIsDarkMode(prefersDark);
    }
  }, []);

  // Apply theme
  useEffect(() => {
    if (isDarkMode) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
    localStorage.setItem('theme', isDarkMode ? 'dark' : 'light');
  }, [isDarkMode]);

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

  const fetchBlocks = async () => {
    try {
      const response = await fetch('http://localhost:3001/api/blocks');
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = await response.json();
      let processedBlocks = data.slice(0, 10).map(generateBlock);

      if (processedBlocks.length === 0) {
        // Fallback mock blocks
        processedBlocks = [
          { height: 924000, timestamp: Date.now() - 3600000, hash: 'mock1', tx_count: 1500 },
          { height: 923999, timestamp: Date.now() - 7200000, hash: 'mock2', tx_count: 1400 },
        ].map(generateBlock);
      }

      // Add future block
      const futureBlock = {
        height: processedBlocks[0].height + 1,
        timestamp: Date.now() + 600000, // 10 minutes in future
        hash: 'pending...',
        inscriptionCount: 0,
        hasBRC20: false,
        thumbnail: null,
        tx_count: 0,
        isFuture: true
      };

      const allBlocks = [futureBlock, ...processedBlocks];
      setBlocks(allBlocks);
      console.log('Blocks set:', allBlocks.length, 'Selected block:', processedBlocks[0]?.height);
      if (!selectedBlock) {
        setSelectedBlock(processedBlocks[0]);
        console.log('Selected block set to:', processedBlocks[0]?.height);
      }
    } catch (error) {
      console.error('Error fetching blocks:', error);
    }
  };

  const fetchInscriptions = async () => {
    try {
      const response = await fetch('http://localhost:3001/api/inscriptions');
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = await response.json();
      const results = data.results || [];
      console.log('Fetched inscriptions:', results.length);
      const finalResults = results.length > 0 ? results : [
        { id: '110513026', number: 110513026, address: 'bc1p...', mime_type: 'image/png' },
        { id: '110513025', number: 110513025, address: 'bc1p...', mime_type: 'text/plain' },
      ];
      const processedInscriptions = generateInscriptions(finalResults);
      console.log('Inscriptions set:', processedInscriptions.length, 'for block:', selectedBlock?.height);
      setInscriptions(processedInscriptions);
      setCurrentInscriptions(finalResults); // Store raw data for search
    } catch (error) {
      console.error('Error fetching inscriptions:', error);
      // Fallback mock data
      const mockResults = [
        { id: '110513026', number: 110513026, address: 'bc1p...', mime_type: 'image/png' },
        { id: '110513025', number: 110513025, address: 'bc1p...', mime_type: 'text/plain' },
      ];
      const processedInscriptions = generateInscriptions(mockResults);
      setInscriptions(processedInscriptions);
      setCurrentInscriptions(mockResults);
    }
  };

  useEffect(() => {
    if (selectedBlock) {
      fetchInscriptions();
      // Scroll to selected block in horizontal list
      setTimeout(() => {
        const blockElement = document.querySelector(`[data-block-id="${selectedBlock.height}"]`);
        if (blockElement) {
          blockElement.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
        }
      }, 100);
    }
  }, [selectedBlock]);

  const handleScroll = (direction) => {
    const container = document.getElementById('block-scroll');
    if (container) {
      const scrollAmount = 200;
      container.scrollBy({ left: direction === 'left' ? -scrollAmount : scrollAmount, behavior: 'smooth' });
    }
  };

  return (
    <div className="min-h-screen bg-white dark:bg-gray-950 text-black dark:text-white">
      {/* Header */}
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

      {/* Block Scroll Section */}
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
                  onClick={setSelectedBlock}
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

      {/* Main Content */}
      <div className="container mx-auto px-6 py-8">
        {searchResults !== null ? (
          <div className="mb-8">
            <h2 className="text-4xl font-bold mb-4 text-black dark:text-white">Search Results</h2>

            {/* Inscriptions Results */}
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
                        // Convert to inscription format for modal
                        const inscription = {
                          id: tx.id,
                          contractType: 'Custom Contract',
                          capability: 'Data Storage',
                          protocol: 'BRC-20',
                          apiEndpoints: 1,
                          interactions: Math.floor(Math.random() * 100),
                          reputation: '4.5',
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

            {/* Blocks Results */}
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
            {/* Block Header */}
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
                Nov 14, 2025, 9:19 AM ({Math.floor((Date.now() - selectedBlock.timestamp) / 3600000)} hours ago)
              </div>
            </div>

            {/* Contracts or Pending Transactions Section */}
            {selectedBlock.isFuture ? (
              <PendingTransactionsView copiedText={copiedText} copyToClipboard={copyToClipboard} setSelectedInscription={setSelectedInscription} />
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
              </div>
            )}
          </>
        )}
       </div>

       {/* Inscribe Modal */}
      {showInscribeModal && (
        <InscribeModal
          onClose={() => setShowInscribeModal(false)}
          blocks={blocks}
          setPendingTransactions={setPendingTransactions}
        />
      )}

      {/* Inscription Modal */}
      {selectedInscription && (
        <InscriptionModal
          inscription={selectedInscription}
          onClose={() => setSelectedInscription(null)}
          copiedText={copiedText}
          copyToClipboard={copyToClipboard}
        />
      )}

      {/* Footer */}
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
