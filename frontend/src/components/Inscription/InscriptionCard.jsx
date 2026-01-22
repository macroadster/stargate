import React, { useEffect, useRef, useState } from 'react';

const InscriptionCard = ({ inscription, onClick }) => {
  const [showTextPreview, setShowTextPreview] = useState(false);
  const [isVisible, setIsVisible] = useState(false);
  const containerRef = useRef(null);
  const mediaObserverRef = useRef(null);

  // Calculate dynamic height for text-only contracts
  const calculateTextHeight = (text, isPreview = false) => {
    if (!text) return 320;
    
    const avgCharsPerLine = 60;
    const lineHeight = 24;
    const padding = 48;
    const maxPreviewLines = isPreview ? 8 : 12;
    
    const lines = Math.ceil(text.length / avgCharsPerLine);
    const limitedLines = Math.min(lines, maxPreviewLines);
    const calculatedHeight = limitedLines * lineHeight + padding;
    
    return Math.max(calculatedHeight, 320);
  };

  useEffect(() => {
    const node = containerRef.current;
    if (!node) return;

    mediaObserverRef.current = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setIsVisible(true);
          mediaObserverRef.current?.disconnect();
        }
      },
      { rootMargin: '200px', threshold: 0.05 }
    );

    mediaObserverRef.current.observe(node);

    return () => {
      mediaObserverRef.current?.disconnect();
    };
  }, []);
  
  const mime = (inscription.mime_type || '').toLowerCase();
  const hasTextContent = inscription.text || inscription.metadata?.extracted_message;
  const textContent = inscription.text || inscription.metadata?.extracted_message || '';
  const isActuallyImageFile = mime.includes('image') && !((inscription.image_url || '').endsWith('.txt'));
  const isTextMime = mime.startsWith('text/');
  const isHtmlContent = mime.includes('text/html') || mime.includes('application/xhtml');
  const isSvgContent = mime === 'image/svg+xml' || (mime.includes('svg') && mime.includes('xml'));
  const isInlineHtml = isHtmlContent && textContent.trim().startsWith('<');
  const imageSource = isActuallyImageFile ? (inscription.thumbnail || inscription.image_url) : null;
  const sandboxSrc = inscription.image_url || inscription.thumbnail;
  const sandboxDoc = (isHtmlContent || isSvgContent) ? (textContent || '') : '';
  const headline = (() => {
    if (textContent) {
      const line = textContent.split('\n').map((l) => l.trim()).find((l) => l);
      if (line) return line.replace(/^#+\s*/, '').slice(0, 80);
    }
    if (inscription.file_name) return inscription.file_name;
    return inscription.id;
  })();
  const sizeBytes = Number(inscription.size_bytes || 0);
  const confidenceScore = Number(inscription.metadata?.confidence || 0);
  const stegoProbability = Number(inscription.metadata?.stego_probability || 0);
  const detectionScore = Math.max(confidenceScore, stegoProbability);
  const detectionPercent = Math.round(detectionScore * 100);
  const shouldLoadMedia = isVisible;
  const showImage = shouldLoadMedia && imageSource && isActuallyImageFile && !isInlineHtml;
  const showSandbox = shouldLoadMedia && (isHtmlContent || isSvgContent) && (sandboxSrc || sandboxDoc);

  const handleTextPreviewToggle = (e) => {
    e.stopPropagation();
    setShowTextPreview(!showTextPreview);
  };

  return (
    <div
      onClick={() => onClick(inscription)}
      ref={containerRef}
      className="relative group cursor-pointer break-inside-avoid mb-6"
    >
      <div className="relative overflow-hidden rounded-2xl border border-gray-200 dark:border-gray-800 hover:border-indigo-400 transition-all duration-200 bg-white dark:bg-gray-900 shadow-sm">
        <div className={`${hasTextContent && !imageSource ? '' : 'max-h-[460px] min-h-[200px]'} flex items-start justify-center bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 relative overflow-hidden`}
             style={hasTextContent && !imageSource ? { minHeight: `${calculateTextHeight(textContent, false)}px` } : {}}>
          {showTextPreview && hasTextContent ? (
            <div className="absolute inset-0 p-3 bg-white dark:bg-gray-900 rounded-lg overflow-hidden">
              <div className="h-full overflow-y-auto text-sm font-mono text-gray-800 dark:text-gray-200 leading-relaxed"
                   style={{ maxHeight: `${calculateTextHeight(textContent, true) - 24}px` }}>
                {textContent.length > 500 ? `${textContent.slice(0, 500)}...` : textContent}
              </div>
            </div>
          ) : showSandbox ? (
            <div className="absolute inset-0 min-h-[200px]">
              <iframe
                title={`inscription-${inscription.id}`}
                src={sandboxSrc || undefined}
                srcDoc={sandboxSrc ? undefined : sandboxDoc}
                sandbox=""
                loading="lazy"
                referrerPolicy="no-referrer"
                className="w-full h-full border-0"
              />
            </div>
          ) : (
            <>
              {showImage ? (
                <img 
                  src={imageSource} 
                  alt={inscription.file_name || inscription.id}
                  loading="lazy"
                  decoding="async"
                  className="w-full h-auto object-cover"
                  onError={(e) => {
                    e.target.style.display = 'none';
                    e.target.nextSibling.style.display = 'flex';
                  }}
                />
              ) : null}
              <div className={`text-${hasTextContent && !imageSource ? 'base' : 'lg'} font-bold text-center px-3 text-gray-800 dark:text-gray-200 ${hasTextContent && !imageSource ? 'py-4' : ''}`} 
                  style={{display: (imageSource && isActuallyImageFile && !isInlineHtml) ? 'none' : 'flex', wordBreak: 'break-word'}}>
                {hasTextContent && !isInlineHtml ? (
                  !imageSource ? (
                    <div className="space-y-2">
                      <div className="text-sm font-normal text-gray-600 dark:text-gray-400 leading-relaxed max-h-[200px] overflow-hidden">
                        {textContent.length > 200 ? `${textContent.slice(0, 200)}...` : textContent}
                      </div>
                      {textContent.length > 200 && (
                        <div className="text-xs text-blue-600 dark:text-blue-400">
                          Click to view full text
                        </div>
                      )}
                    </div>
                  ) : textContent.slice(0, 80) + (textContent.length > 80 ? '...' : '')
                ) : 
                  inscription.contract_type === 'Steganographic Contract' ? '🎨' : '📦'}
              </div>
            </>
          )}
        </div>
        
        <div className="absolute inset-0 bg-gradient-to-t from-black/70 via-black/5 to-transparent opacity-100">
          <div className="absolute bottom-0 left-0 right-0 p-3 text-white">
            <div className="text-xs uppercase tracking-wide text-white/70">
              {inscription.genesis_block_height ? `Block #${inscription.genesis_block_height}` : 'Smart Contract'}
            </div>
            <div className="text-sm font-semibold leading-snug line-clamp-2">{headline}</div>
          </div>
        </div>

        <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-transparent to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <div className="absolute bottom-0 left-0 right-0 p-3 text-white">
            {detectionScore > 0.1 && detectionPercent > 0 && (
              <div className="flex items-center gap-2 mt-2">
                <div className="w-full bg-green-400 rounded-full h-1">
                  <div 
                    className="bg-green-600 h-1 rounded-full" 
                    style={{width: `${detectionPercent}%`}}
                  ></div>
                </div>
                <span className="text-xs font-semibold">
                  {detectionPercent}%
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
                {stegoProbability > 0 && Math.round(stegoProbability * 100) > 0 && (
                  <div className="text-xs text-purple-600 dark:text-purple-400">
                    Probability: <span className="font-semibold">{Math.round(stegoProbability * 100)}%</span>
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
        
        {sizeBytes > 0 && (
          <div className="absolute bottom-2 right-2 bg-white dark:bg-gray-800 rounded-lg px-2 py-1 shadow-lg border border-gray-200 dark:border-gray-600">
            <div className="flex items-center gap-1">
              <span className="text-black dark:text-white text-xs font-bold">
                {(sizeBytes / 1024).toFixed(1)}KB
              </span>
            </div>
          </div>
        )}
      </div>
      
      <div className="mt-2 space-y-1">
        <div className="text-black dark:text-white font-mono text-xs truncate font-medium" title={inscription.id}>
          {inscription.id}
        </div>
        <div className="flex items-center gap-2">
          {inscription.mime_type && (
            <span className="text-gray-500 dark:text-gray-400 text-xs">
              {inscription.mime_type.split('/')[1]?.toUpperCase() || 'UNKNOWN'}
            </span>
          )}
          {isTextMime && (
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
