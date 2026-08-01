import React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import MarkdownContent from './MarkdownContent';

describe('MarkdownContent', () => {
  test('renders headings instead of raw hashes', () => {
    render(<MarkdownContent>{'# Proposal Title\n\nSome body text.'}</MarkdownContent>);
    expect(screen.getByRole('heading', { level: 1, name: 'Proposal Title' })).toBeInTheDocument();
    expect(screen.getByText('Some body text.')).toBeInTheDocument();
    expect(screen.queryByText(/# Proposal Title/)).not.toBeInTheDocument();
  });

  test('renders GFM tables as table cells', () => {
    const md = `| Skill | Level |
| --- | --- |
| Go | Expert |
| React | Intermediate |`;
    render(<MarkdownContent>{md}</MarkdownContent>);
    expect(screen.getByRole('table')).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Skill' })).toBeInTheDocument();
    expect(screen.getByRole('cell', { name: 'Go' })).toBeInTheDocument();
    expect(screen.queryByText(/\| Skill \|/)).not.toBeInTheDocument();
  });

  test('renders fenced code blocks in a pre element', () => {
    const md = '```js\nconst x = 1;\n```';
    const { container } = render(<MarkdownContent>{md}</MarkdownContent>);
    const pre = container.querySelector('pre.md-pre');
    expect(pre).toBeTruthy();
    expect(pre.textContent).toContain('const x = 1;');
    expect(screen.queryByText(/```js/)).not.toBeInTheDocument();
  });

  test('renders inline code without fence markers', () => {
    render(<MarkdownContent>{'Use `budget_sats` field.'}</MarkdownContent>);
    const code = screen.getByText('budget_sats');
    expect(code.tagName.toLowerCase()).toBe('code');
    expect(code).toHaveClass('md-code-inline');
  });

  test('returns null for empty content', () => {
    const { container } = render(<MarkdownContent>{'   '}</MarkdownContent>);
    expect(container.firstChild).toBeNull();
  });
});
