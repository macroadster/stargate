# Sitemap Implementation

This document describes the sitemap implementation for the Starlight frontend application.

## Files Created/Modified

### 1. Sitemap Files
- `public/sitemap.xml` - Main sitemap file
- `public/sitemap-index.xml` - Sitemap index file (for future expansion)
- `public/robots.txt` - Updated to reference the sitemap

### 2. Sitemap Generation Script
- `scripts/generate-sitemap.js` - Node.js script for dynamic sitemap generation

### 3. Package.json Updates
- Added `sitemap` script for manual generation
- Updated `build` script to auto-generate sitemap during builds

## Usage

### Manual Sitemap Generation
```bash
npm run sitemap
```

### Automatic Generation During Build
The sitemap is automatically generated during the build process:
```bash
npm run build
```

## Configuration

### Environment Variables
- `SITE_URL` - Base URL for the site (default: `https://starlight.local`)

Example:
```bash
SITE_URL=https://yourdomain.com npm run sitemap
```

## Static Routes Included

| Route | Change Frequency | Priority |
|-------|------------------|----------|
| `/` | daily | 1.0 |
| `/pending` | hourly | 0.9 |
| `/contracts` | daily | 0.8 |
| `/discover` | weekly | 0.7 |
| `/auth` | monthly | 0.5 |
| `/mcp/docs` | weekly | 0.8 |
| `/docs` | weekly | 0.7 |

## Dynamic Routes

The sitemap generator is designed to support dynamic routes that can be populated from the backend API:

- `/block/:id` - Individual block pages
- `/wish/:id` - Individual wish pages  
- `/contract/:id` - Individual contract pages
- `/proposal/:id` - Individual proposal pages

### Extending Dynamic Routes

To enable dynamic route generation, modify the `fetchDynamicRoutes()` function in `scripts/generate-sitemap.js` to pull data from your backend API.

Example implementation:
```javascript
async function fetchDynamicRoutes() {
  const API_BASE = process.env.API_BASE || 'https://api.starlight.local';
  
  const [blocks, wishes, contracts, proposals] = await Promise.all([
    fetch(`${API_BASE}/api/blocks`).then(r => r.json()),
    fetch(`${API_BASE}/api/wishes`).then(r => r.json()),
    fetch(`${API_BASE}/api/contracts`).then(r => r.json()),
    fetch(`${API_BASE}/api/proposals`).then(r => r.json()),
  ]);
  
  return {
    blocks: blocks.map(b => b.id),
    wishes: wishes.map(w => w.id),
    contracts: contracts.map(c => c.id),
    proposals: proposals.map(p => p.id),
  };
}
```

## Sitemap Structure

The generated sitemap follows the standard sitemap.org XML format with the following fields:
- `loc` - URL location
- `lastmod` - Last modification date (automatically generated)
- `changefreq` - How frequently the page changes
- `priority` - Page priority relative to other pages (0.0-1.0)

## Robots.txt

The `robots.txt` file has been updated to:
- Allow all user agents to crawl the site
- Reference the sitemap location
- Disallow crawling of build and node_modules directories

## Deployment Notes

1. The sitemap is generated during the build process and placed in the `public/` directory
2. For production deployment, ensure the sitemap is accessible at the root domain: `https://yourdomain.com/sitemap.xml`
3. Submit the sitemap to search engines (Google Search Console, Bing Webmaster Tools, etc.)

## Testing

To verify the sitemap is working correctly:

1. Generate the sitemap: `npm run sitemap`
2. Check the output: `cat public/sitemap.xml`
3. Test accessibility: `curl http://localhost:3000/sitemap.xml` (when running locally)
4. Validate the XML: Use an online sitemap validator

## Future Enhancements

- Integration with backend API for dynamic route population
- Automated submission to search engines
- Multiple sitemap support for large sites
- Image sitemap generation for inscriptions and contract images