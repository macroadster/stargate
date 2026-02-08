import { useState, useEffect, useCallback, useRef } from 'react';
import { API_BASE, CONTENT_BASE } from '../apiBase';

const mapContractToDisplayFormat = (contract) => {
  const rawUrl = contract.stego_image_url || '';
  const imageUrl = rawUrl.startsWith('http') ? rawUrl : (rawUrl ? `${CONTENT_BASE}${rawUrl}` : '');
  
  return {
    id: contract.contract_id,
    tx_id: contract.contract_id,
    mime_type: 'application/json',
    image_url: imageUrl,
    file_name: contract.title || 'Untitled Contract',
    size_bytes: 0,
    text: contract.title || '',
    metadata: {
      embedded_message: contract.title,
      extracted_message: contract.title,
      status: contract.status,
      skills: contract.skills || [],
      total_budget_sats: contract.total_budget_sats,
      goals_count: contract.goals_count,
      available_tasks_count: contract.available_tasks_count
    },
    genesis_block_height: contract.confirmed_block_height || 0,
    block_height: contract.confirmed_block_height || 0,
    contract_type: 'Smart Contract',
    confirmed_at: contract.confirmed_at,
    headline: contract.title || 'Untitled Contract'
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
      
      const contractsData = Array.isArray(data.contracts) ? data.contracts : [];
      const nextCursor = data.next_cursor_date || '';
      const more = Boolean(data.has_more) && contractsData.length > 0;

      const mappedContracts = contractsData.map(mapContractToDisplayFormat);
      
      const unique = [];
      mappedContracts.forEach((item) => {
        const key = item.id;
        if (!seenRef.current.has(key)) {
          seenRef.current.add(key);
          unique.push(item);
        }
      });

      setContracts((prev) => [...prev, ...unique]);
      setCursor(nextCursor);
      setHasMore(more);
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
