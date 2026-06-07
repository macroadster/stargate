import React, { useEffect, useRef, useState } from 'react';

const InscriptionCard = ({ inscription, onClick }) => {
  const [showTextPreview, setShowTextPreview] = useState(false);
  const [isVisible, setIsVisible] = useState(false);
  const [isHovered, setIsHovered] = useState(false);
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
  const fileName = (inscription.file_name || '').toLowerCase();
  const url = (inscription.image_url || inscription.thumbnail || '').toLowerCase();
  const urlLooksLikeTextFile = url.endsWith('.txt');

  // Obviously text (don't try to render as img, show text/emoji instead)
  const isObviouslyText = mime.startsWith('text/') ||
                          mime.includes('json') ||
                          fileName.endsWith('.json') ||
                          fileName.endsWith('.txt') ||
                          fileName.endsWith('.bitmap') ||
                          fileName.endsWith('.md') ||
                          fileName.includes('brc-20') ||
                          fileName.includes('brc20');

  const isBlockImage = url.includes('/block-image/');
  const hasContentUrl = !!url && !urlLooksLikeTextFile;
  // Treat as displayable image if mime/block-image, or has a content url but isn't obviously text.
  // This lets real images render (even if summary mime is odd) while preventing text items from
  // attempting <img> (they fall through to text snippet or emoji).
  const isActuallyImageFile = (mime.includes('image') || isBlockImage || (hasContentUrl && !isObviouslyText)) && !urlLooksLikeTextFile;
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
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      className="relative group cursor-pointer mb-6 w-full"
      style={{ breakInside: 'avoid', pageBreakInside: 'avoid' }}
    >
      <div className="relative overflow-hidden rounded-2xl border border-gray-200 dark:border-gray-800 hover:border-indigo-400 transition-all duration-200 bg-white dark:bg-gray-900 shadow-sm">
        <div className={`${hasTextContent && !imageSource ? '' : 'min-h-[280px] sm:min-h-[350px] xl:min-h-[400px]'} flex items-start justify-center bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 relative overflow-hidden`}
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
        
        <div style={{
          position: 'absolute',
          inset: 0,
          background: 'linear-gradient(to top, rgba(0,0,0,0.9), rgba(0,0,0,0.4), transparent)',
          opacity: 1
        }}>
          <div style={{
            position: 'absolute',
            bottom: 0,
            left: 0,
            right: 0,
            padding: '0.75rem',
            color: 'white'
          }}>
            <div style={{ fontSize: '0.75rem', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'rgba(255,255,255,0.7)' }}>
              {inscription.genesis_block_height ? `Block #${inscription.genesis_block_height}` : 'Smart Contract'}
            </div>
            <div style={{ fontSize: '0.875rem', fontWeight: 600, lineHeight: 1.375 }}>{headline}</div>
          </div>
        </div>

        <div style={{
          position: 'absolute',
          inset: 0,
          background: 'linear-gradient(to top, rgba(0,0,0,0.8), transparent, transparent)',
          opacity: isHovered ? 1 : 0,
          transition: 'opacity 0.2s'
        }}>
          <div style={{
            position: 'absolute',
            bottom: 0,
            left: 0,
            right: 0,
            padding: '0.75rem',
            color: 'white'
          }}>
            {detectionScore > 0.1 && detectionPercent > 0 && (
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.5rem' }}>
                <div style={{ width: '100%', backgroundColor: 'rgba(74, 222, 128, 1)', borderRadius: '9999px', height: 4 }}>
                  <div 
                    style={{ backgroundColor: 'rgba(22, 163, 74, 1)', height: 4, borderRadius: '9999px', width: `${detectionPercent}%` }}
                  ></div>
                </div>
                <span style={{ fontSize: '0.75rem', fontWeight: 600 }}>
                  {detectionPercent}%
                </span>
              </div>
            )}
          </div>
        </div>
        
        <div style={{
          position: 'absolute',
          top: '0.5rem',
          left: '0.5rem',
          display: 'flex',
          flexDirection: 'column',
          gap: '0.25rem'
        }}>
          {inscription.contract_type === 'Steganographic Contract' && (
            <div style={{
              padding: '0.25rem 0.5rem',
              background: 'linear-gradient(to right, #9333ea, #7c3aed)',
              color: 'white',
              fontSize: '0.75rem',
              borderRadius: '9999px',
              fontWeight: 600,
              boxShadow: '0 10px 15px -3px rgba(0,0,0,0.1)'
            }}>
              🔐 STEGO
            </div>
          )}
          {hasTextContent && (
            <button
              onClick={handleTextPreviewToggle}
              style={{
                padding: '0.25rem 0.5rem',
                background: 'linear-gradient(to right, #2563eb, #1d4ed8)',
                color: 'white',
                fontSize: '0.75rem',
                borderRadius: '9999px',
                fontWeight: 600,
                boxShadow: '0 10px 15px -3px rgba(0,0,0,0.1)',
                border: 'none',
                cursor: 'pointer'
              }}
              title={showTextPreview ? "Hide text preview" : "Show text preview"}
            >
              {showTextPreview ? "📝 Hide" : "📄 Text"}
            </button>
          )}
        </div>
        
        {sizeBytes > 0 && (
          <div style={{
            position: 'absolute',
            bottom: '0.5rem',
            right: '0.5rem',
            backgroundColor: 'white',
            borderRadius: '0.5rem',
            padding: '0.25rem 0.5rem',
            boxShadow: '0 10px 15px -3px rgba(0,0,0,0.1)',
            border: '1px solid rgba(229, 231, 235, 1)'
          }}>
            <span style={{ color: 'black', fontSize: '0.75rem', fontWeight: 700 }}>
              {(sizeBytes / 1024).toFixed(1)}KB
            </span>
          </div>
        )}
      </div>
      
      <div className="mt-2 space-y-1">
        <div className="flex items-center justify-between gap-2">
          <div className="text-xs text-secondary text-gray-500 dark:text-gray-400 font-mono text-[9px] truncate font-medium flex-1" title={inscription.id}>
            ID: {inscription.id}
          </div>
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
