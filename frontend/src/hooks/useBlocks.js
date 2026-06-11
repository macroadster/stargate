import { useState, useEffect, useCallback, useRef } from 'react';
import { API_BASE } from '../apiBase';
import { apiFetch } from '../utils/api';

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
    thumbnail: block.thumbnail_url || block.thumbnailUrl || (hasImages ? '🎨' : null),
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
  const [hasNewer, setHasNewer] = useState(false);
  const [showHistorical, setShowHistorical] = useState(null);
  const [isInitializing, setIsInitializing] = useState(true);
  const loadingRef = useRef(false);
  const loadingOlderRef = useRef(false);
  const olderCursorRef = useRef(null);
  const hasMoreOlderRef = useRef(true);
  const pendingFetchAroundRef = useRef(null);
  const fetchBlocksAroundRef = useRef(null);
  const blocksRef = useRef([]);
  const latestHeightRef = useRef(null);
  const newBlockTimeoutRef = useRef(null);
  const selectedBlockRef = useRef(null);
  const manualSelectedHeight = useRef(null);
  const networkBlockHeightRef = useRef(null);
  const networkCheckedRef = useRef(false);
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

  // Maximum blocks to keep in memory at any time (sliding window).
  // ~500 blocks ≈ ~3.5 days of Bitcoin blocks — keeps memory bounded.
  const MAX_WINDOW = 500;

  const milestoneMeta = {
    0: {
      timestamp: 1231006505,
      hash: '000000000019d6689c085ae165831e934ff763ae46a2a6c172b3f1b60a8ce26f',
      tx_count: 1
    },
    174923: {
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

  // Merge new blocks into the existing list, deduplicate, sort descending,
  // and apply sliding window around `anchorHeight` (defaults to highest loaded block).
  const mergeAndTrim = (existing, incoming, anchorHeight = null) => {
    const combined = [...existing, ...incoming];
    const seen = new Set();
    let deduped = combined.filter((b) => {
      if (seen.has(b.height)) return false;
      seen.add(b.height);
      return true;
    }).sort((a, b) => b.height - a.height);

    // Sliding window: keep MAX_WINDOW blocks centered around the anchor.
    if (deduped.length > MAX_WINDOW) {
      const anchor = anchorHeight ?? deduped[0]?.height ?? 0;
      const anchorIdx = deduped.findIndex(b => b.height <= anchor);
      const idx = Math.max(0, anchorIdx);
      // Keep ~60% of the window below the anchor and ~40% above for natural scrolling bias
      const above = Math.min(idx, Math.floor(MAX_WINDOW * 0.4));
      const start = idx - above;
      deduped = deduped.slice(start, start + MAX_WINDOW);
    }

    return deduped;
  };

  // Add milestone and future blocks to the list.
  const addSpecialBlocks = (deduped, networkHeight) => {
    // Always include milestone blocks (on mainnet)
    if (showHistorical === true) {
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

    if (networkHeight && networkHeight > 0) {
      const futureHeight = networkHeight + 1;
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

    return deduped.sort((a, b) => b.height - a.height);
  };

  const fetchBlocks = useCallback(async (cursor = null, isPolling = false) => {
    if (loadingRef.current) return;
    if (showHistorical === null) {
      console.log('Waiting for network check (showHistorical is null)');
      return;
    }
    loadingRef.current = true;
    try {
      const url = new URL(`${API_BASE}/api/data/block-summaries`);
      url.searchParams.set('limit', 20);
      if (cursor) url.searchParams.set('cursor_height', cursor);

      const response = await apiFetch(url.toString());
      const data = await response.json();

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      let networkHeight = networkBlockHeightRef.current;

      if (!networkHeight || isPolling) {
        try {
          const healthRes = await apiFetch('/bitcoin/v1/health');
          if (healthRes.ok) {
            const healthData = await healthRes.json();
            networkHeight = healthData?.bitcoin?.block_height || healthData?.bitcoin?.blockHeight;
            if (networkHeight) {
              networkBlockHeightRef.current = networkHeight;
            }
          }
        } catch (healthError) {
          console.error('Failed to fetch network height:', healthError);
        }
      }

      const blocksData = data.blocks || [];
      const recentBlocks = Array.isArray(blocksData)
        ? blocksData.map(generateBlock).filter(b => b.height)
        : [];

      // When loading older blocks via cursor, anchor the window around the
      // currently selected block so we don't evict what the user is viewing.
      const anchor = manualSelectedHeight.current || selectedBlockRef.current?.height || null;
      let deduped = mergeAndTrim(blocksRef.current, recentBlocks, anchor);
      deduped = addSpecialBlocks(deduped, networkHeight);

      // Track whether there are newer blocks above our window
      if (networkHeight && deduped.length > 0) {
        const highestReal = deduped.find(b => !b.isFuture);
        setHasNewer(highestReal && highestReal.height < networkHeight);
      }

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
      setIsInitializing(false);

      // Only update the older-blocks cursor when this is the initial load
      // (no cursor yet) or an explicit cursor-based fetch. Polling (isPolling
      // with no cursor) must NOT overwrite it or the user loses their scroll
      // position into history.
      if (!isPolling || cursor) {
        setNextCursor(data.next_cursor || null);
        setHasMore(Boolean(data.has_more));
        olderCursorRef.current = data.next_cursor || null;
        hasMoreOlderRef.current = Boolean(data.has_more);
      }

      // Keep a user-selected block pinned even as new data loads.
      if (manualSelectedHeight.current) {
        const match = deduped.find((b) => b.height === manualSelectedHeight.current);
        if (match) {
          if (selectedBlockRef.current?.height !== match.height) {
            setSelectedBlock({ ...match });
          }
        } else {
          // Block not in list yet - create/update placeholder if needed
          const targetHeight = manualSelectedHeight.current;
          if (selectedBlockRef.current?.height !== targetHeight) {
            setSelectedBlock({
              height: targetHeight,
              timestamp: Date.now() / 1000,
              hash: `block-${targetHeight}`,
              tx_count: 0,
              inscriptionCount: 0,
              smart_contract_count: 0,
              witness_image_count: 0,
              hasBRC20: false,
              has_images: false,
              smart_contracts: [],
              witness_images: []
            });
          }
          // Schedule fetchBlocksAround after loadingRef is released.
          // This handles the race on initial load: the URL effect calls
          // setManualHeight → fetchBlocksAround, but fetchBlocks is still
          // running so fetchBlocksAround bails. Once fetchBlocks finishes,
          // we retry.
          pendingFetchAroundRef.current = targetHeight;
        }
      } else if (!selectedBlockRef.current && deduped.length && !isPolling) {
        setIsUserNavigating(false);
        setSelectedBlock(deduped[0]);
      }
    } catch (error) {
      console.error('Error fetching blocks:', error);
      setBlocks([]);
      setIsInitializing(false);
      if (!selectedBlockRef.current && !isPolling) {
        setSelectedBlock(null);
      }
    } finally {
      loadingRef.current = false;
      // Drain any pending fetchBlocksAround that was blocked by this fetch.
      // This handles the race on initial page load: URL effect calls
      // setManualHeight → fetchBlocksAround while fetchBlocks is still running.
      const pending = pendingFetchAroundRef.current;
      if (pending !== null) {
        pendingFetchAroundRef.current = null;
        setTimeout(() => fetchBlocksAroundRef.current?.(pending), 0);
      }
    }
  }, [showHistorical]);

  // loadMoreBlocks uses its own cursor (olderCursorRef) that is independent
  // of the 2-minute polling cycle. Polling fetches tip blocks and must never
  // reset the scroll-into-history cursor.
  const loadMoreBlocks = useCallback(async () => {
    if (!hasMoreOlderRef.current || !olderCursorRef.current) return;
    if (loadingOlderRef.current || loadingRef.current) return;
    loadingOlderRef.current = true;
    try {
      const url = new URL(`${API_BASE}/api/data/block-summaries`);
      url.searchParams.set('limit', 20);
      url.searchParams.set('cursor_height', olderCursorRef.current);

      const response = await apiFetch(url.toString());
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();

      const blocksData = data.blocks || [];
      const olderBlocks = Array.isArray(blocksData)
        ? blocksData.map(generateBlock).filter(b => b.height)
        : [];

      // Anchor around the lowest currently-loaded real block so the window
      // expands downward (into history) rather than evicting what the user sees.
      const realBlocks = blocksRef.current.filter(b => !b.isFuture);
      const lowestLoaded = realBlocks.length > 0
        ? realBlocks[realBlocks.length - 1].height
        : null;
      let deduped = mergeAndTrim(blocksRef.current, olderBlocks, lowestLoaded);
      deduped = addSpecialBlocks(deduped, networkBlockHeightRef.current);

      blocksRef.current = deduped;
      setBlocks(deduped);

      // Advance the older-blocks cursor
      olderCursorRef.current = data.next_cursor || null;
      hasMoreOlderRef.current = Boolean(data.has_more);
      setNextCursor(data.next_cursor || null);
      setHasMore(Boolean(data.has_more));
    } catch (error) {
      console.error('Error loading older blocks:', error);
    } finally {
      loadingOlderRef.current = false;
    }
  }, [showHistorical]);

  // Fetch blocks around a specific height (for URL navigation to distant blocks).
  // Replaces the current window with blocks centered on targetHeight.
  const fetchBlocksAround = useCallback(async (targetHeight) => {
    if (loadingRef.current) return;
    if (showHistorical === null) return;
    loadingRef.current = true;
    try {
      // Fetch blocks starting at targetHeight + some buffer above
      const cursorAbove = targetHeight + 11; // fetch a page starting just above target
      const url = new URL(`${API_BASE}/api/data/block-summaries`);
      url.searchParams.set('limit', 40);
      url.searchParams.set('cursor_height', cursorAbove);

      const response = await apiFetch(url.toString());
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();

      let networkHeight = networkBlockHeightRef.current;
      if (!networkHeight) {
        try {
          const healthRes = await apiFetch('/bitcoin/v1/health');
          if (healthRes.ok) {
            const healthData = await healthRes.json();
            networkHeight = healthData?.bitcoin?.block_height || healthData?.bitcoin?.blockHeight;
            if (networkHeight) networkBlockHeightRef.current = networkHeight;
          }
        } catch (e) { /* ignore */ }
      }

      const blocksData = data.blocks || [];
      const fetchedBlocks = Array.isArray(blocksData)
        ? blocksData.map(generateBlock).filter(b => b.height)
        : [];

      // Replace the window, anchored around targetHeight
      let deduped = mergeAndTrim(blocksRef.current, fetchedBlocks, targetHeight);
      deduped = addSpecialBlocks(deduped, networkHeight);

      if (networkHeight) {
        const highestReal = deduped.find(b => !b.isFuture);
        setHasNewer(highestReal && highestReal.height < networkHeight);
      }

      blocksRef.current = deduped;
      setBlocks(deduped);
      setIsInitializing(false);

      // Update the older-blocks cursor so the user can continue scrolling
      // from this new position
      olderCursorRef.current = data.next_cursor || null;
      hasMoreOlderRef.current = Boolean(data.has_more);
      setNextCursor(data.next_cursor || null);
      setHasMore(Boolean(data.has_more));

      // Update selected block with real data if available
      const match = deduped.find(b => b.height === targetHeight);
      if (match && manualSelectedHeight.current === targetHeight) {
        setSelectedBlock({ ...match });
      }
    } catch (error) {
      console.error('Error fetching blocks around height:', error);
    } finally {
      loadingRef.current = false;
    }
  }, [showHistorical]);

  // Keep ref in sync so the deferred setTimeout in fetchBlocks always
  // calls the latest version.
  fetchBlocksAroundRef.current = fetchBlocksAround;

  // Load newer blocks when scrolling left past the current window.
  const loadNewerBlocks = useCallback(() => {
    if (loadingRef.current) return;
    const realBlocks = blocksRef.current.filter(b => !b.isFuture);
    if (realBlocks.length === 0) return;
    const highestLoaded = realBlocks[0].height;
    const networkHeight = networkBlockHeightRef.current;
    if (networkHeight && highestLoaded >= networkHeight) {
      setHasNewer(false);
      return;
    }
    // Fetch blocks starting above our current highest
    fetchBlocksAround(highestLoaded + 20);
  }, [fetchBlocksAround]);

  useEffect(() => {
    const fetchNetwork = async () => {
      try {
        const res = await apiFetch('/bitcoin/v1/health');
        if (!res.ok) return;
        const data = await res.json();
        const network = String(data?.network || '').toLowerCase();
        console.log('Network detected:', network);
        const isMainnet = network === 'mainnet';
        setShowHistorical(isMainnet);
        networkCheckedRef.current = true;
        fetchBlocks(null, false);
      } catch (error) {
        console.error('Failed to fetch network info:', error);
        setShowHistorical(false);
        networkCheckedRef.current = true;
        fetchBlocks(null, false);
      }
    };
    fetchNetwork();
  }, [fetchBlocks]);

  useEffect(() => {
    let intervalId = null;
    const poll = () => {
      if (!networkCheckedRef.current) {
        console.log('Skipping poll: network check not complete');
        return;
      }
      fetchBlocks(null, true);
    };
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
  }, [fetchBlocks, showHistorical]);

  useEffect(() => {
    selectedBlockRef.current = selectedBlock;
  }, [selectedBlock]);

  const handleBlockSelect = (block) => {
    // Clone to avoid React bailing when the same object reference recurs between polls.
    manualSelectedHeight.current = block.height;
    setIsUserNavigating(true);
    setSelectedBlock({ ...block });
  };

  const setManualHeight = (height) => {
    manualSelectedHeight.current = height;
    setIsUserNavigating(true);
    // Check if block exists in current list, otherwise create placeholder
    const existingBlock = blocksRef.current.find(b => b.height === height);
    const networkHeight = networkBlockHeightRef.current;
    const isFutureBlock = !existingBlock?.isFuture && networkHeight && height > networkHeight;
    if (existingBlock) {
      setSelectedBlock({ ...existingBlock });
    } else {
      // Create placeholder block immediately so UI is responsive
      // For future blocks, set isFuture=true and has_images=false to skip fetching and show OpenContractsView
      // For non-future blocks, leave has_images undefined so useInscriptions will fetch the data
      setSelectedBlock({
        height: height,
        timestamp: Date.now() / 1000,
        hash: `block-${height}`,
        tx_count: 0,
        inscriptionCount: 0,
        smart_contract_count: 0,
        witness_image_count: 0,
        hasBRC20: false,
        has_images: isFutureBlock ? false : undefined,
        smart_contracts: [],
        witness_images: [],
        isFuture: isFutureBlock
      });
      // For non-future blocks, fetch real data around the target height
      // so the scroller shows surrounding blocks and we get real block metadata
      if (!isFutureBlock) {
        fetchBlocksAround(height);
      }
    }
  };

  const refreshBlocks = () => fetchBlocks(null, false);

  return {
    blocks,
    selectedBlock,
    isUserNavigating,
    isInitializing,
    hasNewer,
    handleBlockSelect,
    setSelectedBlock,
    setIsUserNavigating,
    setManualHeight,
    loadMoreBlocks,
    loadNewerBlocks,
    refreshBlocks
  };
};
