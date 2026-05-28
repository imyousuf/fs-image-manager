import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AssetTile } from './AssetTile';
import { jpgOnlyAsset, rawJpgAsset, videoAsset } from '../test/fixtures';

describe('AssetTile', () => {
  it('renders a single tile for a RAW+JPG bundle with a RAW badge and no play badge', () => {
    render(<AssetTile asset={rawJpgAsset} onOpen={vi.fn()} />);
    expect(screen.getAllByTestId('asset-tile')).toHaveLength(1);
    expect(screen.getByTestId('raw-badge')).toBeInTheDocument();
    expect(screen.queryByTestId('video-badge')).not.toBeInTheDocument();
    expect(screen.getByAltText('IMG_1234')).toHaveAttribute(
      'src',
      rawJpgAsset.displayThumb,
    );
  });

  it('shows a play badge for video assets and no RAW badge', () => {
    render(<AssetTile asset={videoAsset} onOpen={vi.fn()} />);
    expect(screen.getByTestId('video-badge')).toBeInTheDocument();
    expect(screen.queryByTestId('raw-badge')).not.toBeInTheDocument();
  });

  it('shows neither badge for a plain JPG', () => {
    render(<AssetTile asset={jpgOnlyAsset} onOpen={vi.fn()} />);
    expect(screen.queryByTestId('raw-badge')).not.toBeInTheDocument();
    expect(screen.queryByTestId('video-badge')).not.toBeInTheDocument();
  });

  it('opens on click and toggles selection independently', async () => {
    const onOpen = vi.fn();
    const onToggleSelect = vi.fn();
    render(
      <AssetTile asset={rawJpgAsset} onOpen={onOpen} onToggleSelect={onToggleSelect} />,
    );
    await userEvent.click(screen.getByRole('button', { name: /open img_1234/i }));
    expect(onOpen).toHaveBeenCalledWith(rawJpgAsset);

    await userEvent.click(screen.getByRole('button', { name: /select img_1234/i }));
    expect(onToggleSelect).toHaveBeenCalledWith(rawJpgAsset);
    // Selecting must not trigger open.
    expect(onOpen).toHaveBeenCalledTimes(1);
  });
});
