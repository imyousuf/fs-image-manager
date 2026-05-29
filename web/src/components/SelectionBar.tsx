import type { AssetView } from '../api/types';

interface SelectionBarProps {
  selected: AssetView[];
  onDownload: () => void;
  onClear: () => void;
  busy?: boolean;
}

/** Sticky action bar shown while assets are multi-selected for a zip download. */
export function SelectionBar({ selected, onDownload, onClear, busy }: SelectionBarProps) {
  if (selected.length === 0) return null;
  return (
    <div
      data-testid="selection-bar"
      className="flex items-center justify-between gap-3 border-t border-border bg-surface-raised px-4 py-2"
    >
      <span className="text-sm text-slate-300">{selected.length} selected</span>
      <div className="flex gap-2">
        <button
          type="button"
          onClick={onClear}
          className="rounded-md border border-border px-3 py-1.5 text-sm text-slate-300 hover:bg-surface-hover"
        >
          Clear
        </button>
        <button
          type="button"
          onClick={onDownload}
          disabled={busy}
          className="rounded-md bg-accent px-4 py-1.5 text-sm font-medium text-slate-900 hover:bg-accent-strong disabled:opacity-60"
        >
          {busy ? 'Preparing…' : 'Download zip'}
        </button>
      </div>
    </div>
  );
}
