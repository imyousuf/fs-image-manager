// Asset helpers shared across views. The contract guarantees one AssetView per
// logical asset (RAW+JPG already bundled into `files`), so the UI never has to dedupe;
// these helpers just describe an asset for badges and the download menu.

import type { AssetView, DownloadVariant } from '../api/types';

export function isVideo(asset: AssetView): boolean {
  return asset.kind === 'video';
}

export function hasRaw(asset: AssetView): boolean {
  return asset.files.some((f) => f.kind === 'raw');
}

export function hasJpg(asset: AssetView): boolean {
  return asset.files.some((f) => f.kind === 'jpg');
}

/** Which download variants make sense for this asset, in menu order. */
export function downloadVariants(asset: AssetView): DownloadVariant[] {
  if (isVideo(asset)) return ['video'];
  const variants: DownloadVariant[] = [];
  if (hasJpg(asset)) variants.push('jpg');
  if (hasRaw(asset)) variants.push('raw');
  if (hasJpg(asset) && hasRaw(asset)) variants.push('both');
  // An image asset with neither jpg nor raw (e.g. png) still downloads as "jpg" slot,
  // which the server maps to the display file.
  if (variants.length === 0) variants.push('jpg');
  return variants;
}

export function variantLabel(variant: DownloadVariant): string {
  switch (variant) {
    case 'jpg':
      return 'JPG';
    case 'raw':
      return 'RAW';
    case 'both':
      return 'RAW + JPG';
    case 'video':
      return 'Video';
  }
}

/** Total bytes across an asset's member files. */
export function assetSize(asset: AssetView): number {
  return asset.files.reduce((sum, f) => sum + f.size, 0);
}

export function formatBytes(bytes: number): string {
  if (bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatDate(iso?: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
}
