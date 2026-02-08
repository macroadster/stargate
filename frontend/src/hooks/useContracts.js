import { useState, useEffect, useCallback, useRef } from 'react';
import { API_BASE, CONTENT_BASE } from '../apiBase';

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
    tx_id: id,
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
      total_budget_sats: contract.total_budget_sats || (contract.price ? contract.price * 1e8 : 0),
      goals_count: contract.goals_count,
      available_tasks_count: contract.available_tasks_count,
      visible_pixel_hash: contract.visiblePixelHash
    },
    genesis_block_height: contract.confirmed_block_height || contract.blockHeight || 0,
    block_height: contract.confirmed_block_height || contract.blockHeight || 0,
    contract_type: 'Smart Contract',
    confirmed_at: contract.confirmed_at || contract.timestamp,
    headline: title,
    visible_pixel_hash: contract.visiblePixelHash
  };
};

export const useContracts = () => {
  const [contracts, setContracts] = useState([]);
  const [cursor, setCursor] = useState('');
  const [hasMore, setHasMore] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');
  const seenRef = useRef(new Set());

  const loadMore = useCallback(async () => {
    if (isLoading || !hasMore) return;
    setIsLoading(true);
    setError('');
    try {
      const url = new URL(`${API_BASE}/api/data/contracts-with-pagination`);
      url.searchParams.set('limit', 12);
      url.searchParams.set('status', 'confirmed');
      if (cursor) {
        url.searchParams.set('cursor_date', cursor);
        url.searchParams.set('cursor_type', 'before');
      }
      const res = await fetch(url.toString());
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      
      // Handle both wrapped and unwrapped responses
      const payload = data?.data ?? data;
      const contractsData = Array.isArray(payload.contracts) ? payload.contracts : [];
      const nextCursor = payload.next_cursor_date || '';
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

      setContracts((prev) => [...prev, ...unique]);
      setCursor(nextCursor);
      
      // If we didn't find any new unique contracts but the API says there are more,
      // it might be a pagination issue. Stop to prevent infinite loop "storm".
      if (unique.length === 0 && contractsData.length > 0 && more) {
        console.warn('Pagination returned no new unique items, stopping to prevent storm');
        setHasMore(false);
      } else {
        setHasMore(more);
      }
    } catch (err) {
      console.error('Failed to load contracts', err);
      setError('Unable to load contracts. Please retry.');
    } finally {
      setIsLoading(false);
    }
  }, [cursor, hasMore, isLoading]);

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
