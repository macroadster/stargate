/**
 * WishChat nanobot — conversation logic & parameter extraction for wish inscription.
 * Pure functions (no React) so they are easy to unit test.
 */

export const FUNDING_PAYOUT = 'payout';
export const FUNDING_RAISE = 'raise_fund';

export function emptyDraft(address = '') {
  return {
    message: '',
    price: '',
    priceUnit: 'sats',
    fundingMode: FUNDING_PAYOUT,
    address: address || '',
    imageFile: null,
    imagePreviewUrl: null,
  };
}

export function formatAltPrice(price, priceUnit) {
  const numeric = Number(price);
  if (!Number.isFinite(numeric) || price === '' || price === null || price === undefined) {
    return '';
  }
  if (priceUnit === 'sats') {
    return `${(numeric / 1e8).toFixed(8)} BTC`;
  }
  return `${Math.max(0, Math.trunc(numeric * 1e8))} sats`;
}

export function isDraftReady(draft) {
  const hasMessage = Boolean((draft.message || '').trim());
  const priceNum = Number(draft.price);
  const hasPrice = draft.price !== '' && Number.isFinite(priceNum) && priceNum > 0;
  const hasAddress = Boolean((draft.address || '').trim());
  return hasMessage && hasPrice && hasAddress;
}

export function missingFields(draft) {
  const missing = [];
  if (!(draft.message || '').trim()) missing.push('wish text');
  const priceNum = Number(draft.price);
  if (draft.price === '' || !Number.isFinite(priceNum) || priceNum <= 0) missing.push('price');
  if (!(draft.address || '').trim()) missing.push('wallet address (sign in)');
  return missing;
}

/**
 * Extract structured fields from free-form user text.
 * Returns { draftPatch, cleanedMessage, intent, notes }.
 */
