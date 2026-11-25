import { bridge } from '../client';
import type { PreviewResult } from '../types';

// The service holds an exclusive cache lock for each preview read. Keep the
// side-card reads serial and skip queued work once a selection is obsolete.
let queue = Promise.resolve();

function readSidePreview(read: () => Promise<PreviewResult>, canceled: () => boolean) {
  const request = queue.then(() => {
    if (!canceled()) return read();
  });
  queue = request.then(
    () => {},
    () => {},
  );
  return request;
}

// Keep center reads independent of the side cards and of obsolete centers.
const acceptedDelays = [100, 200, 400, 800, 1600, 2000, 2000, 2000, 0];
const preparedDelays = [100, 200, 400, 800, 1600, 0];

export function readAcceptedPreview(imageID: number, canceled: () => boolean) {
  return retryPreview(() => bridge('Preview', [imageID]), canceled, acceptedDelays, transient);
}

function transient(result: PreviewResult) {
  return result.code === 'busy' || result.code === 'timeout' || result.code === 'request_failed';
}

// Cache preparation can briefly hold the lock. Release the shared queue between
// attempts so accepted and other neighbor reads are not held behind the delay.
export async function readPreparedPreview(imageID: number, canceled: () => boolean) {
  return retryPreview(
    () => readSidePreview(() => bridge('Preview', [imageID]), canceled),
    canceled,
    preparedDelays,
    (result) => result.code === 'busy',
  );
}

export function readNeighborPreview(imageID: number, historyID: string, canceled: () => boolean) {
  return retryPreview(
    () => readSidePreview(() => bridge('PreviewNeighbor', [imageID, historyID]), canceled),
    canceled,
    preparedDelays,
    transient,
  );
}

async function retryPreview(
  read: () => Promise<PreviewResult | undefined>,
  canceled: () => boolean,
  delays: number[],
  shouldRetry: (result: PreviewResult) => boolean,
) {
  const deadline = performance.now() + 12000;
  for (const delay of delays) {
    if (canceled()) return;
    const result = await read();
    if (
      !result ||
      !shouldRetry(result) ||
      canceled() ||
      !delay ||
      performance.now() + delay >= deadline
    )
      return result;
    await new Promise((resolve) => setTimeout(resolve, delay));
    if (canceled()) return;
  }
}
