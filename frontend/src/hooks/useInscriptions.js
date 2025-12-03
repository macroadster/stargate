import { useState, useEffect, useCallback } from 'react';
import { API_BASE } from '../apiBase';

const generateInscriptions = (inscriptions) => {
  return inscriptions.map((insc, i) => ({
    id: insc.id,
    type: insc.mime_type?.split('/')[1]?.toUpperCase() || 'UNKNOWN',
    thumbnail: insc.mime_type?.startsWith('image/') ? insc.image_url : null,
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
    text: insc.text, // Preserve the text content!
    metadata: insc.metadata
  }));
};

export const useInscriptions = (selectedBlock) => {
  const [inscriptions, setInscriptions] = useState([]);
  const [currentInscriptions, setCurrentInscriptions] = useState([]);
  const [allInscriptions, setAllInscriptions] = useState([]);
  const [hasMoreImages, setHasMoreImages] = useState(true);
  const [totalImages, setTotalImages] = useState(0);
  const [displayedCount, setDisplayedCount] = useState(20);
  const [filterMode, setFilterMode] = useState('all'); // 'all' or 'text'
  const [lastFetchedHeight, setLastFetchedHeight] = useState(null);

  const fetchInscriptions = useCallback(async () => {
    if (!selectedBlock || selectedBlock.isFuture || !selectedBlock.height) {
      setInscriptions([]);
      setCurrentInscriptions([]);
      setAllInscriptions([]);
      setHasMoreImages(false);
      setTotalImages(0);
      setDisplayedCount(0);
      setLastFetchedHeight(null);
      return;
    }
    if (selectedBlock.height === lastFetchedHeight) return;
    
    try {
      const response = await fetch(
        `${API_BASE}/api/block-images?height=${selectedBlock.height}`
      );
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = await response.json();
      
      const images = data.images || [];
      
      console.log(`Fetched ${images.length} block images for height ${selectedBlock.height}`);
      
       const convertedResults = await Promise.all(images.map(async (image) => {
         // Use scan_result directly from the image object
         // NOTE: Scan results are embedded directly in each image object by the backend
         // rather than in a separate steganography_scan section for simplicity
         let scanResult = null;

         if (image.scan_result) {
           scanResult = {
             is_stego: image.scan_result.is_stego || false,
             confidence: image.scan_result.confidence || 0.0,
             stego_probability: image.scan_result.stego_probability || 0.0,
             prediction: image.scan_result.prediction || 'clean',
             stego_type: image.scan_result.stego_type || '',
             extracted_message: image.scan_result.extracted_message || '',
             scan_error: image.scan_result.scan_error || '',
             scanned_at: image.scan_result.scanned_at || Date.now() / 1000
           };
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
        let textContent = image.content || '';

        // For text inscriptions without inline content, fetch the text payload
        if (!textContent && (image.content_type || '').startsWith('text/')) {
          try {
            const resp = await fetch(`${API_BASE}/api/block-image/${selectedBlock.height}/${image.file_name}`);
            if (resp.ok) {
              textContent = await resp.text();
            }
          } catch (fetchErr) {
            console.error('Failed to fetch text content for', image.file_name, fetchErr);
          }
        }
        
        return {
          id: image.tx_id,
          number: selectedBlock.height,
          address: 'bc1p...',
          mime_type: image.content_type || 'application/octet-stream',
          genesis_block_height: selectedBlock.height,
          text: textContent || '',
          contract_type: scanResult.is_stego ? 'Steganographic Contract' : 'Inscription',
          file_name: image.file_name,
          file_path: image.file_path,
          size_bytes: image.size_bytes,
          image_url: `${API_BASE}/api/block-image/${selectedBlock.height}/${image.file_name}`,
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
      }));
      
      const processedInscriptions = generateInscriptions(convertedResults);
      
      setCurrentInscriptions(convertedResults);
      setAllInscriptions(processedInscriptions);
      
      // Apply filter if needed
      const filteredInscriptions = filterMode === 'text' 
        ? processedInscriptions.filter(ins => ins.text || ins.metadata?.extracted_message)
        : processedInscriptions;
      
      setTotalImages(filteredInscriptions.length);
      setDisplayedCount(20);
      setHasMoreImages(filteredInscriptions.length > 20);
      
      setInscriptions(filteredInscriptions.slice(0, 20));
      setLastFetchedHeight(selectedBlock.height);
      
      console.log(`Loaded ${processedInscriptions.length} total inscriptions, displaying first 20`);
    } catch (error) {
      console.error('Error fetching block images:', error);
      setInscriptions([]);
      setCurrentInscriptions([]);
      setTotalImages(0);
      setHasMoreImages(false);
    }
  }, [selectedBlock, filterMode, lastFetchedHeight]);

  const loadMoreInscriptions = () => {
    const sourceInscriptions = filterMode === 'text' 
      ? allInscriptions.filter(ins => ins.text || ins.metadata?.extracted_message)
      : allInscriptions;
      
    if (hasMoreImages && sourceInscriptions.length > displayedCount) {
      const newDisplayedCount = Math.min(displayedCount + 20, sourceInscriptions.length);
      setDisplayedCount(newDisplayedCount);
      setInscriptions(sourceInscriptions.slice(0, newDisplayedCount));
      setHasMoreImages(newDisplayedCount < sourceInscriptions.length);
      
      console.log(`Displaying ${newDisplayedCount} of ${sourceInscriptions.length} inscriptions`);
    }
  };

  const setFilter = (mode) => {
    console.log('Setting filter to:', mode);
    setFilterMode(mode);
    const filteredInscriptions = mode === 'text' 
      ? allInscriptions.filter(ins => ins.text || ins.metadata?.extracted_message)
      : allInscriptions;
    
    console.log('Filtered inscriptions count:', filteredInscriptions.length);
    setTotalImages(filteredInscriptions.length);
    setDisplayedCount(Math.min(20, filteredInscriptions.length));
    setHasMoreImages(filteredInscriptions.length > 20);
    setInscriptions(filteredInscriptions.slice(0, 20));
  };

  useEffect(() => {
    if (selectedBlock) {
      fetchInscriptions();
    }
  }, [selectedBlock, fetchInscriptions]);

  return {
    inscriptions,
    currentInscriptions,
    allInscriptions,
    hasMoreImages,
    totalImages,
    displayedCount,
    loadMoreInscriptions,
    setFilter,
    filterMode
  };
};
