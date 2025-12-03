import React, { useState, useEffect, useMemo } from 'react';
import InscriptionCard from '../Inscription/InscriptionCard';
import { API_BASE } from '../../apiBase';

const PendingTransactionsView = ({ setSelectedInscription }) => {
  const [pendingTxs, setPendingTxs] = useState([]);

  useEffect(() => {
    fetchPendingTransactions();
  }, []);

  const fetchPendingTransactions = async () => {
    try {
      const response = await fetch(`${API_BASE}/api/pending-transactions`);
      const data = await response.json();
      const raw = data?.data?.transactions ?? data;
      const normalized = Array.isArray(raw) ? raw : [];
      setPendingTxs(normalized);
    } catch (error) {
      console.error('Error fetching pending transactions:', error);
      setPendingTxs([]);
    }
  };

  const mappedInscriptions = useMemo(() => {
    const list = Array.isArray(pendingTxs) ? pendingTxs : [];
    return list.map((tx) => {
      const uploadFile = tx.imageData ? tx.imageData.split('/').pop() : null;
      const imageUrl = uploadFile ? `${API_BASE}/uploads/${uploadFile}` : null;
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
        address: 'bc1q...pending',
        genesis_block_height: tx.blockHeight || 0,
        mime_type: imageUrl ? 'image/png' : 'text/plain',
        text: tx.text,
        price: tx.price,
        timestamp: tx.timestamp,
        status: tx.status,
        image_url: imageUrl,
        file_name: uploadFile || 'pending.txt',
        size_bytes: tx.text ? tx.text.length : 0,
        metadata: {
          is_stego: false,
          confidence: 0,
          stego_probability: 0,
          transaction_id: tx.id
        }
      };
    });
  }, [pendingTxs]);

  return (
    <div className="mb-4">
      <div className="mb-4">
        <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-yellow-500 pb-2 inline-block">
          Pending Transactions
        </h3>
      </div>

      {Array.isArray(mappedInscriptions) && mappedInscriptions.length > 0 ? (
        <div className="grid grid-cols-5 gap-4">
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

export default PendingTransactionsView;
