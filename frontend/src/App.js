import React, { useState, useEffect, useRef } from 'react';
import { Search, Copy, Check, Moon, Sun, ChevronLeft, ChevronRight, X } from 'lucide-react';
import { QRCodeCanvas } from 'qrcode.react';

// Simulated data generators (adapted for real data)
const generateBlock = (block) => {
  const now = Date.now();
  const inscriptionCounts = [0, 5, 8, 15, 25, 97, 529, 1384, 4039, 3655];
  const randomCount = inscriptionCounts[Math.floor(Math.random() * inscriptionCounts.length)];
  
  return {
    height: block.height,
    timestamp: now - ((923627 - block.height) * 600000),
    hash: block.id,
    inscriptionCount: randomCount,
    hasBRC20: Math.random() > 0.6,
    thumbnail: randomCount > 0 ? (Math.random() > 0.5 ? '🖼️' : '🎨') : null,
    tx_count: block.tx_count
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
      } bg-gradient-to-br ${block.isFuture ? 'from-yellow-200 to-yellow-300 dark:from-yellow-600 dark:to-yellow-800' :
        block.inscriptionCount > 0 ? 'from-indigo-200 to-indigo-300 dark:from-indigo-600 dark:to-indigo-800' : 'from-gray-200 to-gray-300 dark:from-gray-700 dark:to-gray-800'}`}>
        {/* Thumbnail or placeholder */}
        <div className="h-32 flex items-center justify-center bg-black bg-opacity-20">
          {block.thumbnail ? (
            <div className="text-6xl">{block.thumbnail}</div>
          ) : (
            <div className="text-gray-600 text-sm">No inscriptions</div>
          )}
        </div>
        
        {/* Block info */}
        <div className={`p-3 ${block.isFuture ? 'bg-yellow-500 bg-opacity-40' : 'bg-black bg-opacity-40 dark:bg-black dark:bg-opacity-40 bg-white bg-opacity-60'}`}>
          <div className={`font-bold text-lg mb-1 ${block.isFuture ? 'text-yellow-900 dark:text-yellow-100' : 'text-black dark:text-white'}`}>{block.height}</div>
          <div className={`text-xs mb-2 ${block.isFuture ? 'text-yellow-800 dark:text-yellow-200' : 'text-indigo-700 dark:text-indigo-200'}`}>
            {block.isFuture ? 'Pending' : `${block.inscriptionCount} inscription${block.inscriptionCount !== 1 ? 's' : ''}`}
          </div>
          <div className={`text-xs ${block.isFuture ? 'text-yellow-700 dark:text-yellow-300' : 'text-gray-600 dark:text-gray-400'}`}>
            {block.isFuture ? 'Next block' : (timeAgo < 1 ? '< 1 hour ago' : `${timeAgo} hour${timeAgo > 1 ? 's' : ''} ago`)}
          </div>
        </div>
        
        {/* Special badges */}
        {block.hasBRC20 && (
          <div className="absolute top-2 right-2 bg-orange-600 text-white text-xs px-2 py-1 rounded-md font-bold">
            BRC-20
          </div>
        )}
      </div>
    </div>
  );
};

const InscriptionCard = ({ inscription, onClick }) => {
  console.log('Rendering inscription card:', inscription.id);
  return (
    <div
      onClick={() => onClick(inscription)}
      className="relative group cursor-pointer"
    >
      <div className="rounded-lg overflow-hidden bg-gray-200 dark:bg-gray-800 border border-gray-300 dark:border-gray-700 hover:border-indigo-500 transition-all p-4">
        <div className={`w-full h-32 rounded bg-gradient-to-br ${inscription.gradient} to-gray-900 flex items-center justify-center mb-3`}>
          <div className="text-white text-4xl opacity-70">
            {inscription.contractType === 'Knowledge Base' ? '📚' :
             inscription.contractType === 'Data Oracle' ? '🔮' :
             inscription.contractType === 'AI Agent' ? '🤖' :
             inscription.contractType === 'Token Contract' ? '💰' : '⚙️'}
          </div>
        </div>
        
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold text-indigo-600 dark:text-indigo-400">{inscription.contractType}</span>
            <div className={`w-2 h-2 rounded-full ${inscription.isActive ? 'bg-green-500' : 'bg-gray-500'}`}></div>
          </div>

          <div className="flex flex-wrap gap-1">
            <span className="text-xs bg-gray-300 dark:bg-gray-700 text-gray-700 dark:text-gray-300 px-2 py-1 rounded">
              {inscription.capability}
            </span>
            <span className="text-xs bg-gray-300 dark:bg-gray-700 text-gray-700 dark:text-gray-300 px-2 py-1 rounded">
              {inscription.protocol}
            </span>
          </div>

          <div className="grid grid-cols-2 gap-2 text-xs">
            <div>
              <div className="text-gray-500 dark:text-gray-400">APIs</div>
              <div className="text-black dark:text-white font-semibold">{inscription.apiEndpoints}</div>
            </div>
            <div>
              <div className="text-gray-500 dark:text-gray-400">Calls</div>
              <div className="text-black dark:text-white font-semibold">{inscription.interactions}</div>
            </div>
          </div>

          <div className="flex items-center justify-between pt-2 border-t border-gray-300 dark:border-gray-700">
            <div className="text-xs text-gray-500 dark:text-gray-400">Reputation</div>
            <div className="flex items-center gap-1">
              <span className="text-yellow-500 dark:text-yellow-400">★</span>
              <span className="text-black dark:text-white text-sm font-semibold">{inscription.reputation}</span>
            </div>
          </div>
        </div>
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
  const [isDragOver, setIsDragOver] = useState(false);
  const [paymentAddress, setPaymentAddress] = useState('');
  const fileInputRef = useRef(null);

  const handleFileChange = (e) => {
    const file = e.target.files[0];
    if (file && file.type.startsWith('image/')) {
      setImageFile(file);
    }
  };

  const handleDragOver = (e) => {
    e.preventDefault();
    setIsDragOver(true);
  };

  const handleDragLeave = (e) => {
    e.preventDefault();
    setIsDragOver(false);
  };

  const handleDrop = (e) => {
    e.preventDefault();
    setIsDragOver(false);
    const file = e.dataTransfer.files[0];
    if (file && file.type.startsWith('image/')) {
      setImageFile(file);
    }
  };

  const handleClick = () => {
    fileInputRef.current.click();
  };

  const handleFormSubmit = async (e) => {
    e.preventDefault();
    console.log('Form submitted', { imageFile, embedText, price });

    if (!imageFile) {
      alert('Please select an image file');
      return;
    }

    // Generate payment address
    const address = `bc1q${Math.random().toString(36).substring(2, 15)}${Math.random().toString(36).substring(2, 15)}`;
    setPaymentAddress(address);
    setStep(2);
  };

  const handlePaymentSubmit = async () => {
    setIsSubmitting(true);

    try {
      const formData = new FormData();
      formData.append('image', imageFile);
      formData.append('text', embedText);
      formData.append('price', price || '0.001');

      const response = await fetch('http://localhost:3001/api/inscribe', {
        method: 'POST',
        body: formData,
      });

      if (response.ok) {
        const result = await response.json();
        console.log('Inscription request submitted:', result);
        setIsSubmitting(false);
        onClose();
        // Reset
        setStep(1);
        setImageFile(null);
        setEmbedText('');
        setPrice('');
        setPaymentAddress('');
      } else {
        const errorText = await response.text();
        console.error('Failed to submit inscription request:', response.status, errorText);
        alert(`Failed to submit inscription request: ${response.status} ${errorText}`);
        setIsSubmitting(false);
      }
    } catch (error) {
      console.error('Error submitting inscription:', error);
      alert('Error submitting inscription');
      setIsSubmitting(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black dark:bg-black bg-white bg-opacity-90 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-white dark:bg-gray-900 rounded-xl max-w-md w-full border border-gray-300 dark:border-gray-700 flex flex-col" onClick={(e) => e.stopPropagation()}>
        <div className="p-6 border-b border-gray-300 dark:border-gray-800">
          <div className="flex justify-between items-center">
            <h2 className="text-2xl font-bold text-black dark:text-white">
              {step === 1 ? 'Create Inscription' : 'Send Payment'}
            </h2>
            <button
              onClick={onClose}
              className="text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white text-2xl"
            >
              ×
            </button>
          </div>
          {step === 2 && (
            <div className="mt-4 text-sm text-gray-600 dark:text-gray-400">
              Step 2 of 2: Send {price || '0.001'} BTC to the address below to complete your inscription
            </div>
          )}
        </div>

        {step === 1 ? (
          <form onSubmit={handleFormSubmit} className="p-6 space-y-4">
          <div>
            <label className="block text-gray-600 dark:text-gray-400 text-sm mb-2">Upload Image</label>
            <div
              onDragOver={handleDragOver}
              onDragLeave={handleDragLeave}
              onDrop={handleDrop}
              onClick={handleClick}
              className={`w-full border-2 border-dashed rounded-lg p-6 text-center cursor-pointer transition-colors ${
                isDragOver
                  ? 'border-indigo-500 bg-indigo-500 bg-opacity-10'
                  : 'border-gray-400 dark:border-gray-600 hover:border-gray-500 dark:hover:border-gray-500'
              }`}
            >
              {imageFile ? (
                <div>
                  <div className="text-green-400 text-4xl mb-2">✓</div>
                  <div className="text-black dark:text-white font-semibold">{imageFile.name}</div>
                  <div className="text-gray-500 dark:text-gray-400 text-sm">Click or drag to change</div>
                </div>
              ) : (
                <div>
                  <div className="text-gray-500 dark:text-gray-400 text-4xl mb-2">📁</div>
                  <div className="text-black dark:text-white font-semibold">Drop image here or click to browse</div>
                  <div className="text-gray-500 dark:text-gray-400 text-sm">Supports PNG, JPG, GIF, WEBP</div>
                </div>
              )}
            </div>
            <input
              ref={fileInputRef}
              type="file"
              accept="image/*"
              onChange={handleFileChange}
              className="hidden"
            />
          </div>

          <div>
            <label className="block text-gray-600 dark:text-gray-400 text-sm mb-2">Text to Embed</label>
            <textarea
              value={embedText}
              onChange={(e) => setEmbedText(e.target.value)}
              placeholder="Enter the smart contract code or text to hide in the image..."
              className="w-full bg-gray-100 dark:bg-gray-800 text-black dark:text-white rounded-lg p-3 border border-gray-300 dark:border-gray-700 focus:border-indigo-500 focus:outline-none h-32 resize-none"
              required
            />
          </div>

          <div>
            <label className="block text-gray-600 dark:text-gray-400 text-sm mb-2">Price (BTC)</label>
            <input
              type="number"
              step="0.00000001"
              value={price}
              onChange={(e) => setPrice(e.target.value)}
              placeholder="0.001"
              className="w-full bg-gray-100 dark:bg-gray-800 text-black dark:text-white rounded-lg p-3 border border-gray-300 dark:border-gray-700 focus:border-indigo-500 focus:outline-none"
              required
            />
          </div>

          <button
            type="submit"
            className="w-full bg-indigo-600 hover:bg-indigo-700 text-white py-3 rounded-lg font-semibold transition-colors"
          >
            Continue to Payment
          </button>
        </form>
        ) : (
          <div className="p-6 space-y-4">
            <div className="text-center">
              <div className="mb-4 flex justify-center">
                <QRCodeCanvas value={`bitcoin:${paymentAddress}?amount=${price || '0.001'}`} size={200} />
              </div>
              <div className="space-y-2">
                <div className="text-sm text-gray-600 dark:text-gray-400">Send exactly</div>
                <div className="text-2xl font-bold text-black dark:text-white">{price || '0.001'} BTC</div>
                <div className="text-sm text-gray-600 dark:text-gray-400">to</div>
                <div className="bg-gray-100 dark:bg-gray-800 p-3 rounded font-mono text-sm break-all">
                  {paymentAddress}
                </div>
              </div>
            </div>

            <div className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4">
              <div className="text-yellow-800 dark:text-yellow-200 text-sm">
                <strong>Note:</strong> This is a simulation. In a real application, you would send the payment from your Bitcoin wallet to this address.
              </div>
            </div>

            <button
              onClick={handlePaymentSubmit}
              disabled={isSubmitting}
              className="w-full bg-green-600 hover:bg-green-700 disabled:bg-gray-400 dark:disabled:bg-gray-600 text-white py-3 rounded-lg font-semibold transition-colors"
            >
              {isSubmitting ? 'Processing...' : 'I\'ve Sent the Payment'}
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
This is a decentralized smart contract deployed on the Bitcoin blockchain using inscription technology.

## Features
- **Automated Execution**: Contract executes automatically when conditions are met
- **Immutable Storage**: All data is permanently stored on-chain
- **Transparent Logic**: Contract code is publicly visible and auditable

## Contract Methods
\`\`\`javascript
function transfer(address to, uint256 amount) {
  require(balance[msg.sender] >= amount);
  balance[msg.sender] -= amount;
  balance[to] += amount;
}
\`\`\`

## State Variables
- Total Supply: 21,000,000
- Current Holders: 1,234
- Contract Type: ${inscription.type}

## Events
- Transfer(from, to, amount)
- Approval(owner, spender, amount)
`;

  // Generate sample transactions, including the inscription transaction
  const transactions = [
    {
      hash: inscription.id,
      type: 'Inscribe',
      from: 'Your Wallet',
      to: 'Ordinals Protocol',
      amount: '0.001 BTC',
      timestamp: inscription.timestamp ? inscription.timestamp * 1000 : Date.now(),
      status: inscription.status || 'pending'
    },
    {
      hash: 'a1b2c3d4e5f6g7h8i9j0',
      type: 'Deploy',
      from: 'bc1q...xyz123',
      to: 'Contract Creation',
      amount: '0.001 BTC',
      timestamp: Date.now() - 86400000,
      status: 'confirmed'
    },
    {
      hash: 'k1l2m3n4o5p6q7r8s9t0',
      type: 'Transfer',
      from: 'bc1q...abc456',
      to: 'bc1q...def789',
      amount: '1,000 Tokens',
      timestamp: Date.now() - 43200000,
      status: 'confirmed'
    },
    {
      hash: 'u1v2w3x4y5z6a7b8c9d0',
      type: 'Mint',
      from: 'bc1q...ghi012',
      to: 'bc1q...ghi012',
      amount: '5,000 Tokens',
      timestamp: Date.now() - 21600000,
      status: 'confirmed'
    },
    {
      hash: 'e1f2g3h4i5j6k7l8m9n0',
      type: 'Transfer',
      from: 'bc1q...jkl345',
      to: 'bc1q...mno678',
      amount: '500 Tokens',
      timestamp: Date.now() - 10800000,
      status: 'confirmed'
    },
    {
      hash: 'o1p2q3r4s5t6u7v8w9x0',
      type: 'Approval',
      from: 'bc1q...pqr901',
      to: 'bc1q...stu234',
      amount: '10,000 Tokens',
      timestamp: Date.now() - 3600000,
      status: 'pending'
    }
  ];
  
  return (
    <div className="fixed inset-0 bg-black dark:bg-black bg-white bg-opacity-90 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-white dark:bg-gray-900 rounded-xl max-w-6xl w-full max-h-[90vh] overflow-hidden border border-gray-300 dark:border-gray-700 flex flex-col" onClick={(e) => e.stopPropagation()}>
        <div className="p-6 border-b border-gray-300 dark:border-gray-800">
          <div className="flex justify-between items-start">
            <div>
               <h2 className="text-2xl font-bold text-black dark:text-white mb-2">Contract Details</h2>
              <div className="flex gap-2 items-center">
                <span className="text-gray-500 dark:text-gray-400 text-sm font-mono">
                  {inscription.id}
                </span>
                <button onClick={() => copyToClipboard(inscription.id)} className="text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white">
                  {copiedText === inscription.id ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
            </div>
            <button
              onClick={onClose}
              className="text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white text-2xl"
            >
              ×
            </button>
          </div>
          
          {/* Tabs */}
          <div className="flex gap-6 mt-6">
            <button
              onClick={() => setActiveTab('overview')}
              className={`pb-2 font-semibold ${
                activeTab === 'overview'
                  ? 'text-black dark:text-white border-b-2 border-indigo-500'
                  : 'text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white'
              }`}
            >
              Overview
            </button>
            <button
              onClick={() => setActiveTab('markdown')}
              className={`pb-2 font-semibold ${
                activeTab === 'markdown'
                  ? 'text-black dark:text-white border-b-2 border-indigo-500'
                  : 'text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white'
              }`}
            >
              Documentation
            </button>
            <button
              onClick={() => setActiveTab('transactions')}
              className={`pb-2 font-semibold ${
                activeTab === 'transactions'
                  ? 'text-black dark:text-white border-b-2 border-indigo-500'
                  : 'text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white'
              }`}
            >
              Transactions
            </button>
          </div>
        </div>
        
        <div className="flex-1 overflow-y-auto p-6">
          {activeTab === 'overview' && (
            <div className="grid grid-cols-2 gap-6">
              <div>
                {inscription.image ? (
                  <img
                    src={inscription.image}
                    alt="Contract Image"
                    className="w-full aspect-square rounded-lg object-cover mb-4"
                  />
                ) : (
                  <div className={`aspect-square rounded-lg bg-gradient-to-br ${inscription.gradient || 'from-blue-500'} to-gray-900 flex items-center justify-center mb-4`}>
                    <div className="text-white text-9xl opacity-50">🎨</div>
                  </div>
                )}
              </div>
              
              <div className="space-y-4">
                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Contract Type</div>
                  <div className="text-gray-900 dark:text-gray-100 font-semibold">{inscription.contractType}</div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Capability</div>
                  <div className="text-gray-900 dark:text-gray-100 font-mono text-sm">{inscription.capability}</div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Protocol</div>
                  <div className="text-gray-900 dark:text-gray-100">{inscription.protocol}</div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">API Endpoints</div>
                  <div className="text-gray-900 dark:text-gray-100 font-semibold">{inscription.apiEndpoints} endpoints</div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Total Interactions</div>
                  <div className="text-gray-900 dark:text-gray-100 font-semibold">{inscription.interactions?.toLocaleString() || '0'}</div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Status</div>
                  <div className="flex items-center gap-2">
                    <div className={`w-2 h-2 rounded-full ${inscription.isActive ? 'bg-green-500' : 'bg-gray-500'}`}></div>
                    <span className="text-gray-900 dark:text-gray-100">{inscription.isActive ? 'Active' : 'Inactive'}</span>
                  </div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Reputation Score</div>
                  <div className="flex items-center gap-1">
                    <span className="text-yellow-600 dark:text-yellow-400">★</span>
                    <span className="text-gray-900 dark:text-gray-100 font-semibold">{inscription.reputation} / 5.0</span>
                  </div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Contract ID</div>
                  <div className="text-indigo-700 dark:text-indigo-300 font-mono text-sm hover:underline cursor-pointer">
                    {inscription.id}
                  </div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Satoshi</div>
                  <div className="text-gray-900 dark:text-gray-100 font-mono text-sm">
                    {Math.floor(Math.random() * 1000000000)}
                  </div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Owner</div>
                  <div className="text-gray-900 dark:text-gray-100 font-mono text-sm">
                    bc1q{Math.random().toString(36).substring(2, 15)}...
                  </div>
                </div>
                
              </div>
            </div>
          )}
          
          {activeTab === 'markdown' && (
            <div className="prose dark:prose-invert max-w-none">
              <div className="bg-gray-100 dark:bg-gray-800 rounded-lg p-6">
                <pre className="text-gray-700 dark:text-gray-300 text-sm whitespace-pre-wrap font-mono leading-relaxed">
                  {markdownContent}
                </pre>
              </div>
            </div>
          )}
          
          {activeTab === 'transactions' && (
            <div className="space-y-4">
              <div className="flex justify-between items-center mb-4">
                <h3 className="text-xl font-bold text-black dark:text-white">Transaction History</h3>
                <div className="text-sm text-gray-600 dark:text-gray-400">
                  Showing {transactions.length} transactions
                </div>
              </div>
              
              <div className="space-y-3">
                {transactions.map((tx, idx) => (
                  <div key={idx} className="bg-gray-100 dark:bg-gray-800 rounded-lg p-4 border border-gray-300 dark:border-gray-700 hover:border-indigo-500 transition-colors">
                    <div className="flex justify-between items-start mb-3">
                      <div className="flex items-center gap-3">
                        <div className={`px-3 py-1 rounded text-xs font-semibold ${
                          tx.type === 'Deploy' ? 'bg-purple-600 text-white' :
                          tx.type === 'Transfer' ? 'bg-blue-600 text-white' :
                          tx.type === 'Mint' ? 'bg-green-600 text-white' :
                          'bg-yellow-600 text-white'
                        }`}>
                          {tx.type}
                        </div>
                        <div className="text-indigo-600 dark:text-indigo-400 font-mono text-sm hover:underline cursor-pointer">
                          {tx.hash}
                        </div>
                      </div>
                      <div className={`px-2 py-1 rounded text-xs font-semibold ${
                        tx.status === 'confirmed' ? 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-300' : 'bg-yellow-100 dark:bg-yellow-900 text-yellow-800 dark:text-yellow-300'
                      }`}>
                        {tx.status}
                      </div>
                    </div>

                    <div className="grid grid-cols-3 gap-4 text-sm">
                      <div>
                        <div className="text-gray-600 dark:text-gray-400 mb-1">From</div>
                        <div className="text-black dark:text-white font-mono text-xs">{tx.from}</div>
                      </div>
                      <div>
                        <div className="text-gray-600 dark:text-gray-400 mb-1">To</div>
                        <div className="text-black dark:text-white font-mono text-xs">{tx.to}</div>
                      </div>
                      <div>
                        <div className="text-gray-600 dark:text-gray-400 mb-1">Amount</div>
                        <div className="text-black dark:text-white font-semibold">{tx.amount}</div>
                      </div>
                    </div>

                    <div className="mt-3 text-xs text-gray-600 dark:text-gray-400">
                      {new Date(tx.timestamp).toLocaleString()}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
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
