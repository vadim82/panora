import { useEffect, useRef, useState } from 'react';
import { usePanora } from '../client';
import { readAcceptedPreview } from './readPreview';
import { previewImage } from '../previewImage';
import type { Entry, HistoryEntry } from '../types';
import type { ThumbnailCache } from './thumbnailCache';

function matching(
  metadata: HistoryEntry | null | undefined,
  accepted: Entry | undefined,
): metadata is HistoryEntry {
  return Boolean(
    metadata && accepted && metadata.id === accepted.id && metadata.image_id === accepted.image_id,
  );
}
export function useAcceptedPreview(active: boolean, thumbnails?: ThumbnailCache) {
  const ui = usePanora();
  const status = ui.snapshot?.status;
  const accepted = status?.current;
  const metadata = matching(ui.snapshot?.current, accepted) ? ui.snapshot.current : null;
  const [preview, setPreview] = useState<{
      key: string;
      src: string | null;
      revision?: string;
    } | null>(null),
    [previewLoading, setPreviewLoading] = useState(false);
  const [previewRequest, setPreviewRequest] = useState(0);
  const previewRef = useRef(preview);
  previewRef.current = preview;
  const imageID = accepted?.image_id;
  const currentRevision = status?.current_preview_content;
  const cachedSrc = thumbnails?.get(imageID, currentRevision);
  const previewKey = JSON.stringify([
    status?.instance_id || null,
    accepted?.id || null,
    imageID || null,
    accepted?.path || null,
    currentRevision,
  ]);
  useEffect(() => {
    let canceled = false;
    if (!imageID || !ui.connected || !active) {
      setPreviewLoading(false);
      return;
    }
    // Keep successful cached previews across visits. Transient failures are
    // retried within this request, independently of status polling.
    if (cachedSrc || (previewRef.current?.key === previewKey && previewRef.current.src)) {
      setPreviewLoading(false);
      return;
    }
    setPreviewLoading(true);
    void (async () => {
      try {
        const result = await readAcceptedPreview(imageID, () => canceled);
        if (!canceled && result)
          setPreview({
            key: previewKey,
            src: previewImage(result, imageID),
            revision: result.preview?.content_revision,
          });
      } catch {
        if (!canceled) setPreview({ key: previewKey, src: null });
      } finally {
        if (!canceled) setPreviewLoading(false);
      }
    })();
    return () => {
      canceled = true;
    };
  }, [imageID, previewKey, previewRequest, ui.connected, active, cachedSrc]);
  const loading =
    !cachedSrc &&
    (previewLoading || Boolean(imageID && ui.connected && active && preview?.key !== previewKey));
  return {
    accepted,
    metadata,
    imageID,
    previewKey,
    previewLoading: loading,
    previewReady: Boolean(cachedSrc || (preview?.key === previewKey && !loading)),
    src: cachedSrc || (imageID && preview?.key === previewKey ? preview.src : null),
    cacheDecoded(image: HTMLImageElement) {
      if (imageID && currentRevision && (cachedSrc || preview?.revision === currentRevision))
        thumbnails?.remember(imageID, currentRevision, image);
    },
    retry() {
      thumbnails?.forget(imageID, currentRevision);
      setPreview(null);
      setPreviewRequest((request) => request + 1);
    },
    markUnavailable() {
      thumbnails?.forget(imageID, currentRevision);
      setPreview((current) =>
        cachedSrc || current?.key === previewKey ? { key: previewKey, src: null } : current,
      );
    },
  };
}
