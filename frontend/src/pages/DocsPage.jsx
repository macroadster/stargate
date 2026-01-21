import React, { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { FileText, Users, Bot, Book, Settings, Menu, X, HelpCircle, ChevronRight } from 'lucide-react';
import AppHeader from '../components/Common/AppHeader';

const DocsPage = () => {
  const { '*': docPath } = useParams();
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [sidebarOpen, setSidebarOpen] = useState(false);

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
        const response = await fetch(`/docs/${target}`);
        
        if (!response.ok) {
          // Fallback: try loading from root if not found in /docs/ prefix (dev mode support)
          const rootResponse = await fetch(`/${target}`);
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
        setSidebarOpen(false); // Close mobile sidebar on nav
      }
    };

    fetchDoc();
  }, [docPath]);

  const currentDoc = docs[docPath || ''] || docs['README.md'] || { title: 'Documentation', icon: FileText };
  const Icon = currentDoc.icon;

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-950 text-gray-900 dark:text-gray-100 font-sans">
      <AppHeader showThemeToggle={true} />
      
      {/* Mobile Sidebar Toggle */}
      <div className="lg:hidden bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 px-4 py-3 sticky top-0 z-20 flex items-center justify-between">
        <div className="flex items-center gap-2 font-semibold">
          <Icon className="w-5 h-5 text-indigo-600 dark:text-indigo-400" />
          <span>{currentDoc.title}</span>
        </div>
        <button 
          onClick={() => setSidebarOpen(!sidebarOpen)}
          className="p-2 rounded-lg bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-300"
        >
          {sidebarOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
        </button>
      </div>

      <div className="container mx-auto px-4 lg:px-8 py-6 lg:py-10 max-w-7xl">
        <div className="flex flex-col lg:flex-row gap-8 lg:gap-12">
          
          {/* Sidebar Navigation */}
          <aside className={`
            fixed inset-0 z-30 bg-white/95 dark:bg-gray-950/95 backdrop-blur-sm transform transition-transform duration-300 ease-in-out lg:translate-x-0 lg:static lg:z-auto lg:bg-transparent lg:w-72 lg:flex-shrink-0
            ${sidebarOpen ? 'translate-x-0 pt-20 px-6' : '-translate-x-full lg:p-0'}
          `}>
            <div className="lg:sticky lg:top-8 space-y-8">
              <div>
                <h3 className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3 px-3">
                  Documentation
                </h3>
                <nav className="space-y-1">
                  {Object.entries(docs).filter(([p]) => p !== '').map(([path, doc]) => (
                    <Link
                      key={path}
                      to={`/docs/${path}`}
                      className={`flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
                        (docPath === path) || (!docPath && path === 'README.md')
                          ? 'bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 font-medium'
                          : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-900 dark:hover:text-gray-200'
                      }`}
                    >
                      <doc.icon className={`w-4 h-4 ${
                        (docPath === path) || (!docPath && path === 'README.md') ? 'text-indigo-600 dark:text-indigo-400' : 'text-gray-400'
                      }`} />
                      {doc.title}
                    </Link>
                  ))}
                </nav>
              </div>

              {currentDoc.description && (
                <div className="hidden lg:block p-4 rounded-xl bg-indigo-50 dark:bg-indigo-900/20 border border-indigo-100 dark:border-indigo-800/50">
                  <h4 className="text-sm font-semibold text-indigo-900 dark:text-indigo-100 mb-1 flex items-center gap-2">
                    <HelpCircle className="w-4 h-4" />
                    About this guide
                  </h4>
                  <p className="text-xs text-indigo-700 dark:text-indigo-300 leading-relaxed">
                    {currentDoc.description}
                  </p>
                </div>
              )}
            </div>
          </aside>

          {/* Overlay for mobile sidebar */}
          {sidebarOpen && (
            <div 
              className="fixed inset-0 z-20 bg-black/20 dark:bg-black/50 lg:hidden backdrop-blur-sm"
              onClick={() => setSidebarOpen(false)}
            />
          )}

          {/* Main Content */}
          <main className="flex-1 min-w-0">
            {/* Breadcrumbs (Desktop) */}
            <div className="hidden lg:flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400 mb-6">
              <Link to="/docs" className="hover:text-indigo-600 dark:hover:text-indigo-400 transition-colors">Docs</Link>
              <ChevronRight className="w-4 h-4" />
              <span className="font-medium text-gray-900 dark:text-gray-100">{currentDoc.title}</span>
            </div>

            <div className="bg-white dark:bg-gray-900 rounded-2xl border border-gray-200 dark:border-gray-800 shadow-sm overflow-hidden">
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
                  <p className="text-gray-600 dark:text-gray-400 mb-6">{error}</p>
                  <Link 
                    to="/docs"
                    className="inline-flex items-center px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition-colors text-sm font-medium"
                  >
                    Return to Index
                  </Link>
                </div>
              ) : (
                <article className="px-6 py-8 lg:px-12 lg:py-12">
                  <div className="prose prose-lg dark:prose-invert max-w-none 
                    prose-headings:font-bold prose-headings:tracking-tight 
                    prose-a:text-indigo-600 dark:prose-a:text-indigo-400 prose-a:no-underline hover:prose-a:underline
                    prose-pre:bg-gray-50 dark:prose-pre:bg-gray-950 prose-pre:border prose-pre:border-gray-200 dark:prose-pre:border-gray-800
                    prose-code:text-indigo-600 dark:prose-code:text-indigo-300 prose-code:bg-indigo-50 dark:prose-code:bg-indigo-900/30 prose-code:px-1 prose-code:py-0.5 prose-code:rounded prose-code:before:content-none prose-code:after:content-none
                    prose-table:border-collapse prose-th:bg-gray-50 dark:prose-th:bg-gray-800/50 prose-th:p-3 prose-td:p-3 prose-td:border-b prose-td:border-gray-100 dark:prose-td:border-gray-800
                    prose-img:rounded-xl prose-img:shadow-md
                  ">
                    <ReactMarkdown 
                      remarkPlugins={[remarkGfm]}
                      components={{
                        // Custom link handling to use React Router for internal links
                        a: ({node, href, children, ...props}) => {
                          if (href && (href.startsWith('/') || href.startsWith('.'))) {
                            return <Link to={href} {...props}>{children}</Link>;
                          }
                          return <a href={href} target="_blank" rel="noopener noreferrer" {...props}>{children}</a>;
                        }
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
    </div>
  );
};

export default DocsPage;
