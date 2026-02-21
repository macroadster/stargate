const { test, expect } = require('@playwright/test');

const BASE_URL = process.env.BASE_URL || 'http://starlight.local';

test('debug /contracts grid layout', async ({ page }) => {
  await page.goto(`${BASE_URL}/contracts`);
  
  // Wait for grid to render
  await page.waitForSelector('.contracts-grid');
  
  // Get grid element and its computed style
  const grid = page.locator('.contracts-grid').first();
  const style = await grid.evaluate((el) => {
    const computed = window.getComputedStyle(el);
    return {
      columnCount: computed.columnCount,
      columnGap: computed.columnGap,
      display: computed.display,
      width: el.offsetWidth,
    };
  });
  
  console.log('Grid computed style:', style);
  
  // Get all card elements
  const cards = page.locator('.contracts-grid > *');
  const cardCount = await cards.count();
  console.log('Card count:', cardCount);
  
  // Get position of each card to see column layout
  for (let i = 0; i < Math.min(cardCount, 6); i++) {
    const card = cards.nth(i);
    const box = await card.boundingBox();
    console.log(`Card ${i}: x=${box.x}, y=${box.y}, w=${box.width}, h=${box.height}`);
  }
  
  // Test at different viewport sizes
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.waitForTimeout(500);
  
  const gridLarge = page.locator('.contracts-grid').first();
  const styleLarge = await gridLarge.evaluate((el) => {
    const computed = window.getComputedStyle(el);
    return {
      columnCount: computed.columnCount,
      width: el.offsetWidth,
    };
  });
  console.log('Grid at 1280px:', styleLarge);
  
  // Check again
  for (let i = 0; i < Math.min(cardCount, 6); i++) {
    const card = cards.nth(i);
    const box = await card.boundingBox();
    console.log(`Card ${i}: x=${box.x}, y=${box.y}`);
  }
});
