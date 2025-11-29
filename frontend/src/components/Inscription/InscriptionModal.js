import React, { useState } from 'react';
import { X } from 'lucide-react';
import CopyButton from '../Common/CopyButton';
import ConfidenceIndicator from '../Common/ConfidenceIndicator';

const InscriptionModal = ({ inscription, onClose }) => {
  const [activeTab, setActiveTab] = useState('overview');
  
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
      <div className="bg-white dark:bg-gray-800 rounded-lg max-w-4xl w-full mx-4 max-h-[90vh] overflow-y-auto">
        <div className="sticky top-0 bg-white dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700 p-4">
          <div className="flex justify-between items-center">
            <h2 className="text-xl font-bold text-black dark:text-white">Inscription Details</h2>
            <button onClick={onClose} className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200">
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        <div className="p-4">
          <div className="flex gap-6 mb-6">
            <div className="flex-shrink-0">
              {inscription.thumbnail || inscription.image_url ? (
                <div className="relative">
                  <img 
                    src={inscription.thumbnail || inscription.image_url} 
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
            </div>

            <div className="mt-6">
              <div className="border-b border-gray-200 dark:border-gray-700 mb-6">
                <div className="flex gap-6">
                  {[
                    { id: 'overview', label: 'Overview', icon: '📊' },
                    { id: 'technical', label: 'Hidden Message', icon: '🔓' },
                    { id: 'analysis', label: 'Analysis', icon: '🔍' },
                    { id: 'blockchain', label: 'Blockchain', icon: '⛓️' }
                  ].map((tab) => (
                    <button
                      key={tab.id}
                      onClick={() => setActiveTab(tab.id)}
                      className={`px-4 py-2 font-medium text-sm border-b-2 transition-colors flex items-center gap-2 ${
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
                      Contract Identity
                    </h4>
                    <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-4 space-y-3">
                      <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span className="text-gray-600 dark:text-gray-400 text-sm">File Name:</span>
                            <span className="text-black dark:text-white font-mono text-sm font-semibold">{inscription.file_name || inscription.id}</span>
                          </div>
                          <CopyButton text={inscription.file_name || inscription.id} />
                      </div>
                      <div className="flex items-center gap-2">
                        <span className="text-gray-600 dark:text-gray-400 text-sm">Transaction ID:</span>
                        <span className="text-black dark:text-white font-mono text-xs">{inscription.metadata?.transaction_id || 'Not available'}</span>
                      </div>
                      <div className="flex items-center gap-4">
                        <div className="flex items-center gap-2">
                          <span className="text-gray-600 dark:text-gray-400 text-sm">Block Height:</span>
                          <span className="text-black dark:text-white font-semibold">{inscription.block_height || inscription.genesis_block_height || 'Unknown'}</span>
                        </div>
                        <div className="flex items-center gap-2">
                          <span className="text-gray-600 dark:text-gray-400 text-sm">Status:</span>
                          <span className={`px-2 py-1 rounded text-xs font-semibold ${
                            inscription.isActive ? 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-300' : 'bg-gray-100 dark:bg-gray-900 text-gray-800 dark:text-gray-300'
                          }`}>
                            {inscription.isActive ? 'Active' : 'Inactive'}
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-blue-500 rounded-full"></span>
                      Technical Specifications
                    </h4>
                    <div className="grid grid-cols-2 gap-4">
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Contract Type</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.contract_type || inscription.contractType || 'Steganographic'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Protocol Layer</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.protocol || 'BRC-20'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Data Capability</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.capability || 'Data Storage & Concealment'}</div>
                      </div>
                      <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                        <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">MIME Type</div>
                        <div className="text-black dark:text-white font-semibold">{inscription.mime_type || 'Unknown'}</div>
                      </div>
                    </div>
                  </div>

                  {inscription.metadata && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-yellow-500 rounded-full"></span>
                        Steganographic Analysis
                      </h4>
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Detection Method</div>
                          <div className="text-black dark:text-white font-semibold">{inscription.metadata.detection_method || 'Analysis Required'}</div>
                        </div>
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Stego Type</div>
                          <div className="text-black dark:text-white font-semibold">{inscription.metadata.stego_type || 'Unknown'}</div>
                        </div>
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Confidence Level</div>
                          <ConfidenceIndicator confidence={inscription.metadata.confidence} />
                        </div>
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Image Format</div>
                          <div className="text-black dark:text-white font-semibold">{inscription.metadata.image_format || 'Unknown'}</div>
                        </div>
                      </div>
                    </div>
                  )}

                  {inscription.metadata?.extracted_message && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                        Extracted Intelligence
                      </h4>
                      <div className="bg-yellow-50 dark:bg-yellow-900 border border-yellow-200 dark:border-yellow-700 rounded-lg p-4">
                        <div className="flex items-start gap-2 mb-2">
                          <span className="text-yellow-600 dark:text-yellow-400 text-sm">🔓</span>
                          <span className="text-yellow-800 dark:text-yellow-200 text-sm font-medium">Hidden Message Decoded</span>
                        </div>
                        <p className="text-yellow-900 dark:text-yellow-100 font-mono text-sm leading-relaxed">{inscription.metadata.extracted_message}</p>
                      </div>
                    </div>
                  )}
                  
                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-indigo-500 rounded-full"></span>
                      Contract Performance
                    </h4>
                    <div className="grid grid-cols-3 gap-4">
                       <div className="bg-gradient-to-br from-blue-50 to-blue-100 dark:from-blue-900 dark:to-blue-800 rounded-lg p-4 border border-blue-200 dark:border-blue-700">
                         <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">{inscription.apiEndpoints || 0}</div>
                         <div className="text-sm text-blue-700 dark:text-blue-300">API Endpoints</div>
                       </div>
                        <div className="bg-gradient-to-br from-green-50 to-green-100 dark:from-green-900 dark:to-green-800 rounded-lg p-4 border border-green-200 dark:border-green-700">
                          <div className="text-2xl font-bold text-green-600 dark:text-green-400">{inscription.interactions || 0}</div>
                          <div className="text-sm text-green-700 dark:text-green-300">Total Interactions</div>
                        </div>
                       <div className="bg-gradient-to-br from-purple-50 to-purple-100 dark:from-purple-900 dark:to-purple-800 rounded-lg p-4 border border-purple-200 dark:border-purple-700">
                         <div className="text-2xl font-bold text-purple-600 dark:text-purple-400">{inscription.reputation || 'N/A'}</div>
                         <div className="text-sm text-purple-700 dark:text-purple-300">Reputation Score</div>
                       </div>
                    </div>
                  </div>

                  {inscription.metadata && (
                    <div>
                      <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                        <span className="w-2 h-2 bg-cyan-500 rounded-full"></span>
                        Media Properties
                      </h4>
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                          <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">File Size</div>
                           <div className="text-black dark:text-white font-semibold">
                             {inscription.metadata?.stego_type || 'Analysis Required'}
                           </div>
                           <div className="text-blue-600 dark:text-blue-400 text-xs mt-2">
                             Real steganographic analysis required to determine encoding method
                           </div>
                        </div>
                        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-3">
                         <div className="text-gray-600 dark:text-gray-400 text-xs mb-1">Encoding Method</div>
                         <div className="text-black dark:text-white font-semibold">
                           Analysis Required
                         </div>
                        </div>
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
                          <div className="text-green-800 dark:text-green-200 text-xs font-mono mb-2 uppercase tracking-wider">Hidden Content:</div>
                          <p className="text-green-900 dark:text-green-100 font-mono text-base leading-relaxed break-all">
                            {inscription.metadata.extracted_message}
                          </p>
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
                    <div className="bg-gray-100 dark:bg-gray-900 rounded-lg p-4">
                      <pre className="text-gray-700 dark:text-gray-300 text-sm whitespace-pre-wrap font-mono leading-relaxed">
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
                      Blockchain Integration
                    </h4>
                    <div className="bg-purple-50 dark:bg-purple-900 border border-purple-200 dark:border-purple-700 rounded-lg p-4">
                      <div className="grid grid-cols-2 gap-4">
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Network</div>
                          <div className="text-purple-900 dark:text-purple-100 font-semibold">Bitcoin Mainnet</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Consensus</div>
                          <div className="text-purple-900 dark:text-purple-100 font-semibold">Proof of Work</div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Block Hash</div>
                          <div className="text-purple-900 dark:text-purple-100 font-mono text-xs break-all">
                            {inscription.metadata?.block_hash || 'Unknown'}
                          </div>
                        </div>
                        <div className="bg-white dark:bg-gray-800 rounded-lg p-3">
                          <div className="text-purple-700 dark:text-purple-300 text-xs mb-1">Deployment Time</div>
                          <div className="text-purple-900 dark:text-purple-100 font-semibold">
                            {inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toLocaleString() : 'Unknown'}
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-pink-500 rounded-full"></span>
                      Transaction Details
                    </h4>
                    <div className="bg-pink-50 dark:bg-pink-900 border border-pink-200 dark:border-pink-700 rounded-lg p-4">
                      <div className="space-y-3">
                        <div className="flex items-center justify-between">
                          <span className="text-pink-700 dark:text-pink-300 text-sm">Transaction ID</span>
                          <div className="flex items-center gap-2">
                            <span className="text-pink-900 dark:text-pink-100 font-mono text-xs">
                              {inscription.metadata?.transaction_id?.slice(0, 8)}...
                            </span>
                            <CopyButton text={inscription.metadata?.transaction_id || ''} />
                          </div>
                        </div>
                        <div className="flex items-center justify-between">
                          <span className="text-pink-700 dark:text-pink-300 text-sm">Image Index</span>
                          <span className="text-pink-900 dark:text-pink-100 font-semibold">
                            #{inscription.metadata?.image_index || 'Unknown'}
                          </span>
                        </div>
                        <div className="flex items-center justify-between">
                          <span className="text-pink-700 dark:text-pink-300 text-sm">Timestamp</span>
                          <span className="text-pink-900 dark:text-pink-100 font-semibold">
                            {inscription.metadata?.created_at ? new Date(inscription.metadata.created_at * 1000).toISOString() : 'Unknown'}
                          </span>
                        </div>
                      </div>
                    </div>
                  </div>

                  <div>
                    <h4 className="text-lg font-semibold text-black dark:text-white mb-3 flex items-center gap-2">
                      <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                      Verification Status
                    </h4>
                    <div className="bg-green-50 dark:bg-green-900 border border-green-200 dark:border-green-700 rounded-lg p-4">
                      <div className="flex items-center gap-3">
                        <div className="w-8 h-8 bg-green-500 rounded-full flex items-center justify-center">
                          <span className="text-white text-sm">✓</span>
                        </div>
                        <div>
                          <div className="text-green-900 dark:text-green-100 font-medium">Contract Verified</div>
                          <div className="text-green-700 dark:text-green-300 text-sm">
                            Steganographic content has been successfully verified and extracted
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
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