import { Fragment } from 'react';
import { breadcrumbs } from '../lib/mediapath';

interface BreadcrumbsProps {
  path: string;
  libraryName?: string;
  onNavigate: (path: string) => void;
}

/** Folder breadcrumb for the alias-prefixed browse path. */
export function Breadcrumbs({ path, libraryName, onNavigate }: BreadcrumbsProps) {
  const crumbs = breadcrumbs(path, libraryName);
  if (crumbs.length === 0) return null;
  return (
    <nav aria-label="Breadcrumb" data-testid="breadcrumbs" className="flex items-center text-sm">
      {crumbs.map((crumb, i) => {
        const last = i === crumbs.length - 1;
        return (
          <Fragment key={crumb.path}>
            {i > 0 && <span className="px-1 text-slate-600">/</span>}
            <button
              type="button"
              aria-current={last ? 'page' : undefined}
              disabled={last}
              onClick={() => onNavigate(crumb.path)}
              className={
                last
                  ? 'font-medium text-slate-200'
                  : 'text-accent hover:underline'
              }
            >
              {crumb.label}
            </button>
          </Fragment>
        );
      })}
    </nav>
  );
}
