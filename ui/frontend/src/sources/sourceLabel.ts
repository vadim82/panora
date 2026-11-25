import type { Configuration, HistoryEntry } from '../types';
import { providerLabel } from './SourceHealth';

export function sourceLabel(entry: HistoryEntry, configuration: Configuration | null): string {
  return sourceLabels(entry, configuration).join(' · ');
}

export function sourceLabels(entry: HistoryEntry, configuration: Configuration | null): string[] {
  const sources =
    entry.provider === 'local'
      ? configuration?.folders
      : entry.provider === 'wallhaven'
        ? configuration?.searches
        : configuration?.inputs?.filter((input) => input.provider_id === entry.provider);
  const source = sources?.find((item) => item.id === entry.source_id);
  const label = providerLabel(entry.provider, configuration);
  return source
    ? [
        source.name ||
          ('query' in source ? source.query : 'path' in source ? source.path : source.id),
        label,
      ]
    : [label];
}
