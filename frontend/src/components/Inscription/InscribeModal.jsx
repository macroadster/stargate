import React, { useEffect, useRef, useState } from 'react';
import { X } from 'lucide-react';
import { API_BASE } from '../../apiBase';
import { useAuth } from '../../context/AuthContext';

import { apiFetch } from '../../utils/api';

const InscribeModal = ({ onClose, onSuccess }) => {
  const { auth } = useAuth();
  const [step, setStep] = useState(1);
  const [imageFile, setImageFile] = useState(null);
  const [embedText, setEmbedText] = useState('');
  const [price, setPrice] = useState('');
  const [priceUnit, setPriceUnit] = useState('btc');
  const lastUnitRef = useRef('btc');
  const [address, setAddress] = useState(auth.wallet || '');
  const [fundingMode, setFundingMode] = useState('payout');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [inscriptionResult, setInscriptionResult] = useState(null);
  const buildPlaceholderImage = () => {
    const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/Ptq4YQAAAABJRU5ErkJggg==";
    const bytes = Uint8Array.from(atob(pngBase64), c => c.charCodeAt(0));
    return new File([bytes], "placeholder.png", { type: "image/png" });
  };

  const toBase64 = (file) => new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result || '';
      const base64 = typeof result === 'string' ? result.split(',')[1] || '' : '';
      resolve(base64);
    };
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });

  useEffect(() => {
    setAddress(auth.wallet || '');
  }, [auth.wallet]);

  useEffect(() => {
    const lastUnit = lastUnitRef.current;
    if (lastUnit === priceUnit) {
      return;
    }
    lastUnitRef.current = priceUnit;
    if (price === '') {
      return;
    }
    const numeric = Number(price);
    if (!Number.isFinite(numeric)) {
      return;
    }
    if (priceUnit === 'sats') {
      setPrice(String(Math.max(0, Math.trunc(numeric * 1e8))));
    } else {
      setPrice((numeric / 1e8).toFixed(8));
    }
  }, [priceUnit, price]);

  const formatAltPrice = () => {
    const numeric = Number(price);
    if (!Number.isFinite(numeric)) {
      return '';
    }
    if (priceUnit === 'sats') {
      return `${(numeric / 1e8).toFixed(8)} BTC`;
    }
    return `${Math.max(0, Math.trunc(numeric * 1e8))} sats`;
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setIsSubmitting(true);

    try {
      const uploadImage = imageFile || buildPlaceholderImage();
      const payloadPrice =
        priceUnit === 'sats'
          ? String(Math.max(0, Math.trunc(Number(price) || 0)))
          : price;
      const payload = {
        message: embedText,
        price: payloadPrice,
        price_unit: priceUnit,
        address,
        funding_mode: fundingMode,
        method: 'alpha',
      };

      if (uploadImage) {
        payload.image_base64 = await toBase64(uploadImage);
        payload.filename = uploadImage.name;
      }

      const response = await apiFetch('/api/inscribe', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-API-Key': auth.apiKey || ''
        },
        body: JSON.stringify(payload)
      });

      if (response.ok) {
        const result = await response.json();
        setInscriptionResult(result);
        setStep(2);
        if (onSuccess) onSuccess();
      } else {
        console.error('Inscription failed');
      }
    } catch (error) {
      console.error('Error submitting inscription:', error);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="modal-backdrop-overlay">
      <div className="modal-container create-contract-modal" style={{ maxWidth: '28rem', height: 'auto', maxHeight: '90vh', overflow: 'auto' }}>
        <div className="modal-form-content">
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-xl font-bold create-contract-title">Create Smart Contract</h2>
            <button onClick={onClose} className="create-contract-close">
              <X className="w-5 h-5" />
            </button>
          </div>

          {step === 1 ? (
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Image (optional)
              </label>
              <input
                type="file"
                accept="image/*"
                onChange={(e) => setImageFile(e.target.files[0])}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Text Content
              </label>
              <textarea
                value={embedText}
                onChange={(e) => setEmbedText(e.target.value)}
                required
                rows={4}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
                placeholder="Enter text to inscribe..."
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Price
              </label>
              <div className="flex gap-2">
                <input
                  type="number"
                  value={price}
                  onChange={(e) => setPrice(e.target.value)}
                  step={priceUnit === 'sats' ? '1' : '0.00000001'}
                  required
                  className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
                  placeholder={priceUnit === 'sats' ? '1000' : '0.00000000'}
                />
                <select
                  value={priceUnit}
                  onChange={(e) => setPriceUnit(e.target.value)}
                  className="px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
                >
                  <option value="btc">BTC</option>
                  <option value="sats">sats</option>
                </select>
              </div>
              {price !== '' && (
                <div className="mt-2 text-xs text-gray-500 dark:text-gray-400">
                  ≈ {formatAltPrice()}
                </div>
              )}
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Wallet Address (from API key)
              </label>
              <input
                type="text"
                value={address}
                readOnly
                className={`w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-gray-100 dark:bg-gray-700 ${
                  address ? 'text-gray-700 dark:text-gray-300' : 'text-red-500 dark:text-red-400 placeholder-red-500 dark:placeholder-red-400'
                }`}
                placeholder="Not signed in"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Funding Mode
              </label>
              <div className="relative">
                <select
                  value={fundingMode}
                  onChange={(e) => setFundingMode(e.target.value)}
                  className="w-full appearance-none px-3 pr-10 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
                >
                  <option value="payout">Payout to contractors</option>
                  <option value="raise_fund">Raise fund from investor (collect from contractors)</option>
                </select>
              </div>
            </div>

            <div className="flex gap-3">
              <button
                type="button"
                onClick={onClose}
                className="flex-1 px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSubmitting}
                className="flex-1 px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700 disabled:opacity-50"
              >
                {isSubmitting ? 'Creating...' : 'Create Smart Contract'}
              </button>
            </div>
          </form>
        ) : (
          <div className="text-center py-8">
            <div className="text-lg font-semibold contract-submitted-title mb-4">
              Inscription Submitted
            </div>
            
            <div className="text-sm contract-success-text space-y-3">
              <p>Your smart contract is now pending.</p>
              <p>Check Pending Transactions to track confirmations.</p>
              {inscriptionResult?.id && (
                <div className="text-xs contract-success-text">
                  Inscription ID: {inscriptionResult.id}
                </div>
              )}
            </div>
            
            <div className="flex gap-3 mt-6">
              <button
                onClick={() => {
                  setInscriptionResult(null);
                  setStep(1);
                }}
                className="flex-1 px-4 py-2 rounded-md hover:opacity-80 contract-success-btn"
              >
                Create Another
              </button>
              <button
                onClick={() => {
                  onClose();
                }}
                className="flex-1 px-4 py-2 rounded-md hover:opacity-80 contract-success-btn-done"
              >
                Done
              </button>
            </div>
          </div>
        )}
        </div>
      </div>
    </div>
  );
};

export default InscribeModal;
