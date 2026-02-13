import React, { useState, useEffect } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { FileText, Users, Bot, Book, Settings, HelpCircle, ChevronRight } from 'lucide-react';
import AppHeader from '../components/Common/AppHeader';

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

      }
    };

    fetchDoc();
  }, [docPath]);

  const currentDoc = docs[docPath || ''] || docs['README.md'] || { title: 'Documentation', icon: FileText };
  const Icon = currentDoc.icon;

  return (
    <div className="min-h-screen bg-app-main text-gray-900 dark:text-gray-100">
      <AppHeader onInscribe={() => navigate('/')} />

      <div className="container mx-auto px-6 py-10 space-y-8">
        {/* Header */}
        <div className="flex flex-col md:flex-row md:items-end md:justify-between gap-6">
          <div className="flex-1">
            <h1 className="text-4xl font-black text-starlight uppercase tracking-tight leading-none mb-2">Documentation</h1>
            <p className="text-xs text-starlight font-bold uppercase tracking-widest opacity-70">
              Complete guides and reference materials for the Starlight platform.
            </p>
          </div>
        </div>

        <div className="grid lg:grid-cols-4 gap-6">
          {/* Sidebar Navigation */}
          <aside className="hidden lg:block lg:col-span-1">
            <div className="card-premium p-4 md:p-5 space-y-4">
              <div>
                <h3 className="text-[10px] font-black uppercase tracking-[0.2em] text-slate-500 mb-3">
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
                <div className="p-4 rounded-xl bg-indigo-50 dark:bg-indigo-900/20 border border-indigo-100 dark:border-indigo-800/50">
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

          {/* Main Content */}
          <main className="lg:col-span-3 min-w-0">
            {/* Breadcrumbs */}
            <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400 mb-6">
              <Link to="/docs" className="hover:text-indigo-600 dark:hover:text-indigo-400 transition-colors">Docs</Link>
              <ChevronRight className="w-4 h-4" />
              <span className="font-medium text-gray-900 dark:text-gray-100">{currentDoc.title}</span>
            </div>

            <div className="card-premium overflow-hidden">
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
                    className="btn-primary inline-flex items-center px-4 py-2 rounded-lg text-sm font-medium"
                  >
                    Return to Index
                  </Link>
                </div>
              ) : (
                <article className="px-6 py-8 lg:px-10 lg:py-10">
                  <div className="prose max-w-none">
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
