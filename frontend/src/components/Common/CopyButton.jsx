import React, { useState } from 'react';
import { Copy, Check } from 'lucide-react';

const CopyButton = ({ text, className = "" }) => {
  const [copiedText, setCopiedText] = useState('');

  const copyToClipboard = async (textToCopy) => {
    if (!navigator?.clipboard?.writeText) {
      console.warn('Clipboard API unavailable in this context');
      return;
    }
    try {
      await navigator.clipboard.writeText(textToCopy);
      setCopiedText(textToCopy);
      setTimeout(() => setCopiedText(''), 2000);
    } catch (error) {
      console.error('Failed to copy:', error);
    }
  };

  return (
    <button 
      onClick={() => copyToClipboard(text)}
      className={`text-indigo-600 dark:text-white hover:text-indigo-800 dark:hover:text-gray-200 ${className}`}
    >
      {copiedText === text ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
    </button>
  );
};

export default CopyButton;
