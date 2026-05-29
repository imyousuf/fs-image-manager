import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AssetGrid } from './AssetGrid';
import type { AssetView } from '../api/types';
import { jpgOnlyAsset, rawJpgAsset, videoAsset } from '../test/fixtures';

function makeAssets(n: number): AssetView[] {
  return Array.from({ length: n }, (_, i) => ({
    ...jpgOnlyAsset,
    id: `a-${i}`,
    name: `IMG_${i}`,
  }));
}

describe('AssetGrid', () => {
  const assets = [rawJpgAsset, jpgOnlyAsset, videoAsset];

  it('renders one tile per asset (RAW+JPG appears once)', () => {
    render(<AssetGrid assets={assets} columns={3} onOpen={vi.fn()} />);
    expect(screen.getAllByTestId('asset-tile')).toHaveLength(3);
  });

  it('shows the empty label when there are no assets', () => {
    render(<AssetGrid assets={[]} onOpen={vi.fn()} emptyLabel="Nothing" />);
    expect(screen.getByText('Nothing')).toBeInTheDocument();
    expect(screen.queryByTestId('asset-grid')).not.toBeInTheDocument();
  });

  it('forwards open clicks', async () => {
    const onOpen = vi.fn();
    render(<AssetGrid assets={assets} columns={3} onOpen={onOpen} />);
    await userEvent.click(screen.getByRole('button', { name: /open img_1234/i }));
    expect(onOpen).toHaveBeenCalledWith(rawJpgAsset);
  });

  it('virtualizes large lists — only a subset of tiles is mounted', () => {
    render(<AssetGrid assets={makeAssets(2000)} columns={4} onOpen={vi.fn()} />);
    const tiles = screen.getAllByTestId('asset-tile');
    // 2000 assets / 4 cols = 500 rows; virtualization mounts far fewer than 2000 tiles.
    expect(tiles.length).toBeLessThan(200);
    expect(tiles.length).toBeGreaterThan(0);
  });
});
