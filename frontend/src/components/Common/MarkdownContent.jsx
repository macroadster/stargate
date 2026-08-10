import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

/**
 * Shared GFM markdown renderer for proposal descriptions, task notes,
 * deliverables, and other human-readable smart-contract text.
 *
 * Always enables remark-gfm so tables, strikethrough, autolinks, and
 * task lists render instead of showing raw pipe/hash/backtick syntax.
 */
const MarkdownContent = ({ children, className = '', compact = false }) => {
  const content = typeof children === 'string' ? children : (children == null ? '' : String(children));
  if (!content.trim()) return null;

  return (
    <div
      className={`markdown-content${compact ? ' markdown-content--compact' : ''}${className ? ` ${className}` : ''}`}
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          h1: ({ node, ...props }) => <h1 className="md-h1" {...props} />,
          h2: ({ node, ...props }) => <h2 className="md-h2" {...props} />,
          h3: ({ node, ...props }) => <h3 className="md-h3" {...props} />,
          h4: ({ node, ...props }) => <h4 className="md-h4" {...props} />,
          p: ({ node, ...props }) => <p className="md-p" {...props} />,
          ul: ({ node, ...props }) => <ul className="md-ul" {...props} />,
          ol: ({ node, ...props }) => <ol className="md-ol" {...props} />,
          li: ({ node, ...props }) => <li className="md-li" {...props} />,
          a: ({ node, href, ...props }) => (
            <a
              className="md-a"
              href={href}
              target={href && /^https?:\/\//i.test(href) ? '_blank' : undefined}
              rel={href && /^https?:\/\//i.test(href) ? 'noopener noreferrer' : undefined}
              {...props}
            />
          ),
          pre: ({ node, ...props }) => <pre className="md-pre" {...props} />,
          code: ({ node, className: codeClassName, children: codeChildren, ...props }) => {
            // Fenced blocks get a language-* class; bare `code` is inline.
            const isBlock = Boolean(codeClassName);
            if (isBlock) {
              return (
                <code className={`md-code-block ${codeClassName || ''}`.trim()} {...props}>
                  {codeChildren}
                </code>
              );
            }
            return (
              <code className="md-code-inline" {...props}>
                {codeChildren}
              </code>
            );
          },
          table: ({ node, children, ...props }) => (
            <div className="md-table-wrap">
              <table className="md-table" {...props}>{children}</table>
            </div>
          ),
          thead: ({ node, ...props }) => <thead {...props} />,
          tbody: ({ node, ...props }) => <tbody {...props} />,
          tr: ({ node, ...props }) => <tr {...props} />,
          th: ({ node, ...props }) => <th className="md-th" {...props} />,
          td: ({ node, ...props }) => <td className="md-td" {...props} />,
          blockquote: ({ node, ...props }) => <blockquote className="md-blockquote" {...props} />,
          hr: ({ node, ...props }) => <hr className="md-hr" {...props} />,
          img: ({ node, alt, ...props }) => (
            // eslint-disable-next-line jsx-a11y/alt-text
            <img className="md-img" alt={alt || ''} {...props} />
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
};

export default MarkdownContent;
