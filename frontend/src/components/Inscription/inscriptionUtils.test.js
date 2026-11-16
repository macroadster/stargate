import { looksLikeBitcoinAddress } from './inscriptionUtils';

describe('looksLikeBitcoinAddress', () => {
  it('accepts bech32 testnet and mainnet addresses', () => {
    expect(looksLikeBitcoinAddress('tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx')).toBe(true);
    expect(looksLikeBitcoinAddress('bc1qw508d6qejxtdg4y5r3zarvary0c5xw7k5xslslaks')).toBe(true);
  });

  it('accepts legacy base58 addresses', () => {
    expect(looksLikeBitcoinAddress('1BoatSLRHtKNngkdXEeobR76b53LETtpyT')).toBe(true);
    expect(looksLikeBitcoinAddress('2MzQwSSnBXPEqUEBZfnsMf3VhGj6ufNVfUM')).toBe(true);
    expect(looksLikeBitcoinAddress('mn8mBEj8jFHCZdQq6Gv7g2U58Ws6B6R6jB')).toBe(true);
  });

  it('rejects empty and junk values', () => {
    expect(looksLikeBitcoinAddress('')).toBe(false);
    expect(looksLikeBitcoinAddress('stargate-builtin-agent')).toBe(false);
    expect(looksLikeBitcoinAddress('mock-txid')).toBe(false);
  });
});
