import React, { useState } from 'react';
import { X } from 'lucide-react';
import CopyButton from '../Common/CopyButton';
import ConfidenceIndicator from '../Common/ConfidenceIndicator';

const InscriptionModal = ({ inscription, onClose }) => {
  const [activeTab, setActiveTab] = useState('overview');
  const [monoContent, setMonoContent] = useState(true);
  
  const isActuallyImageFile =
    inscription.mime_type?.includes('image') &&
    !inscription.image_url?.endsWith('.txt') &&
    (inscription.image_url || inscription.thumbnail);
  const modalImageSource = isActuallyImageFile ? (inscription.thumbnail || inscription.image_url) : null;
  const mime = (inscription.mime_type || '').toLowerCase();
  const isHtmlContent = mime.includes('text/html') || mime.includes('application/xhtml');
  const isSvgContent = mime === 'image/svg+xml' || (mime.includes('svg') && mime.includes('xml'));
  const sandboxSrc = inscription.image_url || inscription.thumbnail;
  const inlineDoc = (isHtmlContent || isSvgContent) ? (inscription.text || '') : '';
  
  const markdownContent = `# Steganographic Smart Contract Analysis

## Contract Identity
- **Contract ID**: \`${inscription.contract_id || inscription.id}\`
- **Block Height**: ${inscription.block_height || inscription.genesis_block_height || 'Unknown'}
- **Transaction ID**: \`${inscription.metadata?.transaction_id || 'Not available'}\`
- **Deployment Date**: ${inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toLocaleDateString() : 'Unknown'}

## Technical Architecture
- **Contract Type**: ${inscription.contract_type || inscription.contractType || 'Steganographic'}
- **Protocol Layer**: ${inscription.protocol || 'BRC-20'}
- **Data Capability**: ${inscription.capability || 'Data Storage & Concealment'}
- **MIME Type**: ${inscription.mime_type || 'Unknown'}

## Steganographic Specifications
- **Detection Method**: ${inscription.metadata?.detection_method || 'AI-Powered Analysis'}
- **Steganography Type**: ${inscription.metadata?.stego_type || 'Unknown'}
- **Confidence Level**: ${inscription.metadata?.confidence ? Math.round(inscription.metadata.confidence * 100) + '%' : 'N/A'}
- **Probability Score**: ${inscription.metadata?.stego_probability ? Math.round(inscription.metadata.stego_probability * 100) + '%' : 'N/A'}

## Media Properties
- **Image Format**: ${inscription.metadata?.image_format || 'Unknown'}
- **File Size**: ${inscription.metadata?.image_size ? (inscription.metadata.image_size / 1024).toFixed(2) + ' KB' : 'Unknown'}
- **Image Index**: ${inscription.metadata?.image_index || 'Unknown'}
- **Encoding Method**: ${inscription.metadata?.stego_type || 'Analysis Required'}

## Extracted Intelligence
${inscription.metadata?.extracted_message ? `\`\`\`\n${inscription.metadata.extracted_message}\n\`\`\`` : 'No hidden message detected'}

