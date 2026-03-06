import React, { useEffect, useState } from 'react';
import { API_BASE } from '../apiBase';
import AppHeader from '../components/Common/AppHeader';

export default function McpDocsPage() {
  const [docs, setDocs] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    const loadDocs = async () => {
      try {
        const response = await fetch(`${API_BASE}/mcp/docs`);
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}`);
        }
        const html = await response.text();
        setDocs(html);
      } catch (err) {
        console.error('Failed to load MCP docs:', err);
        setError(err.message);
      } finally {
        setLoading(false);
      }
    };

    loadDocs();
  }, []);

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-950 text-black dark:text-white">
      <AppHeader />
      
      <div className="container mx-auto px-6 py-8">
        <div className="mb-8">
          <h1 className="text-3xl font-bold mb-2">MCP API Documentation</h1>
          <p className="text-gray-600 dark:text-gray-400">
            Complete API documentation for the Starlight Model Context Protocol (MCP) interface.
          </p>
          <div className="mt-4 flex flex-wrap gap-3">
            <a
              href={`${API_BASE}/mcp/SKILL.md`}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center rounded-md border border-gray-300 dark:border-gray-700 px-3 py-2 text-sm text-gray-800 dark:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-800"
            >
              Open Agent Skill
            </a>
            <a
              href={`${API_BASE}/mcp/starlight_sdk.sh`}
              className="inline-flex items-center rounded-md border border-gray-300 dark:border-gray-700 px-3 py-2 text-sm text-gray-800 dark:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-800"
            >
              Download SDK Script
            </a>
          </div>
        </div>

        {loading && (
          <div className="flex items-center justify-center py-12">
            <div className="text-gray-500 dark:text-gray-400">Loading documentation...</div>
          </div>
        )}

        {error && (
          <div className="bg-red-50 dark:bg-red-900/40 border border-red-200 dark:border-red-800 rounded-lg p-4 mb-6">
            <div className="text-red-800 dark:text-red-200">
              Failed to load documentation: {error}
            </div>
          </div>
        )}

        {!loading && !error && (
          <div className="bg-gray-50 dark:bg-gray-900 rounded-lg border border-gray-200 dark:border-gray-800 overflow-hidden">
            <div 
              className="p-6"
              dangerouslySetInnerHTML={{ __html: docs }}
            />
          </div>
        )}
      </div>
    </div>
  );
}
