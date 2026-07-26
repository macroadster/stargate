import { useState, useCallback, useRef } from 'react';
import { API_BASE, CONTENT_BASE } from '../apiBase';
import { apiFetch } from '../utils/api';

const mapContractToDisplayFormat = (contract) => {
  const rawUrl = contract.stego_image_url || contract.imageData || '';
  const imageUrl = rawUrl.startsWith('http') ? rawUrl : (rawUrl ? `${CONTENT_BASE}${rawUrl}` : '');
  
  const id = contract.id || contract.contract_id || '';
  const title = contract.title || contract.text || 'Untitled Contract';

  // Extract actual filename from URL for display
  let fileName = title;
  if (rawUrl) {
    const urlParts = rawUrl.split('/');
    const lastPart = urlParts[urlParts.length - 1];
    if (lastPart && lastPart.length > 0) {
      fileName = lastPart.split('?')[0];
    }
  }

  return {
    id: id,
    contract_id: contract.contract_id || contract.id || '',
    tx_id: contract.tx_id || contract.metadata?.transaction_id || id || '',
    mime_type: imageUrl ? 'image/png' : 'application/json',
    image_url: imageUrl,
    file_name: fileName,
    size_bytes: 0,
    text: title,
    metadata: {
      embedded_message: title,
      extracted_message: title,
      status: contract.status,
      skills: contract.skills || [],
      total_budget: contract.totalBudgetSats || contract.total_budget_sats || (contract.price ? contract.price * 1e8 : 0),
      available_tasks: contract.availableTasks || contract.available_tasks_count || 0,
      goals_count: contract.goals_count,
      visible_pixel_hash: contract.visiblePixelHash || contract.visible_pixel_hash
    },
    genesis_block_height: contract.confirmed_block_height || contract.blockHeight || 0,
    block_height: contract.confirmed_block_height || contract.blockHeight || 0,
    contract_type: 'Smart Contract',
    confirmed_at: contract.confirmed_at || contract.timestamp,
    // Preserve unix seconds so cursor fallback can rebuild next_cursor_date.
    timestamp: contract.timestamp || 0,
    headline: title,
    visible_pixel_hash: contract.visiblePixelHash
  };
};

/** Build cursor_date from a mapped item when the API omits next_cursor_date. */
const cursorFromItem = (item) => {
  if (!item) return '';
  if (item.confirmed_at && typeof item.confirmed_at === 'string' && item.confirmed_at.includes('T')) {
    return item.confirmed_at;
  }
  const n = Number(item.timestamp || item.confirmed_at);
  if (Number.isFinite(n) && n > 0) {
    const ms = n < 1e12 ? n * 1000 : n;
    return new Date(ms).toISOString();
  }
  return '';
};

export const useContracts = () => {
  const [contracts, setContracts] = useState([]);
  const [cursor, setCursor] = useState('');
  const [hasMore, setHasMore] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');
  const seenRef = useRef(new Set());
  const loadingRef = useRef(false);

  const loadMore = useCallback(async () => {
    if (loadingRef.current || !hasMore) return;
    loadingRef.current = true;
    setIsLoading(true);
    setError('');
    try {
      // Primary UI contract list path (legacy /api/data/contracts-with-pagination still aliases).
      const url = new URL(`${API_BASE}/api/open-contracts`);
      url.searchParams.set('limit', 12);
      url.searchParams.set('status', 'confirmed');
      if (cursor) {
        url.searchParams.set('cursor_date', cursor);
        url.searchParams.set('cursor_type', 'before');
      }
      const res = await apiFetch(url.toString());
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      
      // Handle both wrapped and unwrapped responses
      const payload = data?.data ?? data;
      const contractsData = Array.isArray(payload.contracts) ? payload.contracts : [];
      const more = Boolean(payload.has_more) && contractsData.length > 0;

      const mappedContracts = contractsData.map(mapContractToDisplayFormat);
      
      const unique = [];
      mappedContracts.forEach((item) => {
        const key = item.id;
        if (key && !seenRef.current.has(key)) {
          seenRef.current.add(key);
          unique.push(item);
        }
      });

      // Prefer API cursor; fall back to last item timestamp so has_more without
      // next_cursor_date cannot strand infinite scroll on the first page.
      let nextCursor = payload.next_cursor_date || '';
      if (!nextCursor && unique.length > 0) {
        nextCursor = cursorFromItem(unique[unique.length - 1]);
      }
      if (!nextCursor && mappedContracts.length > 0) {
        nextCursor = cursorFromItem(mappedContracts[mappedContracts.length - 1]);
      }

      setContracts((prev) => [...prev, ...unique]);
      if (nextCursor) {
        setCursor(nextCursor);
      }
      
      // If we didn't find any new unique contracts but the API says there are more,
      // it might be a pagination issue. Stop to prevent infinite loop "storm".
      if (unique.length === 0 && contractsData.length > 0 && more) {
        console.warn('Pagination returned no new unique items, stopping to prevent storm');
        setHasMore(false);
      } else if (more && !nextCursor) {
        // Cannot page without a cursor; stop rather than re-fetch page 1 forever.
        console.warn('has_more without cursor; stopping contracts pagination');
        setHasMore(false);
      } else {
        setHasMore(more);
      }
    } catch (err) {
      console.error('Failed to load contracts', err);
      setError('Unable to load contracts. Please retry.');
    } finally {
      loadingRef.current = false;
      setIsLoading(false);
    }
  }, [cursor, hasMore]);

  const refresh = useCallback(() => {
    seenRef.current.clear();
    setContracts([]);
    setCursor('');
    setHasMore(true);
    setError('');
    loadMore();
  }, [loadMore]);

  return {
    contracts,
    isLoading,
    error,
    hasMore,
    loadMore,
    refresh
  };
};
