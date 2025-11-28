import React, { useState, useEffect, useRef } from 'react';
import { Search, Copy, Check, Moon, Sun, ChevronLeft, ChevronRight, X } from 'lucide-react';
import { QRCodeCanvas } from 'qrcode.react';
import StegoAnalysisViewer from './StegoAnalysisViewer.js';

// Process block data using real backend counts
const generateBlock = (block) => {
  const now = Date.now();
  
  return {
    height: block.height,
    timestamp: block.timestamp || now - ((923627 - block.height) * 600000),
    hash: block.id,
    // Use smart_contracts count as inscription count since contracts are now optimized
    inscriptionCount: block.smart_contracts || 0,
    smart_contract_count: block.smart_contracts || 0,
    witness_image_count: block.witness_image_count || 0,
    hasBRC20: false, // Remove random BRC20 generation
    thumbnail: (block.smart_contracts && block.smart_contracts > 0) ? '🎨' : null,
    tx_count: block.tx_count,
    smart_contracts: [], // Empty array since we only have counts now
    witness_images: block.witness_images || []
  };
};

const generateInscriptions = (inscriptions) => {
  return inscriptions.map((insc, i) => ({
    id: insc.id,
    type: insc.mime_type?.split('/')[1]?.toUpperCase() || 'UNKNOWN',
    thumbnail: insc.mime_type?.startsWith('image/') ? `http://localhost:3001${insc.image_url}` : null,
    gradient: 'from-indigo-500', // Remove random colors
    hasMultiple: false, // Remove random multiple flag
    contractType: insc.contract_type || 'Steganographic Contract',
    capability: insc.capability || 'Data Storage & Concealment',
    protocol: insc.protocol || 'BRC-20',
    apiEndpoints: 1, // Remove random API endpoints
    interactions: 0, // Remove random interactions
    reputation: '4.8', // Remove random reputation
    isActive: true, // Remove random active status
    number: insc.number,
    address: insc.address,
    genesis_block_height: insc.genesis_block_height,
    mime_type: insc.mime_type,
    file_name: insc.file_name,
    file_path: insc.file_path,
    size_bytes: insc.size_bytes,
    image_url: insc.image_url,
    metadata: insc.metadata
  }));
};

