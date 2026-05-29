import { useEffect } from 'react';
import type { AssetView } from '../api/types';
import {
  assetSize,
  downloadVariants,
  formatBytes,
  formatDate,
  isVideo,
  variantLabel,
} from '../lib/asset';
import { downloadUrl } from '../api/client';

interface LightboxProps {
  asset: AssetView;
  onClose: () => void;
  onPrev?: () => void;
  onNext?: () => void;
}

/**
 * Full-screen detail view. Images render the display-size `preview`; videos use a
 * native <video> pointed at `/stream`, which supports HTTP Range so the browser can
 * seek without downloading the whole file. The download menu offers the variants that
 * exist for the asset (jpg/raw/both for stills, video for clips).
 */
export function Lightbox({ asset, onClose, onPrev, onNext }: LightboxProps) {
  const video = isVideo(asset);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
      else if (e.key === 'ArrowLeft') onPrev?.();
      else if (e.key === 'ArrowRight') onNext?.();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, onPrev, onNext]);

  return (
    <div
      data-testid="lightbox"
      role="dialog"
      aria-modal="true"
      aria-label={asset.name}
      className="fixed inset-0 z-50 flex flex-col bg-black/90"
    >
      <header className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <h2 className="truncate text-sm font-medium text-slate-100">{asset.name}</h2>
          <p className="text-xs text-slate-400">
            {asset.alias}
            {asset.capturedAt ? ` · ${formatDate(asset.capturedAt)}` : ''} ·{' '}
            {formatBytes(assetSize(asset))}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <DownloadMenu asset={asset} />
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="rounded-md border border-border px-3 py-1.5 text-sm text-slate-200 hover:bg-surface-hover"
          >
            Close
          </button>
        </div>
      </header>

      <div className="relative flex flex-1 items-center justify-center overflow-hidden p-2">
        {onPrev && (
          <button
            type="button"
            aria-label="Previous"
            onClick={onPrev}
            className="absolute left-2 top-1/2 z-10 -translate-y-1/2 rounded-full bg-black/50 px-3 py-2 text-xl text-white hover:bg-black/70"
          >
            ‹
          </button>
        )}

        {video ? (
          <video
            data-testid="video-player"
            controls
            playsInline
            preload="metadata"
            poster={asset.displayThumb}
            className="max-h-full max-w-full"
          >
            {/* Range-enabled stream; the browser issues partial requests for seeking. */}
            <source src={asset.stream} />
            Your browser does not support the video tag.
          </video>
        ) : (
          <img
            data-testid="preview-image"
            src={asset.preview}
            alt={asset.name}
            className="max-h-full max-w-full object-contain"
          />
        )}

        {onNext && (
          <button
            type="button"
            aria-label="Next"
            onClick={onNext}
            className="absolute right-2 top-1/2 z-10 -translate-y-1/2 rounded-full bg-black/50 px-3 py-2 text-xl text-white hover:bg-black/70"
          >
            ›
          </button>
        )}
      </div>
    </div>
  );
}

function DownloadMenu({ asset }: { asset: AssetView }) {
  const variants = downloadVariants(asset);
  return (
    <div className="group relative">
      <button
        type="button"
        className="rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-slate-900 hover:bg-accent-strong"
        aria-haspopup="menu"
      >
        Download
      </button>
      <div
        role="menu"
        data-testid="download-menu"
        className="absolute right-0 z-20 mt-1 hidden min-w-36 rounded-md border border-border bg-surface-raised py-1 shadow-xl group-focus-within:block group-hover:block"
      >
        {variants.map((variant) => (
          <a
            key={variant}
            role="menuitem"
            href={downloadUrl(asset.id, variant)}
            download
            className="block px-3 py-1.5 text-sm text-slate-200 hover:bg-surface-hover"
          >
            {variantLabel(variant)}
          </a>
        ))}
      </div>
    </div>
  );
}
