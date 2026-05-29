import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Lightbox } from './Lightbox';
import { rawJpgAsset, videoAsset } from '../test/fixtures';

describe('Lightbox', () => {
  it('renders the preview image for a still and offers RAW/JPG/both downloads', () => {
    render(<Lightbox asset={rawJpgAsset} onClose={vi.fn()} />);
    const img = screen.getByTestId('preview-image');
    expect(img).toHaveAttribute('src', rawJpgAsset.preview);
    expect(screen.queryByTestId('video-player')).not.toBeInTheDocument();

    const menu = screen.getByTestId('download-menu');
    expect(menu).toHaveTextContent('JPG');
    expect(menu).toHaveTextContent('RAW');
    expect(menu).toHaveTextContent('RAW + JPG');
    const both = screen.getByRole('menuitem', { name: 'RAW + JPG' });
    expect(both).toHaveAttribute(
      'href',
      '/api/assets/asset-raw-jpg/download?variant=both',
    );
  });

  it('renders a video player using the Range-enabled stream for video assets', () => {
    render(<Lightbox asset={videoAsset} onClose={vi.fn()} />);
    const player = screen.getByTestId('video-player');
    expect(player).toBeInTheDocument();
    const source = player.querySelector('source');
    expect(source).toHaveAttribute('src', videoAsset.stream);
    // Only a Video download is offered for clips.
    const menu = screen.getByTestId('download-menu');
    expect(menu).toHaveTextContent('Video');
    expect(menu).not.toHaveTextContent('RAW');
  });

  it('closes on Escape and the close button', async () => {
    const onClose = vi.fn();
    render(<Lightbox asset={rawJpgAsset} onClose={onClose} />);
    await userEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('navigates with arrow keys when prev/next are provided', async () => {
    const onPrev = vi.fn();
    const onNext = vi.fn();
    render(<Lightbox asset={rawJpgAsset} onClose={vi.fn()} onPrev={onPrev} onNext={onNext} />);
    await userEvent.keyboard('{ArrowRight}');
    expect(onNext).toHaveBeenCalled();
    await userEvent.keyboard('{ArrowLeft}');
    expect(onPrev).toHaveBeenCalled();
  });
});