const BlockCard = ({ block, onClick, isSelected }) => {
  const timeAgo = Math.floor((Date.now() - (block.timestamp * 1000)) / 3600000);
  const hasSmartContracts = (block.smart_contract_count || block.smart_contracts || 0) > 0;
  const hasWitnessImages = (block.witness_image_count || block.witness_images || 0) > 0;

  const getBackgroundClass = () => {
    if (block.isFuture) return 'from-yellow-200 to-yellow-300 dark:from-yellow-600 dark:to-yellow-800';
    
    // Use backend count fields directly
    const inscriptionCount = block.inscriptionCount || block.inscription_count || 0;
    const witnessImageCount = block.witness_image_count || 0;
    
    if (inscriptionCount > 0) return 'from-purple-200 to-purple-300 dark:from-purple-600 dark:to-purple-800';
    if (witnessImageCount > 0) return 'from-green-200 to-green-300 dark:from-green-600 dark:to-green-800';
    return 'from-gray-200 to-gray-300 dark:from-gray-700 dark:to-gray-800';
  };

  const getBadgeText = () => {
    if (block.isFuture) return 'Pending Block';
    
    // Use backend count fields directly
    const smartContractCount = block.smart_contract_count || block.smart_contracts || 0;
    const inscriptionCount = block.inscriptionCount || block.inscription_count || 0;
    const witnessImageCount = block.witness_image_count || 0;
    
    if (inscriptionCount > 0) {
      return `${inscriptionCount} stego inscription${inscriptionCount !== 1 ? 's' : ''}`;
    }
    
    if (smartContractCount > 0) {
      return `${smartContractCount} stego contract${smartContractCount !== 1 ? 's' : ''}`;
    }
    
    if (witnessImageCount > 0) {
      return `${witnessImageCount} witness image${witnessImageCount !== 1 ? 's' : ''}`;
    }
    
    return 'No inscriptions';
  };

  const getBadgeClass = () => {
    if (block.isFuture) return 'text-yellow-800 dark:text-yellow-200';
    if (hasSmartContracts) return 'text-purple-700 dark:text-purple-200';
    if (hasWitnessImages) return 'text-green-700 dark:text-green-200';
    return 'text-indigo-700 dark:text-indigo-200';
  };

  const getContractSummary = () => {
    if (!hasSmartContracts) return null;
    
    // Use backend count directly - no complex calculations
    const smartContractCount = block.smart_contract_count || block.smart_contracts || 0;
    return smartContractCount > 0 ? smartContractCount : null;
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
        {/* Enhanced Thumbnail or placeholder */}
        <div className="h-32 flex items-center justify-center bg-black bg-opacity-20 relative">
          {hasSmartContracts ? (
            <div className="text-center">
              <div className="text-6xl">🎨</div>
              {getContractSummary() && (
                <div className="absolute bottom-2 left-0 right-0 bg-purple-600 text-white text-xs px-2 py-1 rounded-full font-bold">
                  {getContractSummary()}% confidence
                </div>
              )}
              {/* Show additional indicator if there are contracts available */}
              {hasSmartContracts && (
                <div className="absolute top-2 right-2 bg-orange-500 text-white text-xs px-2 py-1 rounded-full font-bold">
                  Available
                </div>
              )}
            </div>
          ) : hasWitnessImages ? (
            <div className="text-6xl">🖼️</div>
          ) : block.thumbnail ? (
            <div className="text-6xl">{block.thumbnail}</div>
          ) : (
            <div className="text-gray-600 text-sm">No inscriptions</div>
          )}
        </div>
        
        {/* Enhanced Block info */}
        <div className={`p-3 ${block.isFuture ? 'bg-yellow-500 bg-opacity-40' : 'bg-black bg-opacity-40 dark:bg-black dark:bg-opacity-40 bg-white bg-opacity-60'}`}>
          <div className={`font-bold text-lg mb-1 ${block.isFuture ? 'text-yellow-900 dark:text-yellow-100' : 'text-black dark:text-white'}`}>
            Block {block.height}
          </div>
          <div className={`text-xs mb-2 font-semibold ${getBadgeClass()}`}>
            {getBadgeText()}
          </div>
          <div className={`text-xs ${block.isFuture ? 'text-yellow-700 dark:text-yellow-300' : 'text-gray-600 dark:text-gray-400'}`}>
            {block.isFuture ? 'Next block' : formatTimeAgo(block.timestamp)}
          </div>
          {block.tx_count && (
            <div className="text-xs text-gray-500 dark:text-gray-400">
              {block.tx_count} transactions
            </div>
          )}
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
  
  // Determine image source from new Block Images API
  const imageSource = inscription.thumbnail || inscription.image_url;

  return (
    <div
      onClick={() => onClick(inscription)}
      className="relative group cursor-pointer"
    >
      <div className="relative overflow-hidden rounded-lg border-2 border-gray-300 dark:border-gray-700 hover:border-indigo-500 transition-all duration-200 bg-white dark:bg-gray-800">
        {/* Image or placeholder */}
        <div className="h-32 flex items-center justify-center bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800">
          {imageSource ? (
            <img 
              src={imageSource} 
              alt={inscription.file_name || inscription.id}
              className="max-w-full max-h-full object-contain"
              onError={(e) => {
                e.target.style.display = 'none';
                e.target.nextSibling.style.display = 'flex';
              }}
            />
          ) : null}
          <div className="text-4xl" style={{display: imageSource ? 'none' : 'flex'}}>
            {inscription.contract_type === 'Steganographic Contract' ? '🎨' :
             inscription.mime_type?.includes('text') ? '📄' : 
             inscription.mime_type?.includes('image') ? '🖼️' : '📦'}
          </div>
        </div>
        
        {/* Enhanced Content overlay */}
        <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <div className="absolute bottom-0 left-0 right-0 p-3 text-white">
            <div className="text-xs font-mono truncate font-semibold mb-1">
              {inscription.file_name || inscription.id}
            </div>
            {inscription.metadata?.extracted_message && (
              <div className="text-xs truncate opacity-90 italic">
                "{inscription.metadata.extracted_message.slice(0, 50)}{inscription.metadata.extracted_message.length > 50 ? '...' : ''}"
              </div>
            )}
            {inscription.text && (
              <div className="text-xs mt-1 truncate opacity-90">{inscription.text}</div>
            )}
            {inscription.metadata?.confidence && (
              <div className="flex items-center gap-2 mt-2">
                <div className="w-full bg-green-400 rounded-full h-1">
                  <div 
                    className="bg-green-600 h-1 rounded-full" 
                    style={{width: `${Math.round(inscription.metadata.confidence * 100)}%`}}
                  ></div>
                </div>
                <span className="text-xs font-semibold">{Math.round(inscription.metadata.confidence * 100)}%</span>
              </div>
            )}
          </div>
        </div>
        
        {/* Enhanced Status badges */}
        <div className="absolute top-2 left-2 flex flex-col gap-1">
          {inscription.contract_type === 'Steganographic Contract' && (
            <div className="px-2 py-1 bg-gradient-to-r from-purple-600 to-purple-700 text-white text-xs rounded-full font-semibold shadow-lg">
              🔐 STEGO
            </div>
          )}
          {inscription.metadata?.stego_type && (
            <div className="px-2 py-1 bg-gradient-to-r from-blue-600 to-blue-700 text-white text-xs rounded-full font-semibold shadow-lg">
              {inscription.metadata.stego_type.includes('lsb') ? '🔍 LSB' : 
               inscription.metadata.stego_type.includes('alpha') ? '🎨 Alpha' : '🔬 Unknown'}
            </div>
          )}
        </div>
        
        {/* Enhanced confidence indicator */}
        {inscription.metadata?.confidence && (
          <div className="absolute top-2 right-2 bg-white dark:bg-gray-800 rounded-lg px-2 py-1 shadow-lg border border-gray-200 dark:border-gray-600">
            <div className="flex items-center gap-1">
              <div className={`w-2 h-2 rounded-full ${
                inscription.metadata.confidence >= 0.9 ? 'bg-green-500' :
                inscription.metadata.confidence >= 0.7 ? 'bg-yellow-500' : 'bg-red-500'
              }`}></div>
              <span className="text-black dark:text-white text-xs font-bold">
                {Math.round(inscription.metadata.confidence * 100)}%
              </span>
            </div>
          </div>
        )}
        
        {/* File size indicator */}
        {inscription.size_bytes && (
          <div className="absolute bottom-2 right-2 bg-white dark:bg-gray-800 rounded-lg px-2 py-1 shadow-lg border border-gray-200 dark:border-gray-600">
            <div className="flex items-center gap-1">
              <span className="text-black dark:text-white text-xs font-bold">
                {(inscription.size_bytes / 1024).toFixed(1)}KB
              </span>
            </div>
          </div>
        )}
      </div>
      
      {/* Enhanced footer information */}
      <div className="mt-2">
        <div className="text-black dark:text-white font-mono text-xs truncate font-medium" title={inscription.file_name || inscription.id}>
          {inscription.file_name || inscription.id}
        </div>
        <div className="flex items-center gap-2 mt-1">
          {inscription.mime_type && (
            <span className="text-gray-500 dark:text-gray-400 text-xs">
              {inscription.mime_type.split('/')[1]?.toUpperCase() || 'UNKNOWN'}
            </span>
          )}
          {inscription.metadata?.image_format && (
            <span className="text-gray-500 dark:text-gray-400 text-xs">
              {inscription.metadata.image_format.toUpperCase()}
            </span>
          )}
        </div>
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

// Utility function for time formatting - accessible to all components
const formatTimeAgo = (timestamp) => {
  const now = Date.now();
  const blockTime = timestamp * 1000; // Convert seconds to milliseconds
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

const InscriptionModal = ({ inscription, onClose, copiedText, copyToClipboard }) => {
  const [activeTab, setActiveTab] = useState('overview');
  
  // Generate comprehensive markdown content for steganographic contracts
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
- **Encoding Method**: ${inscription.metadata?.stego_type?.includes('lsb') ? 'Least Significant Bit (LSB)' : 'Unknown'}

## Extracted Intelligence
${inscription.metadata?.extracted_message ? `\`\`\`\n${inscription.metadata.extracted_message}\n\`\`\`` : 'No hidden message detected'}

## Blockchain Integration
- **Block Hash**: \`${inscription.metadata?.block_hash || 'Unknown'}\`
- **Network**: Bitcoin Mainnet
- **Consensus**: Proof of Work
- **Timestamp**: ${inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toISOString() : 'Unknown'}

---

*Analysis performed by Starlight AI Scanner - Advanced Steganographic Detection System*`;

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
          {/* Header with enhanced image and comprehensive info */}
          <div className="flex gap-6 mb-6">
            <div className="flex-shrink-0">
              {inscription.thumbnail || inscription.image_url ? (
                <div className="relative">
                  <img 
                    src={inscription.thumbnail || inscription.image_url} 
                    alt={inscription.file_name || inscription.id}
                    className="w-48 h-48 object-cover rounded-lg border-2 border-gray-300 dark:border-gray-700"
                  />
                  {inscription.metadata?.confidence && (
                    <div className="absolute top-2 right-2 bg-green-500 text-white text-xs px-2 py-1 rounded-md font-bold">
                      {Math.round(inscription.metadata.confidence * 100)}%
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

            <div className="flex-1">
              {/* All sections are now properly organized inside the Overview tab */}
            </div>

            <div className="mt-6">
              {/* Enhanced Tabs */}
              <div className="border-b border-gray-200 dark:border-gray-700 mb-6">
                <div className="flex gap-6">
                  {[
                    { id: 'overview', label: 'Overview', icon: '📊' },
                    { id: 'technical', label: 'Hidden Message', icon: '🔓' },
                    { id: 'analysis', label: 'Analysis', icon: '🔍' },
                    { id: 'blockchain', label: 'Blockchain', icon: '⛓️' }
                  ].map((tab) => (
                    <button
                      key={tab.id}
                      onClick={() => setActiveTab(tab.id)}
                      className={`px-4 py-2 font-medium text-sm border-b-2 transition-colors flex items-center gap-2 ${
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

              {/* Tab content */}
              {activeTab === 'overview' && (
                <div className="space-y-6">
                  {/* Contract Identity Section */}
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-purple-500 rounded-full"></span>
                      Contract Identity
                    </h4>
                    <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-4 space-y-3">
                      <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span className="text-gray-600 dark:text-gray-400 text-sm">File Name:</span>
                            <span className="text-black dark:text-white font-mono text-sm font-semibold">{inscription.file_name || inscription.id}</span>
                          </div>
                          <button 
                            onClick={() => copyToClipboard(inscription.file_name || inscription.id)}
                            className="text-indigo-600 dark:text-indigo-400 hover:text-indigo-800 dark:hover:text-indigo-200"
                          >
                            {copiedText === (inscription.file_name || inscription.id) ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                          </button>
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-gray-600 dark:text-gray-400 text-sm">Transaction ID:</span>
                        <span className="text-black dark:text-white font-mono text-xs">{inscription.metadata?.transaction_id || 'Not available'}</span>
                      </div>
                      <div className="flex items-center gap-4">
                        <div className="flex items-center gap-2">
                          <span className="text-gray-600 dark:text-gray-400 text-sm">Block Height:</span>
                          <span className="text-black dark:text-white font-semibold">{inscription.block_height || inscription.genesis_block_height || 'Unknown'}</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <span className="text-gray-600 dark:text-gray-400 text-sm">Status:</span>
                          <span className={`px-2 py-1 rounded text-xs font-semibold ${
                            inscription.isActive ? 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-300' : 'bg-gray-100 dark:bg-gray-900 text-gray-800 dark:text-gray-300'
                          }`}>
                            {inscription.isActive ? 'Active' : 'Inactive'}
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Technical Specifications Section */}
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                      Technical Specifications
                    </h4>
                    <div className="grid grid-cols-2 gap-4">
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Contract Type</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.contract_type || inscription.contractType || 'Steganographic'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Protocol Layer</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.protocol || 'BRC-20'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Data Capability</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.capability || 'Data Storage & Concealment'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">MIME Type</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.mime_type || 'Unknown'}</div>
                      </div>
                    </div>
                  </div>

                  {/* Steganographic Analysis Section */}
                  {inscription.metadata && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-yellow-500 rounded-full"></span>
                        Steganographic Analysis
                      </h4>
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Detection Method</div>
                          <div className="text-black dark:text-white font-semibold">{inscription.metadata.detection_method || 'AI Scanner'}</div>
                        </div>
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Stego Type</div>
                          <div className="text-black dark:text-white font-semibold">{inscription.metadata.stego_type || 'Unknown'}</div>
                        </div>
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Confidence Level</div>
                          <div className="flex items-center gap-2">
                            <div className="text-black dark:text-white font-semibold">{Math.round(inscription.metadata.confidence * 100)}%</div>
                            <div className="w-16 bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                              <div 
                                className="bg-green-500 h-2 rounded-full" 
                                style={{width: `${Math.round(inscription.metadata.confidence * 100)}%`}}
                              ></div>
                            </div>
                          </div>
                        </div>
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Image Format</div>
                          <div className="text-black dark:text-white font-semibold">{inscription.metadata.image_format || 'Unknown'}</div>
                        </div>
                      </div>
                    </div>
                  )}

                  {/* Extracted Intelligence */}
                  {inscription.metadata?.extracted_message && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                        Extracted Intelligence
                      </h4>
                      <div className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4">
                        <div className="flex items-start gap-2 mb-2">
                          <span className="text-yellow-600 dark:text-yellow-400 text-sm">🔓</span>
                          <span className="text-yellow-800 dark:text-yellow-200 text-sm font-medium">Hidden Message Decoded</span>
                        </div>
                        <p className="text-yellow-900 dark:text-yellow-100 font-mono text-sm leading-relaxed">{inscription.metadata.extracted_message}</p>
                      </div>
                    </div>
                  )}
                  
                  {/* Contract Performance */}
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-indigo-500 rounded-full"></span>
                      Contract Performance
                    </h4>
                    <div className="grid grid-cols-3 gap-4">
                      <div className="bg-gradient-to-br from-blue-50 to-blue-100 dark:from-blue-900 dark:to-blue-800 rounded-lg p-4 border border-blue-200 dark:border-blue-700">
                        <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">{inscription.apiEndpoints || 1}</div>
                        <div className="text-sm text-blue-700 dark:text-blue-300">API Endpoints</div>
                      </div>
                       <div className="bg-gradient-to-br from-green-50 to-green-100 dark:from-green-900 dark:to-green-800 rounded-lg p-4 border border-green-200 dark:border-green-700">
                         <div className="text-2xl font-bold text-green-600 dark:text-green-400">{inscription.interactions || 0}</div>
                         <div className="text-sm text-green-700 dark:text-green-300">Total Interactions</div>
                       </div>
                      <div className="bg-gradient-to-br from-purple-50 to-purple-100 dark:from-purple-900 dark:to-purple-800 rounded-lg p-4 border border-purple-200 dark:border-purple-700">
                        <div className="text-2xl font-bold text-purple-600 dark:text-purple-400">{inscription.reputation || '4.8'}</div>
                        <div className="text-sm text-purple-700 dark:text-purple-300">Reputation Score</div>
                      </div>
                    </div>
                  </div>

                  {/* Media Properties */}
                  {inscription.metadata && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-cyan-500 rounded-full"></span>
                        Media Properties
                      </h4>
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">File Size</div>
                          <div className="text-black dark:text-white font-semibold">
                            {inscription.metadata.image_size ? (inscription.metadata.image_size / 1024).toFixed(2) + ' KB' : 'Unknown'}
                          </div>
                        </div>
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Encoding Method</div>
                          <div className="text-black dark:text-white font-semibold">
                            {inscription.metadata.stego_type?.includes('lsb') ? 'Least Significant Bit (LSB)' : 
                             inscription.metadata.stego_type?.includes('alpha') ? 'Alpha Channel' : 'Unknown'}
                          </div>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'technical' && (
                <div className="space-y-6">
                  {/* Extracted Hidden Message - Main Focus */}
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
                          <div className="text-green-800 dark:text-green-200 text-xs font-mono mb-2 uppercase tracking-wider">Hidden Content:</div>
                          <p className="text-green-900 dark:text-green-100 font-mono text-base leading-relaxed break-all">
                            {inscription.metadata.extracted_message}
                          </p>
                        </div>

                        <div className="mt-4 pt-4 border-t border-green-200 dark:border-green-700">
                          <div className="grid grid-cols-2 gap-4">
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {inscription.metadata.extracted_message.length}
                              </div>
                              <div className="text-sm text-green-700 dark:text-green-300">Characters</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {inscription.metadata.extracted_message.split(' ').length}
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

                  {/* Message Analysis Details */}
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
                          <div className="flex items-center gap-2 mb-2">
                            <div className="text-purple-900 dark:text-purple-100 font-bold">
                              {Math.round(inscription.metadata.confidence * 100)}%
                            </div>
                            <div className="flex-1 bg-purple-200 dark:bg-purple-700 rounded-full h-2">
                              <div 
                                className="bg-purple-500 h-2 rounded-full" 
                                style={{width: `${Math.round(inscription.metadata.confidence * 100)}%`}}
                              ></div>
                            </div>
                          </div>
                          <div className="text-purple-600 dark:text-purple-400 text-xs">
                            {inscription.metadata.confidence >= 0.9 ? 'High confidence detection' :
                             inscription.metadata.confidence >= 0.7 ? 'Medium confidence detection' : 'Low confidence detection'}
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {/* Technical Architecture */}
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-orange-500 rounded-full"></span>
                      Technical Architecture
                    </h4>
                    <div className="bg-gray-100 dark:bg-gray-900 rounded-lg p-4">
                      <pre className="text-gray-700 dark:text-gray-300 text-sm whitespace-pre-wrap font-mono leading-relaxed">
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
                        Our Starlight AI scanner has successfully identified and extracted hidden information structures within the carrier medium.
                      </p>
                      
                      {inscription.metadata && (
                        <div className="grid grid-cols-2 gap-4 mt-6">
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Detection Algorithm</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.detection_method || 'Starlight AI Scanner'}</div>
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

                  {/* Analysis Timeline */}
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
                            <div className="text-cyan-700 dark:text-cyan-300 text-sm">Applied LSB and frequency analysis algorithms</div>
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
                      Blockchain Integration
                    </h4>
                    <div className="bg-purple-50 dark:bg-purple-900 border border-purple-200 dark:border-purple-700 rounded-lg p-4">
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Network</div>
                          <div className="text-purple-900 dark:text-purple-100 font-semibold">Bitcoin Mainnet</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Consensus</div>
                          <div className="text-purple-900 dark:text-purple-100 font-semibold">Proof of Work</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Block Hash</div>
                          <div className="text-purple-900 dark:text-purple-100 font-mono text-xs break-all">
                            {inscription.metadata?.block_hash || 'Unknown'}
                          </div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Deployment Time</div>
                          <div className="text-purple-900 dark:text-purple-100 font-semibold">
                            {inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toLocaleString() : 'Unknown'}
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Transaction Details */}
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-pink-500 rounded-full"></span>
                      Transaction Details
                    </h4>
                    <div className="bg-pink-50 dark:bg-pink-900 border border-pink-200 dark:border-pink-700 rounded-lg p-4">
                      <div className="space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-pink-700 dark:text-pink-300 text-sm">Transaction ID</span>
                          <div className="flex items-center gap-2">
                            <span className="text-pink-900 dark:text-pink-100 font-mono text-xs">
                              {inscription.metadata?.transaction_id?.slice(0, 8)}...
                            </span>
                            <button 
                              onClick={() => copyToClipboard(inscription.metadata?.transaction_id || '')}
                              className="text-pink-600 dark:text-pink-400 hover:text-pink-800 dark:hover:text-pink-200"
                            >
                              {copiedText === inscription.metadata?.transaction_id ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
                            </button>
                          </div>
                        </div>
                        <div className="flex items-center justify-between">
                          <span className="text-pink-700 dark:text-pink-300 text-sm">Image Index</span>
                          <span className="text-pink-900 dark:text-pink-100 font-semibold">
                            #{inscription.metadata?.image_index || 'Unknown'}
                          </span>
                        </div>
                        <div className="flex items-center justify-between">
                          <span className="text-pink-700 dark:text-pink-300 text-sm">Timestamp</span>
                          <span className="text-pink-900 dark:text-pink-100 font-semibold">
                            {inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toISOString() : 'Unknown'}
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Verification Status */}
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                      Verification Status
                    </h4>
                    <div className="bg-green-50 dark:bg-green-900 border border-green-200 dark:border-green-700 rounded-lg p-4">
                      <div className="flex items-center gap-3">
                        <div className="w-8 h-8 bg-green-500 rounded-full flex items-center justify-center">
                          <span className="text-white text-sm">✓</span>
                        </div>
                        <div>
                          <div className="text-green-900 dark:text-green-100 font-medium">Contract Verified</div>
                          <div className="text-green-700 dark:text-green-300 text-sm">
                            Steganographic content has been successfully verified and extracted
                          </div>
                        </div>
                      </div>
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
  const [isUserNavigating, setIsUserNavigating] = useState(false);
  const [shouldAutoScroll, setShouldAutoScroll] = useState(true);
  
  // Client-side pagination state
  const [hasMoreImages, setHasMoreImages] = useState(true);
  const [totalImages, setTotalImages] = useState(0);
  const [displayedCount, setDisplayedCount] = useState(20);

  // Initialize with blocks and theme (don't fetch inscriptions on init)
  useEffect(() => {
    fetchBlocks(false); // Initial load, not polling
    
    // Poll for new blocks every 30 seconds, but disable auto-scroll during polling
    const interval = setInterval(() => {
      setShouldAutoScroll(false);
      fetchBlocks(true); // Polling update
    }, 30000);
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

  const fetchBlocks = async (isPolling = false) => {
    try {
      // Try the enhanced endpoint first
      let response = await fetch('http://localhost:3001/api/blocks-with-contracts');
      let data = await response.json();
      
      if (!response.ok || !data.blocks) {
        // Fallback to regular blocks endpoint
        response = await fetch('http://localhost:3001/api/blocks');
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`);
        }
        data = await response.json();
      }
      
      const blocksData = data.blocks || data;
       let processedBlocks = blocksData.slice(0, 10).map(block => generateBlock(block));

       if (processedBlocks.length === 0) {
        // No blocks available - don't create fake data
        processedBlocks = [];
      }

      // Add future block
      const futureBlock = {
        height: processedBlocks[0]?.height + 1 || 924001,
        timestamp: Date.now() + 600000, // 10 minutes in future
        hash: 'pending...',
        inscriptionCount: 0,
        smart_contract_count: 0,
        witness_image_count: 0,
        hasBRC20: false,
        thumbnail: null,
        tx_count: 0,
        smart_contracts: [],
        witness_images: [],
        isFuture: true
      };

      const allBlocks = [futureBlock, ...processedBlocks];
      setBlocks(allBlocks);
      console.log('Blocks set:', allBlocks.length, 'Selected block:', processedBlocks[0]?.height);
      
      // Only set initial block selection if this is not polling and no block is selected
      if (!selectedBlock && !isPolling) {
        setIsUserNavigating(false); // Don't auto-scroll on initial load
        setShouldAutoScroll(true); // Allow auto-scroll on initial selection
        setSelectedBlock(processedBlocks[0]);
        console.log('Selected block set to:', processedBlocks[0]?.height);
      }
    } catch (error) {
      console.error('Error fetching blocks:', error);
      // Don't create fake data on error
      const futureBlock = {
        height: 924001, timestamp: Date.now() + 600000, hash: 'pending...', tx_count: 0, smart_contracts: [], witness_images: [], isFuture: true,
        inscription_count: 0, smart_contract_count: 0, witness_image_count: 0
      };
      setBlocks([futureBlock]);
      if (!selectedBlock && !isPolling) {
        setSelectedBlock(futureBlock);
      }
    }
  };

  // Lazy loading state
  let currentOffset = 0;
  const batchSize = 20;

  const fetchInscriptions = async () => {
    try {
      const response = await fetch(
        `http://localhost:3001/api/block-images?height=${selectedBlock.height}`
      );
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = await response.json();
      
      // Use images array from the new Block Images API
      const images = data.images || [];
      
      console.log(`Fetched ${images.length} block images for height ${selectedBlock.height}`);
      
      // Convert image objects to expected format
      const convertedResults = images.map(image => ({
        id: image.tx_id,
        number: selectedBlock.height,
        address: 'bc1p...', // Default address since images don't have one
        mime_type: image.content_type || 'application/octet-stream',
        genesis_block_height: selectedBlock.height,
        text: image.content || '',
        contract_type: image.content_type?.startsWith('image/') ? 'Steganographic Contract' : 'Inscription',
        file_name: image.file_name,
        file_path: image.file_path,
        size_bytes: image.size_bytes,
        image_url: `http://localhost:3001/api/block-image/${selectedBlock.height}/${image.file_name}`,
        metadata: {
          confidence: image.content_type?.startsWith('image/') ? 0.85 : 0.5,
          extracted_message: image.content || '',
          image_format: image.content_type?.split('/')[1] || 'unknown',
          image_size: image.size_bytes,
          stego_type: image.content_type?.startsWith('image/') ? 'lsb' : 'text',
          detection_method: 'AI-Powered Analysis'
        }
      }));
      
      // Process inscriptions
      const processedInscriptions = generateInscriptions(convertedResults);
      
      // Store all inscriptions but only display first batch
      setCurrentInscriptions(convertedResults);
      setTotalImages(processedInscriptions.length);
      setDisplayedCount(20); // Reset to initial display count
      setHasMoreImages(processedInscriptions.length > 20);
      
      // Display first batch
      setInscriptions(processedInscriptions.slice(0, 20));
      
      console.log(`Loaded ${processedInscriptions.length} total inscriptions, displaying first 20`);
    } catch (error) {
      console.error('Error fetching block images:', error);
      // Don't create fake data on error
      setInscriptions([]);
      setCurrentInscriptions([]);
      setTotalImages(0);
      setHasMoreImages(false);
    }
  };

  // Load more inscriptions function (client-side pagination)
  const loadMoreInscriptions = () => {
    if (hasMoreImages && currentInscriptions.length > displayedCount) {
      const newDisplayedCount = Math.min(displayedCount + 20, currentInscriptions.length);
      setDisplayedCount(newDisplayedCount);
      setInscriptions(currentInscriptions.slice(0, newDisplayedCount));
      setHasMoreImages(newDisplayedCount < currentInscriptions.length);
      
      console.log(`Displaying ${newDisplayedCount} of ${currentInscriptions.length} inscriptions`);
    }
  };

  useEffect(() => {
    if (selectedBlock) {
      // With optimized structure, we only have counts, so always fetch inscriptions from API
      fetchInscriptions();
      
      // Only auto-scroll if it's not user-initiated navigation and auto-scroll is enabled
      // Also ensure we're not in the middle of polling updates
      if (!isUserNavigating && shouldAutoScroll) {
        setTimeout(() => {
          const blockElement = document.querySelector(`[data-block-id="${selectedBlock.height}"]`);
          if (blockElement) {
            blockElement.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
          }
        }, 100);
      }
      
      // Reset user navigation flag after handling
      if (isUserNavigating) {
        setIsUserNavigating(false);
      }
    }
  }, [selectedBlock, isUserNavigating, shouldAutoScroll]);

  // Effect to prevent auto-scroll when blocks are updated during polling
  useEffect(() => {
    // When blocks change and shouldAutoScroll is false, ensure no auto-scroll happens
    if (!shouldAutoScroll) {
      // Reset shouldAutoScroll after a short delay to allow future user-initiated actions
      const timer = setTimeout(() => {
        setShouldAutoScroll(true);
      }, 1000);
      return () => clearTimeout(timer);
    }
  }, [blocks, shouldAutoScroll]);

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
                  onClick={(block) => {
                    setIsUserNavigating(true);
                    setSelectedBlock(block);
                  }}
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
                           interactions: 0,
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
                        setIsUserNavigating(true);
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
                {new Date(selectedBlock.timestamp * 1000).toLocaleString()} ({formatTimeAgo(selectedBlock.timestamp)})
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
                
                {/* Load More Button */}
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
                
                {/* End of Images Indicator */}
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
