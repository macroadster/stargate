import { useState, useEffect, useCallback, useRef } from 'react';
import { API_BASE } from '../apiBase';

const generateBlock = (block) => {
  const now = Date.now();

  // Handle both data API and mempool API formats
  const inscriptionCount = block.inscription_count ?? (block.inscriptions ? block.inscriptions.length : 0);
  const smartContractCount =
    block.smart_contract_count ??
    (block.smart_contracts ? block.smart_contracts.length : 0);
  const hasImages = block.has_images ?? (inscriptionCount > 0 || smartContractCount > 0);
  const stegoCount = block.steganography_summary?.stego_count || 0;
  const txCount = block.tx_count ?? block.total_transactions ?? block.TotalTransactions ?? 0;

  // For UI purposes, treat all inscriptions as "smart contracts" to show the badge
  const displayContractCount = Math.max(smartContractCount, stegoCount);

  const resolvedHeight = Number(block.block_height ?? block.height ?? 0);
  const resolvedTimestamp =
    block.timestamp ??
    (resolvedHeight > 0
      ? now - ((923627 - resolvedHeight) * 600000)
      : 1231006505);

  return {
    height: resolvedHeight,
    timestamp: resolvedTimestamp,
    hash: block.block_hash ?? block.id ?? `block-${resolvedHeight}`,
    inscriptionCount: inscriptionCount,
    inscription_count: inscriptionCount,
    smart_contract_count: displayContractCount,
    smart_contracts: block.smart_contracts || [],
    witness_image_count: block.images ? block.images.length : 0,
    hasBRC20: false,
    has_images: hasImages,
    thumbnail: hasImages ? '🎨' : null,
    tx_count: txCount,
    witness_images: block.images || []
  };
};

