import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import InscriptionModal from '../components/Inscription/InscriptionModal';
import AppHeader from '../components/Common/AppHeader';
import { useContracts } from '../hooks/useContracts';

const extractHeadline = (text) => {
  if (!text) return '';
  const line = text
    .split('\n')
    .map((l) => l.trim())
    .find((l) => l.length > 0);
  if (!line) return '';
  return line.replace(/^#+\s*/, '').slice(0, 140);
};

export default function ContractsPage() {
  const navigate = useNavigate();
  const { contracts, isLoading, hasMore, error, loadMore } = useContracts();
  const [selectedInscription, setSelectedInscription] = useState(null);
  const sentinelRef = useRef(null);

  // Initial load
  useEffect(() => {
    loadMore();
  }, []);

  useEffect(() => {
    if (!sentinelRef.current || !hasMore || isLoading) return;
    const sentinel = sentinelRef.current;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !isLoading && hasMore) {
          loadMore();
        }
      },
      { threshold: 0.1, rootMargin: '100px' }
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [loadMore, hasMore, isLoading]);

  const displayContracts = useMemo(() => {
    return contracts.map((contract) => {
      return {
        ...contract,
        headline: extractHeadline(contract.text) || contract.file_name || 'Untitled Contract'
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
              <div className="relative overflow-hidden rounded-2xl border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900 shadow-sm transition-all duration-300 hover:shadow-md">
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
                  <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-black/20 to-transparent opacity-90" />
                  <div className="absolute bottom-0 left-0 right-0 p-4 text-white">
                    <div className="text-xs uppercase tracking-wide text-white/70 flex justify-between items-center">
                      <span>Block #{contract.block_height || 'Pending'}</span>
                      {contract.metadata?.status && (
                        <span className="px-2 py-0.5 rounded-full bg-white/20 backdrop-blur-sm text-[10px]">
                          {contract.metadata.status}
                        </span>
                      )}
                    </div>
                    <div className="text-lg font-semibold leading-snug mt-1">
                      {contract.headline}
                    </div>
                  </div>
                </div>
                <div className="p-4 space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-[10px] text-gray-500 dark:text-gray-400 font-mono break-all truncate flex-1">
                      ID: {contract.id}
                    </div>
                    {contract.metadata?.visible_pixel_hash && (
                      <div className="text-[10px] text-indigo-500 dark:text-indigo-400 font-mono font-bold">
                        HASH: {contract.metadata.visible_pixel_hash.slice(0, 8)}
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-3 text-xs text-gray-600 dark:text-gray-400">
                    <div className="flex items-center gap-1">
                      <span>💰</span>
                      <span>{(contract.metadata?.total_budget / 1e8 || 0).toFixed(4)} BTC</span>
                    </div>
                    <div className="flex items-center gap-1">
                      <span>📋</span>
                      <span>{contract.metadata?.available_tasks || 0} tasks</span>
                    </div>
                  </div>
                </div>
              </div>
            </button>
          ))}
        </div>

        <div ref={sentinelRef} className="py-10 text-center text-sm text-gray-500 dark:text-gray-400">
          {isLoading ? (
            <div className="flex flex-col items-center gap-2">
              <div className="w-6 h-6 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin"></div>
              <span>Loading more contracts…</span>
            </div>
          ) : hasMore ? (
            'Scroll for more'
          ) : (
            <div className="opacity-60">— End of contracts —</div>
          )}
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
