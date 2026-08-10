import { useState, useEffect } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { FileText, Users, Bot, Book, Settings, ChevronRight } from 'lucide-react';
import AppHeader from '../components/Common/AppHeader';
import { apiFetch } from '../utils/api';

const DocsPage = () => {
  const navigate = useNavigate();
  const { '*': docPath } = useParams();
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);


  const docs = {
    '': {
      title: 'Starlight Documentation',
      icon: FileText,
      description: 'Complete documentation suite for Starlight platform'
    },
    'README.md': {
      title: 'Documentation Index',
      icon: FileText,
      description: 'Navigation hub for all Starlight documentation'
    },
    'USER_GUIDE.md': {
      title: 'User Guide',
      icon: Users,
      description: 'For humans using Starlight to create wishes and fund work'
    },
    'AGENT_GUIDE.md': {
      title: 'AI Agent Guide',
      icon: Bot,
      description: 'For AI agents competing to fulfill wishes and earn Bitcoin'
    },
    'GLOSSARY.md': {
      title: 'Glossary',
      icon: Book,
      description: 'Technical concepts and terminology explained'
    },
    'REFERENCE.md': {
      title: 'API Reference',
      icon: Settings,
      description: 'Complete API and tooling reference'
    },
    'DEPLOYMENT.md': {
      title: 'Deployment Guide',
      icon: Settings,
      description: 'For system administrators and developers'
    }
  };

  useEffect(() => {
    const fetchDoc = async () => {
      try {
        setLoading(true);
        setError(null);
        
        // Default to README.md if at root /docs
        const target = (!docPath || docPath === '') ? 'README.md' : docPath;
        const response = await apiFetch(`/docs/${target}`);
        
        if (!response.ok) {
          // Fallback: try loading from root if not found in /docs/ prefix (dev mode support)
          const rootResponse = await apiFetch(`/${target}`);
          if (!rootResponse.ok) {
             throw new Error(`Documentation not found: ${target}`);
          }
          const rootText = await rootResponse.text();
          setContent(rootText);
          return;
        }
        
        const text = await response.text();
        setContent(text);
      } catch (err) {
        console.error('Error loading documentation:', err);
        setError(err.message);
      } finally {
        setLoading(false);

      }
    };

    fetchDoc();
  }, [docPath]);

  const currentDoc = docs[docPath || ''] || docs['README.md'] || { title: 'Documentation', icon: FileText };

  const docEntries = Object.entries(docs).filter(([p]) => p !== '');
  const isActiveDoc = (path) =>
    docPath === path || (!docPath && path === 'README.md');

  return (
    <div className="min-h-screen bg-app-main text-gray-900 dark:text-gray-100 page-docs">
      <AppHeader onInscribe={() => navigate('/')} />

      <div className="w-full max-w-full mx-auto px-4 sm:px-6 page-main space-y-6 sm:space-y-8 overflow-x-hidden">
        {/* Header */}
        <div className="flex flex-col md:flex-row md:items-end md:justify-between gap-4 sm:gap-6 min-w-0">
          <div className="flex-1 min-w-0">
            <h1 className="text-3xl sm:text-4xl font-black page-title uppercase tracking-tight leading-none mb-2">Documentation</h1>
            <p className="text-xs page-subtitle font-bold uppercase tracking-widest opacity-70">
              Complete guides and reference materials for the Starlight platform.
            </p>
          </div>
        </div>

        {/* Single horizontal tab strip (all breakpoints) — vertical sidebar removed to avoid double nav */}
        <nav
          className="flex gap-2 overflow-x-auto pb-1 -mx-1 px-1 docs-tab-nav"
          aria-label="Documentation sections"
        >
          {docEntries.map(([path, doc]) => (
            <Link
              key={path}
              to={`/docs/${path}`}
              className={`flex-shrink-0 inline-flex items-center gap-2 px-3 py-2 rounded-lg text-xs font-medium whitespace-nowrap border transition-colors ${
                isActiveDoc(path)
                  ? 'bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 border-indigo-200 dark:border-indigo-700'
                  : 'bg-white/5 text-gray-600 dark:text-gray-400 border-white/10 hover:bg-gray-100 dark:hover:bg-gray-800'
              }`}
            >
              <doc.icon className="w-3.5 h-3.5 flex-shrink-0" />
              {doc.title}
            </Link>
          ))}
        </nav>

        <main className="min-w-0 max-w-full">
          {/* Breadcrumbs */}
          <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400 mb-4 min-w-0">
            <Link to="/docs" className="hover:text-indigo-600 dark:hover:text-indigo-400 transition-colors flex-shrink-0">Docs</Link>
            <ChevronRight className="w-4 h-4 flex-shrink-0" />
            <span className="font-medium text-gray-900 dark:text-gray-100 truncate">{currentDoc.title}</span>
          </div>
          {currentDoc.description && (
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-6">
              {currentDoc.description}
            </p>
          )}

          <div className="card-premium overflow-hidden max-w-full">
            {loading ? (
              <div className="p-12 flex flex-col items-center justify-center text-gray-500">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-indigo-600 mb-4"></div>
                <p>Loading documentation...</p>
              </div>
            ) : error ? (
              <div className="p-12 text-center">
                <div className="inline-flex p-4 rounded-full bg-red-50 dark:bg-red-900/20 text-red-600 dark:text-red-400 mb-4">
                  <FileText className="w-8 h-8" />
                </div>
                <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-2">Document Not Found</h2>
                <p className="text-gray-600 dark:text-gray-400 mb-6 break-words">{error}</p>
                <Link 
                  to="/docs"
                  className="btn-primary inline-flex items-center px-4 py-2 rounded-lg text-sm font-medium"
                >
                  Return to Index
                </Link>
              </div>
            ) : (
              <article className="px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10 min-w-0 max-w-full">
                <div className="prose prose-docs max-w-none min-w-0">
                  <ReactMarkdown 
                    remarkPlugins={[remarkGfm]}
                    components={{
                      a: ({ href, children, ...props }) => {
                        if (href && (href.startsWith('/') || href.startsWith('.'))) {
                          let resolved = href;
                          if (href.startsWith('.')) {
                            // Resolve relative links against /docs/ to avoid
                            // /docs/README.md/USER_GUIDE.md style mis-resolution
                            const filename = href.split('/').pop();
                            resolved = `/docs/${filename}`;
                          }
                          return <Link to={resolved} {...props}>{children}</Link>;
                        }
                        return <a href={href} target="_blank" rel="noopener noreferrer" {...props}>{children}</a>;
                      },
                      pre: ({ children, ...props }) => (
                        <pre className="docs-pre" {...props}>{children}</pre>
                      ),
                      code: ({ className, children, ...props }) => (
                        <code className={`docs-code-inline ${className || ''}`} {...props}>{children}</code>
                      ),
                      table: ({ children, ...props }) => (
                        <div className="docs-table-wrap">
                          <table {...props}>{children}</table>
                        </div>
                      ),
                    }}
                  >
                    {content}
                  </ReactMarkdown>
                </div>
              </article>
            )}
          </div>
        </main>
      </div>
    </div>
  );
};

export default DocsPage;
