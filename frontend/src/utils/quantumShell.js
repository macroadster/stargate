/**
 * Starlight Shell integration — install contracts to the shell's desktop.
 *
 * The Starlight Shell (served from /desktop/index.html) renders its desktop icons
 * from the `confirmedContracts` localStorage key. stargate and the shell are
 * same-origin, so installing a contract means writing that entry here; the
 * shell picks it up the next time it boots.
 *
 * `stargate.shell.pendingInstall` is a sentinel the shell checks on boot to
 * broadcast the new state to paired devices (multi-device sync).
 */

export const SHELL_DESKTOP_KEY = 'confirmedContracts';
export const SHELL_PENDING_INSTALL_KEY = 'stargate.shell.pendingInstall';

const IDENTIFIER_RE = /^[A-Za-z0-9_-]{1,160}$/;
const PIXEL_HASH_RE = /^[0-9a-f]{64}$/i;

const stripWish = (value) => String(value || '').trim().replace(/^wish-/i, '');

/**
 * Picks the identifier the shell should open with `/sandbox/<hash>`.
 * Prefers the visible pixel hash; falls back to the contract id.
 */
export function shellCandidateHash(inscription, pixelHashOverride) {
  const fromOverride = stripWish(pixelHashOverride);
  if (fromOverride) return fromOverride;
  const fromMeta = stripWish(inscription?.metadata?.visible_pixel_hash)
    || stripWish(inscription?.visible_pixel_hash);
  if (fromMeta) return fromMeta;
  return stripWish(
    inscription?.metadata?.contract_id || inscription?.contract_id || inscription?.id || '',
  );
}

export function isValidShellIdentifier(hash) {
  return IDENTIFIER_RE.test(hash || '');
}

export function readShellDesktop() {
  try {
    const parsed = JSON.parse(window.localStorage.getItem(SHELL_DESKTOP_KEY) || '[]');
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((entry) => entry && typeof entry === 'object');
  } catch {
    return [];
  }
}

export function isInstalledOnShellDesktop(hash) {
  const clean = stripWish(hash);
  if (!clean) return false;
  return readShellDesktop().some((entry) => stripWish(entry?.identifier_hash) === clean);
}

const unwrapMessage = (value) => {
  // Wish proposals are often JSON like {"message": "..."}; unwrap for display.
  if (typeof value !== 'string') return '';
  const trimmed = value.trim();
  if (!trimmed.startsWith('{')) return trimmed;
  try {
    const parsed = JSON.parse(trimmed);
    return typeof parsed?.message === 'string' ? parsed.message.trim() : trimmed;
  } catch {
    return trimmed;
  }
};

export function shellContractName(inscription, hash) {
  const meta = inscription?.metadata || {};
  const named =
    unwrapMessage(meta.name) ||
    unwrapMessage(meta.title) ||
    unwrapMessage(inscription?.file_name) ||
    unwrapMessage(inscription?.name);
  const cleaned = String(named || '').trim();
  if (cleaned) return cleaned.slice(0, 120);
  const clean = stripWish(hash);
  return `Contract ${clean.slice(0, 6) || 'contract'}`;
}

const usableImageReference = (value) =>
  typeof value === 'string' && (value.startsWith('http://') || value.startsWith('https://') || value.startsWith('/'))
    ? value
    : null;

/**
 * Installs the contract onto the shell desktop.
 *
 * @returns {{status: 'installed', hash: string, name: string}
 *         | {status: 'already-installed', hash: string}
 *         | {status: 'invalid', hash: string}
 *         | {status: 'error', hash: string}}
 */
export function installContractOnShellDesktop(inscription, { pixelHash } = {}) {
  const clean = shellCandidateHash(inscription, pixelHash);
  if (!clean || !isValidShellIdentifier(clean)) {
    return { status: 'invalid', hash: clean };
  }

  const desktop = readShellDesktop();
  if (desktop.some((entry) => stripWish(entry?.identifier_hash) === clean)) {
    return { status: 'already-installed', hash: clean };
  }

  const meta = inscription?.metadata || {};
  const rawPixel = stripWish(pixelHash) || stripWish(meta.visible_pixel_hash) || stripWish(inscription?.visible_pixel_hash);
  const visiblePixelHash = PIXEL_HASH_RE.test(rawPixel) ? rawPixel : (PIXEL_HASH_RE.test(clean) ? clean : null);
  // Without a pixel hash the shell cannot synthesize an icon URL from chain data;
  // fall back to the node's content image so the icon still renders.
  const contentImage = usableImageReference(inscription?.image_url) || usableImageReference(inscription?.thumbnail);
  const stegoImageUrl =
    (typeof meta.stego_image_url === 'string' && meta.stego_image_url) || null;
  const rawTx = inscription?.tx_id || inscription?.id || '';
  const blockHeight = Number(inscription?.block_height || meta?.block_height || 0) || 0;

  const record = {
    identifier_hash: clean,
    name: shellContractName(inscription, clean),
    thumbnail_hash: visiblePixelHash || clean,
    block_height: blockHeight,
    confirmed_block_height: Number(meta?.confirmed_block_height || blockHeight || 0) || 0,
    visible_pixel_hash: visiblePixelHash,
    // Keep the bitcoin tx for metadata only — never used for image URLs
    tx_id: rawTx && rawTx !== 'TBD' ? String(rawTx) : null,
    stego_image_url: stegoImageUrl,
    imageData: visiblePixelHash ? null : contentImage,
    status: inscription?.status || null,
  };

  try {
    window.localStorage.setItem(SHELL_DESKTOP_KEY, JSON.stringify([...desktop, record]));
    window.localStorage.setItem(SHELL_PENDING_INSTALL_KEY, String(Date.now()));
  } catch {
    return { status: 'error', hash: clean };
  }
  return { status: 'installed', hash: clean, name: record.name };
}
