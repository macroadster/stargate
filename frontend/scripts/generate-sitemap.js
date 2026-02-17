#!/usr/bin/env node

/**
 * Dynamic Sitemap Generator for Starlight
 * 
 * This script generates a sitemap.xml based on the application routes
 * and can be extended to pull dynamic data from the backend API.
 * 
 * Usage: node generate-sitemap.js [options]
 */

const fs = require('fs');
const path = require('path');

// Configuration
const SITE_URL = process.env.SITE_URL || 'https://starlight.local';
const OUTPUT_PATH = path.join(__dirname, '..', 'build', 'sitemap.xml');
const PUBLIC_PATH = path.join(__dirname, '..', 'public');
const BUILD_PATH = path.join(__dirname, '..', 'build');

// Static routes from the application
const staticRoutes = [
  { path: '/', changefreq: 'daily', priority: 1.0 },
  { path: '/pending', changefreq: 'hourly', priority: 0.9 },
  { path: '/contracts', changefreq: 'daily', priority: 0.8 },
  { path: '/discover', changefreq: 'weekly', priority: 0.7 },
  { path: '/auth', changefreq: 'monthly', priority: 0.5 },
  { path: '/mcp/docs', changefreq: 'weekly', priority: 0.8 },
  { path: '/docs', changefreq: 'weekly', priority: 0.7 },
];

// Dynamic route templates (these would be populated from backend)
const dynamicRoutes = {
  blocks: { pattern: '/block/:id', changefreq: 'daily', priority: 0.6 },
  wishes: { pattern: '/wish/:id', changefreq: 'weekly', priority: 0.5 },
  contracts: { pattern: '/contract/:id', changefreq: 'weekly', priority: 0.5 },
  proposals: { pattern: '/proposal/:id', changefreq: 'weekly', priority: 0.5 },
};

/**
 * Format date for sitemap (YYYY-MM-DD)
 */
function formatDate(date = new Date()) {
  return date.toISOString().split('T')[0];
}

/**
 * Escape XML special characters
 */
function escapeXml(unsafe) {
  return unsafe
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

/**
 * Generate a single URL entry for the sitemap
 */
function generateUrlEntry(route) {
  return `  <url>
    <loc>${escapeXml(`${SITE_URL}${route.path}`)}</loc>
    <lastmod>${formatDate(route.lastmod)}</lastmod>
    <changefreq>${route.changefreq}</changefreq>
    <priority>${route.priority}</priority>
  </url>`;
}

/**
 * Generate sitemap XML content
 */
function generateSitemap(routes) {
  const urls = routes.map(route => generateUrlEntry(route)).join('\n');

  return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls}
</urlset>`;
}

/**
 * Fetch dynamic routes from backend API (placeholder)
 */
async function fetchDynamicRoutes() {
  try {
    // This would fetch actual data from the backend API
    // For now, return empty arrays as examples
    
    // Example of how you might fetch blocks:
    // const blocksResponse = await fetch(`${API_BASE}/api/blocks`);
    // const blocks = await blocksResponse.json();
    
    return {
      blocks: [], // Array of block IDs
      wishes: [], // Array of wish IDs  
      contracts: [], // Array of contract IDs
      proposals: [], // Array of proposal IDs
    };
  } catch (error) {
    console.warn('Failed to fetch dynamic routes:', error.message);
    return {
      blocks: [],
      wishes: [],
      contracts: [],
      proposals: [],
    };
  }
}

/**
 * Main generation function
 */
async function generateSitemapFile() {
  try {
    console.log('Generating sitemap...');
    
    // Combine static routes with dynamic routes
    const routes = [...staticRoutes];
    
    // Uncomment this section when you have backend API available
    /*
    const dynamicData = await fetchDynamicRoutes();
    
    // Add dynamic block routes
    dynamicData.blocks.forEach(id => {
      routes.push({
        path: `/block/${id}`,
        changefreq: dynamicRoutes.blocks.changefreq,
        priority: dynamicRoutes.blocks.priority
      });
    });
    
    // Add other dynamic routes...
    */
    
    // Add example dynamic routes (remove when backend is connected)
    routes.push(
      { path: '/block/0', changefreq: 'daily', priority: 0.6 },
      { path: '/block/latest', changefreq: 'hourly', priority: 0.7 }
    );

    const sitemapContent = generateSitemap(routes);
    
    // Write sitemap to build directory
    fs.writeFileSync(OUTPUT_PATH, sitemapContent, 'utf8');
    
    // Copy static files to build directory
    const staticFiles = ['robots.txt', 'sitemap-index.xml'];
    staticFiles.forEach(file => {
      const src = path.join(PUBLIC_PATH, file);
      const dest = path.join(BUILD_PATH, file);
      if (fs.existsSync(src)) {
        let content = fs.readFileSync(src, 'utf8');
        content = content.replace(/https:\/\/starlight\.local/g, SITE_URL);
        fs.writeFileSync(dest, content, 'utf8');
      }
    });
    
    console.log(`✅ Sitemap generated successfully!`);
    console.log(`📍 Location: ${OUTPUT_PATH}`);
    console.log(`🌐 Site URL: ${SITE_URL}`);
    console.log(`📝 Total URLs: ${routes.length}`);
    
  } catch (error) {
    console.error('❌ Failed to generate sitemap:', error);
    process.exit(1);
  }
}

// Run the generator
if (require.main === module) {
  generateSitemapFile();
}

module.exports = { generateSitemapFile };