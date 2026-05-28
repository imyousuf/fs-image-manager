// Types mirror the HTTP API in docs/specs/_contracts.md §5. Keep them in lockstep
// with that contract; if the backend shape drifts, this file is the single place to
// update on the frontend.

/** A library root, e.g. {alias: "pictures", name: "Pictures"}. */
export interface Library {
  alias: string;
  name: string;
}

export type FileKind = 'jpg' | 'png' | 'raw' | 'video' | 'sidecar' | 'other';

/** A member file of an asset. `mediaPath` is the alias-prefixed "<alias>/<relpath>". */
export interface AssetFile {
  mediaPath: string;
  kind: FileKind;
  size: number;
}

export type AssetKind = 'image' | 'video';

/**
 * One logical photo/video. RAW+JPG bundles surface as a single AssetView with both
 * files in `files`. The URL fields are server-provided, already alias-aware.
 */
export interface AssetView {
  id: string;
  alias: string;
  name: string;
  kind: AssetKind;
  displayThumb: string;
  preview: string;
  stream: string;
  download: string;
  files: AssetFile[];
  capturedAt?: string | null;
}

/** Paged list response shared by folder browse and search. */
export interface AssetPage {
  items: AssetView[];
  next?: string | null;
  /**
   * Subfolder names of the browsed path (browse only; search omits it). These are
   * per-path and not paginated, so they repeat across pages of the same path.
   */
  dirs?: string[];
}

/** A timeline bucket: a date and the number of assets captured on it. */
export interface TimelineBucket {
  date: string;
  count: number;
}

export interface Person {
  id: string;
  name: string;
  coverFaceId: string;
}

/** Error envelope: {"error":{"code","message"}}. */
export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
  };
}

/** Parameters for folder browse (`GET /api/assets`). */
export interface BrowseParams {
  /** "<alias>/<dir>" or just "<alias>" for a library root. */
  path: string;
  cursor?: string;
  limit?: number;
}

/** Parameters for search (`GET /api/assets/search`). */
export interface SearchParams {
  q?: string;
  from?: string;
  to?: string;
  alias?: string;
  camera?: string;
  person?: string;
  cursor?: string;
  limit?: number;
}

export interface TimelineParams {
  alias?: string;
  from?: string;
  to?: string;
}

export type DownloadVariant = 'jpg' | 'raw' | 'both' | 'video';
