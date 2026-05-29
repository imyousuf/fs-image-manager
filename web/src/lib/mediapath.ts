// Helpers for the alias-prefixed media path "<alias>/<relpath>" (see _contracts.md §2).
// The frontend never resolves these to disk paths — it only splits/joins them for the
// breadcrumb navigation and upload target. Relative segments are not produced here.

export interface Crumb {
  label: string;
  /** The browse `path` value for this crumb ("<alias>" or "<alias>/<dir>"). */
  path: string;
}

/** Split a browse path into its alias and relative directory. */
export function splitPath(path: string): { alias: string; dir: string } {
  const trimmed = path.replace(/^\/+|\/+$/g, '');
  const slash = trimmed.indexOf('/');
  if (slash === -1) return { alias: trimmed, dir: '' };
  return { alias: trimmed.slice(0, slash), dir: trimmed.slice(slash + 1) };
}

/** Build breadcrumb entries from a browse path, using the library name for the root. */
export function breadcrumbs(path: string, libraryName?: string): Crumb[] {
  const { alias, dir } = splitPath(path);
  if (!alias) return [];
  const crumbs: Crumb[] = [{ label: libraryName ?? alias, path: alias }];
  if (!dir) return crumbs;
  const parts = dir.split('/').filter(Boolean);
  let acc = alias;
  for (const part of parts) {
    acc = `${acc}/${part}`;
    crumbs.push({ label: part, path: acc });
  }
  return crumbs;
}

/** The directory portion of a browse path, used as the default upload target. */
export function dirOf(path: string): string {
  return splitPath(path).dir;
}
