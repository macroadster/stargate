import React from 'react';
import CopyButton from '../Common/CopyButton';
import ConfidenceIndicator from '../Common/ConfidenceIndicator';

const InscriptionCard = ({ inscription, onClick }) => {
  console.log('Rendering inscription card:', inscription.id);
  
  const imageSource = inscription.thumbnail || inscription.image_url;

  return (
    <div
      onClick={() => onClick(inscription)}
      className="relative group cursor-pointer"
    >
      <div className="relative overflow-hidden rounded-lg border-2 border-gray-300 dark:border-gray-700 hover:border-indigo-500 transition-all duration-200 bg-white dark:bg-gray-800">
        <div className="h-32 flex items-center justify-center bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800">
          {imageSource ? (
            <img 
              src={imageSource} 
              alt={inscription.file_name || inscription.id}
              className="max-w-full max-h-full object-contain"
              onError={(e) => {
                e.target.style.display = 'none';
                e.target.nextSibling.style.display = 'flex';
              }}
            />
          ) : null}
          <div className="text-4xl" style={{display: imageSource ? 'none' : 'flex'}}>
            {inscription.contract_type === 'Steganographic Contract' ? '🎨' :
             inscription.mime_type?.includes('text') ? '📄' : 
             inscription.mime_type?.includes('image') ? '🖼️' : '📦'}
          </div>
        </div>
        
        <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <div className="absolute bottom-0 left-0 right-0 p-3 text-white">
            <div className="text-xs font-mono truncate font-semibold mb-1">
              {inscription.file_name || inscription.id}
            </div>
            {inscription.metadata?.extracted_message && (
              <div className="text-xs truncate opacity-90 italic">
                "{inscription.metadata.extracted_message.slice(0, 50)}{inscription.metadata.extracted_message.length > 50 ? '...' : ''}"
              </div>
            )}
            {inscription.text && (
              <div className="text-xs mt-1 truncate opacity-90">{inscription.text}</div>
            )}
            {inscription.metadata?.confidence && inscription.metadata.confidence > 0 && (
              <div className="flex items-center gap-2 mt-2">
                <div className="w-full bg-green-400 rounded-full h-1">
                  <div 
                    className="bg-green-600 h-1 rounded-full" 
                    style={{width: `${Math.round(inscription.metadata.confidence * 100)}%`}}
                  ></div>
                </div>
                <span className="text-xs font-semibold">{Math.round(inscription.metadata.confidence * 100)}%</span>
              </div>
            )}
            
            {inscription.metadata?.is_stego && (
              <div className="mt-2 p-2 bg-purple-50 dark:bg-purple-900 border border-purple-200 dark:border-purple-700 rounded-lg">
                <div className="flex items-center gap-2 mb-1">
                  <div className="w-2 h-2 bg-purple-500 rounded-full animate-pulse"></div>
                  <span className="text-purple-700 dark:text-purple-300 text-xs font-semibold">STEGANOGRAPHY DETECTED</span>
                </div>
                {inscription.metadata.stego_type && (
                  <div className="text-xs text-purple-600 dark:text-purple-400">
                    Method: <span className="font-mono font-semibold">{inscription.metadata.stego_type.toUpperCase()}</span>
                  </div>
                )}
                {inscription.metadata.stego_probability && (
                  <div className="text-xs text-purple-600 dark:text-purple-400">
                    Probability: <span className="font-semibold">{Math.round(inscription.metadata.stego_probability * 100)}%</span>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
        
        <div className="absolute top-2 left-2 flex flex-col gap-1">
          {inscription.contract_type === 'Steganographic Contract' && (
            <div className="px-2 py-1 bg-gradient-to-r from-purple-600 to-purple-700 text-white text-xs rounded-full font-semibold shadow-lg">
              🔐 STEGO
            </div>
          )}
        </div>
        
        {inscription.size_bytes && (
          <div className="absolute bottom-2 right-2 bg-white dark:bg-gray-800 rounded-lg px-2 py-1 shadow-lg border border-gray-200 dark:border-gray-600">
            <div className="flex items-center gap-1">
              <span className="text-black dark:text-white text-xs font-bold">
                {(inscription.size_bytes / 1024).toFixed(1)}KB
              </span>
            </div>
          </div>
        )}
      </div>
      
      <div className="mt-2">
        <div className="text-black dark:text-white font-mono text-xs truncate font-medium" title={inscription.file_name || inscription.id}>
          {inscription.file_name || inscription.id}
        </div>
        <div className="flex items-center gap-2 mt-1">
          {inscription.mime_type && (
            <span className="text-gray-500 dark:text-gray-400 text-xs">
              {inscription.mime_type.split('/')[1]?.toUpperCase() || 'UNKNOWN'}
            </span>
          )}
          {inscription.metadata?.image_format && (
            <span className="text-gray-500 dark:text-gray-400 text-xs">
              {inscription.metadata.image_format.toUpperCase()}
            </span>
          )}
        </div>
      </div>
    </div>
  );
};

export default InscriptionCard;