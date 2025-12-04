import { useState, useEffect, useCallback, useRef } from 'react';
import { API_BASE } from '../apiBase';

const generateBlock = (block) => {
  const now = Date.now();

  // Handle both data API and mempool API formats
  const inscriptionCount = block.inscription_count ?? (block.inscriptions ? block.inscriptions.length : (block.smart_contracts || 0));
  const hasImages = block.has_images ?? (inscriptionCount > 0);
  const stegoCount = block.steganography_summary?.stego_count || 0;
  const txCount = block.tx_count ?? block.total_transactions ?? block.TotalTransactions ?? 0;

  // For UI purposes, treat all inscriptions as "smart contracts" to show the badge
  const displayContractCount = Math.max(inscriptionCount, stegoCount);

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
    smart_contracts: block.inscriptions || [],
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
  const loadingRef = useRef(false);
  const blocksRef = useRef([]);
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

      const combined = [...blocksRef.current, ...recentBlocks];
      const seenFinal = new Set();
      let deduped = combined.filter((b) => {
        if (seenFinal.has(b.height)) return false;
        seenFinal.add(b.height);
        return true;
      }).sort((a, b) => b.height - a.height);

      // Always include milestone blocks
      pinnedMilestones.current.forEach((height) => {
        if (!deduped.some((b) => b.height === height)) {
          deduped.push({
            height,
            timestamp: height === 0 ? 1231006505 : Date.now() - 600000 * 10,
            hash: height === 0 ? '000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f' : `milestone-${height}`,
            inscriptionCount: 0,
            smart_contract_count: 0,
            witness_image_count: 0,
            hasBRC20: false,
            thumbnail: null,
            tx_count: 0,
            smart_contracts: [],
            witness_images: [],
            isGenesis: height === 0,
            has_images: false
          });
        }
      });

      // Add pending/future placeholder one above the highest real block
      const realMaxHeight = deduped
        .filter((b) => !b.isFuture && typeof b.height === 'number' && b.height > 0)
        .reduce((max, b) => Math.max(max, b.height || 0), 0);

      if (realMaxHeight > 0) {
        const futureHeight = realMaxHeight + 1;
        deduped = deduped.filter((b) => !b.isFuture);
        deduped.push({
          height: futureHeight,
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

      deduped = deduped.sort((a, b) => b.height - a.height);

      blocksRef.current = deduped;
      setBlocks(deduped);
      setNextCursor(data.next_cursor || null);
      setHasMore(Boolean(data.has_more));

      // Keep a user-selected block pinned even as new data loads.
      if (manualSelectedHeight.current) {
        const match = deduped.find((b) => b.height === manualSelectedHeight.current);
        if (match) {
          setSelectedBlock({ ...match });
        }
      } else if (!selectedBlock && deduped.length && !isPolling) {
        setIsUserNavigating(false);
        setSelectedBlock(deduped[0]);
      }
    } catch (error) {
      console.error('Error fetching blocks:', error);
      setBlocks([]);
      if (!selectedBlock && !isPolling) {
        setSelectedBlock(null);
      }
    } finally {
      loadingRef.current = false;
    }
  }, [selectedBlock]);

  const loadMoreBlocks = useCallback(() => {
    if (!hasMore || !nextCursor) return;
    if (loadingRef.current) return;
    fetchBlocks(nextCursor, true);
  }, [fetchBlocks, hasMore, nextCursor]);

  useEffect(() => {
    fetchBlocks(null, false);
    const interval = setInterval(() => {
      fetchBlocks(null, true);
    }, 30000);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleBlockSelect = (block) => {
    // Clone to avoid React bailing when the same object reference recurs between polls.
    manualSelectedHeight.current = block.height;
    setIsUserNavigating(true);
    setSelectedBlock({ ...block });
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
