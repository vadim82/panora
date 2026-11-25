import type { Exclusion, HistoryEntry } from './types';

// Labels use saved metadata only; rendering must not resolve the original image.
export function imageLabel(
  entry: HistoryEntry | Exclusion,
  includeWallhavenProvider = true,
): string {
  const title =
    ('attribution' in entry ? entry.attribution?.title : undefined) ||
    ('title' in entry ? entry.title : undefined);
  const filename = 'original_path' in entry ? entry.original_path?.split('/').pop() : undefined;
  return (
    title ||
    filename ||
    (includeWallhavenProvider && entry.provider === 'wallhaven' && entry.image
      ? `Wallhaven · ${entry.image}`
      : entry.image) ||
    'Untitled image'
  );
}
