import React, { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import InscriptionCard from '../Inscription/InscriptionCard';
import { API_BASE } from '../../apiBase';
import { apiFetch } from '../../utils/api';

const PAGE_LIMIT = 20;

const isOpenStatus = (status) => {
  const s = (status || '').toLowerCase();
  return !['superseded', 'completed', 'complete', 'confirmed', 'rejected'].includes(s);
};

const OpenContractsView = ({ setSelectedInscription, refreshKey }) => {
  const [pendingTxs, setPendingTxs] = useState([]);
  const [isLoading, setIsLoading] = useState(false);
  const [hasMore, setHasMore] = useState(true);
  const [cursor, setCursor] = useState('');
  const loadingRef = useRef(false);
  const seenRef = useRef(new Set());
  const cursorRef = useRef('');
  const hasMoreRef = useRef(true);
  const sentinelRef = useRef(null);

  // Keep refs in sync so loadMore doesn't need cursor/hasMore in deps (avoids observer churn).
  useEffect(() => {
    cursorRef.current = cursor;
  }, [cursor]);
  useEffect(() => {
    hasMoreRef.current = hasMore;
  }, [hasMore]);

  const loadMore = useCallback(async ({ reset = false } = {}) => {
    if (loadingRef.current) return;
    if (!reset && !hasMoreRef.current) return;

    loadingRef.current = true;
    setIsLoading(true);

    if (reset) {
      seenRef.current.clear();
      cursorRef.current = '';
      hasMoreRef.current = true;
      setCursor('');
      setHasMore(true);
      setPendingTxs([]);
    }

    try {
      const url = new URL(`${API_BASE}/api/open-contracts`);
      url.searchParams.set('open', 'true');
      url.searchParams.set('limit', String(PAGE_LIMIT));
      const pageCursor = reset ? '' : cursorRef.current;
      if (pageCursor) {
        url.searchParams.set('cursor_date', pageCursor);
        url.searchParams.set('cursor_type', 'before');
      }

      const response = await apiFetch(url.toString());
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();
      const payload = data?.data ?? data;
      const raw = payload?.transactions ?? payload?.contracts ?? payload;
      const normalized = Array.isArray(raw) ? raw : [];

      // Safety filter (server should have already filtered open statuses)
      const filtered = normalized.filter((contract) => isOpenStatus(contract.status));

      const unique = [];
      filtered.forEach((item) => {
        const key = item.id || item.contract_id;
        if (key && !seenRef.current.has(key)) {
          seenRef.current.add(key);
          unique.push(item);
        }
      });

      const nextCursor = payload?.next_cursor_date || '';
      const more = Boolean(payload?.has_more) && filtered.length > 0;

      setPendingTxs((prev) => (reset ? unique : [...prev, ...unique]));
      setCursor(nextCursor);
      cursorRef.current = nextCursor;

      // Stop if API claims more but returned only duplicates (prevents scroll storm).
      if (unique.length === 0 && filtered.length > 0 && more) {
        console.warn('Open contracts pagination returned no new unique items; stopping');
        setHasMore(false);
        hasMoreRef.current = false;
      } else {
        setHasMore(more);
        hasMoreRef.current = more;
      }
    } catch (error) {
      console.error('Error fetching open contracts:', error);
      if (reset) {
        setPendingTxs([]);
        setHasMore(false);
        hasMoreRef.current = false;
      }
    } finally {
      loadingRef.current = false;
      setIsLoading(false);
    }
  }, []);

  // Initial load + explicit refresh (e.g. after inscribe)
  useEffect(() => {
    loadMore({ reset: true });
  }, [loadMore, refreshKey]);

  // Soft poll: only refresh first page when user has not scrolled deeper
  useEffect(() => {
    const intervalId = setInterval(() => {
      if (cursorRef.current) return; // keep infinite-scroll position stable
      loadMore({ reset: true });
    }, 15000);
    return () => clearInterval(intervalId);
  }, [loadMore]);

  // Infinite scroll sentinel
  useEffect(() => {
    if (!sentinelRef.current || !hasMore) return;
    const sentinel = sentinelRef.current;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !loadingRef.current && hasMoreRef.current) {
          loadMore();
        }
      },
      { threshold: 0.1, rootMargin: '200px' }
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [loadMore, hasMore, pendingTxs.length]);

  const mappedInscriptions = useMemo(() => {
    const list = Array.isArray(pendingTxs) ? pendingTxs : [];
    return list
      .filter((tx) => isOpenStatus(tx.status))
      .map((tx) => {
        const imagePath = tx.imageData || tx.image_url || tx.stego_image_url || '';
        let uploadFile = 'pending.txt';
        if (imagePath) {
          const urlParts = imagePath.split('/');
          const lastPart = urlParts[urlParts.length - 1];
          if (lastPart) {
            uploadFile = lastPart.split('?')[0];
          }
        }
        let imageUrl = null;
        if (imagePath.startsWith('http')) {
          imageUrl = imagePath;
        } else if (imagePath.startsWith('/uploads/')) {
          imageUrl = `${API_BASE}${imagePath}`;
        } else if (imagePath.startsWith('/api/block-image/')) {
          imageUrl = `${API_BASE}${imagePath}`;
        } else if (uploadFile && uploadFile !== 'pending.txt') {
          imageUrl = `${API_BASE}/uploads/${encodeURIComponent(uploadFile)}`;
        }
        const wishText = tx.wish_text || tx.embedded_message || tx.message || tx.text || '';
        const id = tx.id || tx.contract_id || '';
        return {
          id,
          contract_type: 'Pending Contract',
          capability: 'Data Storage',
          protocol: 'BRC-20',
          apiEndpoints: 0,
          interactions: 0,
          reputation: 'Pending',
          isActive: false,
          number: parseInt(String(id).split('_')[1], 10) || 0,
          address: tx.address || 'bc1q...pending',
          genesis_block_height: tx.blockHeight || tx.block_height || 0,
          mime_type: imageUrl ? 'image/png' : 'text/plain',
          text: wishText || tx.text,
          price: tx.price,
          timestamp: tx.timestamp,
          status: tx.status,
          image_url: imageUrl,
          file_name: uploadFile,
          size_bytes: tx.text ? tx.text.length : 0,
          metadata: {
            is_stego: !!(tx.visiblePixelHash || tx.visible_pixel_hash),
            confidence: 0,
            stego_probability: 0,
            transaction_id: id,
            wish_text: wishText,
            visible_pixel_hash: tx.visiblePixelHash || tx.visible_pixel_hash,
            total_budget: tx.totalBudgetSats || tx.total_budget_sats || (tx.price ? tx.price * 1e8 : 0),
            available_tasks: tx.availableTasks || tx.available_tasks_count || 0
          }
        };
      });
  }, [pendingTxs]);

  return (
    <div className="mb-4">
      <div className="mb-6">
        <h2 className="page-subtitle text-2xl font-bold pb-2 inline-block">
          Open Contracts
        </h2>
      </div>

      {Array.isArray(mappedInscriptions) && mappedInscriptions.length > 0 ? (
        <div className="contracts-grid">
          {mappedInscriptions.map((inscription) => (
            <InscriptionCard
              key={inscription.id}
              inscription={inscription}
              onClick={setSelectedInscription}
            />
          ))}
        </div>
      ) : (
        !isLoading && (
          <div className="text-center py-8 text-gray-500 dark:text-gray-400">
            No pending transactions
          </div>
        )
      )}

      <div ref={sentinelRef} className="py-8 text-center">
        {isLoading ? (
          <div className="flex flex-col items-center gap-2">
            <div className="w-6 h-6 border-2 border-starlight border-t-transparent rounded-full animate-spin" />
            <span className="text-[10px] font-black uppercase tracking-widest text-gray-500">
              Loading open contracts…
            </span>
          </div>
        ) : hasMore && mappedInscriptions.length > 0 ? (
          <span className="text-[10px] font-black uppercase tracking-widest text-gray-500">
            Scroll for more
          </span>
        ) : mappedInscriptions.length > 0 ? (
          <div className="text-[10px] font-black uppercase tracking-widest text-gray-500 opacity-60">
            — End of open contracts —
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default OpenContractsView;