export function parseUserMessage(text, currentDraft = {}) {
  const raw = String(text || '').trim();
  const notes = [];
  const draftPatch = {};
  let intent = 'chat'; // chat | confirm | cancel | reset | help | status
  let working = raw;

  if (!raw) {
    return { draftPatch, cleanedMessage: '', intent: 'empty', notes };
  }

  // Intents
  if (/^(help|\/help|\?|commands)$/i.test(raw.trim())) {
    return { draftPatch, cleanedMessage: raw, intent: 'help', notes };
  }
  if (/^(status|summary|review|draft|\/status)$/i.test(raw.trim())) {
    return { draftPatch, cleanedMessage: raw, intent: 'status', notes };
  }
  if (/^(reset|start over|clear|\/reset)$/i.test(raw.trim())) {
    return { draftPatch, cleanedMessage: raw, intent: 'reset', notes };
  }
  if (/^(cancel|nevermind|never mind|stop)$/i.test(raw.trim())) {
    return { draftPatch, cleanedMessage: raw, intent: 'cancel', notes };
  }
  if (
    /^(yes|y|yeah|yep|confirm|go|go ahead|do it|inscribe|create|submit|looks good|ok|okay|ship it|let'?s go)$/i.test(
      raw.trim(),
    )
  ) {
    return { draftPatch, cleanedMessage: raw, intent: 'confirm', notes };
  }
  if (/^(no|n|not yet|wait|hold on)$/i.test(raw.trim())) {
    return { draftPatch, cleanedMessage: raw, intent: 'defer', notes };
  }

  // Explicit slash commands
  const priceCmd = raw.match(/^\/price\s+(\d+(?:\.\d+)?)\s*(btc|sats?)?$/i);
  if (priceCmd) {
    draftPatch.price = priceCmd[1];
    const unit = (priceCmd[2] || '').toLowerCase();
    if (unit.startsWith('btc')) draftPatch.priceUnit = 'btc';
    else if (unit.startsWith('sat')) draftPatch.priceUnit = 'sats';
    notes.push(`Set price to ${draftPatch.price} ${draftPatch.priceUnit || currentDraft.priceUnit || 'sats'}`);
    return { draftPatch, cleanedMessage: '', intent: 'set_price', notes };
  }

  const modeCmd = raw.match(/^\/mode\s+(payout|raise[_ ]?fund|raise|fundraise|fundraising)$/i);
  if (modeCmd) {
    const m = modeCmd[1].toLowerCase();
    draftPatch.fundingMode = /raise|fund/.test(m) ? FUNDING_RAISE : FUNDING_PAYOUT;
    notes.push(
      draftPatch.fundingMode === FUNDING_RAISE
        ? 'Funding mode: raise fund from investors'
        : 'Funding mode: payout to contractors',
    );
    return { draftPatch, cleanedMessage: '', intent: 'set_mode', notes };
  }

  // Funding mode phrases
  if (/\b(raise\s*fund|fund\s*rais|fundraising|fundraise|collect from contractors)\b/i.test(working)) {
    draftPatch.fundingMode = FUNDING_RAISE;
    notes.push('Funding mode: raise fund from investors');
    working = working
      .replace(/\b(raise\s*fund(s|ing)?|fund\s*rais(e|ing)|fundraising|fundraise|collect from contractors)\b/gi, ' ')
      .trim();
  } else if (/\b(payout(\s+to\s+contractors)?|pay\s+contractors)\b/i.test(working)) {
    draftPatch.fundingMode = FUNDING_PAYOUT;
    notes.push('Funding mode: payout to contractors');
    working = working.replace(/\b(payout(\s+to\s+contractors)?|pay\s+contractors)\b/gi, ' ').trim();
  }

  // Price: "1000 sats", "0.001 btc", "price 5000", "budget: 0.01 BTC"
  const priceWithUnit = working.match(
    /(?:price|budget|pay|offer|reward|bounty)?\s*[:=]?\s*(\d+(?:\.\d+)?)\s*(btc|bitcoin|sats?|satoshis?)\b/i,
  );
  if (priceWithUnit) {
    draftPatch.price = priceWithUnit[1];
    const unit = priceWithUnit[2].toLowerCase();
    draftPatch.priceUnit = unit.startsWith('btc') || unit.startsWith('bitcoin') ? 'btc' : 'sats';
    notes.push(`Set price to ${draftPatch.price} ${draftPatch.priceUnit}`);
    working = working.replace(priceWithUnit[0], ' ').trim();
  } else {
    const barePrice = working.match(/(?:price|budget|pay|offer|reward|bounty)\s*[:=]?\s*(\d+(?:\.\d+)?)\b/i);
    if (barePrice) {
      draftPatch.price = barePrice[1];
      // Heuristic: integers >= 100 default to sats; small decimals to btc
      const n = Number(barePrice[1]);
      if (Number.isFinite(n) && n < 1) {
        draftPatch.priceUnit = 'btc';
      } else if (!currentDraft.priceUnit) {
        draftPatch.priceUnit = 'sats';
      }
      notes.push(`Set price to ${draftPatch.price} ${draftPatch.priceUnit || currentDraft.priceUnit || 'sats'}`);
      working = working.replace(barePrice[0], ' ').trim();
    }
  }

  // Clean leftover punctuation/spaces and dangling connectors after price extraction
  working = working
    .replace(/\s{2,}/g, ' ')
    .replace(/^[,.\-–—:\s]+|[,.\-–—:\s]+$/g, '')
    .replace(/\b(for|at|with|of|priced?)\s*$/i, '')
    .replace(/^[,.\-–—:\s]+|[,.\-–—:\s]+$/g, '')
    .trim();

  // Remaining text becomes / updates wish message (unless it was only structured)
  if (working.length > 0) {
    // Short connector phrases alone shouldn't overwrite message
    if (!/^(please|thanks|thank you|hi|hello|hey)$/i.test(working)) {
      draftPatch.message = working;
      notes.push('Captured wish text');
      intent = 'set_message';
    }
  }

  if (Object.keys(draftPatch).length > 0 && intent === 'chat') {
    intent = 'update';
  }

  return { draftPatch, cleanedMessage: working, intent, notes };
}

export function applyDraftPatch(draft, patch) {
  const next = { ...draft, ...patch };
  // Revoke old preview if image changes
  if (patch.imageFile !== undefined && draft.imagePreviewUrl && patch.imageFile !== draft.imageFile) {
    try {
      URL.revokeObjectURL(draft.imagePreviewUrl);
    } catch {
      /* ignore */
    }
  }
  if (patch.imageFile instanceof File) {
    next.imagePreviewUrl = URL.createObjectURL(patch.imageFile);
  } else if (patch.imageFile === null) {
    next.imagePreviewUrl = null;
  }
  return next;
}

