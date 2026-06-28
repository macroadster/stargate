// Shared helpers for Inscription modal / cards

/** QR version 40-L max byte capacity (base64 uses byte mode). */
export const QR_BYTE_LIMIT = 2953;

export const guessNetworkFromAddress = (addr) => {
  const a = (addr || '').trim().toLowerCase();
  if (!a) return '';
  if (a.startsWith('tb1') || a.startsWith('m') || a.startsWith('n') || a.startsWith('2')) return 'testnet4';
  if (a.startsWith('bc1') || a.startsWith('1') || a.startsWith('3')) return 'mainnet';
  return '';
};

export const shouldShowProposalAction = (status) => {
  const normalized = (status || '').toLowerCase();
  return !(normalized === 'rejected' || normalized === 'published');
};

export const looksLikeRaiseFund = (value) => {
  const normalized = (value || '').toLowerCase();
  return (
    normalized.includes('fund raising') ||
    normalized.includes('fundraising') ||
    normalized.includes('raise fund') ||
    normalized.includes('fundraise')
  );
};

export const expandContractCandidates = (inscription) => {
  const rawIds = [
    inscription?.contract_id,
    inscription?.id,
    inscription?.metadata?.contract_id,
    inscription?.metadata?.visible_pixel_hash,
    inscription?.metadata?.ingestion_id,
  ].filter(Boolean);
  const expanded = new Set();
  rawIds.forEach((id) => {
    expanded.add(id);
    if (String(id).startsWith('wish-')) {
      expanded.add(String(id).replace(/^wish-/, ''));
    } else {
      expanded.add(`wish-${id}`);
    }
  });
  return Array.from(expanded);
};

export const isPlaceholderAddress = (value) => {
  const cleaned = (value || '').trim().toLowerCase();
  if (!cleaned) return true;
  return cleaned.includes('...') || cleaned.includes('pending') || cleaned === 'bc1p-simulated-funding-address';
};

export const isConfirmedContract = (inscription) => {
  const isConfirmedStatus = (value) => (value || '').toLowerCase() === 'confirmed';
  const confirmationStatus = (inscription?.metadata?.confirmation_status || inscription?.confirmation_status || '').toLowerCase();
  const metadataStatus = (inscription?.metadata?.status || '').toLowerCase();
  const scanStatus = (inscription?.scan_result?.confirmation_status || inscription?.scan_result?.status || '').toLowerCase();
  return Boolean(
    inscription?.metadata?.confirmed_txid ||
      inscription?.metadata?.confirmed_height ||
      inscription?.confirmed_txid ||
      inscription?.confirmed_height ||
      inscription?.metadata?.confirmed === true ||
      inscription?.confirmed === true ||
      isConfirmedStatus(confirmationStatus) ||
      isConfirmedStatus(metadataStatus) ||
      isConfirmedStatus(scanStatus) ||
      isConfirmedStatus(inscription?.status),
  );
};

export const resolveModalImage = (inscription) => {
  const mime = (inscription?.mime_type || '').toLowerCase();
  const fileName = (inscription?.file_name || '').toLowerCase();
  const url = (inscription?.image_url || inscription?.thumbnail || '').toLowerCase();
  const urlLooksLikeTextFile = url.endsWith('.txt');
  const isObviouslyText =
    mime.startsWith('text/') ||
    mime.includes('json') ||
    fileName.endsWith('.json') ||
    fileName.endsWith('.txt') ||
    fileName.endsWith('.bitmap') ||
    fileName.endsWith('.md') ||
    fileName.includes('brc-20') ||
    fileName.includes('brc20');
  const isBlockImage = url.includes('/block-image/');
  const hasContentUrl = !!url && !urlLooksLikeTextFile;
  const isImageByMimeOrName =
    mime.includes('image') ||
    ['jpeg', 'jpg', 'png', 'gif', 'webp', 'avif', 'bmp', 'svg'].includes(mime) ||
    fileName.endsWith('.jpeg') ||
    fileName.endsWith('.jpg') ||
    fileName.endsWith('.png') ||
    fileName.endsWith('.gif') ||
    fileName.endsWith('.webp') ||
    fileName.endsWith('.avif') ||
    fileName.endsWith('.bmp') ||
    fileName.endsWith('.svg');
  const isActuallyImageFile =
    (isImageByMimeOrName || isBlockImage || (hasContentUrl && !isObviouslyText)) && !urlLooksLikeTextFile;
  const modalImageSource = isActuallyImageFile ? inscription?.thumbnail || inscription?.image_url : null;
  const scanImageSource = modalImageSource || inscription?.image_url || inscription?.thumbnail || '';
  const isHtmlContent = mime.includes('text/html') || mime.includes('application/xhtml');
  const isSvgContent = mime === 'image/svg+xml' || (mime.includes('svg') && mime.includes('xml'));
  const sandboxSrc = inscription?.image_url || inscription?.thumbnail;
  const inlineDoc = isHtmlContent || isSvgContent ? inscription?.text || '' : '';
  return { modalImageSource, scanImageSource, isHtmlContent, isSvgContent, sandboxSrc, inlineDoc, isActuallyImageFile };
};

export const parseStegoManifest = (raw) => {
  if (!raw) return null;
  const manifest = {};
  String(raw).split('\n').forEach((line) => {
    const idx = line.indexOf(':');
    if (idx < 0) return;
    const key = line.slice(0, idx).trim();
    const value = line.slice(idx + 1).trim();
    if (key) manifest[key] = value;
  });
  return Object.keys(manifest).length > 0 ? manifest : null;
};

export const submissionTimestamp = (submission) => {
  const raw = submission?.submitted_at || submission?.created_at;
  if (!raw) return 0;
  const parsed = Date.parse(raw);
  return Number.isNaN(parsed) ? 0 : parsed;
};
