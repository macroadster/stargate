import { useState, useEffect } from 'react';

const generateInscriptions = (inscriptions) => {
  return inscriptions.map((insc, i) => ({
    id: insc.id,
    type: insc.mime_type?.split('/')[1]?.toUpperCase() || 'UNKNOWN',
    thumbnail: insc.mime_type?.startsWith('image/') ? `http://localhost:3001${insc.image_url}` : null,
    gradient: 'from-indigo-500',
    hasMultiple: false,
    contractType: insc.contract_type || 'Steganographic Contract',
    capability: insc.capability || 'Data Storage & Concealment',
    protocol: insc.protocol || 'BRC-20',
    apiEndpoints: 0,
    interactions: 0,
    reputation: 'N/A',
    isActive: true,
    number: insc.number,
    address: insc.address,
    genesis_block_height: insc.genesis_block_height,
    mime_type: insc.mime_type,
    file_name: insc.file_name,
    file_path: insc.file_path,
    size_bytes: insc.size_bytes,
    image_url: insc.image_url,
    metadata: insc.metadata
  }));
};

export const useInscriptions = (selectedBlock) => {
  const [inscriptions, setInscriptions] = useState([]);
  const [currentInscriptions, setCurrentInscriptions] = useState([]);
  const [hasMoreImages, setHasMoreImages] = useState(true);
  const [totalImages, setTotalImages] = useState(0);
  const [displayedCount, setDisplayedCount] = useState(20);

  const fetchInscriptions = async () => {
    if (!selectedBlock) return;
    
    try {
      const response = await fetch(
        `http://localhost:3001/api/block-images?height=${selectedBlock.height}`
      );
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = await response.json();
      
      const images = data.images || [];
      
      console.log(`Fetched ${images.length} block images for height ${selectedBlock.height}`);
      
      const convertedResults = images.map(image => {
        const hasEnhancedScanData = data.steganography_scan && data.steganography_scan.scan_results;
        let scanResult = null;
        
        if (hasEnhancedScanData) {
          const imageScanResult = data.steganography_scan.scan_results.find(
            scan => scan.tx_id === image.tx_id && scan.image_index === image.input_index
          );
          
          if (imageScanResult) {
            scanResult = {
              is_stego: imageScanResult.is_stego || false,
              confidence: imageScanResult.confidence || 0.0,
              stego_probability: imageScanResult.stego_probability || 0.0,
              prediction: imageScanResult.prediction || 'clean',
              stego_type: imageScanResult.stego_type || '',
              extracted_message: imageScanResult.extracted_message || '',
              scan_error: imageScanResult.scan_error || '',
              scanned_at: imageScanResult.scanned_at
            };
          }
        }
        
        if (!scanResult) {
          scanResult = {
            is_stego: false,
            confidence: 0.0,
            stego_probability: 0.0,
            prediction: 'unanalyzed',
            stego_type: '',
            extracted_message: '',
            scan_error: 'Analysis not performed',
            scanned_at: Date.now() / 1000
          };
        }
        
        return {
          id: image.tx_id,
          number: selectedBlock.height,
          address: 'bc1p...',
          mime_type: image.content_type || 'application/octet-stream',
          genesis_block_height: selectedBlock.height,
          text: image.content || '',
          contract_type: scanResult.is_stego ? 'Steganographic Contract' : 'Inscription',
          file_name: image.file_name,
          file_path: image.file_path,
          size_bytes: image.size_bytes,
          image_url: `http://localhost:3001/api/block-image/${selectedBlock.height}/${image.file_name}`,
          metadata: {
            confidence: scanResult.confidence,
            extracted_message: scanResult.extracted_message,
            image_format: image.content_type?.split('/')[1] || 'unknown',
            image_size: image.size_bytes,
            stego_type: scanResult.stego_type,
            detection_method: 'Analysis Required',
            stego_probability: scanResult.stego_probability,
            prediction: scanResult.prediction,
            scanned_at: scanResult.scanned_at,
            is_stego: scanResult.is_stego
          }
        };
      });
      
      const processedInscriptions = generateInscriptions(convertedResults);
      
      setCurrentInscriptions(convertedResults);
      setTotalImages(processedInscriptions.length);
      setDisplayedCount(20);
      setHasMoreImages(processedInscriptions.length > 20);
      
      setInscriptions(processedInscriptions.slice(0, 20));
      
      console.log(`Loaded ${processedInscriptions.length} total inscriptions, displaying first 20`);
    } catch (error) {
      console.error('Error fetching block images:', error);
      setInscriptions([]);
      setCurrentInscriptions([]);
      setTotalImages(0);
      setHasMoreImages(false);
    }
  };

  const loadMoreInscriptions = () => {
    if (hasMoreImages && currentInscriptions.length > displayedCount) {
      const newDisplayedCount = Math.min(displayedCount + 20, currentInscriptions.length);
      setDisplayedCount(newDisplayedCount);
      setInscriptions(currentInscriptions.slice(0, newDisplayedCount));
      setHasMoreImages(newDisplayedCount < currentInscriptions.length);
      
      console.log(`Displaying ${newDisplayedCount} of ${currentInscriptions.length} inscriptions`);
    }
  };

  useEffect(() => {
    if (selectedBlock) {
      fetchInscriptions();
    }
  }, [selectedBlock]);

  return {
    inscriptions,
    hasMoreImages,
    totalImages,
    displayedCount,
    loadMoreInscriptions
  };
};