// Typed API client for the serve HTTP API (docs/specs/_contracts.md §5).
//
// Two flavours of access:
//   - JSON endpoints go through `request()` (adds the bearer token, decodes the
//     error envelope).
//   - Media bytes (thumb/preview/stream/download) are referenced by URL directly in
//     <img>/<video> src, so we expose `mediaUrl()` builders. The server already
//     returns absolute-ish URLs for AssetView (displayThumb/preview/stream/download),
//     but the builders cover cases where we only have an asset id.

import { getConfig } from './config';
import type {
  AssetPage,
  BrowseParams,
  DownloadVariant,
  Library,
  Person,
  SearchParams,
  TimelineBucket,
  TimelineParams,
} from './types';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

function authHeaders(): HeadersInit {
  const { token } = getConfig();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

function withBase(path: string): string {
  const { baseUrl } = getConfig();
  if (!baseUrl) return path;
  return `${baseUrl.replace(/\/$/, '')}${path}`;
}

function buildQuery(params: Record<string, string | number | undefined | null>): string {
  const usp = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue;
    usp.set(key, String(value));
  }
  const q = usp.toString();
  return q ? `?${q}` : '';
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(withBase(path), {
    ...init,
    headers: {
      Accept: 'application/json',
      ...authHeaders(),
      ...(init?.headers ?? {}),
    },
  });

  if (!res.ok) {
    let code = 'http_error';
    let message = `Request failed with status ${res.status}`;
    try {
      const body = await res.json();
      if (body?.error) {
        code = body.error.code ?? code;
        message = body.error.message ?? message;
      }
    } catch {
      // Non-JSON error body; keep the status-derived message.
    }
    throw new ApiError(res.status, code, message);
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

// ---- JSON endpoints -------------------------------------------------------

export function getLibraries(signal?: AbortSignal): Promise<Library[]> {
  return request<Library[]>('/api/libraries', { signal });
}

export function browseAssets(params: BrowseParams, signal?: AbortSignal): Promise<AssetPage> {
  const query = buildQuery({
    path: params.path,
    cursor: params.cursor,
    limit: params.limit,
  });
  // The decoded page carries `items`, `next`, and (browse only) `dirs` — the
  // subfolder names of `path` — straight through to the caller.
  return request<AssetPage>(`/api/assets${query}`, { signal });
}

export function searchAssets(params: SearchParams, signal?: AbortSignal): Promise<AssetPage> {
  const query = buildQuery({ ...params });
  return request<AssetPage>(`/api/assets/search${query}`, { signal });
}

export function getTimeline(params: TimelineParams, signal?: AbortSignal): Promise<TimelineBucket[]> {
  const query = buildQuery({ ...params });
  return request<TimelineBucket[]>(`/api/assets/timeline${query}`, { signal });
}

export function getPeople(signal?: AbortSignal): Promise<Person[]> {
  return request<Person[]>('/api/people', { signal });
}

export function getPersonAssets(id: string, signal?: AbortSignal): Promise<AssetPage> {
  return request<AssetPage>(`/api/people/${encodeURIComponent(id)}/assets`, { signal });
}

export function renamePerson(id: string, name: string): Promise<Person> {
  return request<Person>(`/api/people/${encodeURIComponent(id)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
}

/** Multi-select zip download. `paths` are alias-prefixed MediaPaths. */
export async function downloadPaths(paths: string[]): Promise<Blob> {
  const res = await fetch(withBase('/api/download'), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
    },
    body: JSON.stringify({ paths }),
  });
  if (!res.ok) {
    throw new ApiError(res.status, 'download_failed', `Download failed (${res.status})`);
  }
  return res.blob();
}

/** Upload a file into a library folder; triggers ingestion server-side. */
export async function uploadFile(
  alias: string,
  dir: string,
  file: File,
  signal?: AbortSignal,
): Promise<void> {
  const form = new FormData();
  form.append('file', file, file.name);
  const query = buildQuery({ alias, dir });
  const res = await fetch(withBase(`/api/upload${query}`), {
    method: 'POST',
    headers: { ...authHeaders() },
    body: form,
    signal,
  });
  if (!res.ok) {
    let message = `Upload failed (${res.status})`;
    try {
      const body = await res.json();
      if (body?.error?.message) message = body.error.message;
    } catch {
      /* keep default */
    }
    throw new ApiError(res.status, 'upload_failed', message);
  }
}

// ---- Media URL builders ---------------------------------------------------

export function thumbUrl(assetId: string, size = 320): string {
  return withBase(`/api/assets/${encodeURIComponent(assetId)}/thumb?size=${size}`);
}

export function previewUrl(assetId: string): string {
  return withBase(`/api/assets/${encodeURIComponent(assetId)}/preview`);
}

export function streamUrl(assetId: string): string {
  return withBase(`/api/assets/${encodeURIComponent(assetId)}/stream`);
}

export function downloadUrl(assetId: string, variant: DownloadVariant): string {
  return withBase(`/api/assets/${encodeURIComponent(assetId)}/download?variant=${variant}`);
}
