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
  const [filterMode, setFilterMode] = useState('all'); // 'all' or 'text'
  const [lastFetchedHeight, setLastFetchedHeight] = useState(null);
  const [nextCursor, setNextCursor] = useState(null);
  const [isLoading, setIsLoading] = useState(false);

  const fetchInscriptions = useCallback(async (cursor = null) => {
    if (!selectedBlock || !selectedBlock.height) {
      setInscriptions([]);
      setCurrentInscriptions([]);
      setAllInscriptions([]);
      setHasMoreImages(false);
      setTotalImages(0);
      setLastFetchedHeight(null);
      setNextCursor(null);
      return;
    }
    // Do not fetch for pending or known-empty blocks; clear state so UI can show empty state.
    if (selectedBlock.isFuture || selectedBlock.has_images === false) {
      setInscriptions([]);
      setCurrentInscriptions([]);
      setAllInscriptions([]);
      setHasMoreImages(false);
      setTotalImages(0);
      setLastFetchedHeight(selectedBlock.height);
      setNextCursor(null);
      return;
    }
    if (isLoading) return;
    if (!cursor && lastFetchedHeight === selectedBlock.height) return;
    if (!cursor && lastFetchedHeight !== selectedBlock.height) {
      setAllInscriptions([]);
      setInscriptions([]);
      setNextCursor(null);
    }
    
    try {
      setIsLoading(true);
      const url = new URL(`${API_BASE}/api/data/block-inscriptions/${selectedBlock.height}`);
      url.searchParams.set('limit', 20);
      url.searchParams.set('fields', 'summary');
      if (filterMode === 'text') {
        url.searchParams.set('filter', 'text');
      }
      if (cursor) {
        url.searchParams.set('cursor', cursor);
      }

      const response = await fetch(url.toString());
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      const data = await response.json();

      const images = data.inscriptions || [];

      const convertedResults = await Promise.all(images.map(async (image) => {
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

      const merged = cursor ? [...allInscriptions, ...processedInscriptions] : processedInscriptions;
      setCurrentInscriptions(convertedResults);
      setAllInscriptions(merged);
      
      // Apply filter if needed
      const filteredInscriptions = filterMode === 'text' 
        ? merged.filter(ins => ins.text || ins.metadata?.extracted_message)
        : merged;
      
      setTotalImages(filteredInscriptions.length);
      setHasMoreImages(Boolean(data.has_more));
      setInscriptions(filteredInscriptions);
      setLastFetchedHeight(selectedBlock.height);
      setNextCursor(data.next_cursor || null);
    } catch (error) {
      if (String(error?.message || '').includes('404')) {
        // Historical/pinned block not available; treat as empty without spamming logs
        setInscriptions([]);
        setCurrentInscriptions([]);
        setAllInscriptions([]);
        setTotalImages(0);
        setHasMoreImages(false);
        setLastFetchedHeight(selectedBlock.height);
        setNextCursor(null);
        setIsLoading(false);
        return;
      }
      console.error('Error fetching block images:', error);
      setInscriptions([]);
      setCurrentInscriptions([]);
      setTotalImages(0);
      setHasMoreImages(false);
    } finally {
      setIsLoading(false);
    }
  }, [selectedBlock, filterMode, lastFetchedHeight, isLoading, allInscriptions]);

  const loadMoreInscriptions = () => {
    if (!hasMoreImages || !nextCursor) return;
    fetchInscriptions(nextCursor);
  };

  const setFilter = (mode) => {
    setFilterMode(mode);
    const filteredInscriptions = mode === 'text' 
      ? allInscriptions.filter(ins => ins.text || ins.metadata?.extracted_message)
      : allInscriptions;
    
    setTotalImages(filteredInscriptions.length);
    setHasMoreImages(filteredInscriptions.length > 0);
    setInscriptions(filteredInscriptions);
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
    loadMoreInscriptions,
    setFilter,
    filterMode
  };
};
