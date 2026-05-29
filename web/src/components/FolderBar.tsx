import type { JSX } from 'react';

interface FolderBarProps {
  /** Subfolder names of the current browse path (the page's `dirs`). */
  dirs: string[];
  /** Called with the joined browse path "<currentPath>/<dir>" on click. */
  onOpen: (path: string) => void;
  /** The current alias-prefixed browse path the dirs are relative to. */
  currentPath: string;
}

/**
 * Navigable subfolder entries shown above the asset grid in browse mode. The real
 * library is deeply nested, so a folder's page is mostly other folders rather than
 * loose files — these tiles are the only way to descend. Renders nothing when there
 * are no subfolders, so it sits cleanly above the grid.
 */
export function FolderBar({ dirs, onOpen, currentPath }: FolderBarProps): JSX.Element | null {
  if (dirs.length === 0) return null;
  const base = currentPath.replace(/\/+$/, '');
  return (
    <div data-testid="folder-bar" className="px-4 pb-2 pt-3">
      <div className="flex flex-wrap gap-2">
        {dirs.map((dir) => (
          <button
            key={dir}
            type="button"
            data-testid="folder-entry"
            onClick={() => onOpen(`${base}/${dir}`)}
            className="flex items-center gap-2 rounded-md border border-border bg-surface-raised px-3 py-2 text-sm text-slate-200 transition hover:bg-surface-hover"
          >
            <svg
              viewBox="0 0 24 24"
              width="16"
              height="16"
              fill="currentColor"
              aria-hidden
              className="shrink-0 text-accent"
            >
              <path d="M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z" />
            </svg>
            <span className="truncate">{dir}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
