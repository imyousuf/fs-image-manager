import { useState } from 'react';
import clsx from 'clsx';
import type { AssetView } from '../api/types';
import { hasRaw, isVideo } from '../lib/asset';

interface AssetTileProps {
  asset: AssetView;
  selected?: boolean;
  onOpen: (asset: AssetView) => void;
  onToggleSelect?: (asset: AssetView) => void;
}

/**
 * One grid cell = one logical asset. RAW+JPG bundles render once (the server's
 * `displayThumb`). Videos show a play badge over the poster; RAW-bearing assets show a
 * RAW chip. Purely presentational: data and selection come from props.
 */
export function AssetTile({ asset, selected, onOpen, onToggleSelect }: AssetTileProps) {
  const [broken, setBroken] = useState(false);
  const video = isVideo(asset);

  return (
    <div
      data-testid="asset-tile"
      data-asset-id={asset.id}
      className={clsx(
        'group relative aspect-square overflow-hidden rounded-md bg-surface-raised',
        'ring-2 ring-transparent transition',
        selected && 'ring-accent',
      )}
    >
      <button
        type="button"
        className="block h-full w-full"
        aria-label={`Open ${asset.name}`}
        onClick={() => onOpen(asset)}
      >
        {broken ? (
          <div className="flex h-full w-full items-center justify-center text-xs text-slate-500">
            {asset.name}
          </div>
        ) : (
          <img
            src={asset.displayThumb}
            alt={asset.name}
            loading="lazy"
            decoding="async"
            className="h-full w-full object-cover transition group-hover:scale-[1.03]"
            onError={() => setBroken(true)}
          />
        )}
      </button>

      {video && (
        <div
          data-testid="video-badge"
          aria-hidden
          className="pointer-events-none absolute inset-0 flex items-center justify-center"
        >
          <span className="flex h-12 w-12 items-center justify-center rounded-full bg-black/55 text-white shadow-lg">
            <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor" aria-hidden>
              <path d="M8 5v14l11-7z" />
            </svg>
          </span>
        </div>
      )}

      {!video && hasRaw(asset) && (
        <span
          data-testid="raw-badge"
          className="pointer-events-none absolute left-1.5 top-1.5 rounded bg-black/65 px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-amber-300"
        >
          RAW
        </span>
      )}

      {onToggleSelect && (
        <button
          type="button"
          aria-label={selected ? `Deselect ${asset.name}` : `Select ${asset.name}`}
          aria-pressed={selected}
          onClick={(e) => {
            e.stopPropagation();
            onToggleSelect(asset);
          }}
          className={clsx(
            'absolute right-1.5 top-1.5 flex h-6 w-6 items-center justify-center rounded-full border text-xs transition',
            selected
              ? 'border-accent bg-accent text-slate-900'
              : 'border-white/60 bg-black/40 text-white opacity-0 group-hover:opacity-100',
          )}
        >
          {selected ? '✓' : ''}
        </button>
      )}

      <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/70 to-transparent px-2 pb-1 pt-4 opacity-0 transition group-hover:opacity-100">
        <p className="truncate text-[11px] text-slate-200">{asset.name}</p>
      </div>
    </div>
  );
}
