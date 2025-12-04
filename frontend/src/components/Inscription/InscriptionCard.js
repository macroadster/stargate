import React, { useState } from 'react';

const InscriptionCard = ({ inscription, onClick }) => {
  const [showTextPreview, setShowTextPreview] = useState(false);
  
  const hasTextContent = inscription.text || inscription.metadata?.extracted_message;
  const textContent = inscription.text || inscription.metadata?.extracted_message || '';
  const isActuallyImageFile = inscription.mime_type?.includes('image') && !inscription.image_url?.endsWith('.txt');
  const imageSource = isActuallyImageFile ? (inscription.thumbnail || inscription.image_url) : null;

  const handleTextPreviewToggle = (e) => {
    e.stopPropagation();
    setShowTextPreview(!showTextPreview);
  };

  return (
    <div
      onClick={() => onClick(inscription)}
      className="relative group cursor-pointer"
    >
      <div className="relative overflow-hidden rounded-lg border-2 border-gray-300 dark:border-gray-700 hover:border-indigo-500 transition-all duration-200 bg-white dark:bg-gray-800">
        <div className="h-32 flex items-center justify-center bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 relative">
          {showTextPreview && hasTextContent ? (
            <div className="absolute inset-0 p-2 bg-white dark:bg-gray-900 rounded-lg overflow-hidden">
              <div className="h-full overflow-y-auto text-xs font-mono text-gray-800 dark:text-gray-200 leading-tight">
                {textContent.length > 200 ? `${textContent.slice(0, 200)}...` : textContent}
              </div>
            </div>
          ) : (
            <>
              {imageSource && isActuallyImageFile ? (
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
              <div className="text-lg font-bold text-center px-2 text-gray-800 dark:text-gray-200" style={{display: imageSource && isActuallyImageFile ? 'none' : 'flex'}}>
                {hasTextContent ? textContent.slice(0, 20) + (textContent.length > 20 ? '...' : '') : 
                 inscription.contract_type === 'Steganographic Contract' ? '🎨' : '📦'}
              </div>
            </>
          )}
        </div>
        
        <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <div className="absolute bottom-0 left-0 right-0 p-3 text-white">
            {((inscription.metadata?.confidence && inscription.metadata.confidence > 0.1) ||
              (inscription.metadata?.stego_probability && inscription.metadata.stego_probability > 0.1)) && (
              <div className="flex items-center gap-2 mt-2">
                <div className="w-full bg-green-400 rounded-full h-1">
                  <div 
                    className="bg-green-600 h-1 rounded-full" 
                    style={{width: `${Math.round((inscription.metadata.confidence || inscription.metadata.stego_probability || 0) * 100)}%`}}
                  ></div>
                </div>
                <span className="text-xs font-semibold">
                  {Math.round((inscription.metadata.confidence || inscription.metadata.stego_probability || 0) * 100)}%
                </span>
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
          {hasTextContent && (
            <button
              onClick={handleTextPreviewToggle}
              className="px-2 py-1 bg-gradient-to-r from-blue-600 to-blue-700 text-white text-xs rounded-full font-semibold shadow-lg hover:from-blue-700 hover:to-blue-800 transition-colors"
              title={showTextPreview ? "Hide text preview" : "Show text preview"}
            >
              {showTextPreview ? "📝 Hide" : "📄 Text"}
            </button>
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
        <div className="text-black dark:text-white font-mono text-xs truncate font-medium" title={inscription.id}>
          {inscription.id}
        </div>
        <div className="flex items-center gap-2 mt-1">
          {inscription.mime_type && (
            <span className="text-gray-500 dark:text-gray-400 text-xs">
              {inscription.mime_type.split('/')[1]?.toUpperCase() || 'UNKNOWN'}
            </span>
          )}
          {hasTextContent && (
            <span className="px-2 py-0.5 rounded-full bg-blue-100 text-blue-700 dark:bg-blue-900/60 dark:text-blue-200 text-[11px] font-semibold">
              TEXT
            </span>
          )}
        </div>
      </div>
    </div>
  );
};

export default InscriptionCard;
