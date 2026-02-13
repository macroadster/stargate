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
    <div className="min-h-screen bg-app-main text-gray-900 dark:text-gray-100">
      <AppHeader onInscribe={() => navigate('/')} />
      <div className="w-full mx-auto px-6 py-10 space-y-8">
        <div>
          <h1 className="text-4xl font-black text-gray-900 dark:text-white uppercase tracking-tight leading-none mb-2">Contracts</h1>
          <p className="text-xs text-starlight font-bold uppercase tracking-widest opacity-70">
            Newest contracts first, with infinite scroll.
          </p>
        </div>

        {error && (
          <div className="p-3 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-[10px] font-black uppercase tracking-widest text-center shadow-lg">
            {error}
          </div>
        )}

        <div className="contracts-grid">
          {displayContracts.map((contract) => (
            <button
              key={`${contract.id}-${contract.block_height}`}
              onClick={() => setSelectedInscription(contract)}
              className="group text-left"
            >
              <div className="contract-card transition-all duration-300 hover:shadow-xl hover:translate-y-[-5px]">
                <div className="relative">
                  {contract.image_url ? (
                    <img
                      src={contract.image_url}
                      alt={contract.file_name || contract.id}
                      className="contract-card-image transition-transform duration-300 group-hover:scale-[1.02]"
                      loading="lazy"
                    />
                  ) : (
                    <div className="contract-card-image flex flex-col items-center justify-center p-6 text-center bg-gray-100 dark:bg-gray-800">
                      <div className="text-5xl mb-4">🧩</div>
                      <div className="text-sm font-medium text-gray-700 dark:text-gray-300 line-clamp-3">
                        {contract.headline}
                      </div>
                    </div>
                  )}
                  <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-black/20 to-transparent opacity-90" />
                  <div className="absolute bottom-0 left-0 right-0 p-4 text-white">
                    <div className="text-[10px] font-black uppercase tracking-[0.2em] text-white/70 flex justify-between items-center">
                      <span>Block #{contract.block_height || 'Pending'}</span>
                      {contract.metadata?.status && (
                        <span className="badge badge-primary">
                          {contract.metadata.status}
                        </span>
                      )}
                    </div>
                    <div className="text-lg font-semibold leading-snug mt-1">
                      {contract.headline}
                    </div>
                  </div>
                </div>
                <div className="p-3 space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <div className="contract-id text-xs text-gray-500 dark:text-gray-400 truncate flex-1">
                      ID: {contract.id}
                    </div>
                    {contract.metadata?.visible_pixel_hash && (
                      <div className="contract-hash text-[10px] text-starlight font-mono font-bold">
                        HASH: {contract.metadata.visible_pixel_hash.slice(0, 8)}
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-3 text-xs text-gray-600 dark:text-gray-400">
                    <div className="flex items-center gap-1">
                      <span className="text-starlight">💰</span>
                      <span>{(contract.metadata?.total_budget / 1e8 || 0).toFixed(4)} BTC</span>
                    </div>
                    <div className="flex items-center gap-1">
                      <span className="text-starlight">📋</span>
                      <span>{contract.metadata?.available_tasks || 0} tasks</span>
                    </div>
                  </div>
                </div>
              </div>
            </button>
          ))}
        </div>

        <div ref={sentinelRef} className="py-10 text-center">
          {isLoading ? (
            <div className="flex flex-col items-center gap-2">
              <div className="w-6 h-6 border-2 border-starlight border-t-transparent rounded-full animate-spin"></div>
              <span className="text-[10px] font-black uppercase tracking-widest text-gray-500">Loading more contracts…</span>
            </div>
          ) : hasMore ? (
            <span className="text-[10px] font-black uppercase tracking-widest text-gray-500">Scroll for more</span>
          ) : (
            <div className="text-[10px] font-black uppercase tracking-widest text-gray-500 opacity-60">— End of contracts —</div>
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
