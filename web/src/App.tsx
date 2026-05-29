import { useEffect, useMemo, useState } from 'react';
import type { AssetView, Person, SearchParams } from './api/types';
import {
  useBrowse,
  useLibraries,
  usePeople,
  useRenamePerson,
  useSearch,
  useTimeline,
  useUpload,
} from './api/hooks';
import { downloadPaths } from './api/client';
import { LibrarySwitcher } from './components/LibrarySwitcher';
import { Breadcrumbs } from './components/Breadcrumbs';
import { FolderBar } from './components/FolderBar';
import { AssetGrid } from './components/AssetGrid';
import { Lightbox } from './components/Lightbox';
import { SearchBar } from './components/SearchBar';
import { Timeline } from './components/Timeline';
import { UploadDropzone } from './components/UploadDropzone';
import { PeopleView } from './components/PeopleView';
import { SelectionBar } from './components/SelectionBar';
import { TokenField } from './components/TokenField';
import { dirOf, splitPath } from './lib/mediapath';

type Mode = 'browse' | 'search' | 'people';

export default function App() {
  const libraries = useLibraries();
  const people = usePeople();

  const [activeAlias, setActiveAlias] = useState<string | null>(null);
  const [path, setPath] = useState<string | null>(null);
  const [mode, setMode] = useState<Mode>('browse');
  const [searchParams, setSearchParams] = useState<SearchParams>({});
  const [openIndex, setOpenIndex] = useState<number | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [showUpload, setShowUpload] = useState(false);
  const [zipping, setZipping] = useState(false);

  // Pick the first library once they load.
  useEffect(() => {
    if (!activeAlias && libraries.data && libraries.data.length > 0) {
      const first = libraries.data[0].alias;
      setActiveAlias(first);
      setPath(first);
    }
  }, [libraries.data, activeAlias]);

  const browse = useBrowse(mode === 'browse' ? path : null);
  const search = useSearch(searchParams, mode === 'search');
  const timeline = useTimeline({ alias: activeAlias ?? undefined }, activeAlias != null);
  const renamePerson = useRenamePerson();
  const upload = useUpload();

  const active = mode === 'search' ? search : browse;
  const assets: AssetView[] = useMemo(
    () => (mode === 'people' ? [] : (active.data?.pages.flatMap((p) => p.items) ?? [])),
    [active.data, mode],
  );

  // Subfolders of the browsed path. `dirs` repeat across pages of the same path, so
  // the last loaded page is authoritative. Browse only — search responses have no dirs.
  const dirs: string[] = useMemo(() => {
    if (mode !== 'browse') return [];
    const pages = browse.data?.pages;
    return pages?.[pages.length - 1]?.dirs ?? [];
  }, [browse.data, mode]);

  const selectedAssets = useMemo(
    () => assets.filter((a) => selectedIds.has(a.id)),
    [assets, selectedIds],
  );

  const activeLibraryName = libraries.data?.find((l) => l.alias === activeAlias)?.name;

  const switchLibrary = (alias: string) => {
    setActiveAlias(alias);
    setPath(alias);
    setMode('browse');
    setOpenIndex(null);
    setSelectedIds(new Set());
  };

  const navigate = (next: string) => {
    setPath(next);
    setMode('browse');
    setOpenIndex(null);
  };

  const runSearch = (params: SearchParams) => {
    setSearchParams(params);
    setMode('search');
    setOpenIndex(null);
  };

  const clearSearch = () => {
    setSearchParams({});
    setMode('browse');
  };

  const toggleSelect = (asset: AssetView) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(asset.id)) next.delete(asset.id);
      else next.add(asset.id);
      return next;
    });
  };

  const downloadSelection = async () => {
    if (selectedAssets.length === 0) return;
    setZipping(true);
    try {
      const paths = selectedAssets.flatMap((a) => a.files.map((f) => f.mediaPath));
      const blob = await downloadPaths(paths);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'media.zip';
      a.click();
      URL.revokeObjectURL(url);
      setSelectedIds(new Set());
    } finally {
      setZipping(false);
    }
  };

  const openPerson = (person: Person) => {
    runSearch({ person: person.id });
    setMode('search');
  };

  const uploadTarget = useMemo(() => {
    const base = path ?? activeAlias ?? '';
    return { alias: splitPath(base).alias || (activeAlias ?? ''), dir: dirOf(base) };
  }, [path, activeAlias]);

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b border-border bg-surface-raised px-4 py-3">
        <h1 className="text-sm font-semibold text-slate-100">Media</h1>
        <LibrarySwitcher
          libraries={libraries.data ?? []}
          activeAlias={activeAlias}
          onSelect={switchLibrary}
        />
        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            onClick={() => setMode('browse')}
            aria-pressed={mode === 'browse'}
            className={tabClass(mode === 'browse')}
          >
            Browse
          </button>
          <button
            type="button"
            onClick={() => setMode('people')}
            aria-pressed={mode === 'people'}
            className={tabClass(mode === 'people')}
          >
            People
          </button>
          <button
            type="button"
            onClick={() => setShowUpload((s) => !s)}
            aria-pressed={showUpload}
            className={tabClass(showUpload)}
          >
            Upload
          </button>
          <TokenField />
        </div>
      </header>

      <div className="border-b border-border px-4 py-3">
        <SearchBar
          libraries={libraries.data ?? []}
          people={people.data ?? []}
          initial={searchParams}
          onSearch={runSearch}
          onClear={clearSearch}
        />
        {activeAlias && (
          <div className="mt-2">
            <Timeline
              buckets={timeline.data ?? []}
              onSelect={(date) => runSearch({ ...searchParams, alias: activeAlias, from: date, to: date })}
            />
          </div>
        )}
      </div>

      {showUpload && uploadTarget.alias && (
        <div className="border-b border-border px-4 py-3">
          <UploadDropzone
            alias={uploadTarget.alias}
            dir={uploadTarget.dir}
            disabled={upload.isPending}
            onUpload={(file) =>
              upload.mutateAsync({ alias: uploadTarget.alias, dir: uploadTarget.dir, file })
            }
          />
        </div>
      )}

      {mode === 'browse' && path && (
        <div className="px-4 py-2">
          <Breadcrumbs path={path} libraryName={activeLibraryName} onNavigate={navigate} />
        </div>
      )}

      <main className="flex min-h-0 flex-1 flex-col">
        {mode === 'people' ? (
          <PeopleView
            people={people.data ?? []}
            onRename={(id, name) => renamePerson.mutate({ id, name })}
            onOpenPerson={openPerson}
            renamingId={renamePerson.isPending ? renamePerson.variables?.id : null}
          />
        ) : active.isLoading ? (
          <div className="flex h-full items-center justify-center text-sm text-slate-500">
            Loading…
          </div>
        ) : active.isError ? (
          <div className="flex h-full items-center justify-center text-sm text-red-400">
            Failed to load assets.
          </div>
        ) : (
          <>
            {mode === 'browse' && path && (
              <FolderBar dirs={dirs} currentPath={path} onOpen={navigate} />
            )}
            <div className="min-h-0 flex-1">
              <AssetGrid
                assets={assets}
                selectedIds={selectedIds}
                onOpen={(a) => setOpenIndex(assets.findIndex((x) => x.id === a.id))}
                onToggleSelect={toggleSelect}
                onLoadMore={() => {
                  if (active.hasNextPage && !active.isFetchingNextPage) void active.fetchNextPage();
                }}
                hasMore={active.hasNextPage}
                emptyLabel={
                  mode === 'search'
                    ? 'No results.'
                    : dirs.length > 0
                      ? 'No files in this folder.'
                      : 'This folder is empty.'
                }
              />
            </div>
          </>
        )}
      </main>

      <SelectionBar
        selected={selectedAssets}
        onDownload={downloadSelection}
        onClear={() => setSelectedIds(new Set())}
        busy={zipping}
      />

      {openIndex != null && assets[openIndex] && (
        <Lightbox
          asset={assets[openIndex]}
          onClose={() => setOpenIndex(null)}
          onPrev={openIndex > 0 ? () => setOpenIndex(openIndex - 1) : undefined}
          onNext={
            openIndex < assets.length - 1 ? () => setOpenIndex(openIndex + 1) : undefined
          }
        />
      )}
    </div>
  );
}

function tabClass(active: boolean): string {
  return active
    ? 'rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-slate-900'
    : 'rounded-md px-3 py-1.5 text-sm text-slate-300 hover:bg-surface-hover';
}
