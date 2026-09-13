import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import BlockCard from './BlockCard';

const baseBlock = {
  height: 152155,
  timestamp: Math.floor(Date.now() / 1000) - 600,
  tx_count: 12,
  inscription_count: 1,
  has_images: true,
  smart_contract_count: 0,
};

describe('BlockCard hide-images thumbnails', () => {
  it('does not render a regular inscription thumbnail when hideImages is on', () => {
    render(
      <BlockCard
        block={{ ...baseBlock, thumbnail: '/content/abc?witness=0', thumbnailIsContract: false }}
        onClick={() => {}}
        hideImages
      />
    );
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
    expect(screen.getByText('Block 152155')).toBeInTheDocument();
  });

  it('does not treat an on-disk /block-image fallback as visible when hideImages is on', () => {
    render(
      <BlockCard
        block={{
          ...baseBlock,
          thumbnail: '/api/block-image/152155/spam.avif',
          thumbnailIsContract: false,
        }}
        onClick={() => {}}
        hideImages
      />
    );
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
  });

  it('still shows a smart-contract cover thumbnail when hideImages is on', () => {
    render(
      <BlockCard
        block={{
          ...baseBlock,
          smart_contract_count: 1,
          thumbnail: '/api/block-image/152155/stego.png',
          thumbnailIsContract: true,
        }}
        onClick={() => {}}
        hideImages
      />
    );
    expect(screen.getByRole('img')).toHaveAttribute('src', '/api/block-image/152155/stego.png');
  });

  it('shows regular inscription thumbnails when hideImages is off', () => {
    render(
      <BlockCard
        block={{ ...baseBlock, thumbnail: '/content/abc?witness=0', thumbnailIsContract: false }}
        onClick={() => {}}
        hideImages={false}
      />
    );
    expect(screen.getByRole('img')).toHaveAttribute('src', '/content/abc?witness=0');
  });
});
