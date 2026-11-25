import { describe, expect, it } from 'vitest';
import { imageLabel } from './imageLabel';
import type { Exclusion, HistoryEntry } from './types';

describe('imageLabel', () => {
  const saved: HistoryEntry = {
    id: 'accepted',
    image_id: 7,
    provider: 'wallhaven',
    image: '94x38z',
    attribution: { page_url: 'https://wallhaven.cc/w/94x38z' },
    availability: 'available',
  };
  const excluded: Exclusion = {
    id: 7,
    provider: 'wallhaven',
    image: '94x38z',
    page_url: 'https://wallhaven.cc/w/94x38z',
    excluded_at: '2026-11-22T00:00:00Z',
  };
  it('labels existing untitled Wallhaven records consistently', () => {
    expect(imageLabel(saved)).toBe('Wallhaven · 94x38z');
    expect(imageLabel(excluded)).toBe('Wallhaven · 94x38z');
  });
  it('prefers supplied titles and filenames', () => {
    expect(imageLabel({ ...saved, attribution: { title: 'Sunset' } })).toBe('Sunset');
    expect(imageLabel({ ...excluded, title: 'Sunset' })).toBe('Sunset');
    expect(imageLabel({ ...saved, provider: 'local', original_path: '/Pictures/coast.jpg' })).toBe(
      'coast.jpg',
    );
  });
});
