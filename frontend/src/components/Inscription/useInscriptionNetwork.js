import { useState, useEffect } from 'react';
import { apiFetch } from '../../utils/api';
import { guessNetworkFromAddress } from './inscriptionUtils';

/** Resolve Bitcoin network from wallet/inscription metadata with optional health check. */
export function useInscriptionNetwork(inscription, wallet) {
  const [network, setNetwork] = useState(
    inscription?.metadata?.network ||
      inscription?.network ||
      (inscription?.contract_type?.toLowerCase().includes('testnet') ? 'testnet' : 'testnet4')
  );

  useEffect(() => {
    const walletGuess =
      guessNetworkFromAddress(wallet) ||
      guessNetworkFromAddress(inscription?.metadata?.funding_address) ||
      guessNetworkFromAddress(inscription?.metadata?.address) ||
      guessNetworkFromAddress(inscription?.metadata?.contractor_wallet);
    const metaNetwork =
      inscription?.metadata?.network ||
      inscription?.network ||
      (inscription?.contract_type?.toLowerCase().includes('testnet') ? 'testnet4' : '');
    const localNetwork = walletGuess || metaNetwork || 'testnet4';
    setNetwork(localNetwork);

    let cancelled = false;
    const fetchNetwork = async () => {
      try {
        const response = await apiFetch('/bitcoin/v1/health');
        if (response.ok) {
          const data = await response.json();
          if (!cancelled && !walletGuess) {
            setNetwork(data.network || localNetwork || 'testnet4');
          }
        }
      } catch (error) {
        console.error('Failed to fetch network info:', error);
      }
    };
    fetchNetwork();
    return () => { cancelled = true; };
  }, [inscription, wallet]);

  return [network, setNetwork];
}
