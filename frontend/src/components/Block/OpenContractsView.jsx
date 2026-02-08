import React, { useState, useEffect, useMemo, useCallback } from 'react';
import InscriptionCard from '../Inscription/InscriptionCard';
import { API_BASE } from '../../apiBase';

const OpenContractsView = ({ setSelectedInscription, refreshKey }) => {
  const [pendingTxs, setPendingTxs] = useState([]);
  const [isLoading, setIsLoading] = useState(false);

  const fetchOpenContracts = useCallback(async () => {
    if (isLoading) return;
    setIsLoading(true);
    try {
      const response = await fetch(`${API_BASE}/api/open-contracts`);
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();
      const raw = data?.data?.transactions ?? data;
      const normalized = Array.isArray(raw) ? raw : [];
      
      // Filter out superseded contracts - only show active/open contracts
      const filtered = normalized.filter(contract => 
        contract.status !== 'superseded' && 
        contract.status !== 'completed' && 
        contract.status !== 'confirmed'
      );
      
      setPendingTxs(filtered);
    } catch (error) {
      console.error('Error fetching open contracts:', error);
      setPendingTxs([]);
    } finally {
      setIsLoading(false);
    }
  }, [isLoading]);

  useEffect(() => {
    fetchOpenContracts();
  }, [fetchOpenContracts, refreshKey]);

  useEffect(() => {
    const intervalId = setInterval(() => {
      fetchOpenContracts();
    }, 8000);
    return () => clearInterval(intervalId);
  }, [fetchOpenContracts]);

  const mappedInscriptions = useMemo(() => {
    const list = Array.isArray(pendingTxs) ? pendingTxs : [];
    return list
      .filter((tx) => !['confirmed', 'complete', 'rejected'].includes((tx.status || '').toLowerCase()))
      .map((tx) => {
      const imagePath = tx.imageData || '';
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
      const wishText = tx.wish_text || tx.embedded_message || tx.message || '';
      return {
        id: tx.id,
        contract_type: 'Pending Contract',
        capability: 'Data Storage',
        protocol: 'BRC-20',
        apiEndpoints: 0,
        interactions: 0,
        reputation: 'Pending',
        isActive: false,
        number: parseInt(tx.id.split('_')[1]) || 0,
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
          is_stego: !!tx.visiblePixelHash,
          confidence: 0,
          stego_probability: 0,
          transaction_id: tx.id,
          wish_text: wishText,
          visible_pixel_hash: tx.visiblePixelHash,
          total_budget: tx.totalBudgetSats || (tx.price ? tx.price * 1e8 : 0),
          available_tasks: tx.availableTasks || 0
        }
      };
    });
  }, [pendingTxs]);

  return (
    <div className="mb-4">
      <div className="mb-4">
        <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-yellow-500 pb-2 inline-block">
          Open Contracts
        </h3>
      </div>

      {Array.isArray(mappedInscriptions) && mappedInscriptions.length > 0 ? (
        <div className="columns-1 sm:columns-2 xl:columns-3 gap-6">
          {mappedInscriptions.map((inscription, idx) => (
            <InscriptionCard
              key={idx}
              inscription={inscription}
              onClick={setSelectedInscription}
            />
          ))}
        </div>
      ) : (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400">
          No pending transactions
        </div>
      )}

    </div>
  );
};

export default OpenContractsView;
