// Shared test fixtures shaped exactly like the API contract (_contracts.md §5).
// Two libraries (pictures, videos), a RAW+JPG asset that must render once, a video
// asset with a play badge, and a couple of people clusters (one named, one unknown).

import type { AssetView, Library, Person, TimelineBucket } from '../api/types';

export const libraries: Library[] = [
  { alias: 'pictures', name: 'Pictures' },
  { alias: 'videos', name: 'Videos' },
];

export const rawJpgAsset: AssetView = {
  id: 'asset-raw-jpg',
  alias: 'pictures',
  name: 'IMG_1234',
  kind: 'image',
  displayThumb: '/api/assets/asset-raw-jpg/thumb?size=320',
  preview: '/api/assets/asset-raw-jpg/preview',
  stream: '/api/assets/asset-raw-jpg/stream',
  download: '/api/assets/asset-raw-jpg/download',
  files: [
    { mediaPath: 'pictures/2021/IMG_1234.CR3', kind: 'raw', size: 25_000_000 },
    { mediaPath: 'pictures/2021/IMG_1234.JPG', kind: 'jpg', size: 6_000_000 },
    { mediaPath: 'pictures/2021/IMG_1234.XMP', kind: 'sidecar', size: 4_000 },
  ],
  capturedAt: '2021-06-15T10:30:00Z',
};

export const jpgOnlyAsset: AssetView = {
  id: 'asset-jpg',
  alias: 'pictures',
  name: 'IMG_5678',
  kind: 'image',
  displayThumb: '/api/assets/asset-jpg/thumb?size=320',
  preview: '/api/assets/asset-jpg/preview',
  stream: '/api/assets/asset-jpg/stream',
  download: '/api/assets/asset-jpg/download',
  files: [{ mediaPath: 'pictures/2021/IMG_5678.JPG', kind: 'jpg', size: 5_000_000 }],
  capturedAt: '2021-06-16T08:00:00Z',
};

export const videoAsset: AssetView = {
  id: 'asset-video',
  alias: 'videos',
  name: 'MVI_0001',
  kind: 'video',
  displayThumb: '/api/assets/asset-video/thumb?size=320',
  preview: '/api/assets/asset-video/preview',
  stream: '/api/assets/asset-video/stream',
  download: '/api/assets/asset-video/download',
  files: [{ mediaPath: 'videos/2022/MVI_0001.MOV', kind: 'video', size: 800_000_000 }],
  capturedAt: '2022-01-02T12:00:00Z',
};

export const picturesAssets: AssetView[] = [rawJpgAsset, jpgOnlyAsset];
export const videosAssets: AssetView[] = [videoAsset];

/** Subfolders keyed by browse path — `dirs` in the browse response (_contracts.md §5). */
export const dirsForPath: Record<string, string[]> = {
  pictures: ['album1', '2021'],
};

export const timeline: TimelineBucket[] = [
  { date: '2021-06-15', count: 2 },
  { date: '2022-01-02', count: 1 },
];

export const people: Person[] = [
  { id: 'person-alice', name: 'Alice', coverFaceId: 'asset-raw-jpg' },
  { id: 'person-unknown', name: '', coverFaceId: 'asset-jpg' },
];
