import React from 'react';

const BlockCard = ({ block, onClick, isSelected }) => {
  const hasSmartContracts = (block.smart_contract_count || block.smart_contracts || 0) > 0;
  const hasWitnessImages = (block.witness_image_count || block.witness_images || 0) > 0;

  const getBackgroundClass = () => {
    if (block.isFuture) return 'from-yellow-200 to-yellow-300 dark:from-yellow-600 dark:to-yellow-800';
    
    const inscriptionCount = block.inscriptionCount || block.inscription_count || 0;
    const witnessImageCount = block.witness_image_count || 0;
    
    if (inscriptionCount > 0) return 'from-purple-200 to-purple-300 dark:from-purple-600 dark:to-purple-800';
    if (witnessImageCount > 0) return 'from-green-200 to-green-300 dark:from-green-600 dark:to-green-800';
    return 'from-gray-200 to-gray-300 dark:from-gray-700 dark:to-gray-800';
  };

  const getBadgeText = () => {
    if (block.isFuture) return 'Pending Block';
    
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
          {hasSmartContracts ? (
            <div className="text-center">
              <div className="text-6xl">🎨</div>
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
};

export default BlockCard;