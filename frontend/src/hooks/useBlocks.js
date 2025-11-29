import { useState, useEffect } from 'react';

const generateBlock = (block) => {
  const now = Date.now();
  
  return {
    height: block.height,
    timestamp: block.timestamp || now - ((923627 - block.height) * 600000),
    hash: block.id,
    inscriptionCount: block.smart_contracts || 0,
    smart_contract_count: block.smart_contracts || 0,
    witness_image_count: block.witness_image_count || 0,
    hasBRC20: false,
    thumbnail: (block.smart_contracts && block.smart_contracts > 0) ? '🎨' : null,
    tx_count: block.tx_count,
    smart_contracts: [],
    witness_images: block.witness_images || []
  };
};

export const useBlocks = () => {
  const [blocks, setBlocks] = useState([]);
  const [selectedBlock, setSelectedBlock] = useState(null);
  const [isUserNavigating, setIsUserNavigating] = useState(false);
  const [shouldAutoScroll, setShouldAutoScroll] = useState(true);

  const fetchBlocks = async (isPolling = false) => {
    try {
      let response = await fetch('http://localhost:3001/api/blocks?contracts=true');
      let data = await response.json();
      
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      
      const blocksData = data.data || data.blocks || data;
      let processedBlocks = Array.isArray(blocksData) ? blocksData.slice(0, 10).map(block => generateBlock(block)) : [];

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
  };

  useEffect(() => {
    fetchBlocks(false);
    
    const interval = setInterval(() => {
      setShouldAutoScroll(false);
      fetchBlocks(true);
    }, 30000);
    return () => clearInterval(interval);
  }, [fetchBlocks]);

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