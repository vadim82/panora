import { afterEach, expect, it, vi } from 'vitest';
import { bridge } from '../client';
import { readAcceptedPreview } from './readPreview';

vi.mock('../client', () => ({ bridge: vi.fn() }));
afterEach(() => vi.useRealTimers());

it('recovers the center after most of the lookahead budget is spent', async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'performance'] });
  const beginning = performance.now();
  const available = { preview: { image_id: 1, available: true } };
  vi.mocked(bridge<'Preview'>).mockImplementation(async () =>
    performance.now() - beginning < 7500 ? { code: 'busy' } : available,
  );
  const preview = readAcceptedPreview(1, () => false);
  await vi.advanceTimersByTimeAsync(9500);
  await expect(preview).resolves.toEqual(available);
});