export function draftSummaryLines(draft) {
  const lines = [];
  lines.push(`**Wish:** ${draft.message?.trim() || '_(not set)_'}`);
  if (draft.price !== '' && draft.price != null) {
    const alt = formatAltPrice(draft.price, draft.priceUnit);
    lines.push(`**Price:** ${draft.price} ${draft.priceUnit}${alt ? ` (≈ ${alt})` : ''}`);
  } else {
    lines.push('**Price:** _(not set)_');
  }
  lines.push(
    `**Funding:** ${
      draft.fundingMode === FUNDING_RAISE ? 'Raise fund from investors' : 'Payout to contractors'
    }`,
  );
  lines.push(`**Image:** ${draft.imageFile ? draft.imageFile.name : '_(none)_'}`);
  lines.push(`**Wallet:** ${draft.address?.trim() || '_(sign in required)_'}`);
  return lines;
}

/**
 * Build the bot's reply after a user turn.
 * @returns {{ messages: Array<{role, content, kind?}>, draft: object, phase: string }}
 */
export function respondToUser({ userText, draft, hasImageThisTurn = false }) {
  const messages = [];
  let nextDraft = { ...draft };
  const { draftPatch, intent, notes } = parseUserMessage(userText, draft);

  if (intent === 'empty' && !hasImageThisTurn) {
    messages.push({
      role: 'bot',
      content: 'Say a few words about your wish, drop an image, or type **help** for commands.',
    });
    return { messages, draft: nextDraft, phase: phaseFor(nextDraft) };
  }

  if (intent === 'help') {
    messages.push({
      role: 'bot',
      content: [
        "I'm **WishBot** — I'll gather what we need to inscribe your wish.",
        '',
        'You can write naturally, for example:',
        '> Build a pixel art cat game, price 1000 sats',
        '',
        '**Commands**',
        '- `/price 1000 sats` or `/price 0.00001 btc`',
        '- `/mode payout` or `/mode raise_fund`',
        '- `status` — show the draft',
        '- `reset` — clear and start over',
        '- `yes` / `inscribe` — submit when ready',
        '',
        'You can also **drag & drop** an image into this window.',
      ].join('\n'),
    });
    return { messages, draft: nextDraft, phase: phaseFor(nextDraft) };
  }

  if (intent === 'reset') {
    const address = draft.address;
    if (draft.imagePreviewUrl) {
      try {
        URL.revokeObjectURL(draft.imagePreviewUrl);
      } catch {
        /* ignore */
      }
    }
    nextDraft = emptyDraft(address);
    messages.push({
      role: 'bot',
      content: 'Draft cleared. Tell me your new wish — what should agents build or deliver?',
    });
    return { messages, draft: nextDraft, phase: 'collecting' };
  }

  if (intent === 'cancel') {
    messages.push({
      role: 'bot',
      content: 'No problem — draft is still here if you change your mind. Close the window anytime, or keep chatting.',
    });
    return { messages, draft: nextDraft, phase: phaseFor(nextDraft) };
  }

  if (intent === 'defer') {
    messages.push({
      role: 'bot',
      content: "Alright, take your time. Edit anything by chatting, or type **status** to review the draft.",
    });
    return { messages, draft: nextDraft, phase: phaseFor(nextDraft) };
  }

  // Apply structured patches from text
  if (Object.keys(draftPatch).length > 0) {
    nextDraft = applyDraftPatch(nextDraft, draftPatch);
  }

  if (intent === 'confirm') {
    const missing = missingFields(nextDraft);
    if (missing.length > 0) {
      messages.push({
        role: 'bot',
        content: `Almost — still need: **${missing.join(', ')}**. ${hintForMissing(missing)}`,
      });
      return { messages, draft: nextDraft, phase: 'collecting' };
    }
    messages.push({
      role: 'bot',
      content: 'Great — submitting your wish for inscription now…',
      kind: 'submit',
    });
    return { messages, draft: nextDraft, phase: 'submitting' };
  }

  if (intent === 'status') {
    messages.push({
      role: 'bot',
      content: ['Here is your current draft:', '', ...draftSummaryLines(nextDraft)].join('\n'),
      kind: 'summary',
    });
    if (isDraftReady(nextDraft)) {
      messages.push({
        role: 'bot',
        content: 'Looks complete. Reply **yes** or **inscribe** to submit, or keep editing.',
      });
    } else {
      const missing = missingFields(nextDraft);
      messages.push({
        role: 'bot',
        content: `Still missing: **${missing.join(', ')}**. ${hintForMissing(missing)}`,
      });
    }
    return { messages, draft: nextDraft, phase: phaseFor(nextDraft) };
  }

  // Normal update path
  if (notes.length > 0 || hasImageThisTurn) {
    const bits = [];
    if (hasImageThisTurn) bits.push('Got the image');
    if (notes.length) bits.push(notes.join('. '));
    messages.push({
      role: 'bot',
      content: bits.join('. ') + '.',
    });
  } else if (userText?.trim()) {
    messages.push({
      role: 'bot',
      content: "I heard you — could you rephrase? Include a wish description and a price (e.g. `1000 sats`). Type **help** for tips.",
    });
  }

  // Prompt for next missing field
  const missing = missingFields(nextDraft);
  if (missing.length === 0) {
    messages.push({
      role: 'bot',
      content: ['Draft looks ready:', '', ...draftSummaryLines(nextDraft), '', 'Reply **yes** to inscribe, or keep refining.'].join(
        '\n',
      ),
      kind: 'summary',
    });
    return { messages, draft: nextDraft, phase: 'ready' };
  }

  messages.push({
    role: 'bot',
    content: nextPrompt(missing, nextDraft),
  });
  return { messages, draft: nextDraft, phase: 'collecting' };
}

