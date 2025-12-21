import React, { useState } from 'react';
import { X } from 'lucide-react';
import { API_BASE } from '../../apiBase';
import { useAuth } from '../../context/AuthContext';

const InscribeModal = ({ onClose, onSuccess }) => {
  const { auth } = useAuth();
  const [step, setStep] = useState(1);
  const [imageFile, setImageFile] = useState(null);
  const [embedText, setEmbedText] = useState('');
  const [price, setPrice] = useState('');
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

  const handleSubmit = async (e) => {
    e.preventDefault();
    setIsSubmitting(true);

    try {
      const uploadImage = imageFile || buildPlaceholderImage();
      const payload = {
        message: embedText,
        price,
        address,
        funding_mode: fundingMode,
        method: 'alpha',
      };

      if (uploadImage) {
        payload.image_base64 = await toBase64(uploadImage);
        payload.filename = uploadImage.name;
      }

      const response = await fetch(`${API_BASE}/api/inscribe`, {
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
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg p-6 max-w-md w-full mx-4">
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-xl font-bold text-black dark:text-white">Create Smart Contract</h2>
          <button onClick={onClose} className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200">
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
                Price (BTC)
              </label>
              <input
                type="number"
                value={price}
                onChange={(e) => setPrice(e.target.value)}
                step="0.00000001"
                required
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
                placeholder="0.00000000"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Wallet Address
              </label>
              <input
                type="text"
                value={address}
                onChange={(e) => setAddress(e.target.value)}
                required
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:bg-gray-700 dark:text-white"
                placeholder="bc1..."
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
                <svg
                  className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500 dark:text-gray-300"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                >
                  <path d="M5.25 7.5l4.5 4.5 4.5-4.5h-9z" />
                </svg>
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
            <div className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
              Inscription Submitted
            </div>
            
            <div className="text-sm text-gray-600 dark:text-gray-400 space-y-3">
              <p>Your smart contract is now pending.</p>
              <p>Check Pending Transactions to track confirmations.</p>
              {inscriptionResult?.id && (
                <div className="text-xs text-gray-500 dark:text-gray-500">
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
                className="flex-1 px-4 py-2 bg-gray-600 text-white rounded-md hover:bg-gray-700"
              >
                Create Another
              </button>
              <button
                onClick={() => {
                  onClose();
                }}
                className="flex-1 px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700"
              >
                Done
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default InscribeModal;
