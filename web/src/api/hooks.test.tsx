import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '../test/server';
import * as fx from '../test/fixtures';
import { useBrowse, useSearch } from './hooks';

function wrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

describe('infinite-query pagination cursor', () => {
  // Regression: the backend sends next:"" (empty string), not null, to mean "no more
  // pages". `last.next ?? undefined` keeps "" as a truthy-to-React-Query cursor, so
  // hasNextPage stays true and the grid refetches the same page forever. `|| undefined`
  // collapses "" → undefined so pagination terminates after the first page.
  it('treats next:"" as the end of pages and does not refetch (useBrowse)', async () => {
    let calls = 0;
    server.use(
      http.get('/api/assets', () => {
        calls += 1;
        return HttpResponse.json({ items: fx.picturesAssets, next: '', dirs: [] });
      }),
    );

    const { result } = renderHook(() => useBrowse('pictures'), { wrapper: wrapper() });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.hasNextPage).toBe(false);

    // Asking React Query to fetch again must be a no-op when there is no next page.
    const before = calls;
    await result.current.fetchNextPage();
    await new Promise((r) => setTimeout(r, 20));
    expect(calls).toBe(before);
    expect(result.current.data?.pages).toHaveLength(1);
  });

  it('treats next:"" as the end of pages and does not refetch (useSearch)', async () => {
    let calls = 0;
    server.use(
      http.get('/api/assets/search', () => {
        calls += 1;
        return HttpResponse.json({ items: fx.picturesAssets, next: '' });
      }),
    );

    const { result } = renderHook(() => useSearch({ q: 'IMG' }, true), { wrapper: wrapper() });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.hasNextPage).toBe(false);

    const before = calls;
    await result.current.fetchNextPage();
    await new Promise((r) => setTimeout(r, 20));
    expect(calls).toBe(before);
    expect(result.current.data?.pages).toHaveLength(1);
  });

  it('still paginates when next is a real cursor', async () => {
    const fetchSpy = vi.fn();
    server.use(
      http.get('/api/assets', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        fetchSpy(cursor);
        // First page hands out a cursor; the second page closes with next:"".
        return cursor
          ? HttpResponse.json({ items: [fx.jpgOnlyAsset], next: '', dirs: [] })
          : HttpResponse.json({ items: [fx.rawJpgAsset], next: 'cursor-2', dirs: [] });
      }),
    );

    const { result } = renderHook(() => useBrowse('pictures'), { wrapper: wrapper() });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.hasNextPage).toBe(true);

    await result.current.fetchNextPage();
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(2));
    expect(result.current.hasNextPage).toBe(false);
    expect(fetchSpy).toHaveBeenCalledWith('cursor-2');
  });
});
