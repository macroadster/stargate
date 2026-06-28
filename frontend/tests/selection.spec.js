/**
 * Playwright smoke test to ensure block selection stays stable when switching
 * between pending, historical, recent, and genesis blocks.
 *
 * Assumptions:
 * - App served at http://starlight.local (ingress enabled) or overridden via BASE_URL.
 * - Genesis block (0) and at least two recent blocks are visible in the horizontal list.
 */

const { test, expect } = require('@playwright/test');

const BASE_URL = process.env.BASE_URL || 'http://starlight.local';

// Helper to click a block card by height
async function clickBlock(page, height) {
  await page.locator(`[data-block-id="${height}"]`).first().click();
}

async function selectedHeight(page) {
  const heading = page.locator('h2:has-text("Block ")').first();
  const text = await heading.textContent();
  const match = text.match(/Block\s+(\d+)/i);
  return match ? parseInt(match[1], 10) : null;
}

test('block selection does not snap back across pending, historical, recent, genesis', async ({ page }) => {
  await page.goto(`${BASE_URL}/`);

  // Wait for blocks to render
  await page.waitForSelector('[data-block-id]');

  // Collect a few block ids from the strip
  const ids = await page.locator('[data-block-id]').evaluateAll((els) =>
    els.map((e) => parseInt(e.getAttribute('data-block-id'), 10)).filter((n) => !Number.isNaN(n))
  );

  const genesis = 0;
  const pending = Math.max(...ids);
  const recent = ids.find((h) => h > 0 && h < pending) || ids[0];
  const historical = ids.find((h) => h < recent && h > 0) || recent;

  // Click pending
  await clickBlock(page, pending);
  await expect.poll(async () => selectedHeight(page)).toBe(pending);

  // Click historical
  await clickBlock(page, historical);
  await expect.poll(async () => selectedHeight(page)).toBe(historical);

  // Click recent
  await clickBlock(page, recent);
  await expect.poll(async () => selectedHeight(page)).toBe(recent);

  // Click genesis (only if visible in this environment)
  if (ids.includes(genesis)) {
    await clickBlock(page, genesis);
    await expect.poll(async () => selectedHeight(page)).toBe(genesis);
  }

  // Return to recent; ensure no snap-back occurs
  await clickBlock(page, recent);
  await expect.poll(async () => selectedHeight(page)).toBe(recent);
});

