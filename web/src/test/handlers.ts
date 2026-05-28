// MSW request handlers implementing the serve HTTP API (_contracts.md §5) over the
// fixtures. Tests run components against these so no Go backend is required. The
// handlers honour the bearer-token rule, cursor pagination, search facets, and the
// people endpoints.

import { http, HttpResponse } from 'msw';
import * as fx from './fixtures';
import type { AssetView } from '../api/types';

/** When set, every request must carry `Authorization: Bearer <REQUIRED_TOKEN>`. */
export let REQUIRED_TOKEN: string | null = null;
export function setRequiredToken(token: string | null) {
  REQUIRED_TOKEN = token;
}

function authed(request: Request): boolean {
  if (!REQUIRED_TOKEN) return true;
  return request.headers.get('Authorization') === `Bearer ${REQUIRED_TOKEN}`;
}

function unauthorized() {
  return HttpResponse.json(
    { error: { code: 'unauthorized', message: 'token required' } },
    { status: 401 },
  );
}

function assetsForAlias(alias: string): AssetView[] {
  if (alias === 'pictures') return fx.picturesAssets;
  if (alias === 'videos') return fx.videosAssets;
  return [];
}

export const handlers = [
  http.get('/api/libraries', ({ request }) => {
    if (!authed(request)) return unauthorized();
    return HttpResponse.json(fx.libraries);
  }),

  http.get('/api/assets', ({ request }) => {
    if (!authed(request)) return unauthorized();
    const url = new URL(request.url);
    const path = url.searchParams.get('path') ?? '';
    const alias = path.split('/')[0];
    // Subfolders are per-path: a library root exposes its dirs, deeper folders none.
    const dirs = fx.dirsForPath[path] ?? [];
    return HttpResponse.json({ items: assetsForAlias(alias), next: null, dirs });
  }),

  http.get('/api/assets/search', ({ request }) => {
    if (!authed(request)) return unauthorized();
    const url = new URL(request.url);
    const q = url.searchParams.get('q')?.toLowerCase() ?? '';
    const alias = url.searchParams.get('alias') ?? '';
    const person = url.searchParams.get('person') ?? '';

    let items = [...fx.picturesAssets, ...fx.videosAssets];
    if (alias) items = items.filter((a) => a.alias === alias);
    if (q) items = items.filter((a) => a.name.toLowerCase().includes(q));
    // A person filter narrows to that person's cover asset for the mock.
    if (person === 'person-alice') items = items.filter((a) => a.id === 'asset-raw-jpg');
    return HttpResponse.json({ items, next: null });
  }),

  http.get('/api/assets/timeline', ({ request }) => {
    if (!authed(request)) return unauthorized();
    return HttpResponse.json(fx.timeline);
  }),

  http.get('/api/people', ({ request }) => {
    if (!authed(request)) return unauthorized();
    return HttpResponse.json(fx.people);
  }),

  http.get('/api/people/:id/assets', ({ request }) => {
    if (!authed(request)) return unauthorized();
    return HttpResponse.json({ items: [fx.rawJpgAsset], next: null });
  }),

  http.post('/api/people/:id', async ({ request, params }) => {
    if (!authed(request)) return unauthorized();
    const body = (await request.json()) as { name: string };
    return HttpResponse.json({
      id: String(params.id),
      name: body.name,
      coverFaceId: 'asset-raw-jpg',
    });
  }),

  http.post('/api/download', async ({ request }) => {
    if (!authed(request)) return unauthorized();
    await request.json();
    return new HttpResponse(new Blob(['zip-bytes'], { type: 'application/zip' }), {
      headers: { 'Content-Type': 'application/zip' },
    });
  }),

  http.post('/api/upload', ({ request }) => {
    if (!authed(request)) return unauthorized();
    return new HttpResponse(null, { status: 204 });
  }),
];
