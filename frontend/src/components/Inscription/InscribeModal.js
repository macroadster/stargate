import React, { useState } from 'react';
import { X } from 'lucide-react';
import { QRCodeCanvas } from 'qrcode.react';

const InscribeModal = ({ onClose, setPendingTransactions }) => {
  const [step, setStep] = useState(1);
  const [imageFile, setImageFile] = useState(null);
  const [embedText, setEmbedText] = useState('');
  const [price, setPrice] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [paymentData, setPaymentData] = useState(null);
  const buildPlaceholderImage = () => {
    const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/Ptq4YQAAAABJRU5ErkJggg==";
    const bytes = Uint8Array.from(atob(pngBase64), c => c.charCodeAt(0));
    return new File([bytes], "placeholder.png", { type: "image/png" });
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    setIsSubmitting(true);

    try {
      const formData = new FormData();
      const uploadImage = imageFile || buildPlaceholderImage();
      formData.append('image', uploadImage);
      formData.append('message', embedText);
      formData.append('method', 'alpha');

      const backendBase = `${window.location.protocol}//${window.location.hostname}:3001`;
      const response = await fetch(`${backendBase}/api/inscribe`, {
        method: 'POST',
        body: formData
      });

      if (response.ok) {
        const result = await response.json();
        console.log('Inscription successful:', result);
        
        // Generate payment QR code data
        const paymentAddress = "bc1qexampleaddress123456789"; // Demo address
        const paymentAmount = price;
        setPaymentData({
          address: paymentAddress,
          amount: paymentAmount,
          inscriptionId: result.id
        });
        
        setStep(2);
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
          <h2 className="text-xl font-bold text-black dark:text-white">Create Inscription</h2>
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
                {isSubmitting ? 'Creating...' : 'Create Inscription'}
              </button>
            </div>
          </form>
        ) : (
          <div className="text-center py-8">
            <div className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
              Complete Payment
            </div>
            
            {paymentData && (
              <div className="space-y-4">
                <div className="flex justify-center mb-4">
                  <QRCodeCanvas 
                    value={`bitcoin:${paymentData.address}?amount=${paymentData.amount}`}
                    size={200}
                    level="M"
                    includeMargin={true}
                  />
                </div>
                
                <div className="text-sm text-gray-600 dark:text-gray-400">
                  <p className="mb-2">Send <span className="font-mono font-bold">{paymentData.amount} BTC</span></p>
                  <p className="mb-2">to address:</p>
                  <p className="font-mono text-xs bg-gray-100 dark:bg-gray-700 p-2 rounded break-all">
                    {paymentData.address}
                  </p>
                </div>
                
                <div className="text-xs text-gray-500 dark:text-gray-500">
                  Inscription ID: {paymentData.inscriptionId}
                </div>
              </div>
            )}
            
            <div className="flex gap-3 mt-6">
              <button
                onClick={() => setStep(1)}
                className="flex-1 px-4 py-2 bg-gray-600 text-white rounded-md hover:bg-gray-700"
              >
                Back
              </button>
              <button
                onClick={() => {
                  setTimeout(() => {
                    fetch(`${window.location.protocol}//${window.location.hostname}:3001/api/pending-transactions`)
                      .then(res => res.json())
                      .then(data => setPendingTransactions(data || []))
                      .catch(err => console.error('Error fetching pending transactions:', err));
                  }, 1000);
                  onClose();
                }}
                className="flex-1 px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700"
              >
                Payment Sent
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default InscribeModal;