export function respondToImage(draft, file) {
  const nextDraft = applyDraftPatch(draft, { imageFile: file });
  const messages = [
    {
      role: 'bot',
      content: `Attached **${file.name}** (${prettyBytes(file.size)}). ${
        isDraftReady(nextDraft)
          ? 'Draft is ready — reply **yes** to inscribe.'
          : nextPrompt(missingFields(nextDraft), nextDraft)
      }`,
    },
  ];
  return { messages, draft: nextDraft, phase: phaseFor(nextDraft) };
}

export function welcomeMessages(address) {
  const signedIn = Boolean((address || '').trim());
  return [
    {
      role: 'bot',
      id: 'welcome',
      content: [
        "Hi — I'm **WishBot**.",
        '',
        'Tell me the wish you want agents to fulfill. You can:',
        '• Describe the wish in plain language',
        '• Include a **price** (e.g. `1000 sats`)',
        '• **Drag & drop** a cover image',
        '• Set funding with `raise fund` or `payout`',
        '',
        signedIn
          ? 'You are signed in — wallet is ready.'
          : '⚠️ You are not signed in. Sign in so we have a payout wallet before inscribing.',
        '',
        'What would you like to wish for?',
      ].join('\n'),
    },
  ];
}

function phaseFor(draft) {
  return isDraftReady(draft) ? 'ready' : 'collecting';
}

function hintForMissing(missing) {
  if (missing.includes('wish text')) {
    return 'Describe what you want built or delivered.';
  }
  if (missing.includes('price')) {
    return 'Try e.g. `price 1000 sats`.';
  }
  if (missing.includes('wallet address (sign in)')) {
    return 'Open the account menu and sign in, then come back.';
  }
  return '';
}

function nextPrompt(missing, draft) {
  if (missing.includes('wish text')) {
    return 'What is the wish? Describe the outcome you want on-chain.';
  }
  if (missing.includes('price')) {
    return `Nice${draft.message ? ` — "${truncate(draft.message, 60)}"` : ''}. What **price** should we set? (e.g. \`1000 sats\`)`;
  }
  if (missing.includes('wallet address (sign in)')) {
    return 'Wish and price are set, but I need you to **sign in** so we know the wallet address.';
  }
  return `Still need: ${missing.join(', ')}.`;
}

function truncate(s, n) {
  const t = String(s || '');
  return t.length <= n ? t : `${t.slice(0, n - 1)}…`;
}

function prettyBytes(n) {
  if (!Number.isFinite(n)) return '';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/** 1x1 PNG placeholder when user skips an image (matches legacy InscribeModal). */
export function buildPlaceholderImage() {
  const pngBase64 =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/Ptq4YQAAAABJRU5ErkJggg==';
  const bytes = Uint8Array.from(atob(pngBase64), (c) => c.charCodeAt(0));
  return new File([bytes], 'placeholder.png', { type: 'image/png' });
}

export function toBase64(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result || '';
      const base64 = typeof result === 'string' ? result.split(',')[1] || '' : '';
      resolve(base64);
    };
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

export function buildInscribePayload(draft) {
  const priceUnit = draft.priceUnit === 'btc' ? 'btc' : 'sats';
  const payloadPrice =
    priceUnit === 'sats' ? String(Math.max(0, Math.trunc(Number(draft.price) || 0))) : String(draft.price);
  return {
    message: (draft.message || '').trim(),
    price: payloadPrice,
    price_unit: priceUnit,
    address: (draft.address || '').trim(),
    funding_mode: draft.fundingMode === FUNDING_RAISE ? FUNDING_RAISE : FUNDING_PAYOUT,
    method: 'alpha',
  };
}
