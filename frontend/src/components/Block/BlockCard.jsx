import React from 'react';

const BlockCard = ({ block, onClick, isSelected }) => {
  const stegoCount = block.steganography_summary?.stego_count || 0;
  const smartContractCount = block.smart_contract_count ?? stegoCount;
  const hasSmartContracts = smartContractCount > 0;
  const hasWitnessImages = (block.witness_image_count || block.witness_images || 0) > 0;
  const inscriptionCount = block.inscription_count ?? block.inscriptionCount ?? 0;
  const hasImages = block.has_images !== undefined ? block.has_images : inscriptionCount > 0;
  const txCount = block.tx_count || 0;


  const getGlowClass = () => {
    if (block.isFuture) return 'shadow-lg';
    
    // Check for historical significance first
    const historical = getHistoricalSignificance(block.height);
    if (historical) {
      return 'shadow-lg';
    }
    
    const txCount = block.tx_count || 0;
    
    if (txCount > 3000) return 'shadow-lg';
    if (txCount > 0) return '';
    return '';
  };

  const getHistoricalSignificance = (height) => {
    // Famous historical blocks
    const historicalBlocks = {
      0: { 
        title: "Genesis Block", 
        description: "First ever Bitcoin block - 'The Times 03/Jan/2009 Chancellor on brink of second bailout for banks'",
        emoji: "🌅"
      },
      1: { 
        title: "Block 1", 
        description: "Second block, mined by Satoshi Nakamoto",
        emoji: "🔨"
      },
      174923: { 
        title: "Pizza Day Block", 
        description: "First commercial Bitcoin transaction - 2 pizzas for 10,000 BTC (May 22, 2010)",
        emoji: "🍕"
      },
      210000: { 
        title: "First Halving", 
        description: "Reward reduced from 50 → 25 BTC (Nov 2012)",
        emoji: "💎"
      },
      481824: { 
        title: "SegWit Activation", 
        description: "Segregated Witness officially activated on Bitcoin network",
        emoji: "⚡"
      },
      709632: { 
        title: "Taproot Activation", 
        description: "Schnorr signatures and Tapscript go live (Nov 2021)",
        emoji: "🌲"
      },
      420000: { 
        title: "Halving #2", 
        description: "Second Bitcoin halving - reward reduced from 25 to 12.5 BTC",
        emoji: "🔷"
      },
      630000: { 
        title: "Halving #3", 
        description: "Third Bitcoin halving - reward reduced from 12.5 to 6.25 BTC",
        emoji: "⛏️"
      },
      840000: {
        title: "Halving #4",
        description: "Fourth Bitcoin halving - reward reduced from 6.25 to 3.125 BTC",
        emoji: "🪙"
      }
    };
    
    return historicalBlocks[height] || null;
  };

  const getBadgeText = () => {
    if (block.isFuture) return 'Pending Block';
    
    const historical = getHistoricalSignificance(block.height);
    if (historical) {
      return historical.title;
    }
    
    if (smartContractCount > 0) return 'Smart contracts detected';
    if (hasImages) return 'Inscriptions present';
    if (txCount > 0) return 'Active block';
    return 'Empty block';
  };

  const getBadgeClass = () => {
    if (block.isFuture) return 'text-warning';
    if (txCount > 3000) return 'text-primary';
    if (txCount > 0) return 'text-secondary';
    return 'text-primary';
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
      className={`flex-shrink-0 w-40 ${block.isNew ? 'block-slide-in' : ''} ${
        block.isFuture ? 'opacity-75' : ''
      }`}
      title={undefined}
    >
      <div className={`card relative p-4 cursor-pointer transition-transform ${getGlowClass()} ${
        isSelected ? 'border-2 border-primary scale-105' : 'hover:scale-105'
      } ${block.isFuture ? 'border-2 border-warning' : ''}`}>
        {/* Notification badges at top */}
        {block.tx_count > 3000 && (
          <div className="absolute top-2 right-2 z-10 badge badge-primary">
            BUSY
          </div>
        )}
        {(() => {
          const historical = getHistoricalSignificance(block.height);
          if (historical) {
            return (
              <div className="absolute top-2 right-2 z-10 badge badge-warning">
                HISTORIC
              </div>
            );
          }
          if (block.tx_count > 0 && block.tx_count <= 3000) {
            return (
              <div className="absolute top-2 right-2 z-10 badge badge-secondary">
                ACTIVE
              </div>
            );
          }
          return null;
        })()}
        
        <div className="h-32 flex items-center justify-center">
          {(() => {
            const historical = getHistoricalSignificance(block.height);
            if (historical) {
              return (
                <div className="text-center">
                  <div className="text-5xl">{historical.emoji}</div>
                </div>
              );
            }
            if (hasSmartContracts) {
              return (
                <div className="text-center">
                  <div className="text-5xl">🎨</div>
                </div>
              );
            }
            if (hasWitnessImages) {
              return <div className="text-5xl">🖼️</div>;
            }
            if (block.thumbnail) {
              return <div className="text-5xl">{block.thumbnail}</div>;
            }
            return <div className="text-5xl">⛏️</div>;
          })()}
        </div>
        
        <div className="mt-4">
          <div className={`font-bold text-lg mb-1 ${block.isFuture ? 'text-warning' : 'text-primary'}`}>
            Block {block.height}
          </div>
          <div className={`text-xs mb-2 font-semibold ${getBadgeClass()}`}>
            {getBadgeText()}
          </div>
          <div className="flex flex-col gap-1 mb-2 text-xs text-secondary">
            <div>{`${smartContractCount} smart contract${smartContractCount === 1 ? '' : 's'}`}</div>
            <div>{formatTimeAgo(block.timestamp)}</div>
            <div>{`${txCount} transaction${txCount === 1 ? '' : 's'}`}</div>
          </div>
          <div className="flex items-center justify-center gap-2 mb-2 text-xs">
            {smartContractCount > 0 ? (
              <span className="badge badge-primary">
                {smartContractCount} smart contract{smartContractCount === 1 ? '' : 's'}
              </span>
            ) : (
              <span className={`badge ${hasImages ? 'badge-success' : 'badge-secondary'}`}>
                {hasImages ? `${inscriptionCount} inscription${inscriptionCount === 1 ? '' : 's'}` : 'No inscriptions'}
              </span>
            )}
            {hasWitnessImages && (
              <span className="badge badge-primary">
                Witness images
              </span>
            )}
          </div>
          {(() => {
            const historical = getHistoricalSignificance(block.height);
            if (historical) {
              return (
                <div className="text-xs text-secondary italic">
                  {historical.description}
                </div>
              );
            }
            return null;
          })()}
        </div>
      </div>
    </div>
  );
};

export default BlockCard;
