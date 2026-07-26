import { describe, it, expect } from 'vitest';
import {
  emptyDraft,
  parseUserMessage,
  respondToUser,
  isDraftReady,
  missingFields,
  buildInscribePayload,
  formatAltPrice,
  FUNDING_RAISE,
  FUNDING_PAYOUT,
} from './wishChatBot';

describe('parseUserMessage', () => {
  it('extracts sats price and wish text', () => {
    const { draftPatch, intent } = parseUserMessage(
      'Build a pixel cat game for 50000 sats',
    );
    expect(draftPatch.price).toBe('50000');
    expect(draftPatch.priceUnit).toBe('sats');
    expect(draftPatch.message).toMatch(/pixel cat/i);
    expect(['set_message', 'update']).toContain(intent);
  });

  it('extracts btc price', () => {
    const { draftPatch } = parseUserMessage('price 0.001 btc');
    expect(draftPatch.price).toBe('0.001');
    expect(draftPatch.priceUnit).toBe('btc');
  });

  it('detects raise fund mode', () => {
    const { draftPatch } = parseUserMessage('raise fund for community art');
    expect(draftPatch.fundingMode).toBe(FUNDING_RAISE);
    expect(draftPatch.message).toMatch(/community art/i);
  });

  it('handles slash price command', () => {
    const { draftPatch, intent } = parseUserMessage('/price 1200 sats');
    expect(intent).toBe('set_price');
    expect(draftPatch.price).toBe('1200');
    expect(draftPatch.priceUnit).toBe('sats');
  });

  it('detects confirm intent', () => {
    expect(parseUserMessage('yes').intent).toBe('confirm');
    expect(parseUserMessage('inscribe').intent).toBe('confirm');
  });

  it('detects help and reset', () => {
    expect(parseUserMessage('help').intent).toBe('help');
    expect(parseUserMessage('reset').intent).toBe('reset');
  });
});

describe('draft readiness', () => {
  it('requires message, price, and address', () => {
    const d = emptyDraft('');
    expect(isDraftReady(d)).toBe(false);
    expect(missingFields(d)).toEqual(
      expect.arrayContaining(['wish text', 'price', 'wallet address (sign in)']),
    );

    d.message = 'Ship a game';
    d.price = '1000';
    d.address = 'tb1qtest';
    expect(isDraftReady(d)).toBe(true);
    expect(missingFields(d)).toEqual([]);
  });
});

describe('respondToUser', () => {
  it('welcomes structured updates and asks for missing fields', () => {
    const draft = emptyDraft('tb1qabc');
    const { draft: next, phase, messages } = respondToUser({
      userText: 'Make a logo for 25000 sats',
      draft,
    });
    expect(next.message).toMatch(/logo/i);
    expect(next.price).toBe('25000');
    expect(phase).toBe('ready');
    expect(messages.some((m) => /ready|yes/i.test(m.content))).toBe(true);
  });

  it('returns submitting phase on confirm when ready', () => {
    const draft = {
      ...emptyDraft('tb1qabc'),
      message: 'Do the thing',
      price: '100',
      priceUnit: 'sats',
      fundingMode: FUNDING_PAYOUT,
    };
    const { phase } = respondToUser({ userText: 'yes', draft });
    expect(phase).toBe('submitting');
  });

  it('blocks confirm when incomplete', () => {
    const draft = emptyDraft('tb1qabc');
    const { phase, messages } = respondToUser({ userText: 'yes', draft });
    expect(phase).toBe('collecting');
    expect(messages[0].content).toMatch(/still need/i);
  });
});

describe('buildInscribePayload', () => {
  it('shapes API payload from draft', () => {
    const payload = buildInscribePayload({
      message: '  Hello wish  ',
      price: '1234.9',
      priceUnit: 'sats',
      address: ' tb1qxyz ',
      fundingMode: FUNDING_RAISE,
    });
    expect(payload).toEqual({
      message: 'Hello wish',
      price: '1234',
      price_unit: 'sats',
      address: 'tb1qxyz',
      funding_mode: FUNDING_RAISE,
      method: 'alpha',
    });
  });
});

describe('formatAltPrice', () => {
  it('converts sats to btc display', () => {
    expect(formatAltPrice('100000000', 'sats')).toBe('1.00000000 BTC');
  });
});
