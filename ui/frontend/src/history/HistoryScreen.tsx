import { Button, Notice } from '../components/ui';
import { sourceLabel } from '../sources/sourceLabel';
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { bridge, usePanora } from '../client';
import { useOnboarding } from '../onboarding/Onboarding';
import type { Exclusion, ExclusionPage, HistoryEntry, HistoryPage, PreviewResult } from '../types';
import { reviewImageRules } from '../sources/SourceHealth';
import { previewImage } from '../previewImage';
import { ExcludedCard, HistoryCard } from './HistoryCard';
import type { Thumbnail } from './HistoryCard';

type Mode = 'recent' | 'excluded';
interface RecentView {
  page: HistoryPage | null;
  scope: string | null;
  cursor: string;
  previous: string[];
}
interface ExcludedView {
  page: ExclusionPage | null;
  scope: string | null;
  cursor: number;
  previous: number[];
}
type PageData = { mode: 'recent'; page: HistoryPage } | { mode: 'excluded'; page: ExclusionPage };
interface ReadOutcome {
  data?: PageData;
  error?: string;
}
export function HistoryScreen() {
  const onboarding = useOnboarding();
  const ui = usePanora(),
    [mode, setMode] = useState<Mode>('recent'),
    [recent, setRecent] = useState<RecentView>({
      page: null,
      scope: null,
      cursor: '',
      previous: [],
    }),
    [excluded, setExcluded] = useState<ExcludedView>({
      page: null,
      scope: null,
      cursor: 0,
      previous: [],
    });
  const [loading, setLoading] = useState(false),
    [operation, setOperation] = useState(false),
    [notice, setNotice] = useState(''),
    [previews, setPreviews] = useState<Record<number, Thumbnail>>({});
  const readingRef = useRef(false),
    operationRef = useRef(false),
    revision = useRef(0),
    inFlight = useRef<{ key: string; promise: Promise<ReadOutcome> } | null>(null),
    previewReads = useRef(new Map<string, Promise<PreviewResult>>()),
    listRef = useRef<HTMLUListElement>(null),
    lastCurrent = useRef<string | undefined>(undefined),
    requestedNavigation = useRef<string | null>(null),
    readFailure = useRef<string | null>(null);
  const status = ui.snapshot?.status,
    currentID = status?.current?.id;
  const scope = ui.connected ? JSON.stringify([status?.instance_id || null]) : null;
  const navigation = JSON.stringify([ui.page, mode, scope]);
  const navigationRef = useRef(navigation);
  navigationRef.current = navigation;
  const blocked =
    operation ||
    loading ||
    ui.busy ||
    !ui.connected ||
    ui.uncertain ||
    Boolean(status?.busy || status?.uncertain);
  const view = mode === 'recent' ? recent : excluded,
    page = view.page;
  const stale = view.scope !== scope;
  function recoverFocus(previous: Element | null) {
    requestAnimationFrame(() => {
      if (
        !previous ||
        ui.getState().page !== 'history' ||
        (document.activeElement !== document.body &&
          document.activeElement !== document.documentElement &&
          document.activeElement !== previous)
      )
        return;
      if (
        previous instanceof HTMLElement &&
        previous.isConnected &&
        !previous.closest('[hidden]') &&
        !(previous instanceof HTMLButtonElement && previous.disabled)
      ) {
        previous.focus();
        return;
      }
      const card = previous.closest<HTMLElement>('[data-history-id]'),
        action = previous instanceof HTMLElement ? previous.dataset.historyAction : undefined;
      const replacement = Array.from(
        listRef.current?.querySelectorAll<HTMLButtonElement>('button') || [],
      ).find(
        (node) =>
          node.closest<HTMLElement>('[data-history-id]')?.dataset.historyId ===
            card?.dataset.historyId &&
          node.dataset.historyAction === action &&
          !node.disabled,
      );
      (replacement || document.getElementById('history-title'))?.focus();
    });
  }
  function read(target: Mode, cursor: string | number): Promise<ReadOutcome> {
    const key = JSON.stringify([scope, target, cursor]);
    if (inFlight.current?.key === key) return inFlight.current.promise;
    const promise = (async (): Promise<ReadOutcome> => {
      try {
        if (target === 'recent') {
          const result = await ui.request('History', [String(cursor)]);
          if (!result.page)
            return {
              error: result.error || 'This page could not be loaded. Reload history to try again.',
            };
          if (!Array.isArray(result.page.entries) || result.page.entries.length > 10)
            return { error: 'Panora returned an invalid history page. Reconnect to check it.' };
          return { data: { mode: 'recent', page: result.page } };
        }
        const result = await ui.request('Exclusions', [Number(cursor)]);
        if (!result.page)
          return {
            error: result.error || 'This page could not be loaded. Reload history to try again.',
          };
        if (!Array.isArray(result.page.entries) || result.page.entries.length > 10)
          return { error: 'Panora returned an invalid exclusions page. Reconnect to check it.' };
        return { data: { mode: 'excluded', page: result.page } };
      } catch {
        return { error: 'Panora did not respond. Reconnect or reload this page.' };
      }
    })();
    inFlight.current = { key, promise };
    void promise.finally(() => {
      if (inFlight.current?.promise === promise) inFlight.current = null;
    });
    return promise;
  }
  async function load(
    target: Mode,
    cursor: string | number,
    previous: string[] | number[],
    quiet = false,
    focus: Element | null = document.activeElement,
  ) {
    if (!ui.getState().connected) {
      setNotice('Reconnect to Panora before loading history. The last loaded page is kept.');
      return;
    }
    const token = ++revision.current,
      requestNavigation = navigationRef.current,
      requestScope = scope;
    requestedNavigation.current = requestNavigation;
    readingRef.current = true;
    setLoading(true);
    if (!quiet) {
      setNotice('');
      readFailure.current = null;
    }
    try {
      const result = await read(target, cursor),
        latest = ui.getState();
      if (
        token !== revision.current ||
        requestNavigation !== navigationRef.current ||
        latest.page !== 'history' ||
        !latest.connected ||
        requestScope !== JSON.stringify([latest.snapshot?.status?.instance_id || null])
      )
        return;
      if (!result.data) {
        readFailure.current = result.error || 'History could not be loaded.';
        setNotice(readFailure.current);
        return;
      }
      const previousFailure = readFailure.current;
      if (previousFailure) setNotice((current) => (current === previousFailure ? '' : current));
      readFailure.current = null;
      if (result.data.mode === 'recent')
        setRecent({
          page: result.data.page,
          scope: requestScope,
          cursor: String(cursor),
          previous: previous.map(String),
        });
      else
        setExcluded({
          page: result.data.page,
          scope: requestScope,
          cursor: Number(cursor),
          previous: previous.map(Number),
        });
    } finally {
      if (token === revision.current) {
        readingRef.current = false;
        setLoading(false);
        recoverFocus(focus);
      }
    }
  }
  const loadOnNavigation = useEffectEvent(() => {
    const current = mode === 'recent' ? recent : excluded;
    if (
      ui.busy ||
      operationRef.current ||
      !ui.connected ||
      requestedNavigation.current === navigation
    )
      return;
    requestedNavigation.current = navigation;
    void load(
      mode,
      current.scope === scope ? current.cursor : mode === 'recent' ? '' : 0,
      current.scope === scope ? current.previous : [],
      Boolean(current.page),
    );
  });
  const invalidateNavigation = useEffectEvent(() => {
    ++revision.current;
    inFlight.current = null;
    requestedNavigation.current = null;
    readingRef.current = false;
    setLoading(false);
  });
  useEffect(() => {
    if (ui.connected) return;
    setRecent((current) => (current.scope === null ? current : { ...current, scope: null }));
    setExcluded((current) => (current.scope === null ? current : { ...current, scope: null }));
  }, [ui.connected]);
  useEffect(() => {
    if (ui.page !== 'history') return;
    return () => invalidateNavigation();
  }, [navigation, ui.page]);
  useEffect(() => {
    if (ui.page === 'history') loadOnNavigation();
  }, [navigation, ui.page, ui.busy, operation]);
  useEffect(() => {
    let canceled = false;
    setPreviews({});
    // Navigation can start a page read in an earlier effect from this same render.
    if (
      ui.page !== 'history' ||
      mode !== 'recent' ||
      !scope ||
      loading ||
      readingRef.current ||
      requestedNavigation.current !== navigation ||
      stale
    )
      return;
    const pageRevision = revision.current;
    const isCurrent = () =>
      !canceled && navigationRef.current === navigation && revision.current === pageRevision;
    void (async () => {
      for (const entry of recent.page?.entries || []) {
        // An old visit can no longer publish, but its read still holds this service's cache lock.
        await previewReads.current.get(scope)?.catch(() => undefined);
        if (!isCurrent()) break;
        let thumbnail: Thumbnail = null;
        try {
          const request = bridge('Preview', [entry.image_id]);
          previewReads.current.set(scope, request);
          try {
            const result = await request;
            const src = previewImage(result, entry.image_id);
            if (src) thumbnail = { src };
            else if (result.code === 'busy') thumbnail = 'busy';
          } finally {
            previewReads.current.delete(scope);
          }
        } catch {
          // Preserve the unavailable message for other failed preview reads.
        }
        if (!isCurrent()) break;
        setPreviews((current) => ({ ...current, [entry.image_id]: thumbnail }));
      }
    })();
    return () => {
      canceled = true;
    };
  }, [recent.page, ui.page, mode, scope, loading, stale, navigation]);
  useEffect(() => {
    const current = currentID;
    if (
      lastCurrent.current &&
      current !== lastCurrent.current &&
      recent.page &&
      mode === 'recent' &&
      ui.page === 'history' &&
      !readingRef.current &&
      !operationRef.current
    )
      setNotice('The current wallpaper changed. Reload history to see the latest entries.');
    lastCurrent.current = current;
  }, [currentID, recent.page, mode, ui.page]);
  function canOperate(ownOperation = false): boolean {
    const current = ui.getState();
    return (
      (ownOperation || !operationRef.current) &&
      !readingRef.current &&
      current.page === 'history' &&
      view.scope === JSON.stringify([current.snapshot?.status?.instance_id || null]) &&
      navigationRef.current === navigation &&
      !current.busy &&
      current.connected &&
      !current.uncertain &&
      !current.snapshot?.status?.busy &&
      !current.snapshot?.status?.uncertain
    );
  }
  async function mutate(entry: HistoryEntry | Exclusion) {
    if (!canOperate() || (mode === 'recent' && onboarding.blocked)) return;
    const focus = document.activeElement,
      target = mode,
      current = target === 'recent' ? recent : excluded;
    operationRef.current = true;
    setOperation(true);
    setNotice('');
    try {
      await ui.refresh({ background: true });
      if (!canOperate(true)) return;
      const result =
        target === 'recent'
          ? await ui.request('ReplayHistory', [String(entry.id)], {
              mutation: true,
              settings: false,
            })
          : await ui.request('RestoreExclusion', [Number(entry.id)], {
              mutation: true,
              settings: false,
            });
      const success =
        target === 'recent'
          ? 'Wallpaper request accepted. Rotation and pause remain controlled by the service.'
          : 'Image restored to rotation. It will be checked against the current image rules.';
      setNotice(result.error || success);
      await ui.refresh();
      if (result.connected && ui.getState().page === 'history')
        await load(target, current.cursor, current.previous, true, focus);
      if (!result.error) setNotice(success);
    } finally {
      operationRef.current = false;
      setOperation(false);
      recoverFocus(focus);
    }
  }
  async function open(entry: HistoryEntry, target: 'page' | 'folder') {
    if (!canOperate()) return;
    operationRef.current = true;
    setOperation(true);
    setNotice('');
    const focus = document.activeElement;
    try {
      await ui.refresh({ background: true });
      if (!canOperate(true)) return;
      const result = await ui.request('OpenHistory', [entry.id, entry.image_id, target]);
      if (!result.opened)
        setNotice(
          result.error || 'This original location is unavailable. Reload history to check it.',
        );
    } finally {
      operationRef.current = false;
      setOperation(false);
      recoverFocus(focus);
    }
  }
  function older() {
    if (blocked || stale || readingRef.current || !page?.next_before) return;
    if (mode === 'recent')
      void load('recent', recent.page?.next_before || '', [...recent.previous, recent.cursor]);
    else
      void load('excluded', excluded.page?.next_before || 0, [
        ...excluded.previous,
        excluded.cursor,
      ]);
  }
  function newer() {
    if (blocked || stale || readingRef.current) return;
    if (mode === 'recent') {
      const previous = recent.previous.at(-1);
      if (previous !== undefined) void load('recent', previous, recent.previous.slice(0, -1));
    } else {
      const previous = excluded.previous.at(-1);
      if (previous !== undefined) void load('excluded', previous, excluded.previous.slice(0, -1));
    }
  }
  const pageState =
    !ui.connected && page
      ? 'Disconnected · Showing the last loaded page.'
      : stale && page
        ? 'Reload history to check this page before using its actions.'
        : loading
          ? 'Loading…'
          : page
            ? `Page ${view.previous.length + 1} · ${page.entries.length} ${page.entries.length === 1 ? 'image' : 'images'}`
            : '';
  return (
    <>
      <div className="heading">
        <h1 id="history-title" tabIndex={-1}>
          History
        </h1>
        <Button
          id="history-refresh"
          className="outline"
          disabled={blocked}
          onClick={() => void load(mode, mode === 'recent' ? '' : 0, [])}
        >
          Reload history
        </Button>
      </div>
      <div className="history-content">
        <div className="history-tabs" role="group" aria-label="History view">
          <Button
            id="history-recent"
            aria-pressed={mode === 'recent'}
            disabled={operation || loading}
            onClick={() => setMode('recent')}
          >
            Recently displayed
          </Button>
          <Button
            id="history-excluded"
            aria-pressed={mode === 'excluded'}
            disabled={operation || loading}
            onClick={() => setMode('excluded')}
          >
            Excluded
          </Button>
        </div>
        <p id="history-explanation" className="subtext">
          {mode === 'recent'
            ? 'Images Panora sent to the desktop, newest first. Display again rechecks the current image rules.'
            : 'Excluded images are skipped in future rotation. Restore removes the mark without changing your current wallpaper.'}
        </p>
        {mode === 'recent' && onboarding.blocked && (
          <p className="subtext">
            Check the first wallpaper on Now before displaying another image.{' '}
            <Button type="button" className="outline" onClick={() => ui.navigate('now')}>
              Go to Now
            </Button>
          </p>
        )}
        <Notice id="history-notice" hidden={!notice}>
          {notice}
        </Notice>
        {page && !page.entries.length && (
          <p id="history-empty" className="history-empty">
            {mode === 'recent'
              ? 'No wallpaper history yet. Display your first image from Now.'
              : 'No excluded images. Images you exclude from Now will appear here.'}
          </p>
        )}
        <ul
          ref={listRef}
          id="history-list"
          className="history-list"
          aria-label={mode === 'recent' ? 'Recently displayed wallpapers' : 'Excluded images'}
          aria-busy={loading || operation}
        >
          {mode === 'excluded'
            ? excluded.page?.entries.map((entry) => (
                <ExcludedCard
                  key={entry.id}
                  entry={entry}
                  disabled={blocked || stale}
                  onRestore={() => void mutate(entry)}
                />
              ))
            : recent.page?.entries.map((entry) => {
                const thumbnail = previews[entry.image_id];
                return (
                  <HistoryCard
                    key={entry.id}
                    entry={entry}
                    thumbnail={stale ? null : thumbnail}
                    source={sourceLabel(entry, ui.liveConfiguration || ui.configuration)}
                    currentLabel={
                      status?.current?.id === entry.id
                        ? ui.connected
                          ? 'Current wallpaper'
                          : 'Last known current wallpaper'
                        : undefined
                    }
                    excluded={Boolean(
                      status?.current_excluded && entry.image_id === status.current?.image_id,
                    )}
                    disabled={blocked || stale}
                    replayDisabled={onboarding.blocked}
                    onPreviewError={() =>
                      setPreviews((current) =>
                        current[entry.image_id] === thumbnail
                          ? { ...current, [entry.image_id]: null }
                          : current,
                      )
                    }
                    onReplay={() => void mutate(entry)}
                    onOpen={(target) => void open(entry, target)}
                    onManageExcluded={() => setMode('excluded')}
                    onReviewSources={() => ui.navigate('providers')}
                    onReviewRules={() => reviewImageRules(ui.navigate)}
                  />
                );
              })}
        </ul>
        <div className="history-pagination">
          <Button
            id="history-newer"
            className="outline"
            hidden={!view.previous.length}
            disabled={blocked || stale}
            onClick={newer}
          >
            ← Newer
          </Button>
          <p id="history-page-state" className="subtext">
            {pageState}
          </p>
          <Button
            id="history-older"
            className="outline"
            hidden={!page?.next_before}
            disabled={blocked || stale}
            onClick={older}
          >
            Older →
          </Button>
        </div>
        <p id="history-retention" className="subtext">
          {mode === 'recent'
            ? 'History keeps up to 100 accepted requests. Cached previews may be removed to make room; original images are kept.'
            : 'Exclusions are kept even after an image leaves history. Restoring keeps original files and checks current image rules when it plays.'}
        </p>
      </div>
    </>
  );
}
