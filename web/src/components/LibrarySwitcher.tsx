import type { Library } from '../api/types';

interface LibrarySwitcherProps {
  libraries: Library[];
  activeAlias: string | null;
  onSelect: (alias: string) => void;
}

/** Switches the active library (from /api/libraries). Renders one button per root. */
export function LibrarySwitcher({ libraries, activeAlias, onSelect }: LibrarySwitcherProps) {
  if (libraries.length === 0) {
    return <span className="text-xs text-slate-500">No libraries configured</span>;
  }
  return (
    <nav aria-label="Libraries" className="flex gap-1" data-testid="library-switcher">
      {libraries.map((lib) => {
        const active = lib.alias === activeAlias;
        return (
          <button
            key={lib.alias}
            type="button"
            aria-current={active ? 'page' : undefined}
            onClick={() => onSelect(lib.alias)}
            className={
              active
                ? 'rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-slate-900'
                : 'rounded-md px-3 py-1.5 text-sm text-slate-300 hover:bg-surface-hover'
            }
          >
            {lib.name}
          </button>
        );
      })}
    </nav>
  );
}
