import { useEffect, useMemo, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { AssetView } from '../api/types';
import { AssetTile } from './AssetTile';

interface AssetGridProps {
  assets: AssetView[];
  selectedIds?: ReadonlySet<string>;
  onOpen: (asset: AssetView) => void;
  onToggleSelect?: (asset: AssetView) => void;
  onLoadMore?: () => void;
  hasMore?: boolean;
  /** Fixed columns (used in tests for determinism); otherwise responsive. */
  columns?: number;
  emptyLabel?: string;
}

const MIN_TILE = 180; // px; responsive column count derives from container width.
const GAP = 8;

/**
 * Row-virtualized asset grid — mandatory at DSLR-library scale (thousands of large
 * assets). Only the visible rows are mounted. Scrolling near the end triggers
 * `onLoadMore` for cursor pagination.
 */
export function AssetGrid({
  assets,
  selectedIds,
  onOpen,
  onToggleSelect,
  onLoadMore,
  hasMore,
  columns,
  emptyLabel = 'Nothing here yet.',
}: AssetGridProps) {
  const parentRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);

  useEffect(() => {
    const el = parentRef.current;
    if (!el) return;
    const update = () => setWidth(el.clientWidth);
    update();
    // jsdom lacks ResizeObserver layout, so a width of 0 falls back to `columns`.
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(update) : null;
    ro?.observe(el);
    return () => ro?.disconnect();
  }, []);

  const cols = useMemo(() => {
    if (columns && columns > 0) return columns;
    if (width <= 0) return 4;
    return Math.max(1, Math.floor((width + GAP) / (MIN_TILE + GAP)));
  }, [columns, width]);

  const rowCount = Math.ceil(assets.length / cols);
  const tileSize = cols > 0 && width > 0 ? (width - GAP * (cols - 1)) / cols : MIN_TILE;

  const rowVirtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => parentRef.current,
    estimateSize: () => tileSize + GAP,
    overscan: 4,
  });

  // Cursor pagination: load more once the last row is rendered.
  const virtualRows = rowVirtualizer.getVirtualItems();
  useEffect(() => {
    if (!onLoadMore || !hasMore) return;
    const last = virtualRows[virtualRows.length - 1];
    if (last && last.index >= rowCount - 1) {
      onLoadMore();
    }
  }, [virtualRows, rowCount, hasMore, onLoadMore]);

  if (assets.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-slate-500">
        {emptyLabel}
      </div>
    );
  }

  return (
    <div
      ref={parentRef}
      data-testid="asset-grid"
      className="scroll-area h-full overflow-auto"
    >
      <div
        style={{ height: rowVirtualizer.getTotalSize(), position: 'relative', width: '100%' }}
      >
        {virtualRows.map((virtualRow) => {
          const start = virtualRow.index * cols;
          const rowAssets = assets.slice(start, start + cols);
          return (
            <div
              key={virtualRow.key}
              data-testid="grid-row"
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                transform: `translateY(${virtualRow.start}px)`,
                display: 'grid',
                gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
                gap: GAP,
              }}
            >
              {rowAssets.map((asset) => (
                <AssetTile
                  key={asset.id}
                  asset={asset}
                  selected={selectedIds?.has(asset.id)}
                  onOpen={onOpen}
                  onToggleSelect={onToggleSelect}
                />
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}
