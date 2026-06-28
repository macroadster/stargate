import React from 'react';
import CopyButton from '../Common/CopyButton';

/**
 * Content tab panel for InscriptionModal — displays text, metadata, and stego scan.
 */
const InscriptionContentTab = ({
  inscription,
  monoContent,
  setMonoContent,
  stegoPayload,
  stegoPayloadLoading,
  stegoPayloadError,
  scanMessage,
  scanLoading,
  scanError,
  onScan,
}) => {
  const textContent =
    inscription?.text_content ||
    inscription?.message ||
    inscription?.metadata?.wish_text ||
    '';

  return (
    <div className="space-y-6">
      {textContent && (
        <div className="modal-text-box">
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1">
              <div className="flex items-center justify-between">
                <span className="modal-data-label">Text Content</span>
                <CopyButton text={textContent} />
              </div>
              <pre className={monoContent ? 'font-mono text-sm whitespace-pre-wrap break-words' : 'text-sm whitespace-pre-wrap break-words'}>
                {textContent}
              </pre>
              <label className="flex items-center gap-2 text-xs opacity-70">
                <input
                  type="checkbox"
                  checked={monoContent}
                  onChange={(e) => setMonoContent(e.target.checked)}
                />
                Monospace
              </label>
            </div>
          </div>
        </div>
      )}
      {(stegoPayload || stegoPayloadLoading || stegoPayloadError || scanMessage || scanError) && (
        <div className="modal-text-box">
          <div className="flex items-center justify-between mb-2">
            <span className="modal-data-label">Hidden Message / Stego</span>
            {onScan && (
              <button type="button" className="btn btn-sm" onClick={onScan} disabled={scanLoading}>
                {scanLoading ? 'Scanning…' : 'Scan'}
              </button>
            )}
          </div>
          {stegoPayloadLoading && <div className="text-sm opacity-70">Loading payload…</div>}
          {stegoPayloadError && <div className="text-sm text-red-400">{stegoPayloadError}</div>}
          {scanError && <div className="text-sm text-red-400">{scanError}</div>}
          {scanMessage && <pre className="text-sm whitespace-pre-wrap break-words">{scanMessage}</pre>}
          {stegoPayload && (
            <pre className="font-mono text-xs whitespace-pre-wrap break-words">
              {typeof stegoPayload === 'string' ? stegoPayload : JSON.stringify(stegoPayload, null, 2)}
            </pre>
          )}
        </div>
      )}
    </div>
  );
};

export default InscriptionContentTab;