## Blockchain Integration
- **Block Hash**: \`${inscription.metadata?.block_hash || 'Unknown'}\`
- **Network**: Bitcoin Mainnet
- **Consensus**: Proof of Work
- **Timestamp**: ${inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toISOString() : 'Unknown'}

---

*Analysis performed by Steganography Detection System*`;

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg max-w-5xl w-full mx-4 min-h-[80vh] max-h-[85vh] overflow-hidden flex flex-col shadow-2xl">
        <div className="sticky top-0 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 p-4 flex-shrink-0">
          <div className="flex justify-between items-center">
            <h2 className="text-xl font-bold text-black dark:text-white">Smart Contract Details</h2>
            <button onClick={onClose} className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200">
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        <div className="p-4 flex-1 overflow-y-auto overflow-x-hidden">
            <div className="flex flex-col lg:flex-row gap-6 mb-6">
              <div className="flex-shrink-0">
                {modalImageSource ? (
                  <div className="relative">
                    <img
                      src={modalImageSource}
                      alt={inscription.file_name || inscription.id}
                      className="w-48 h-48 object-cover rounded-lg border-2 border-gray-300 dark:border-gray-700"
                    />
                    {inscription.metadata?.confidence && inscription.metadata.confidence > 0 && (
                      <div className="absolute top-2 right-2 bg-green-500 text-white text-xs px-2 py-1 rounded-md font-bold">
                        {Math.round(inscription.metadata.confidence * 100)}%
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="w-48 h-48 bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 rounded-lg flex items-center justify-center border-2 border-gray-300 dark:border-gray-700">
                    <div className="text-6xl text-center">
                      {inscription.contract_type === 'Steganographic Contract' ? '🎨' :
                       inscription.mime_type?.includes('text') ? '📄' :
                       inscription.mime_type?.includes('image') ? '🖼️' : '📦'}
                    </div>
                  </div>
                )}
              </div>

              <div className="flex-1">
              <div className="border-b border-gray-200 dark:border-gray-700 mb-6">
                <div className="flex gap-6 relative">
                  {[
                    { id: 'overview', label: 'Details', icon: '📋' },
                    { id: 'content', label: 'Content', icon: '📄' },
                    { id: 'blockchain', label: 'Blockchain', icon: '⛓️' }
                  ].map((tab) => (
                    <button
                      key={tab.id}
                      onClick={() => setActiveTab(tab.id)}
                      className={`px-4 py-2 font-medium text-sm border-b-2 transition-colors flex items-center gap-2 whitespace-nowrap ${
                        activeTab === tab.id
                          ? 'border-indigo-500 text-indigo-600 dark:text-indigo-400'
                          : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
                      }`}
                    >
                      <span>{tab.icon}</span>
                      {tab.label}
                    </button>
                  ))}
                </div>
              </div>

              {activeTab === 'overview' && (
                <div className="space-y-6">
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-purple-500 rounded-full"></span>
                      Inscription Identity
                    </h4>
                    <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-4 space-y-3">
                      <div className="space-y-2">
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex items-start gap-2">
                            <span className="text-gray-600 dark:text-gray-400 text-sm whitespace-nowrap">Inscription ID:</span>
                            <span className="text-black dark:text-white font-mono text-xs break-all leading-tight">{inscription.id}</span>
                          </div>
                          <CopyButton text={inscription.id} />
                        </div>
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex items-start gap-2">
                            <span className="text-gray-600 dark:text-gray-400 text-sm whitespace-nowrap">Transaction ID:</span>
                            <span className="text-black dark:text-white font-mono text-xs break-all leading-tight">{inscription.metadata?.transaction_id || inscription.id}</span>
                          </div>
                          <CopyButton text={inscription.metadata?.transaction_id || inscription.id} />
                        </div>
                      </div>
                      <div className="flex items-center gap-4">
                        <div className="flex items-center gap-2">
                          <span className="text-gray-600 dark:text-gray-400 text-sm">Block Height:</span>
                          <span className="text-black dark:text-white font-semibold">{inscription.block_height || inscription.genesis_block_height || 'Unknown'}</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <span className="text-gray-600 dark:text-gray-400 text-sm">Type:</span>
                          <span className="text-black dark:text-white font-semibold">{inscription.mime_type?.split('/')[1]?.toUpperCase() || 'UNKNOWN'}</span>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                      File Information
                    </h4>
                    <div className="grid grid-cols-2 gap-4">
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">File Name</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.file_name || 'N/A'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">File Size</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.size_bytes ? `${(inscription.size_bytes / 1024).toFixed(2)} KB` : 'Unknown'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Content Type</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.mime_type || 'Unknown'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Contract Type</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.contract_type || 'Standard'}</div>
                      </div>
                    </div>
                  </div>

                  {inscription.metadata?.is_stego && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-yellow-500 rounded-full"></span>
                        Steganographic Analysis
                      </h4>
                      <div className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4">
                        <div className="grid grid-cols-2 gap-4">
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Detection Status</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">Steganography Detected</div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Confidence Level</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{Math.round((inscription.metadata.confidence || 0) * 100)}%</div>
                          </div>
                          {inscription.metadata.stego_type && (
                            <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                              <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Method</div>
                              <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.stego_type.toUpperCase()}</div>
                            </div>
                          )}
                          {inscription.metadata.extracted_message && (
                            <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                              <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Hidden Message</div>
                              <div className="text-yellow-900 dark:text-yellow-100 font-semibold">Available</div>
                            </div>
                          )}
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'content' && (
                <div className="space-y-6">
                  {(isHtmlContent || isSvgContent) && (sandboxSrc || inlineDoc) && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-indigo-500 rounded-full"></span>
                        Sandboxed Preview
                      </h4>
                      <div className="rounded-lg border border-indigo-200 dark:border-indigo-700 overflow-hidden bg-gray-50 dark:bg-gray-900">
                        <iframe
                          title="inscription-sandbox"
                          src={sandboxSrc || undefined}
                          srcDoc={sandboxSrc ? undefined : inlineDoc}
                          sandbox=""
                          referrerPolicy="no-referrer"
                          className="w-full min-h-[420px] bg-white"
                        />
                      </div>
                      <div className="text-xs text-gray-600 dark:text-gray-400 mt-2">
                        Rendered in an isolated sandbox (scripts/DOM access blocked).
                      </div>
                    </div>
                  )}

                  {inscription.text && !(isHtmlContent || isSvgContent) && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                        Text Content
                      </h4>
                      <div className="bg-blue-50 dark:bg-blue-900 border border-blue-200 dark:border-blue-700 rounded-lg p-4">
                        <div className="flex items-center justify-between mb-2">
                          <div className="flex items-start gap-2">
                            <span className="text-blue-600 dark:text-blue-400 text-sm">📄</span>
                            <span className="text-blue-800 dark:text-blue-200 text-sm font-medium">Inscription Text Data</span>
                          </div>
                          <CopyButton text={inscription.text} />
                        </div>
                        <div className="flex items-center gap-3 mb-3">
                          <label className="flex items-center gap-2 text-sm text-blue-800 dark:text-blue-200">
                            <input
                              type="checkbox"
                              checked={monoContent}
                              onChange={() => setMonoContent(!monoContent)}
                              className="form-checkbox h-4 w-4 text-blue-600"
                            />
                            Monospace
                          </label>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded p-4 max-h-96 min-h-[200px] overflow-y-auto w-full">
                          <pre className={`${monoContent ? 'font-mono text-sm' : 'font-sans text-sm'} text-blue-900 dark:text-blue-100 leading-relaxed whitespace-pre-wrap break-words max-w-full`}>
                            {inscription.text}
                          </pre>
                        </div>
                        <div className="mt-4 pt-4 border-t border-blue-200 dark:border-blue-700">
                          <div className="grid grid-cols-3 gap-4 w-full">
                            <div className="text-center">
                              <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">
                                {inscription.text.length}
                              </div>
                              <div className="text-sm text-blue-700 dark:text-blue-300">Characters</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">
                                {inscription.text.split(' ').filter(word => word.length > 0).length}
                              </div>
                              <div className="text-sm text-blue-700 dark:text-blue-300">Words</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">
                                {inscription.text.split('\n').length}
                              </div>
                              <div className="text-sm text-blue-700 dark:text-blue-300">Lines</div>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {inscription.metadata?.extracted_message && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                        Hidden Message
                      </h4>
                        <div className="bg-green-50 dark:bg-green-900 border border-green-200 dark:border-green-700 rounded-lg p-4">
                          <div className="flex items-center justify-between mb-2">
                            <div className="flex items-start gap-2">
                              <span className="text-green-600 dark:text-green-400 text-sm">🔓</span>
                              <span className="text-green-800 dark:text-green-200 text-sm font-medium">Extracted Hidden Data</span>
                            </div>
                            <CopyButton text={inscription.metadata.extracted_message} />
                          </div>
                        <div className="bg-white dark:bg-gray-800 rounded p-4 max-h-96 min-h-[200px] overflow-y-auto w-full">
                            <pre className="text-green-900 dark:text-green-100 font-mono text-sm leading-relaxed whitespace-pre-wrap break-words max-w-full">
                              {inscription.metadata.extracted_message}
                            </pre>
                          </div>
                        <div className="mt-4 pt-4 border-t border-green-200 dark:border-green-700">
                          <div className="grid grid-cols-3 gap-4 w-full">
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {inscription.metadata.extracted_message.length}
                              </div>
                              <div className="text-sm text-green-700 dark:text-green-300">Characters</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {inscription.metadata.extracted_message.split(' ').filter(word => word.length > 0).length}
                              </div>
                              <div className="text-sm text-green-700 dark:text-green-300">Words</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {inscription.metadata.extracted_message.split('\n').length}
                              </div>
                              <div className="text-sm text-green-700 dark:text-green-300">Lines</div>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {!inscription.text && !inscription.metadata?.extracted_message && (
                    <div className="text-center py-12">
                      <div className="text-6xl mb-4">📦</div>
                      <div className="text-gray-600 dark:text-gray-400 font-semibold">No Text Content Available</div>
                      <div className="text-gray-500 dark:text-gray-500 text-sm mt-2">
                        This inscription contains binary data or media content that cannot be displayed as text.
                      </div>
                    </div>
                  )}
                </div>
              )}

              {activeTab === 'technical' && (
                <div className="space-y-6">
                  {inscription.metadata?.extracted_message ? (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                        Extracted Hidden Message
                      </h4>
                      <div className="bg-gradient-to-br from-green-50 to-green-100 dark:from-green-900 dark:to-green-800 border border-green-200 dark:border-green-700 rounded-lg p-6">
                        <div className="flex items-start gap-3 mb-4">
                          <div className="w-8 h-8 bg-green-500 rounded-full flex items-center justify-center flex-shrink-0">
                            <span className="text-white text-lg">🔓</span>
                          </div>
                          <div>
                            <div className="text-green-900 dark:text-green-100 font-semibold text-lg">Successfully Decoded Message</div>
                            <div className="text-green-700 dark:text-green-300 text-sm">Hidden data extracted from steganographic carrier</div>
                          </div>
                        </div>
                        
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-4 border border-green-300 dark:border-green-600">
                          <div className="flex items-center justify-between mb-2">
                            <div className="text-green-800 dark:text-green-200 text-xs font-mono uppercase tracking-wider">Hidden Content:</div>
                            <CopyButton text={inscription.metadata.extracted_message} />
                          </div>
                           <div className="bg-gray-50 dark:bg-gray-900 rounded p-3 max-h-64 overflow-y-auto">
                             <pre className="text-green-900 dark:text-green-100 font-mono text-sm leading-relaxed whitespace-pre-wrap break-words max-w-full">
                               {inscription.metadata.extracted_message}
                             </pre>
                           </div>
                        </div>

                        <div className="mt-4 pt-4 border-t border-green-200 dark:border-green-700">
                          <div className="grid grid-cols-2 gap-4">
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {inscription.metadata.extracted_message.length}
                              </div>
                              <div className="text-sm text-green-700 dark:text-green-300">Characters</div>
                            </div>
                            <div className="text-center">
                              <div className="text-2xl font-bold text-green-600 dark:text-green-400">
                                {inscription.metadata.extracted_message.split(' ').length}
                              </div>
                              <div className="text-sm text-green-700 dark:text-green-300">Words</div>
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  ) : (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-gray-500 rounded-full"></span>
                        Hidden Message Analysis
                      </h4>
                      <div className="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg p-6">
                        <div className="text-center">
                          <div className="text-6xl mb-4">🔍</div>
                          <div className="text-gray-600 dark:text-gray-400 font-semibold">No Hidden Message Detected</div>
                          <div className="text-gray-500 dark:text-gray-500 text-sm mt-2">
                            This contract may not contain extractable hidden data, or the message may be encoded using a different method.
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {inscription.metadata && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                        Message Analysis Details
                      </h4>
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-blue-50 dark:bg-blue-900 border border-blue-200 dark:border-blue-700 rounded-lg p-4">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">Encoding Method</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">
                            {inscription.metadata.stego_type?.includes('lsb') ? 'Least Significant Bit (LSB)' : 
                             inscription.metadata.stego_type?.includes('alpha') ? 'Alpha Channel' : 'Unknown'}
                          </div>
                          <div className="text-blue-600 dark:text-blue-400 text-xs mt-2">
                            {inscription.metadata.stego_type?.includes('lsb') ? 'Data hidden in image pixel values' : 
                             inscription.metadata.stego_type?.includes('alpha') ? 'Data hidden in transparency channel' : 'Unknown encoding method'}
                          </div>
                        </div>
                        <div className="bg-purple-50 dark:bg-purple-900 border border-purple-200 dark:border-purple-700 rounded-lg p-4">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Detection Confidence</div>
                          <ConfidenceIndicator confidence={inscription.metadata.confidence} />
                           <div className="text-purple-600 dark:text-purple-400 text-xs">
                             {inscription.metadata?.confidence ? 
                              (inscription.metadata.confidence >= 0.9 ? 'High confidence detection' :
                               inscription.metadata.confidence >= 0.7 ? 'Medium confidence detection' : 'Low confidence detection') :
                              'Analysis required for confidence assessment'}
                           </div>
                        </div>
                      </div>
                    </div>
                  )}

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-orange-500 rounded-full"></span>
                      Technical Architecture
                    </h4>
                    <div className="bg-gray-100 dark:bg-gray-900 rounded-lg p-4 max-h-64 overflow-y-auto">
                      <pre className="text-gray-700 dark:text-gray-300 text-sm whitespace-pre-wrap font-mono leading-relaxed max-w-full break-words">
                        {markdownContent}
                      </pre>
                    </div>
                  </div>
                </div>
              )}

              {activeTab === 'analysis' && (
                <div className="space-y-6">
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-yellow-500 rounded-full"></span>
                      Steganographic Analysis Report
                    </h4>
                    <div className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4">
                      <div className="flex items-center gap-2 mb-4">
                        <div className="w-3 h-3 bg-yellow-500 rounded-full animate-pulse"></div>
                        <span className="text-yellow-800 dark:text-yellow-200 font-medium">Analysis Complete - Hidden Data Detected</span>
                      </div>
                      <p className="text-yellow-700 dark:text-yellow-300 mb-4 leading-relaxed">
                        This smart contract contains embedded data patterns consistent with advanced steganographic techniques. 
                        Steganographic analysis has identified patterns within the carrier medium.
                      </p>
                      
                      {inscription.metadata && (
                        <div className="grid grid-cols-2 gap-4 mt-6">
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Detection Algorithm</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.detection_method || 'Analysis Required'}</div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Steganography Type</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.stego_type || 'Unknown'}</div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Carrier Format</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.image_format || 'Unknown'}</div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3 border border-yellow-300 dark:border-yellow-600">
                            <div className="text-yellow-700 dark:text-yellow-300 text-xs mb-1">Data Payload</div>
                            <div className="text-yellow-900 dark:text-yellow-100 font-semibold">{inscription.metadata.image_size || 'Unknown'} bytes</div>
                          </div>
                        </div>
                      )}
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-cyan-500 rounded-full"></span>
                      Analysis Timeline
                    </h4>
                    <div className="bg-cyan-50 dark:bg-cyan-900 border border-cyan-200 dark:border-cyan-700 rounded-lg p-4">
                      <div className="space-y-3">
                        <div className="flex items-center gap-3">
                          <div className="w-2 h-2 bg-cyan-500 rounded-full"></div>
                          <div className="flex-1">
                            <div className="text-cyan-900 dark:text-cyan-100 font-medium">Image Extraction</div>
                            <div className="text-cyan-700 dark:text-cyan-300 text-sm">Successfully extracted image from transaction witness data</div>
                          </div>
                        </div>
                        <div className="flex items-center gap-3">
                          <div className="w-2 h-2 bg-cyan-500 rounded-full"></div>
                          <div className="flex-1">
                            <div className="text-cyan-900 dark:text-cyan-100 font-medium">Pattern Analysis</div>
                            <div className="text-cyan-700 dark:text-cyan-300 text-sm">Applied steganographic analysis algorithms</div>
                          </div>
                        </div>
                        <div className="flex items-center gap-3">
                          <div className="w-2 h-2 bg-green-500 rounded-full"></div>
                          <div className="flex-1">
                            <div className="text-cyan-900 dark:text-cyan-100 font-medium">Message Extraction</div>
                            <div className="text-cyan-700 dark:text-cyan-300 text-sm">Successfully decoded hidden message from carrier</div>
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {activeTab === 'blockchain' && (
                <div className="space-y-6">
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-purple-500 rounded-full"></span>
                      Transaction Information
                    </h4>
                    <div className="bg-purple-50 dark:bg-purple-900 border border-purple-200 dark:border-purple-700 rounded-lg p-4">
                      <div className="space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-purple-700 dark:text-purple-300 text-sm">Transaction ID</span>
                          <div className="flex items-center gap-2">
                            <span className="text-purple-900 dark:text-purple-100 font-mono text-xs">
                              {inscription.id?.slice(0, 12)}...
                            </span>
                            <CopyButton text={inscription.id || ''} />
                          </div>
                        </div>
                        <div className="flex items-center justify-between">
                          <span className="text-purple-700 dark:text-purple-300 text-sm">Block Height</span>
                          <span className="text-purple-900 dark:text-purple-100 font-semibold">
                            {inscription.block_height || inscription.genesis_block_height || 'Unknown'}
                          </span>
                        </div>
                        <div className="flex items-center justify-between">
                          <span className="text-purple-700 dark:text-purple-300 text-sm">Network</span>
                          <span className="text-purple-900 dark:text-purple-100 font-semibold">Bitcoin Mainnet</span>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                      File Details
                    </h4>
                    <div className="bg-blue-50 dark:bg-blue-900 border border-blue-200 dark:border-blue-700 rounded-lg p-4">
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">File Name</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">{inscription.file_name || 'N/A'}</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">File Size</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">{inscription.size_bytes ? `${(inscription.size_bytes / 1024).toFixed(2)} KB` : 'Unknown'}</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">Content Type</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">{inscription.mime_type || 'Unknown'}</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-blue-700 dark:text-blue-300 text-xs mb-1">Contract Type</div>
                          <div className="text-blue-900 dark:text-blue-100 font-semibold">{inscription.contract_type || 'Standard'}</div>
                        </div>
                      </div>
                    </div>
                  </div>

                  {inscription.metadata?.scanned_at && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                        Analysis Information
                      </h4>
                      <div className="bg-green-50 dark:bg-green-900 border border-green-200 dark:border-green-700 rounded-lg p-4">
                        <div className="grid grid-cols-2 gap-4">
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-green-700 dark:text-green-300 text-xs mb-1">Scan Status</div>
                            <div className="text-green-900 dark:text-green-100 font-semibold">
                              {inscription.metadata.is_stego ? 'Steganography Detected' : 'No Hidden Data'}
                            </div>
                          </div>
                          <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                            <div className="text-green-700 dark:text-green-300 text-xs mb-1">Last Scanned</div>
                            <div className="text-green-900 dark:text-green-100 font-semibold">
                              {new Date(inscription.metadata.scanned_at * 1000).toLocaleString()}
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
               )}
             </div>
           </div>
         </div>
       </div>
     </div>
  );
};

export default InscriptionModal;