export const useBlocks = () => {
  const [blocks, setBlocks] = useState([]);
  const [selectedBlock, setSelectedBlock] = useState(null);
  const [isUserNavigating, setIsUserNavigating] = useState(false);
  const [nextCursor, setNextCursor] = useState(null);
  const [hasMore, setHasMore] = useState(true);
  const [showHistorical, setShowHistorical] = useState(true);
  const loadingRef = useRef(false);
  const blocksRef = useRef([]);
  const latestHeightRef = useRef(null);
  const newBlockTimeoutRef = useRef(null);
  const selectedBlockRef = useRef(null);
  const manualSelectedHeight = useRef(null);
  const pinnedMilestones = useRef([
    0, // genesis
    174923, // Pizza Day
    210000, // Halving #1
    420000, // Halving #2
    481824, // SegWit
    630000, // Halving #3
    709632, // Taproot
    840000  // Halving #4
  ]);

  const fetchBlocks = useCallback(async (cursor = null, isPolling = false) => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    try {
      const url = new URL(`${API_BASE}/api/data/block-summaries`);
      url.searchParams.set('limit', 20);
      if (cursor) url.searchParams.set('cursor_height', cursor);

      const response = await fetch(url.toString());
      const data = await response.json();

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      const blocksData = data.blocks || [];

      const recentBlocks = Array.isArray(blocksData)
        ? blocksData
            .map(generateBlock)
            .filter(b => b.height)
        : [];
      const milestoneSet = new Set(pinnedMilestones.current);
      const maxHeight = recentBlocks
        .filter((b) => !milestoneSet.has(b.height))
        .reduce((max, b) => Math.max(max, b.height || 0), 0);
      if (showHistorical && maxHeight > 0 && maxHeight < 200000) {
        setShowHistorical(false);
      }

      const combined = [...blocksRef.current, ...recentBlocks];
      const seenFinal = new Set();
      let deduped = combined.filter((b) => {
        if (seenFinal.has(b.height)) return false;
        seenFinal.add(b.height);
        return true;
      }).sort((a, b) => b.height - a.height);

      // Limit to last 200 blocks to prevent memory accumulation
      deduped = deduped.slice(0, 200);

      // Always include milestone blocks
      const milestoneMeta = {
        0: {
          timestamp: 1231006505,
          hash: '000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f',
          tx_count: 1
        },
        174923: {
          // Pizza Day block approximate timestamp/tx count
          timestamp: new Date('2010-05-22T00:00:00Z').getTime() / 1000,
          hash: 'pizza-day',
          tx_count: 5
        },
        210000: { hash: 'halving-1', tx_count: 700, timestamp: new Date('2012-11-28T15:00:00Z').getTime() / 1000 },
        420000: { hash: 'halving-2', tx_count: 1600, timestamp: new Date('2016-07-09T17:00:00Z').getTime() / 1000 },
        481824: { hash: 'segwit', tx_count: 2000, timestamp: new Date('2017-08-24T00:00:00Z').getTime() / 1000 },
        630000: { hash: 'halving-3', tx_count: 2500, timestamp: new Date('2020-05-11T19:00:00Z').getTime() / 1000 },
        709632: { hash: 'taproot', tx_count: 2100, timestamp: new Date('2021-11-14T05:00:00Z').getTime() / 1000 },
        840000: { hash: 'halving-4', tx_count: 3100, timestamp: new Date('2024-04-20T00:00:00Z').getTime() / 1000 }
      };

      if (showHistorical) {
        pinnedMilestones.current.forEach((height) => {
          if (!deduped.some((b) => b.height === height)) {
            const meta = milestoneMeta[height] || {};
            deduped.push({
              height,
              timestamp: meta.timestamp || (height === 0 ? 1231006505 : Date.now() - 600000 * 10),
              hash: meta.hash || (height === 0 ? '000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f' : `milestone-${height}`),
              inscriptionCount: 0,
              smart_contract_count: 0,
              witness_image_count: 0,
              hasBRC20: false,
              thumbnail: null,
              tx_count: meta.tx_count || 0,
              smart_contracts: [],
              witness_images: [],
              isGenesis: height === 0,
              has_images: false
            });
          }
        });
      }
      if (!showHistorical) {
        deduped = deduped.filter((b) => !milestoneSet.has(b.height));
      }

      // Add pending/future placeholder one above the highest real block
      const realMaxHeight = deduped
        .filter((b) => !b.isFuture && typeof b.height === 'number' && b.height > 0)
        .reduce((max, b) => Math.max(max, b.height || 0), 0);

      if (realMaxHeight > 0) {
        const futureHeight = realMaxHeight + 1;
        const existingFuture = deduped.find((b) => b.isFuture);
        const futureTimestamp = existingFuture?.timestamp || Math.floor(Date.now() / 1000) + 600;
        deduped = deduped.filter((b) => !b.isFuture);
        deduped.push({
          height: futureHeight,
          timestamp: futureTimestamp,
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

      deduped = deduped.sort((a, b) => b.height - a.height);

      const topBlock = deduped.find((b) => !b.isFuture && typeof b.height === 'number');
      let newHeight = null;
      if (topBlock && latestHeightRef.current !== null && topBlock.height > latestHeightRef.current) {
        newHeight = topBlock.height;
      }
      if (topBlock && (latestHeightRef.current === null || topBlock.height > latestHeightRef.current)) {
        latestHeightRef.current = topBlock.height;
      }
      if (newHeight !== null) {
        deduped = deduped.map((block) =>
          block.height === newHeight ? { ...block, isNew: true } : block
        );
        if (newBlockTimeoutRef.current) {
          clearTimeout(newBlockTimeoutRef.current);
        }
        newBlockTimeoutRef.current = setTimeout(() => {
          blocksRef.current = blocksRef.current.map((block) =>
            block.height === newHeight ? { ...block, isNew: false } : block
          );
          setBlocks([...blocksRef.current]);
        }, 1200);
      }

      blocksRef.current = deduped;
      setBlocks(deduped);
      setNextCursor(data.next_cursor || null);
      setHasMore(Boolean(data.has_more));

      // Keep a user-selected block pinned even as new data loads.
      if (manualSelectedHeight.current) {
        const match = deduped.find((b) => b.height === manualSelectedHeight.current);
        if (match && selectedBlockRef.current?.height !== match.height) {
          setSelectedBlock({ ...match });
        }
      } else if (!selectedBlockRef.current && deduped.length && !isPolling) {
        setIsUserNavigating(false);
        setSelectedBlock(deduped[0]);
      }
    } catch (error) {
      console.error('Error fetching blocks:', error);
      setBlocks([]);
      if (!selectedBlockRef.current && !isPolling) {
        setSelectedBlock(null);
      }
    } finally {
      loadingRef.current = false;
    }
  }, [showHistorical]);

  const loadMoreBlocks = useCallback(() => {
    if (!hasMore || !nextCursor) return;
    if (loadingRef.current) return;
    fetchBlocks(nextCursor, true);
  }, [fetchBlocks, hasMore, nextCursor]);

  useEffect(() => {
    const fetchNetwork = async () => {
      try {
        const res = await fetch(`${API_BASE}/bitcoin/v1/health`);
        if (!res.ok) return;
        const data = await res.json();
        const network = String(data?.network || '').toLowerCase();
        if (network && network !== 'mainnet') {
          setShowHistorical(false);
        }
      } catch (error) {
        console.error('Failed to fetch network info:', error);
      }
    };
    fetchNetwork();
  }, []);

  useEffect(() => {
    let intervalId = null;
    const poll = () => fetchBlocks(null, true);
    fetchBlocks(null, false);
    const startPolling = () => {
      if (intervalId) return;
      intervalId = setInterval(poll, 120000);
    };
    const stopPolling = () => {
      if (intervalId) {
        clearInterval(intervalId);
        intervalId = null;
      }
    };
    const handleVisibility = () => {
      if (document.visibilityState === 'visible') {
        poll();
        startPolling();
      } else {
        stopPolling();
      }
    };
    handleVisibility();
    document.addEventListener('visibilitychange', handleVisibility);
    return () => {
      stopPolling();
      if (newBlockTimeoutRef.current) {
        clearTimeout(newBlockTimeoutRef.current);
        newBlockTimeoutRef.current = null;
      }
      document.removeEventListener('visibilitychange', handleVisibility);
    };
  }, [fetchBlocks]);

  useEffect(() => {
    selectedBlockRef.current = selectedBlock;
  }, [selectedBlock]);

  const handleBlockSelect = (block) => {
    // Clone to avoid React bailing when the same object reference recurs between polls.
    manualSelectedHeight.current = block.height;
    setIsUserNavigating(true);
    setSelectedBlock({ ...block });
  };

  const refreshBlocks = () => fetchBlocks(null, false);

  return {
    blocks,
    selectedBlock,
    isUserNavigating,
    handleBlockSelect,
    setSelectedBlock,
    setIsUserNavigating,
    loadMoreBlocks,
    refreshBlocks
  };
};
