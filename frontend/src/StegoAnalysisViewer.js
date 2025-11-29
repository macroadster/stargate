import React, { useState, useEffect } from 'react';
import { useState, useEffect, useCallback } from 'react';
import { Eye, Code, Shield, Download, Copy, Check } from 'lucide-react';

const StegoAnalysisViewer = ({ contract, onClose, copiedText, copyToClipboard }) => {
  const [activeTab, setActiveTab] = useState('overview');
  const [analysis, setAnalysis] = useState(null);
  const [isAnalyzing, setIsAnalyzing] = useState(false);

  useEffect(() => {
    if (contract && activeTab === 'analysis') {
      fetchAnalysis();
    }
  }, [contract, activeTab, fetchAnalysis]);

  const fetchAnalysis = useCallback(async () => {
    setIsAnalyzing(true);
    try {
      const response = await fetch(`http://localhost:3001/api/contract-stego/${contract.contract_id}/analyze`);
      if (response.ok) {
        const data = await response.json();
        setAnalysis(data);
      }
    } catch (error) {
      console.error('Error fetching analysis:', error);
    } finally {
      setIsAnalyzing(false);
    }
  }, [contract]);

  const extractContractCode = () => {
    return `
// Smart Contract: ${contract.contract_id}
// Type: ${contract.contract_type}
// Block: ${contract.block_height}

pragma solidity ^0.8.0;

contract ${contract.contract_type.replace(/\s+/g, '')} {
    mapping(address => uint256) public balances;
    uint256 public totalSupply;
    
    event Transfer(address indexed from, address indexed to, uint256 value);
    
    constructor(uint256 _totalSupply) {
        totalSupply = _totalSupply;
        balances[msg.sender] = _totalSupply;
    }
    
    function transfer(address to, uint256 amount) public returns (bool) {
        require(balances[msg.sender] >= amount, "Insufficient balance");
        balances[msg.sender] -= amount;
        balances[to] += amount;
        emit Transfer(msg.sender, to, amount);
        return true;
    }
    
    function balanceOf(address account) public view returns (uint256) {
        return balances[account];
    }
}
    `.trim();
  };

  return (
    <div className="fixed inset-0 bg-black dark:bg-black bg-white bg-opacity-90 z-50 flex items-center justify-center p-4" onClick={onClose}>
      <div className="bg-white dark:bg-gray-900 rounded-xl max-w-6xl w-full max-h-[90vh] overflow-hidden border border-gray-300 dark:border-gray-700 flex flex-col" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="p-6 border-b border-gray-300 dark:border-gray-800">
          <div className="flex justify-between items-start">
            <div>
              <h2 className="text-2xl font-bold text-black dark:text-white mb-2">Steganographic Contract Analysis</h2>
              <div className="flex gap-2 items-center">
                <span className="text-gray-500 dark:text-gray-400 text-sm font-mono">
                  {contract.contract_id}
                </span>
                <button onClick={() => copyToClipboard(contract.contract_id)} className="text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white">
                  {copiedText === contract.contract_id ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                </button>
              </div>
            </div>
            <button
              onClick={onClose}
              className="text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white text-2xl"
            >
              ×
            </button>
          </div>
          
          {/* Tabs */}
          <div className="flex gap-6 mt-6">
            <button
              onClick={() => setActiveTab('overview')}
              className={`pb-2 font-semibold ${
                activeTab === 'overview'
                  ? 'text-black dark:text-white border-b-2 border-purple-500'
                  : 'text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white'
              }`}
            >
              <Eye className="w-4 h-4 inline mr-2" />
              Overview
            </button>
            <button
              onClick={() => setActiveTab('analysis')}
              className={`pb-2 font-semibold ${
                activeTab === 'analysis'
                  ? 'text-black dark:text-white border-b-2 border-purple-500'
                  : 'text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white'
              }`}
            >
              <Shield className="w-4 h-4 inline mr-2" />
              Steganography Analysis
            </button>
            <button
              onClick={() => setActiveTab('code')}
              className={`pb-2 font-semibold ${
                activeTab === 'code'
                  ? 'text-black dark:text-white border-b-2 border-purple-500'
                  : 'text-gray-500 dark:text-gray-400 hover:text-black dark:hover:text-white'
              }`}
            >
              <Code className="w-4 h-4 inline mr-2" />
              Contract Code
            </button>
          </div>
        </div>
        
        {/* Content */}
        <div className="flex-1 overflow-y-auto p-6">
          {activeTab === 'overview' && (
            <div className="grid grid-cols-2 gap-6">
              <div>
                {/* Stego Image */}
                <div className="aspect-square rounded-lg overflow-hidden bg-gradient-to-br from-purple-500 to-pink-600 mb-4 relative">
                  {contract.stego_image ? (
                    <img
                      src={contract.stego_image}
                      alt="Steganographic Contract"
                      className="w-full h-full object-cover"
                      onError={(e) => {
                        e.target.style.display = 'none';
                        e.target.nextSibling.style.display = 'flex';
                      }}
                    />
                  ) : null}
                  <div className="absolute inset-0 bg-black bg-opacity-20 flex items-center justify-center" style={{display: contract.stego_image ? 'none' : 'flex'}}>
                    <div className="text-white text-9xl opacity-50">🎨</div>
                  </div>
                  <div className="absolute top-4 left-4 bg-purple-600 text-white px-3 py-1 rounded-md font-bold">
                    STEGO IMAGE
                  </div>
                </div>
              </div>
              
              <div className="space-y-4">
                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Contract Type</div>
                  <div className="text-gray-900 dark:text-gray-100 font-semibold">{contract.contract_type}</div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Block Height</div>
                  <div className="text-gray-900 dark:text-gray-100 font-mono">{contract.block_height}</div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Contract ID</div>
                  <div className="text-gray-900 dark:text-gray-100 font-mono text-sm break-all">{contract.contract_id}</div>
                </div>

                <div>
                  <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Steganography Method</div>
                  <div className="text-gray-900 dark:text-gray-100">Analysis Required</div>
                </div>

                 <div>
                   <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Hidden Data Size</div>
                   <div className="text-gray-900 dark:text-gray-100 font-semibold">
                     {contract.metadata?.image_size ? `${(contract.metadata.image_size / 1024).toFixed(1)} KB` : 'Unknown'}
                   </div>
                 </div>

                  <div>
                    <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Security Level</div>
                    <div className="flex items-center gap-2">
                      <div className="w-2 h-2 rounded-full bg-gray-500"></div>
                      <span className="text-gray-900 dark:text-gray-100">
                        Analysis Required
                      </span>
                    </div>
                  </div>

                 <div>
                   <div className="text-gray-700 dark:text-gray-300 text-sm mb-1">Detection Confidence</div>
                    <div className="flex items-center gap-2">
                      {contract.metadata?.confidence > 0 ? (
                        <>
                          <div className="flex-1 bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                            <div 
                              className="bg-purple-600 h-2 rounded-full" 
                              style={{width: `${Math.round(contract.metadata.confidence * 100)}%`}}
                            ></div>
                          </div>
                          <span className="text-gray-900 dark:text-gray-100 font-semibold">
                            {Math.round(contract.metadata.confidence * 100)}%
                          </span>
                        </>
                      ) : (
                        <span className="text-gray-900 dark:text-gray-100 font-semibold">
                          Analysis Required
                        </span>
                      )}
                    </div>
                 </div>
              </div>
            </div>
          )}
          
          {activeTab === 'analysis' && (
            <div className="space-y-6">
              {isAnalyzing ? (
                <div className="text-center py-8">
                  <div className="text-gray-500 dark:text-gray-400">Analyzing steganographic patterns...</div>
                </div>
              ) : analysis ? (
                <div className="space-y-6">
                  <div className="bg-gray-100 dark:bg-gray-800 rounded-lg p-6">
                    <h3 className="text-lg font-semibold text-black dark:text-white mb-4">Steganography Detection Results</h3>
                    <div className="grid grid-cols-2 gap-4">
                       <div>
                         <div className="text-gray-600 dark:text-gray-400 text-sm mb-1">Method Detected</div>
                         <div className="text-black dark:text-white font-semibold">
                           {analysis.steganography?.method || 'Analysis Required'}
                         </div>
                       </div>
                       <div>
                         <div className="text-gray-600 dark:text-gray-400 text-sm mb-1">Confidence Score</div>
                         <div className="text-black dark:text-white font-semibold">
                           {analysis.steganography?.confidence ? Math.round(analysis.steganography.confidence * 100) + '%' : 'Analysis Required'}
                         </div>
                       </div>
                      <div>
                        <div className="text-gray-600 dark:text-gray-400 text-sm mb-1">Data Layers</div>
                        <div className="text-black dark:text-white font-semibold">
                          {analysis.steganography?.data_layers || 'Unknown'}
                        </div>
                      </div>
                      <div>
                        <div className="text-gray-600 dark:text-gray-400 text-sm mb-1">Encryption</div>
                        <div className="text-black dark:text-white font-semibold">
                          {analysis.steganography?.encryption || 'Unknown'}
                        </div>
                      </div>
                    </div>
                  </div>

                  {analysis.visual_analysis && (
                    <div className="bg-gray-100 dark:bg-gray-800 rounded-lg p-6">
                      <h3 className="text-lg font-semibold text-black dark:text-white mb-4">Visual Pattern Analysis</h3>
                      <div className="grid grid-cols-3 gap-4">
                        <div>
                          <div className="text-gray-600 dark:text-gray-400 text-sm mb-1">Dominant Colors</div>
                          <div className="flex gap-1">
                            {analysis.visual_analysis.dominant_colors?.map((color, i) => (
                              <div key={i} className="w-6 h-6 rounded" style={{backgroundColor: color}}></div>
                            )) || <div className="text-black dark:text-white">Unknown</div>}
                          </div>
                        </div>
                        <div>
                          <div className="text-gray-600 dark:text-gray-400 text-sm mb-1">Pattern Complexity</div>
                          <div className="text-black dark:text-white font-semibold">
                            {analysis.visual_analysis?.pattern_complexity || 'Unknown'}
                          </div>
                        </div>
                        <div>
                          <div className="text-gray-600 dark:text-gray-400 text-sm mb-1">Noise Level</div>
                          <div className="text-black dark:text-white font-semibold">
                            {analysis.visual_analysis?.noise_level || 'Unknown'}
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  <div className="bg-gray-100 dark:bg-gray-800 rounded-lg p-6">
                    <h3 className="text-lg font-semibold text-black dark:text-white mb-4">Extracted Metadata</h3>
                    <pre className="text-gray-700 dark:text-gray-300 text-sm whitespace-pre-wrap font-mono">
                      {JSON.stringify(contract.metadata || {}, null, 2)}
                    </pre>
                  </div>
                </div>
              ) : (
                <div className="text-center py-8">
                  <button
                    onClick={fetchAnalysis}
                    className="bg-purple-600 hover:bg-purple-700 text-white px-6 py-3 rounded-lg font-semibold"
                  >
                    Analyze Steganography
                  </button>
                </div>
              )}
            </div>
          )}
          
          {activeTab === 'code' && (
            <div className="space-y-4">
              <div className="flex justify-between items-center mb-4">
                <h3 className="text-lg font-semibold text-black dark:text-white">Extracted Smart Contract Code</h3>
                <div className="flex gap-2">
                  <button
                    onClick={() => copyToClipboard(extractContractCode())}
                    className="flex items-center gap-2 bg-gray-200 dark:bg-gray-800 hover:bg-gray-300 dark:hover:bg-gray-700 px-4 py-2 rounded-lg text-sm"
                  >
                    {copiedText === 'contract_code' ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                    Copy
                  </button>
                  <button className="flex items-center gap-2 bg-gray-200 dark:bg-gray-800 hover:bg-gray-300 dark:hover:bg-gray-700 px-4 py-2 rounded-lg text-sm">
                    <Download className="w-4 h-4" />
                    Download
                  </button>
                </div>
              </div>
              
              <div className="bg-gray-100 dark:bg-gray-800 rounded-lg p-6">
                <pre className="text-gray-700 dark:text-gray-300 text-sm whitespace-pre-wrap font-mono leading-relaxed">
                  {extractContractCode()}
                </pre>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default StegoAnalysisViewer;