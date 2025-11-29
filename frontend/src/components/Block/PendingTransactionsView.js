import React, { useState, useEffect } from 'react';
import CopyButton from '../Common/CopyButton';

const PendingTransactionsView = ({ setSelectedInscription }) => {
  const [pendingTxs, setPendingTxs] = useState([]);

  useEffect(() => {
    fetchPendingTransactions();
  }, []);

  const fetchPendingTransactions = async () => {
    try {
      const response = await fetch('http://localhost:3001/api/pending-transactions');
      const data = await response.json();
      setPendingTxs(data || []);
    } catch (error) {
      console.error('Error fetching pending transactions:', error);
      setPendingTxs([]);
    }
  };

  return (
    <div className="mb-4">
      <div className="mb-4">
        <h3 className="text-black dark:text-white text-lg font-semibold border-b-2 border-yellow-500 pb-2 inline-block">
          Pending Transactions
        </h3>
      </div>

      {Array.isArray(pendingTxs) && pendingTxs.length > 0 ? (
        <div className="space-y-3">
                  {pendingTxs.map((tx, idx) => (
                    <div
                      key={idx}
                      className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4 cursor-pointer hover:bg-yellow-100 dark:hover:bg-yellow-800 transition-colors"
                      onClick={() => {
                        const inscription = {
                          id: tx.id,
                          contractType: 'Pending Contract',
                          capability: 'Data Storage',
                          protocol: 'BRC-20',
                          apiEndpoints: 0,
                          interactions: 0,
                          reputation: 'Pending',
                          isActive: false,
                          number: parseInt(tx.id.split('_')[1]) || 0,
                          address: 'bc1q...pending',
                          genesis_block_height: tx.blockHeight,
                          mime_type: 'text/plain',
                          text: tx.text,
                          price: tx.price,
                          timestamp: tx.timestamp,
                          status: tx.status,
                          image: tx.imageData ? `http://localhost:3001/uploads/${tx.imageData.split('/').pop()}` : null,
                        };
                        setSelectedInscription(inscription);
                      }}
                    >
                      <div className="flex justify-between items-start mb-3">
                        <div className="flex items-center gap-3">
                          <div className="px-3 py-1 rounded text-xs font-semibold bg-yellow-600 text-white">
                            Inscribe
                          </div>
                          <div className="text-yellow-800 dark:text-yellow-200 font-mono text-sm">
                            {tx.id}
                          </div>
                          <CopyButton text={tx.id} />
                        </div>
                        <div className="px-2 py-1 rounded text-xs font-semibold bg-yellow-100 dark:bg-yellow-800 text-yellow-800 dark:text-yellow-200">
                          {tx.status}
                        </div>
                      </div>

                      <div className="grid grid-cols-2 gap-4 text-sm">
                        <div>
                          <div className="text-yellow-700 dark:text-yellow-300 mb-1">Text Length</div>
                          <div className="text-yellow-900 dark:text-yellow-100">{tx.text?.length || 0} chars</div>
                        </div>
                        <div>
                          <div className="text-yellow-700 dark:text-yellow-300 mb-1">Price</div>
                          <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{tx.price} BTC</div>
                        </div>
                      </div>

                      <div className="mt-3 text-xs text-yellow-600 dark:text-yellow-400">
                        Submitted {new Date(tx.timestamp * 1000).toLocaleString()}
                      </div>
                    </div>
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