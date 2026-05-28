// React Query hooks over the API client. Components consume these; tests exercise
// them against MSW-mocked endpoints. Folder browse and search are paginated via the
// `next` cursor using infinite queries.

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import * as api from './client';
import type {
  AssetPage,
  BrowseParams,
  SearchParams,
  TimelineParams,
} from './types';

export function useLibraries() {
  return useQuery({
    queryKey: ['libraries'],
    queryFn: ({ signal }) => api.getLibraries(signal),
    staleTime: 5 * 60 * 1000,
  });
}

export function useBrowse(path: string | null, limit = 60) {
  return useInfiniteQuery({
    queryKey: ['browse', path, limit],
    enabled: path != null && path.length > 0,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => {
      const params: BrowseParams = { path: path as string, limit };
      if (pageParam) params.cursor = pageParam;
      return api.browseAssets(params, signal);
    },
    // Backend sends next:"" (empty string) for "no more pages"; `||` maps that to
    // undefined so React Query reports hasNextPage=false (?? would keep "" → infinite fetch).
    getNextPageParam: (last: AssetPage) => last.next || undefined,
  });
}

export function useSearch(params: SearchParams, enabled: boolean, limit = 60) {
  return useInfiniteQuery({
    queryKey: ['search', params, limit],
    enabled,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      api.searchAssets({ ...params, cursor: pageParam, limit }, signal),
    // Backend sends next:"" (empty string) for "no more pages"; `||` maps that to
    // undefined so React Query reports hasNextPage=false (?? would keep "" → infinite fetch).
    getNextPageParam: (last: AssetPage) => last.next || undefined,
  });
}

export function useTimeline(params: TimelineParams, enabled = true) {
  return useQuery({
    queryKey: ['timeline', params],
    enabled,
    queryFn: ({ signal }) => api.getTimeline(params, signal),
  });
}

export function usePeople() {
  return useQuery({
    queryKey: ['people'],
    queryFn: ({ signal }) => api.getPeople(signal),
  });
}

export function usePersonAssets(id: string | null) {
  return useQuery({
    queryKey: ['person-assets', id],
    enabled: id != null,
    queryFn: ({ signal }) => api.getPersonAssets(id as string, signal),
  });
}

export function useRenamePerson() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => api.renamePerson(id, name),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['people'] });
    },
  });
}

export function useUpload() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ alias, dir, file }: { alias: string; dir: string; file: File }) =>
      api.uploadFile(alias, dir, file),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['browse'] });
    },
  });
}
