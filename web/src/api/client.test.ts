import { describe, expect, it, beforeEach } from 'vitest';
import * as api from './client';
import { ApiError } from './client';
import { setStoredToken } from './config';
import { server } from '../test/server';
import { setRequiredToken } from '../test/handlers';
import { http, HttpResponse } from 'msw';

describe('API client (against the MSW contract)', () => {
  beforeEach(() => {
    setStoredToken(null);
    setRequiredToken(null);
  });

  it('lists libraries', async () => {
    const libs = await api.getLibraries();
    expect(libs.map((l) => l.alias)).toEqual(['pictures', 'videos']);
  });

  it('browses a folder by alias-prefixed path', async () => {
    const page = await api.browseAssets({ path: 'pictures' });
    expect(page.items).toHaveLength(2);
    expect(page.items[0].files.some((f) => f.kind === 'raw')).toBe(true);
  });

  it('searches with facets', async () => {
    const page = await api.searchAssets({ alias: 'videos' });
    expect(page.items).toHaveLength(1);
    expect(page.items[0].kind).toBe('video');
  });

  it('attaches the bearer token when configured and is rejected without it', async () => {
    setRequiredToken('secret-123');

    // No token → 401 mapped to ApiError.
    await expect(api.getLibraries()).rejects.toBeInstanceOf(ApiError);

    // With the stored token the request succeeds.
    setStoredToken('secret-123');
    const libs = await api.getLibraries();
    expect(libs).toHaveLength(2);
  });

  it('decodes the error envelope into code + message', async () => {
    server.use(
      http.get('/api/libraries', () =>
        HttpResponse.json(
          { error: { code: 'boom', message: 'kaboom' } },
          { status: 500 },
        ),
      ),
    );
    await expect(api.getLibraries()).rejects.toMatchObject({
      code: 'boom',
      message: 'kaboom',
      status: 500,
    });
  });

  it('renames a person', async () => {
    const person = await api.renamePerson('person-unknown', 'Bob');
    expect(person.name).toBe('Bob');
  });

  it('downloads a multi-select zip blob', async () => {
    const blob = await api.downloadPaths(['pictures/2021/IMG_1234.JPG']);
    expect(blob.type).toContain('zip');
  });

  it('builds media URLs with the asset id and size', () => {
    expect(api.thumbUrl('asset-1', 320)).toBe('/api/assets/asset-1/thumb?size=320');
    expect(api.streamUrl('asset-1')).toBe('/api/assets/asset-1/stream');
    expect(api.downloadUrl('asset-1', 'both')).toBe(
      '/api/assets/asset-1/download?variant=both',
    );
  });
});
