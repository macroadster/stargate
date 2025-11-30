import { useState, useEffect, useCallback } from 'react';

const generateBlock = (block) => {
  const now = Date.now();
  
  // Handle both data API and mempool API formats
  const inscriptionCount = block.inscriptions ? block.inscriptions.length : (block.smart_contracts || 0);
  const stegoCount = block.steganography_summary?.stego_count || 0;
  
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
    tx_count: block.tx_count || inscriptionCount || 0,
    witness_images: block.images || []
  };
};

export const useBlocks = () => {
  const [blocks, setBlocks] = useState([]);
  const [selectedBlock, setSelectedBlock] = useState(null);
  const [isUserNavigating, setIsUserNavigating] = useState(false);
  const [shouldAutoScroll, setShouldAutoScroll] = useState(true);

  const fetchBlocks = useCallback(async (isPolling = false) => {
    try {
      let response = await fetch('http://localhost:3001/api/data/blocks?limit=10');
      let data = await response.json();
      
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      
      const blocksData = data.blocks || data.data || data;
      console.log('Raw API response:', blocksData);
      
      let processedBlocks = Array.isArray(blocksData) ? blocksData.slice(0, 10).map(block => {
        const generated = generateBlock(block);
        return generated;
      }) : [];

      if (processedBlocks.length === 0) {
        processedBlocks = [];
      }

      const futureBlock = {
        height: processedBlocks[0]?.height + 1 || 924001,
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
      };

      const allBlocks = [futureBlock, ...processedBlocks];
      setBlocks(allBlocks);
      
      if (!selectedBlock && !isPolling) {
        setIsUserNavigating(false);
        setShouldAutoScroll(true);
        setSelectedBlock(processedBlocks[0]);
      }
    } catch (error) {
      console.error('Error fetching blocks:', error);
      const futureBlock = {
        height: 924001, timestamp: Date.now() + 600000, hash: 'pending...', tx_count: 0, smart_contracts: [], witness_images: [], isFuture: true,
        inscription_count: 0, smart_contract_count: 0, witness_image_count: 0
      };
      setBlocks([futureBlock]);
      if (!selectedBlock && !isPolling) {
        setSelectedBlock(futureBlock);
      }
    }
  }, [selectedBlock]);

  useEffect(() => {
    fetchBlocks(false);
    
    const interval = setInterval(() => {
      setShouldAutoScroll(false);
      fetchBlocks(true);
    }, 30000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (!shouldAutoScroll) {
      const timer = setTimeout(() => {
        setShouldAutoScroll(true);
      }, 1000);
      return () => clearTimeout(timer);
    }
  }, [blocks, shouldAutoScroll]);

  const handleBlockSelect = (block) => {
    setIsUserNavigating(true);
    setSelectedBlock(block);
  };

  return {
    blocks,
    selectedBlock,
    isUserNavigating,
    shouldAutoScroll,
    handleBlockSelect,
    setSelectedBlock,
    setIsUserNavigating
  };
};