import React, { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import InscriptionCard from '../Inscription/InscriptionCard';
import { API_BASE } from '../../apiBase';
import { apiFetch } from '../../utils/api';

const PAGE_LIMIT = 20;
const POLL_MS = 15000;
/** Soft poll only when user is near the top of the scroll container. */
const TOP_SCROLL_PX = 48;
/** Prefetch when sentinel is within this many px of the scroller bottom. */
const LOAD_MORE_MARGIN_PX = 200;

const isOpenStatus = (status) => {
  const s = (status || '').toLowerCase();
  return !['superseded', 'completed', 'complete', 'confirmed', 'rejected'].includes(s);
};

const itemKey = (item) => item?.id || item?.contract_id || '';

/** Find nearest vertically scrollable ancestor (App uses overflow:auto, not window). */
const getScrollParent = (node) => {
  let el = node?.parentElement;
  while (el && el !== document.documentElement) {
    const style = window.getComputedStyle(el);
    const oy = style.overflowY;
    const canScrollY = oy === 'auto' || oy === 'scroll' || oy === 'overlay';
    // Prefer real scroll containers (scrollHeight can exceed clientHeight later).
    if (canScrollY) {
      return el;
    }
    el = el.parentElement;
  }
  return null;
};

/**
 * True when the sentinel is in/near the visible area of `root` (or the viewport).
 * Used after loads because IntersectionObserver does not re-fire if the target
 * stayed intersecting the whole time loadingRef was true.
 */
const isNearViewport = (sentinel, root, margin = LOAD_MORE_MARGIN_PX) => {
  if (!sentinel) return false;
  const s = sentinel.getBoundingClientRect();
  if (root) {
    const r = root.getBoundingClientRect();
    return s.top < r.bottom + margin && s.bottom > r.top - margin;
  }
  return s.top < window.innerHeight + margin && s.bottom > -margin;
};

/**
 * Prepend brand-new first-page items and refresh fields for ids already shown.
 * Never removes or reorders existing cards (avoids scroll jumps).
 */
const softMergeFirstPage = (prev, firstPage) => {
  const firstById = new Map();
  firstPage.forEach((item) => {
    const k = itemKey(item);
    if (k) firstById.set(k, item);
  });
  const prevIds = new Set(prev.map(itemKey).filter(Boolean));
  const brandNew = firstPage.filter((item) => {
    const k = itemKey(item);
    return k && !prevIds.has(k);
  });
  const updated = prev.map((item) => {
    const k = itemKey(item);
    return (k && firstById.get(k)) || item;
  });
  return brandNew.length ? [...brandNew, ...updated] : updated;
};

const cursorFromItem = (item) => {
  if (!item) return '';
  if (item.timestamp) {
    const n = Number(item.timestamp);
    if (Number.isFinite(n) && n > 0) {
      // API timestamps are unix seconds for inscription-shaped rows.
      const ms = n < 1e12 ? n * 1000 : n;
      return new Date(ms).toISOString();
    }
  }
  return '';
};

const OpenContractsView = ({ setSelectedInscription, refreshKey }) => {
  const [pendingTxs, setPendingTxs] = useState([]);
  const [isLoading, setIsLoading] = useState(false);
  const [hasMore, setHasMore] = useState(true);
  const loadingRef = useRef(false);
  const seenRef = useRef(new Set());
  const cursorRef = useRef('');
  const hasMoreRef = useRef(true);
  /** True after any page-2+ load — disables hard resets / full replaces. */
  const paginatedRef = useRef(false);
  const sentinelRef = useRef(null);
  const rootElRef = useRef(null);
  const scrollParentRef = useRef(null);

  const resolveScrollParent = useCallback(() => {
    const node = sentinelRef.current || rootElRef.current;
    const parent = getScrollParent(node);
    scrollParentRef.current = parent;
    return parent;
  }, []);

  const applyPageMeta = (payload, filtered, unique, { append }) => {
    let nextCursor = payload?.next_cursor_date || '';
    // Fallback: last item timestamp so "more" always has a cursor when the API
    // claims has_more but omits next_cursor_date (common for open/created_at lists).
    if (!nextCursor && unique.length > 0) {
      nextCursor = cursorFromItem(unique[unique.length - 1]);
    }
    const more = Boolean(payload?.has_more) && filtered.length > 0;

    if (append) {
      unique.forEach((item) => {
        const k = itemKey(item);
        if (k) seenRef.current.add(k);
      });
      setPendingTxs((prev) => [...prev, ...unique]);
      if (unique.length > 0) {
        paginatedRef.current = true;
      }
      if (nextCursor) {
        cursorRef.current = nextCursor;
      }
    } else {
      const nextSeen = new Set();
      unique.forEach((item) => {
        const k = itemKey(item);
        if (k) nextSeen.add(k);
      });
      seenRef.current = nextSeen;
      setPendingTxs(unique);
      cursorRef.current = nextCursor;
      paginatedRef.current = false;
    }

    if (unique.length === 0 && filtered.length > 0 && more) {
      console.warn('Open contracts pagination returned no new unique items; stopping');
      hasMoreRef.current = false;
      setHasMore(false);
    } else {
      hasMoreRef.current = more;
      setHasMore(more);
    }
  };

  const fetchOpenPage = useCallback(async (mode) => {
    // mode: 'initial' | 'more' | 'refreshKey' | 'poll'
    if (loadingRef.current) return false;
    if (mode === 'more' && !hasMoreRef.current) return false;
    // Need a cursor for page 2+; without it we'd re-fetch the first page forever.
    if (mode === 'more' && !cursorRef.current) {
      hasMoreRef.current = false;
      setHasMore(false);
      return false;
    }

    if (mode === 'poll') {
      if (paginatedRef.current) return false;
      const scroller = scrollParentRef.current;
      if (scroller && scroller.scrollTop > TOP_SCROLL_PX) return false;
    }

    loadingRef.current = true;
    if (mode === 'initial' || mode === 'more') {
      setIsLoading(true);
    }

    try {
      const url = new URL(`${API_BASE}/api/open-contracts`);
      url.searchParams.set('open', 'true');
      url.searchParams.set('limit', String(PAGE_LIMIT));

      const append = mode === 'more';
      const pageCursor = append ? cursorRef.current : '';
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
      const filtered = normalized.filter((contract) => isOpenStatus(contract.status));

      if (mode === 'poll' || (mode === 'refreshKey' && paginatedRef.current)) {
        setPendingTxs((prev) => softMergeFirstPage(prev, filtered));
        filtered.forEach((item) => {
          const k = itemKey(item);
          if (k) seenRef.current.add(k);
        });
        return true;
      }

      const unique = [];
      if (append) {
        filtered.forEach((item) => {
          const k = itemKey(item);
          if (k && !seenRef.current.has(k)) {
            unique.push(item);
          }
        });
      } else {
        filtered.forEach((item) => {
          const k = itemKey(item);
          if (k) unique.push(item);
        });
      }

      applyPageMeta(payload, filtered, unique, { append });
      return true;
    } catch (error) {
      console.error('Error fetching open contracts:', error);
      if (mode === 'initial') {
        setPendingTxs([]);
        hasMoreRef.current = false;
        setHasMore(false);
      }
      return false;
    } finally {
      loadingRef.current = false;
      setIsLoading(false);
    }
  }, []);

  /** Load next page if the sentinel is still near the visible area. */
  const maybeLoadMore = useCallback(() => {
    if (loadingRef.current || !hasMoreRef.current) return;
    const root = scrollParentRef.current || resolveScrollParent();
    if (!isNearViewport(sentinelRef.current, root)) return;
    fetchOpenPage('more');
  }, [fetchOpenPage, resolveScrollParent]);

  // Initial load + explicit refresh after inscribe (refreshKey).
  useEffect(() => {
    fetchOpenPage(refreshKey ? 'refreshKey' : 'initial');
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only re-run on refreshKey
  }, [refreshKey]);

  // Soft poll for new open contracts when still on the first page and near top.
  useEffect(() => {
    const intervalId = setInterval(() => {
      fetchOpenPage('poll');
    }, POLL_MS);
    return () => clearInterval(intervalId);
  }, [fetchOpenPage]);

  // Resolve scroll parent after mount / when list size changes (layout shifts).
  useEffect(() => {
    resolveScrollParent();
  }, [resolveScrollParent, pendingTxs.length]);

  // After a load finishes with hasMore, re-check: IntersectionObserver will not
  // re-fire if the sentinel stayed visible the whole time we were loading.
  useEffect(() => {
    if (isLoading || !hasMore) return;
    const id = requestAnimationFrame(() => {
      maybeLoadMore();
    });
    return () => cancelAnimationFrame(id);
  }, [isLoading, hasMore, pendingTxs.length, maybeLoadMore]);

  // Scroll listener backup (works even when IO root detection fails).
  useEffect(() => {
    if (!hasMore) return;
    const root = resolveScrollParent();
    const target = root || window;
    const onScroll = () => {
      maybeLoadMore();
    };
    target.addEventListener('scroll', onScroll, { passive: true });
    // Also listen on window in case the real scroller is the document.
    if (root) {
      window.addEventListener('scroll', onScroll, { passive: true });
    }
    return () => {
      target.removeEventListener('scroll', onScroll);
      if (root) window.removeEventListener('scroll', onScroll);
    };
  }, [hasMore, pendingTxs.length, maybeLoadMore, resolveScrollParent]);

  // IntersectionObserver — re-create when loading ends so a still-visible
  // sentinel triggers the next page (same pattern as ContractsPage).
  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel || !hasMore || isLoading) return;

    const root = resolveScrollParent();
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          maybeLoadMore();
        }
      },
      {
        root: root || null,
        threshold: 0,
        rootMargin: `${LOAD_MORE_MARGIN_PX}px 0px`,
      }
    );
    observer.observe(sentinel);
    // Synchronous first check for browsers that delay the initial callback.
    maybeLoadMore();
    return () => observer.disconnect();
  }, [hasMore, isLoading, maybeLoadMore, resolveScrollParent, pendingTxs.length]);

  // Preserve API order (newest → older appends). Do not re-sort.
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
    <div className="mb-4" ref={rootElRef}>
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

      <div ref={sentinelRef} className="py-8 text-center min-h-[3rem]">
        {isLoading ? (
          <div className="flex flex-col items-center gap-2">
            <div className="w-6 h-6 border-2 border-starlight border-t-transparent rounded-full animate-spin" />
            <span className="text-[10px] font-black uppercase tracking-widest text-gray-500">
              Loading open contracts…
            </span>
          </div>
        ) : hasMore && mappedInscriptions.length > 0 ? (
          <button
            type="button"
            onClick={() => fetchOpenPage('more')}
            className="text-[10px] font-black uppercase tracking-widest text-gray-500 hover:text-primary underline-offset-2 hover:underline"
          >
            Scroll for more
          </button>
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
