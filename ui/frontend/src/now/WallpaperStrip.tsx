import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { readNeighborPreview, readPreparedPreview } from './readPreview';
import { previewImage } from '../previewImage';
import type { Status } from '../types';
import type { ThumbnailCache } from './thumbnailCache';

const transitionTiming = { duration: 170, easing: 'cubic-bezier(.22,.72,.23,1)' };

function useThumbnail(
  entry: Status['previous_history'],
  scope: string,
  enabled: boolean,
  thumbnails: ThumbnailCache,
  reuse: boolean,
  direct = false,
) {
  const historyID = entry?.id;
  const imageID = entry?.image_id;
  const key = JSON.stringify([scope, historyID, imageID, entry?.content_revision]);
  const revision = entry?.content_revision;
  const cachedSrc = reuse && thumbnails.get(imageID, revision);
  const [preview, setPreview] = useState<{ key: string; src: string | null; revision?: string }>();

  useEffect(() => {
    if (!enabled || !imageID || cachedSrc || (!direct && !historyID)) return;
    let canceled = false;
    void (async () => {
      try {
        const result = direct
          ? await readPreparedPreview(imageID, () => canceled)
          : await readNeighborPreview(imageID, historyID!, () => canceled);
        if (!canceled && result)
          setPreview({
            key,
            src: previewImage(result, imageID),
            revision: result.preview?.content_revision,
          });
      } catch {
        if (!canceled) setPreview({ key, src: null });
      }
    })();
    return () => {
      canceled = true;
    };
  }, [cachedSrc, direct, enabled, historyID, imageID, key]);

  return {
    src: cachedSrc || (preview?.key === key ? preview.src : null),
    decoded: Boolean(cachedSrc),
    cacheDecoded(image: HTMLImageElement) {
      if (imageID) thumbnails.remember(imageID, cachedSrc ? revision : preview?.revision, image);
    },
    markUnavailable() {
      if (cachedSrc) thumbnails.forget(imageID, revision);
      setPreview((current) => (current?.key === key ? { key, src: null } : current));
    },
  };
}

type Frame = { id: string; src: string; instance: string; cursor?: number };

