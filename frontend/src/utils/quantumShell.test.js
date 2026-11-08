import { beforeEach, describe, expect, it } from 'vitest';
import {
  SHELL_DESKTOP_KEY,
  SHELL_PENDING_INSTALL_KEY,
  installContractOnShellDesktop,
  isInstalledOnShellDesktop,
  isValidShellIdentifier,
  readShellDesktop,
  shellCandidateHash,
  shellContractName,
} from './quantumShell';

const PIXEL = 'a'.repeat(64);

const baseInscription = {
  id: 'wish-' + PIXEL,
  contract_id: 'wish-' + PIXEL,
  status: 'confirmed',
  block_height: 152155,
  tx_id: 'b'.repeat(64),
  mime_type: 'image/png',
  file_name: 'stego.png',
  image_url: '/uploads/' + PIXEL,
  metadata: { visible_pixel_hash: PIXEL, confidence: 0.91 },
};

beforeEach(() => {
  window.localStorage.clear();
});

describe('shellCandidateHash', () => {
  it('prefers the visible pixel hash from metadata', () => {
    expect(shellCandidateHash(baseInscription)).toBe(PIXEL);
  });

  it('prefers an explicit pixel hash override', () => {
    const other = 'c'.repeat(64);
    expect(shellCandidateHash(baseInscription, other)).toBe(other);
  });

  it('strips the wish- alias prefix', () => {
    expect(shellCandidateHash({ id: 'wish-' + PIXEL })).toBe(PIXEL);
  });

  it('falls back to the contract id when no pixel hash exists', () => {
    expect(shellCandidateHash({ id: 'contract-osv4-rest-open' })).toBe('contract-osv4-rest-open');
  });

  it('returns empty string for empty input', () => {
    expect(shellCandidateHash(null)).toBe('');
    expect(shellCandidateHash({})).toBe('');
  });
});

describe('isValidShellIdentifier', () => {
  it('accepts hashes and contract ids', () => {
    expect(isValidShellIdentifier(PIXEL)).toBe(true);
    expect(isValidShellIdentifier('contract-osv4-rest-open')).toBe(true);
  });

  it('rejects values that would break out of attribute contexts', () => {
    expect(isValidShellIdentifier('"><script>alert(1)</script>')).toBe(false);
    expect(isValidShellIdentifier("'><img src=x>")).toBe(false);
    expect(isValidShellIdentifier('a b')).toBe(false);
    expect(isValidShellIdentifier('a/b')).toBe(false);
    expect(isValidShellIdentifier('')).toBe(false);
  });
});

describe('shellContractName', () => {
  it('unwraps JSON wish titles to their message', () => {
    const inscription = { file_name: 'wish.png', metadata: { title: '{"message":"Build a lighthouse"}' } };
    expect(shellContractName(inscription, PIXEL)).toBe('Build a lighthouse');
  });

  it('uses the file name when no title is present', () => {
    expect(shellContractName({ file_name: 'artwork.png' }, PIXEL)).toBe('artwork.png');
  });

  it('falls back to a short hash label', () => {
    expect(shellContractName({}, PIXEL)).toBe(`Contract ${PIXEL.slice(0, 6)}`);
  });
});

describe('installContractOnShellDesktop', () => {
  it('writes a confirmedContracts entry the shell can render', () => {
    const result = installContractOnShellDesktop(baseInscription);
    expect(result.status).toBe('installed');
    expect(result.hash).toBe(PIXEL);

    const stored = readShellDesktop();
    expect(stored).toHaveLength(1);
    expect(stored[0]).toMatchObject({
      identifier_hash: PIXEL,
      name: 'stego.png',
      thumbnail_hash: PIXEL,
      visible_pixel_hash: PIXEL,
      tx_id: 'b'.repeat(64),
      block_height: 152155,
      status: 'confirmed',
    });
    expect(stored[0].imageData).toBeNull();
  });

  it('flags a pending sync install for the shell boot sequence', () => {
    installContractOnShellDesktop(baseInscription);
    expect(window.localStorage.getItem(SHELL_PENDING_INSTALL_KEY)).toBeTruthy();
  });

  it('is idempotent for the same contract', () => {
    expect(installContractOnShellDesktop(baseInscription).status).toBe('installed');
    expect(installContractOnShellDesktop(baseInscription).status).toBe('already-installed');
    expect(readShellDesktop()).toHaveLength(1);
  });

  it('dedupes wish- aliased entries against the bare pixel hash', () => {
    window.localStorage.setItem(
      SHELL_DESKTOP_KEY,
      JSON.stringify([{ identifier_hash: 'wish-' + PIXEL, name: 'existing' }]),
    );
    expect(isInstalledOnShellDesktop(PIXEL)).toBe(true);
    expect(installContractOnShellDesktop(baseInscription).status).toBe('already-installed');
  });

  it('installs with the contract id when no pixel hash exists, using the content image as icon', () => {
    const inscription = {
      id: 'contract-osv4-rest-open',
      file_name: 'proposal.json',
      image_url: '/api/block-image/100/stego.png',
      status: 'funded',
      metadata: {},
    };
    const result = installContractOnShellDesktop(inscription);
    expect(result.status).toBe('installed');
    const stored = readShellDesktop();
    expect(stored[0].identifier_hash).toBe('contract-osv4-rest-open');
    expect(stored[0].visible_pixel_hash).toBeNull();
    expect(stored[0].imageData).toBe('/api/block-image/100/stego.png');
  });

  it('rejects identifiers that are not safe for markup contexts', () => {
    const result = installContractOnShellDesktop({ id: 'x" onerror="alert(1)' });
    expect(result.status).toBe('invalid');
    expect(readShellDesktop()).toHaveLength(0);
  });

  it('preserves unrelated existing desktop entries', () => {
    const existing = { identifier_hash: 'f'.repeat(64), name: 'keep me' };
    window.localStorage.setItem(SHELL_DESKTOP_KEY, JSON.stringify([existing]));
    installContractOnShellDesktop(baseInscription);
    const stored = readShellDesktop();
    expect(stored).toHaveLength(2);
    expect(stored[0]).toEqual(existing);
  });

  it('ignores malformed confirmedContracts payloads', () => {
    window.localStorage.setItem(SHELL_DESKTOP_KEY, '{"not":"an array"}');
    expect(readShellDesktop()).toEqual([]);
    expect(isInstalledOnShellDesktop(PIXEL)).toBe(false);
    expect(installContractOnShellDesktop(baseInscription).status).toBe('installed');
  });

  it('does not throw on corrupt JSON and still installs', () => {
    window.localStorage.setItem(SHELL_DESKTOP_KEY, '{oops');
    expect(installContractOnShellDesktop(baseInscription).status).toBe('installed');
    expect(readShellDesktop()).toHaveLength(1);
  });
});
