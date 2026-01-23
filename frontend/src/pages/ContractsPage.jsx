import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { API_BASE, CONTENT_BASE } from '../apiBase';
import { useNavigate } from 'react-router-dom';
import InscriptionModal from '../components/Inscription/InscriptionModal';
import AppHeader from '../components/Common/AppHeader';

const extractHeadline = (text) => {
  if (!text) return '';
  const line = text
    .split('\n')
    .map((l) => l.trim())
    .find((l) => l.length > 0);
  if (!line) return '';
  return line.replace(/^#+\s*/, '').slice(0, 140);
};

const mapContractItem = (item, height) => {
  const rawUrl = item.image_url || '';
  const imageUrl = rawUrl.startsWith('http') ? rawUrl : (rawUrl ? `${CONTENT_BASE}${rawUrl}` : '');
  return {
    id: item.tx_id || item.id,
    mime_type: item.content_type || 'application/octet-stream',
    image_url: imageUrl,
    file_name: item.file_name,
    size_bytes: item.size_bytes,
    text: item.content || '',
    metadata: item.metadata || {},
    genesis_block_height: height,
    block_height: height,
    contract_type: 'Smart Contract'
  };
};

export default function ContractsPage() {
  const navigate = useNavigate();
  const [contracts, setContracts] = useState([]);
  const [cursor, setCursor] = useState('');
  const [hasMore, setHasMore] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');
  const [selectedInscription, setSelectedInscription] = useState(null);
  const sentinelRef = useRef(null);
  const seenRef = useRef(new Set());

  const loadMore = useCallback(async () => {
    if (isLoading || !hasMore) return;
    setIsLoading(true);
    setError('');
    try {
      const url = new URL(`${API_BASE}/api/data/block-summaries`);
      url.searchParams.set('limit', 12);
      if (cursor) url.searchParams.set('cursor_height', cursor);
      const res = await fetch(url.toString());
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      const blocks = Array.isArray(data.blocks) ? data.blocks : [];
      const nextCursor = data.next_cursor || '';
      const more = Boolean(data.has_more);

      const blockFetches = blocks.map(async (block) => {
        const height = Number(block.block_height || block.height);
        if (!height || block.smart_contract_count <= 0) {
          return [];
        }
        const blockRes = await fetch(`${API_BASE}/api/data/block-inscriptions/${height}?limit=50&fields=summary`);
        if (!blockRes.ok) return [];
        const blockData = await blockRes.json();
        const items = Array.isArray(blockData.inscriptions) ? blockData.inscriptions : [];
        return items.map((item) => mapContractItem(item, height));
      });

      const results = await Promise.all(blockFetches);
      const flattened = results.flat();
      const unique = [];
      flattened.forEach((item) => {
        const key = `${item.id}-${item.metadata?.input_index ?? ''}`;
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

  useEffect(() => {
    loadMore();
  }, [loadMore]);

  useEffect(() => {
    if (!sentinelRef.current || !hasMore) return;
    const sentinel = sentinelRef.current;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          loadMore();
        }
      },
      { threshold: 0.6 }
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [loadMore, hasMore]);

  const displayContracts = useMemo(() => {
    return contracts.map((contract) => {
      const rawText = contract.text || contract.metadata?.embedded_message || contract.metadata?.extracted_message || '';
      return {
        ...contract,
        headline: extractHeadline(rawText) || contract.file_name || 'Untitled Contract'
      };
    });
  }, [contracts]);

  return (
    <div className="min-h-screen bg-gradient-to-b from-slate-50 via-white to-slate-100 dark:from-gray-950 dark:via-gray-900 dark:to-gray-950 text-gray-900 dark:text-gray-100">
      <AppHeader onInscribe={() => navigate('/')} />
      <div className="container mx-auto px-4 sm:px-6 py-8 sm:py-10">
        <div className="mb-8">
          <h1 className="text-3xl font-bold">Contracts</h1>
          <p className="text-gray-600 dark:text-gray-400">Newest contracts first, with infinite scroll.</p>
        </div>

        {error && (
          <div className="mb-6 rounded-lg border border-red-200 bg-red-50 text-red-800 px-4 py-3 text-sm">
            {error}
          </div>
        )}

        <div className="grid gap-4 sm:gap-6 sm:grid-cols-2 xl:grid-cols-3">
          {displayContracts.map((contract) => (
            <button
              key={`${contract.id}-${contract.block_height}`}
              onClick={() => setSelectedInscription(contract)}
              className="group text-left"
            >
              <div className="relative overflow-hidden rounded-2xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 shadow-sm">
                <div className={`relative ${contract.image_url ? 'aspect-[3/4] min-h-[200px]' : 'min-h-[320px]'} bg-gray-100 dark:bg-gray-800`}>
                  {contract.image_url ? (
                    <img
                      src={contract.image_url}
                      alt={contract.file_name || contract.id}
                      className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.02]"
                      loading="lazy"
                    />
                  ) : (
                    <div className="h-full w-full flex flex-col items-center justify-center p-6 text-center">
                      <div className="text-5xl mb-4">🧩</div>
                      <div className="text-sm font-medium text-gray-700 dark:text-gray-300 line-clamp-3">
                        {contract.headline}
                      </div>
                    </div>
                  )}
                  <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-black/10 to-transparent opacity-90" />
                  <div className="absolute bottom-0 left-0 right-0 p-4 text-white">
                    <div className="text-xs uppercase tracking-wide text-white/70">
                      Block #{contract.block_height}
                    </div>
                    <div className="text-lg font-semibold leading-snug">
                      {contract.headline}
                    </div>
                  </div>
                </div>
                <div className="p-4">
                  <div className="text-xs text-gray-500 dark:text-gray-400 font-mono break-all">
                    {contract.id.slice(0, 10)}...{contract.id.slice(-6)}
                  </div>
                  <div className="mt-2 text-sm text-gray-700 dark:text-gray-300">
                    {contract.headline}
                  </div>
                </div>
              </div>
            </button>
          ))}
        </div>

        <div ref={sentinelRef} className="py-6 text-center text-sm text-gray-500 dark:text-gray-400">
          {isLoading ? 'Loading more contracts…' : hasMore ? 'Scroll for more' : 'End of contracts'}
        </div>
      </div>

      {selectedInscription && (
        <InscriptionModal
          inscription={selectedInscription}
          onClose={() => setSelectedInscription(null)}
        />
      )}
    </div>
  );
}