export function WallpaperStrip({
  status,
  active,
  previewReady,
  settingsRevision,
  thumbnails,
  allowHistoryReuse,
  src,
  previous,
  next,
  previousDisabled,
  nextDisabled,
  navigation,
  targetOffset,
  pendingImage,
  navigationOrigin,
  navigationResult,
  stripRef,
  children,
  caption,
}: {
  status: Status;
  active: boolean;
  previewReady: boolean;
  settingsRevision?: string;
  thumbnails: ThumbnailCache;
  allowHistoryReuse: boolean;
  src: string | null;
  previous: () => void;
  next: () => void;
  previousDisabled: boolean;
  nextDisabled: boolean;
  navigation?: 'previous' | 'next';
  targetOffset?: number;
  pendingImage?: { id: number; content?: string };
  navigationOrigin?: string;
  navigationResult?: { id: string; imageID: number; instance: string };
  stripRef: React.RefObject<HTMLDivElement | null>;
  children: (onLoad: React.ReactEventHandler<HTMLImageElement>) => ReactNode;
  caption: ReactNode;
}) {
  const thumbnailScope = thumbnails.scope;
  const instance = status.instance_id || '';
  const currentID = status.current?.id;
  const scope = JSON.stringify([thumbnailScope, currentID]);
  // Let the initial settings read settle so its revision cannot invalidate a
  // request that just started. Loading the center image is independent.
  const loadNeighbors = active && settingsRevision !== undefined;
  const earlier = useThumbnail(
    status.previous_history,
    scope,
    loadNeighbors,
    thumbnails,
    allowHistoryReuse,
  );
  const earlierCard = useRef<HTMLSpanElement>(null);
  const laterCard = useRef<HTMLSpanElement>(null);
  const nextImageID =
    settingsRevision &&
    status.prepared_next_config_revision &&
    settingsRevision !== status.prepared_next_config_revision
      ? undefined
      : status.prepared_next_image_id;
  const nextEntry = nextImageID
    ? { id: '', image_id: nextImageID, content_revision: status.prepared_next_preview_content }
    : status.forward_history;
  const preparedBeyond = nextImageID ? status.prepared_beyond : undefined;
  const beyondEntry = preparedBeyond ?? status.forward_beyond;
  const later = useThumbnail(
    nextEntry,
    nextImageID ? JSON.stringify([scope, status.prepared_next_revision]) : scope,
    loadNeighbors,
    thumbnails,
    Boolean(nextImageID) || allowHistoryReuse,
    Boolean(nextImageID),
  );
  const offset = targetOffset ?? (navigation === 'previous' ? -1 : navigation === 'next' ? 1 : 0);
  const earlierBeyond = useThumbnail(
    status.previous_beyond,
    scope,
    loadNeighbors && targetOffset !== undefined && offset < 0,
    thumbnails,
    allowHistoryReuse,
  );
  const laterBeyond = useThumbnail(
    beyondEntry,
    scope,
    loadNeighbors && targetOffset !== undefined && offset > 0,
    thumbnails,
    Boolean(preparedBeyond) || allowHistoryReuse,
    Boolean(preparedBeyond),
  );
  const [decoded, setDecoded] = useState<string[]>([]);
  const [candidate, target] =
    offset === -2
      ? [earlierBeyond, status.previous_beyond]
      : offset < 0
        ? [earlier, status.previous_history]
        : offset === 2
          ? [laterBeyond, beyondEntry]
          : [later, offset ? nextEntry : undefined];
  const candidateID = target?.image_id;
  const candidateContent = target?.content_revision;
  const pendingDiffers = Boolean(
    pendingImage && (pendingImage.id !== candidateID || pendingImage.content !== candidateContent),
  );
  const actual = useThumbnail(
    pendingImage
      ? { id: '', image_id: pendingImage.id, content_revision: pendingImage.content }
      : undefined,
    scope,
    active && Boolean(navigation && pendingDiffers),
    thumbnails,
    true,
    true,
  );
  const speculative = pendingDiffers ? actual : candidate;
  const optimistic = offset ? speculative.src : null;
  const showing = Boolean(
    navigation && optimistic && (speculative.decoded || decoded.includes(optimistic)),
  );
  const optimisticRef = useRef<HTMLImageElement>(null);
  const held = useRef<
    | {
        src: string;
        imageID: number;
        instance: string;
        scope: string;
        content?: string;
      }
    | undefined
  >(undefined);
  const [frame, setFrame] = useState<Frame>();
  const navigating = useRef(false);
  const optimisticID = pendingDiffers ? pendingImage?.id : candidateID;
  const optimisticContent = pendingDiffers ? pendingImage?.content : candidateContent;
  useLayoutEffect(() => {
    if (navigation && !navigating.current) held.current = undefined;
    navigating.current = Boolean(navigation);
    if (navigation && currentID === navigationOrigin && showing && optimistic && optimisticID)
      held.current = {
        src: optimistic,
        imageID: optimisticID,
        instance,
        scope: thumbnailScope,
        content: optimisticContent,
      };
  }, [
    navigation,
    navigationOrigin,
    optimistic,
    optimisticID,
    showing,
    currentID,
    instance,
    thumbnailScope,
    optimisticContent,
  ]);
  const matchesHeld =
    held.current &&
    held.current.instance === instance &&
    held.current.scope === thumbnailScope &&
    (!status.current_preview_content || held.current.content === status.current_preview_content);
  const confirmedHeld =
    navigationResult &&
    matchesHeld &&
    held.current &&
    navigationResult.instance === held.current.instance &&
    navigationResult.imageID === held.current.imageID &&
    status.current?.id === navigationResult.id &&
    frame?.id !== navigationResult.id
      ? held.current.src
      : null;
  const transitionHeld =
    navigation &&
    matchesHeld &&
    held.current &&
    status.current?.id !== navigationOrigin &&
    status.current?.image_id === held.current?.imageID &&
    instance === held.current.instance
      ? held.current.src
      : null;
  const displayOptimistic =
    navigation && status.current?.id === navigationOrigin
      ? optimistic
      : transitionHeld || confirmedHeld;
  const displayReady =
    navigation && currentID === navigationOrigin ? showing : Boolean(displayOptimistic);
  function markDecoded(src: string) {
    setDecoded((existing) => (existing.includes(src) ? existing : [...existing.slice(-3), src]));
  }
  useLayoutEffect(() => {
    const image = optimisticRef.current;
    const card = (offset < 0 ? earlierCard : laterCard).current;
    if (
      !showing ||
      !image ||
      !card ||
      !image.animate ||
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    )
      return;
    const from = card.getBoundingClientRect();
    const to = image.getBoundingClientRect();
    if (!from.width || !to.width) return;
    const x = from.left + from.width / 2 - to.left - to.width / 2;
    const y = from.top + from.height / 2 - to.top - to.height / 2;
    const animation = image.animate(
      [
        { transform: `translate(${x}px, ${y}px) scale(${from.width / to.width})`, opacity: 0.65 },
        { transform: 'none', opacity: 1 },
      ],
      transitionTiming,
    );
    return () => animation.cancel();
  }, [offset, optimistic, showing]);
  const departing = useRef<HTMLImageElement>(null);
  const retained =
    active &&
    (src || !previewReady) &&
    frame?.instance === instance &&
    frame.id !== status.current?.id
      ? frame.src
      : null;
  const motion = useRef<Animation[]>([]);

  useEffect(() => () => motion.current.forEach((animation) => animation.cancel()), []);

  function animate(event: React.SyntheticEvent<HTMLImageElement>) {
    const center = event.currentTarget;
    const outgoing = departing.current;
    const before = frame;
    center.parentElement?.style.setProperty(
      '--wallpaper-ratio',
      String(center.naturalWidth / center.naturalHeight),
    );
    if (!status.current || !src || !outgoing) return;
    setFrame({ id: status.current.id, src, instance, cursor: status.history_cursor });
    if (before?.id === status.current.id) return;
    if (
      matchesHeld &&
      held.current?.imageID === status.current.image_id &&
      (navigation || navigationResult?.id === status.current.id)
    )
      return;

    motion.current.forEach((animation) => animation.cancel());
    outgoing.hidden = true;
    if (
      !before ||
      before.instance !== instance ||
      !active ||
      !center.animate ||
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    )
      return;

    const backward =
      typeof before.cursor === 'number' && typeof status.history_cursor === 'number'
        ? status.history_cursor < before.cursor
        : status.forward_history?.id === before.id;
    const from = (backward ? earlierCard : laterCard).current?.getBoundingClientRect();
    const toward = (backward ? laterCard : earlierCard).current?.getBoundingClientRect();
    const bounds = center.getBoundingClientRect();
    if (!from?.width || !toward?.width || !bounds.width) return;

    function transform(card: DOMRect, tilt: number) {
      const x = card.left - bounds.left + (card.width - bounds.width) / 2;
      const y = card.top - bounds.top + (card.height - bounds.height) / 2;
      const scale = Math.max(0.4, card.width / bounds.width);
      return `translate(${x}px, ${y}px) perspective(650px) rotateY(${tilt}deg) scale(${scale})`;
    }
    const tilt = window.matchMedia('(max-width: 520px)').matches ? 0 : backward ? 31 : -31;
    const timing = transitionTiming;
    outgoing.src = before.src;
    outgoing.hidden = false;
    const departure = outgoing.animate(
      [
        { transform: 'none', opacity: 1 },
        { transform: transform(toward, -tilt), opacity: 0 },
      ],
      timing,
    );
    departure.onfinish = () => {
      outgoing.hidden = true;
      outgoing.removeAttribute('src');
    };
    motion.current = [
      departure,
      center.animate(
        [
          { transform: transform(from, tilt), opacity: 0.65 },
          { transform: 'none', opacity: 1 },
        ],
        timing,
      ),
    ];
  }

  const browsing = targetOffset !== undefined && showing && currentID === navigationOrigin;
  const acceptedNeighbor = {
    src,
    cacheDecoded: undefined,
    markUnavailable: undefined,
  };
  const hiddenNeighbor = { ...acceptedNeighbor, src: null };
  const shownEarlier = !browsing
    ? earlier
    : offset === 1
      ? acceptedNeighbor
      : offset === -1
        ? earlierBeyond
        : offset === 2
          ? later
          : hiddenNeighbor;
  const shownLater = !browsing
    ? later
    : offset === -1
      ? acceptedNeighbor
      : offset === 1
        ? laterBeyond
        : offset === -2
          ? earlier
          : hiddenNeighbor;
  const loadingTarget =
    targetOffset !== undefined &&
    offset !== 0 &&
    !showing &&
    navigation &&
    currentID === navigationOrigin &&
    held.current?.scope === thumbnailScope
      ? held.current.src
      : null;

  return (
    <figure className="wallpaper-figure">
      <div
        className="wallpaper-strip"
        role="group"
        aria-label="Wallpaper navigation"
        tabIndex={0}
        ref={stripRef}
        data-retaining={Boolean(retained)}
        onKeyDown={(event) => {
          if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
          const deck = event.currentTarget;
          const target = event.target;
          if (
            target !== deck &&
            target !== deck.querySelector('#previous') &&
            target !== deck.querySelector('#next')
          )
            return;
          if (event.key === 'ArrowLeft') {
            event.preventDefault();
            if (!previousDisabled) previous();
          }
          if (event.key === 'ArrowRight') {
            event.preventDefault();
            if (!nextDisabled) next();
          }
        }}
      >
        {(src || retained) && (
          <img
            className="wallpaper-wash"
            alt=""
            aria-hidden="true"
            src={src || retained || undefined}
          />
        )}
        {retained && (
          <img className="wallpaper-retained" alt="" aria-hidden="true" src={retained} />
        )}
        <img className="wallpaper-departing" alt="" aria-hidden="true" hidden ref={departing} />
        <button
          type="button"
          id="previous"
          className="wallpaper-neighbor"
          aria-label="Previous wallpaper"
          disabled={previousDisabled}
          data-start={(status.history_cursor ?? 0) + offset === 0}
          onClick={previous}
        >
          <span ref={earlierCard} className="neighbor-card" aria-hidden="true">
            {shownEarlier.src && (
              <img
                key={shownEarlier.src}
                alt=""
                src={shownEarlier.src}
                onLoad={(event) => {
                  shownEarlier.cacheDecoded?.(event.currentTarget);
                  markDecoded(shownEarlier.src!);
                }}
                onError={shownEarlier.markUnavailable}
              />
            )}
            <span className="neighbor-chevron" />
          </span>
        </button>
        {children(animate)}
        {loadingTarget && (
          <img
            className="wallpaper-optimistic wallpaper-optimistic-held"
            src={loadingTarget}
            alt=""
            aria-hidden="true"
          />
        )}
        {displayOptimistic && (
          <img
            key={displayOptimistic}
            ref={optimisticRef}
            className="wallpaper-optimistic"
            data-ready={displayReady}
            src={displayOptimistic}
            alt=""
            aria-hidden="true"
            onLoad={(event) => {
              if (optimistic === displayOptimistic) speculative.cacheDecoded(event.currentTarget);
              markDecoded(displayOptimistic);
            }}
          />
        )}
        {navigation && (
          <span className="wallpaper-pending" role="status">
            Applying wallpaper…
          </span>
        )}
        <button
          type="button"
          id="next"
          className="wallpaper-neighbor"
          aria-label="Next wallpaper"
          disabled={nextDisabled}
          onClick={next}
        >
          <span ref={laterCard} className="neighbor-card" aria-hidden="true">
            {shownLater.src && (
              <img
                key={shownLater.src}
                alt=""
                src={shownLater.src}
                onLoad={(event) => {
                  shownLater.cacheDecoded?.(event.currentTarget);
                  markDecoded(shownLater.src!);
                }}
                onError={shownLater.markUnavailable}
              />
            )}
            <span className="neighbor-chevron" />
          </span>
        </button>
      </div>
      {caption}
    </figure>
  );
}
