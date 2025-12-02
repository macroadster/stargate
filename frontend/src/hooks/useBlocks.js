import { useState, useEffect, useCallback } from 'react';

const generateBlock = (block) => {
  const now = Date.now();
  
  // Handle both data API and mempool API formats
  const inscriptionCount = block.inscriptions ? block.inscriptions.length : (block.smart_contracts || 0);
  const stegoCount = block.steganography_summary?.stego_count || 0;
  const txCount = block.tx_count ?? block.total_transactions ?? block.TotalTransactions ?? 0;
  
  // For UI purposes, treat all inscriptions as "smart contracts" to show the badge
  const displayContractCount = Math.max(inscriptionCount, stegoCount);
  

  
  return {
    height: block.block_height || block.height,
    timestamp: block.timestamp || now - ((923627 - (block.block_height || block.height)) * 600000),
    hash: block.block_hash || block.id,
    inscriptionCount: inscriptionCount,
    inscription_count: inscriptionCount,
    smart_contract_count: displayContractCount,
    smart_contracts: block.inscriptions || [],
    witness_image_count: block.images ? block.images.length : 0,
    hasBRC20: false,
    thumbnail: (inscriptionCount > 0) ? '🎨' : null,
    tx_count: txCount,
    witness_images: block.images || []
  };
};

export const useBlocks = () => {
  const [blocks, setBlocks] = useState([]);
  const [selectedBlock, setSelectedBlock] = useState(null);
  const [isUserNavigating, setIsUserNavigating] = useState(false);
  const [blockLimit, setBlockLimit] = useState(20);

  const fetchBlocks = useCallback(async (isPolling = false, limitOverride) => {
    try {
      // Fetch recent blocks
      const limit = limitOverride || blockLimit;
      let response = await fetch(`http://localhost:3001/api/data/blocks?limit=${limit}`);
      let data = await response.json();
      
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      
      const blocksData = data.blocks || data.data || data;

      let processedBlocks = Array.isArray(blocksData)
        ? blocksData
            .map(generateBlock)
            .filter(b => b.height)
        : [];

      // Deduplicate by height and sort desc
      const seen = new Set();
      processedBlocks = processedBlocks
        .filter(b => {
          if (seen.has(b.height)) return false;
          seen.add(b.height);
          return true;
        })
        .sort((a, b) => b.height - a.height)
        .slice(0, limit);

      // Pin genesis if missing
      const genesisHeight = 0;
      if (!processedBlocks.some(b => b.height === genesisHeight)) {
        processedBlocks.push({
          height: genesisHeight,
          timestamp: 1231006505,
          hash: '000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f',
          inscriptionCount: 0,
          smart_contract_count: 0,
          witness_image_count: 0,
          hasBRC20: false,
          thumbnail: null,
          tx_count: 1,
          smart_contracts: [],
          witness_images: [],
          isGenesis: true
        });
      }

      // Add a future/pending block one height above the latest known real block
      if (processedBlocks.length > 0) {
        const maxHeight = processedBlocks.reduce((max, b) => Math.max(max, b.height), 0);
        const nextHeight = maxHeight + 1;
        processedBlocks.unshift({
          height: nextHeight,
          timestamp: Date.now() + 600000,
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
        });
      }

      setBlocks(processedBlocks);

      if (!selectedBlock && processedBlocks.length && !isPolling) {
        setIsUserNavigating(false);
        setSelectedBlock(processedBlocks[0]);
      }
    } catch (error) {
      console.error('Error fetching blocks:', error);
      setBlocks([]);
      if (!selectedBlock && !isPolling) {
        setSelectedBlock(null);
      }
    }
  }, [selectedBlock, blockLimit]);

  const loadMoreBlocks = useCallback(() => {
    setBlockLimit((prev) => Math.min(prev + 10, 100));
    fetchBlocks(true, blockLimit + 10);
  }, [fetchBlocks, blockLimit]);

  useEffect(() => {
    fetchBlocks(false);
    
    const interval = setInterval(() => {
      fetchBlocks(true);
    }, 30000);
    return () => clearInterval(interval);
  }, [fetchBlocks]);

  const handleBlockSelect = (block) => {
    setIsUserNavigating(true);
    setSelectedBlock(block);
  };

  return {
    blocks,
    selectedBlock,
    isUserNavigating,
    handleBlockSelect,
    setSelectedBlock,
    setIsUserNavigating,
    loadMoreBlocks
  };
};
