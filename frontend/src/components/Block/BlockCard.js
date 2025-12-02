import React from 'react';

const BlockCard = ({ block, onClick, isSelected }) => {
  const hasSmartContracts = (block.smart_contract_count || block.smart_contracts || 0) > 0;
  const hasWitnessImages = (block.witness_image_count || block.witness_images || 0) > 0;
  


  const getBackgroundClass = () => {
    if (block.isFuture) return 'from-yellow-200 to-yellow-300 dark:from-yellow-600 dark:to-yellow-800';
    
    // Check for historical significance first
    const historical = getHistoricalSignificance(block.height, block.timestamp);
    if (historical) {
      return historical.color;
    }
    
    const txCount = block.tx_count || 0;
    
    if (txCount > 3000) return 'from-blue-200 to-blue-300 dark:from-blue-600 dark:to-blue-800';
    if (txCount > 0) return 'from-gray-200 to-gray-300 dark:from-gray-700 dark:to-gray-800';
    return 'from-red-200 to-red-300 dark:from-red-600 dark:to-red-800';
  };

  const getHistoricalSignificance = (height, timestamp) => {
    // Famous historical blocks
    const historicalBlocks = {
      0: { 
        title: "Genesis Block", 
        description: "First ever Bitcoin block - 'The Times 03/Jan/2009 Chancellor on brink of second bailout for banks'",
        emoji: "🌅",
        color: "from-yellow-200 to-yellow-300 dark:from-yellow-600 dark:to-yellow-800"
      },
      1: { 
        title: "Block 1", 
        description: "Second block, mined by Satoshi Nakamoto",
        emoji: "🔨",
        color: "from-gray-200 to-gray-300 dark:from-gray-700 dark:to-gray-800"
      },
      174923: { 
        title: "Pizza Day Block", 
        description: "First commercial Bitcoin transaction - 2 pizzas for 10,000 BTC (May 22, 2010)",
        emoji: "🍕",
        color: "from-orange-200 to-orange-300 dark:from-orange-600 dark:to-orange-800"
      },
      210000: { 
        title: "First Halving", 
        description: "Reward reduced from 50 → 25 BTC (Nov 2012)",
        emoji: "💎",
        color: "from-purple-200 to-purple-300 dark:from-purple-600 dark:to-purple-800"
      },
      481824: { 
        title: "SegWit Activation", 
        description: "Segregated Witness officially activated on Bitcoin network",
        emoji: "⚡",
        color: "from-blue-200 to-blue-300 dark:from-blue-600 dark:to-blue-800"
      },
      709632: { 
        title: "Taproot Activation", 
        description: "Schnorr signatures and Tapscript go live (Nov 2021)",
        emoji: "🌲",
        color: "from-green-200 to-green-300 dark:from-green-600 dark:to-green-800"
      },
      420000: { 
        title: "Halving #2", 
        description: "Second Bitcoin halving - reward reduced from 25 to 12.5 BTC",
        emoji: "🔷",
        color: "from-indigo-200 to-indigo-300 dark:from-indigo-600 dark:to-indigo-800"
      },
      630000: { 
        title: "Halving #3", 
        description: "Third Bitcoin halving - reward reduced from 12.5 to 6.25 BTC",
        emoji: "⛏️",
        color: "from-pink-200 to-pink-300 dark:from-pink-600 dark:to-pink-800"
      },
      840000: {
        title: "Halving #4",
        description: "Fourth Bitcoin halving - reward reduced from 6.25 to 3.125 BTC",
        emoji: "🪙",
        color: "from-cyan-200 to-cyan-300 dark:from-cyan-600 dark:to-cyan-800"
      }
    };
    
    return historicalBlocks[height] || null;
  };

  const getBadgeText = () => {
    if (block.isFuture) return 'Pending Block';
    
    const historical = getHistoricalSignificance(block.height, block.timestamp);
    if (historical) {
      return historical.title;
    }
    
    const inscriptionCount = block.inscriptionCount || block.inscription_count || 0;
    const txCount = block.tx_count || 0;
    
    // Show inscription count for modern blocks with inscriptions
    if (inscriptionCount > 0) {
      return `${inscriptionCount} inscription${inscriptionCount !== 1 ? 's' : ''}`;
    }
    
    // Show transaction count for blocks without inscriptions or historical blocks
    if (txCount > 0) {
      return `${txCount} transaction${txCount !== 1 ? 's' : ''}`;
    }
    
    return 'Empty block';
  };

  const getBadgeClass = () => {
    if (block.isFuture) return 'text-yellow-800 dark:text-yellow-200';
    const txCount = block.tx_count || 0;
    if (txCount > 3000) return 'text-blue-700 dark:text-blue-200';
    if (txCount > 0) return 'text-gray-700 dark:text-gray-200';
    return 'text-indigo-700 dark:text-indigo-200';
  };

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
        <div className="h-32 flex items-center justify-center bg-black bg-opacity-20 relative">
          {(() => {
            const historical = getHistoricalSignificance(block.height, block.timestamp);
            if (historical) {
              return (
                <div className="text-center">
                  <div className="text-6xl">{historical.emoji}</div>
                </div>
              );
            }
            if (hasSmartContracts) {
              return (
                <div className="text-center">
                  <div className="text-6xl">🎨</div>
                </div>
              );
            }
            if (hasWitnessImages) {
              return <div className="text-6xl">🖼️</div>;
            }
            if (block.thumbnail) {
              return <div className="text-6xl">{block.thumbnail}</div>;
            }
            return <div className="text-6xl">⛏️</div>;
          })()}
        </div>
        
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
        {(() => {
          const historical = getHistoricalSignificance(block.height, block.timestamp);
          if (historical) {
            return (
              <div className="text-xs text-gray-600 dark:text-gray-400 italic">
                {historical.description}
              </div>
            );
          }
          if (block.tx_count) {
            return (
              <div className="text-xs text-gray-500 dark:text-gray-400">
                {block.tx_count} transactions
              </div>
            );
          }
          return null;
        })()}
        </div>
        
        {block.tx_count > 3000 && (
          <div className="absolute top-2 right-2 bg-blue-600 text-white text-xs px-2 py-1 rounded-md font-bold">
            BUSY
          </div>
        )}
        {(() => {
          const historical = getHistoricalSignificance(block.height, block.timestamp);
          if (historical) {
            return (
              <div className="absolute top-2 right-2 bg-orange-600 text-white text-xs px-2 py-1 rounded-md font-bold">
                HISTORIC
              </div>
            );
          }
          if (block.tx_count > 0 && block.tx_count <= 3000) {
            return (
              <div className="absolute top-2 right-2 bg-gray-600 text-white text-xs px-2 py-1 rounded-md font-bold">
                ACTIVE
              </div>
            );
          }
          return null;
        })()}
      </div>
    </div>
  );
};

export default BlockCard;
