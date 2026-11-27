import { Button, FormActions, Notice } from '../components/ui';
import { sourceLabels } from '../sources/sourceLabel';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { bridge, usePanora } from '../client';
import { AddSources, StartWallpapers, useOnboarding } from '../onboarding/Onboarding';
import { ActionMenu } from '../components/ui/ActionMenu';
import { playbackWarnings } from './playbackWarnings';
import { useAcceptedPreview } from './useAcceptedPreview';
import { reviewImageRules, sourceAttention } from '../sources/SourceHealth';
import { imageLabel } from '../imageLabel';
import { WallpaperStrip } from './WallpaperStrip';
import { useSaveWallpaper } from './useSaveWallpaper';
import { hasLiveAspectRule, ThumbnailCache } from './thumbnailCache';
import type { NavigationRequest, Snapshot } from '../types';

type Direction = 'next' | 'previous';

interface NavigationBurst {
  instance: string;
  earliest: number;
  completed: number;
  desired: number;
  active?: { token: string; target: number; canceled: boolean };
}

function navigationToken() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('');
}

function clock(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? 'Time unavailable'
    : date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}
function aspectRatio(width: number, height: number): string {
  if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height) || width < 1 || height < 1) {
    return '';
  }
  let divisor = width;
  let remainder = height;
  while (remainder !== 0) {
    [divisor, remainder] = [remainder, divisor % remainder];
  }
  return `${width / divisor}:${height / divisor}`;
}

export function NowScreen() {
  const onboarding = useOnboarding();
  const ui = usePanora();
  const status = ui.snapshot?.status;
  const config = ui.liveConfiguration || ui.configuration;
  const settingsRevision = config?.revision ?? (ui.settingsError ? '' : undefined);
  const thumbnailScope = JSON.stringify([
    status?.instance_id,
    status?.requested_backend,
    settingsRevision,
    status?.omarchy_theme_revision,
    status?.omarchy_theme_issue,
    status?.history_preview_revision,
  ]);
  const thumbnails = useMemo(() => new ThumbnailCache(thumbnailScope), [thumbnailScope]);
  const {
    accepted,
    metadata,
    imageID,
    previewKey,
    previewLoading,
    previewReady,
    src,
    retry,
    markUnavailable,
    cacheDecoded,
  } = useAcceptedPreview(ui.page === 'now', thumbnails);
  const [pending, setPending] = useState(''),
    [openNotice, setOpenNotice] = useState('');
  const [pendingImage, setPendingImage] = useState<{ id: number; content?: string }>();
  const [navigationOrigin, setNavigationOrigin] = useState<string>();
  const [navigationResult, setNavigationResult] = useState<{
    id: string;
    imageID: number;
    instance: string;
  }>();
  const [navigationView, setNavigationView] = useState<{
    offset: number;
    usesPending: boolean;
    token?: string;
  }>();
  const burstRef = useRef<NavigationBurst | undefined>(undefined);
  const pendingRef = useRef(false),
    restoreFocus = useRef<{ name: string; origin: Element | null } | null>(null),
    stripRef = useRef<HTMLDivElement>(null);
  const navigationInstance = status?.instance_id;
  const navigationCurrent = status?.current?.id;
  useEffect(() => {
    if (
      (pending !== 'next' && pending !== 'previous') ||
      navigationView?.usesPending === false ||
      !ui.connected ||
      ui.page !== 'now'
    ) {
      setPendingImage(undefined);
      return;
    }
    let canceled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function poll() {
      try {
        const result = await bridge('Status');
        if (canceled || navigationView?.token !== burstRef.current?.active?.token) return;
        if (
          result.connected &&
          result.status &&
          result.status.instance_id === navigationInstance &&
          result.status.current?.id === navigationCurrent
        )
          setPendingImage(
            result.status.pending_image_id
              ? {
                  id: result.status.pending_image_id,
                  content: result.status.pending_preview_content,
                }
              : undefined,
          );
      } catch {
        // This read is advisory. The action owns the authoritative result.
      }
      if (!canceled) timer = setTimeout(() => void poll(), 150);
    }
    void poll();
    return () => {
      canceled = true;
      if (timer) clearTimeout(timer);
    };
  }, [
    pending,
    navigationInstance,
    navigationCurrent,
    navigationView?.usesPending,
    navigationView?.token,
    ui.connected,
    ui.page,
  ]);
  useEffect(() => {
    setOpenNotice('');
  }, [accepted?.id, imageID]);
  useLayoutEffect(() => {
    if (!restoreFocus.current || pending || ui.busy) return;
    const { name, origin } = restoreFocus.current;
    restoreFocus.current = null;
    if (
      ui.getState().page !== 'now' ||
      ![document.body, document.documentElement, origin].includes(document.activeElement)
    )
      return;
    const excluded = name === 'exclude' && status?.current_excluded;
    const target = excluded ? document.getElementById('image-actions') : origin;
    if (
      target instanceof HTMLElement &&
      target.isConnected &&
      !target.matches(':disabled') &&
      !target.closest('[hidden]')
    ) {
      if (excluded) target.closest('details')?.removeAttribute('open');
      target.focus();
    } else if (name === 'next' || name === 'previous') stripRef.current?.focus();
  });
  const blocked =
    ui.busy || pendingRef.current || !ui.connected || ui.uncertain || Boolean(status?.busy);
  const playbackBlocked = blocked || onboarding.blocked || Boolean(status?.uncertain);
  const saveWallpaper = useSaveWallpaper(playbackBlocked);
  const navigationBlocked =
    blocked ||
    (status?.uncertain ? onboarding.working || onboarding.enrolling : onboarding.blocked);
  const hasBurst = navigationView !== undefined;
  const canQueue = hasBurst && ui.connected && !ui.uncertain && !status?.setup_required;
  const previousStart = (status?.history_cursor ?? 0) + (navigationView?.offset ?? 0) === 0;
  const title = metadata ? imageLabel(metadata, false) : 'Image details unavailable';
  function openPictureSettings() {
    ui.navigate('settings');
    requestAnimationFrame(() => {
      if (ui.getState().page !== 'settings') return;
      const heading = document.getElementById('image-settings-title');
      heading?.focus();
      heading?.scrollIntoView?.({ block: 'nearest' });
    });
  }
  function showDestination(burst: NavigationBurst) {
    const offset = burst.desired - burst.completed;
    const usesPending = burst.active?.target === burst.desired;
    if (!usesPending) setPendingImage(undefined);
    setNavigationView({ offset, usesPending, token: burst.active?.token });
  }

  async function navigateRapidly(direction: Direction) {
    const step = direction === 'next' ? 1 : -1;
    const existing = burstRef.current;
    if (existing) {
      const live = ui.getState();
      if (
        live.page !== 'now' ||
        !live.connected ||
        live.uncertain ||
        live.snapshot?.status?.setup_required ||
        live.snapshot?.status?.instance_id !== existing.instance
      )
        return;
      const activeTarget = existing.active?.target ?? existing.completed;
      const low = Math.max(existing.earliest, existing.completed - 2, activeTarget - 2);
      const high = Math.min(existing.completed + 2, activeTarget + 2);
      const desired = Math.min(high, Math.max(low, existing.desired + step));
      if (desired === existing.desired) return;
      existing.desired = desired;
      showDestination(existing);
      const active = existing.active;
      if (active && !active.canceled && desired !== active.target) {
        active.canceled = true;
        void Promise.resolve()
          .then(() => bridge('CancelNavigation', [existing.instance, active.token]))
          .catch(() => {
            // The action result still decides whether the setter was reached.
          });
      }
      return;
    }
    const initial = ui.getState().snapshot?.status;
    if (navigationBlocked || !initial?.instance_id || initial.setup_required) return;
    if (direction === 'previous' && !initial.history_cursor) return;
    const burst: NavigationBurst = {
      instance: initial.instance_id,
      earliest: -Math.max(0, initial.history_cursor ?? 0),
      completed: 0,
      desired: step,
    };
    const focus = document.activeElement;
    burstRef.current = burst;
    pendingRef.current = true;
    let acceptedOrigin = initial.current?.id ?? '';
    let result: Snapshot | null = null;
    try {
      while (
        burst.desired !== burst.completed &&
        ui.getState().page === 'now' &&
        ui.getState().snapshot?.status?.instance_id === burst.instance
      ) {
        const target = burst.desired;
        const request: NavigationRequest = {
          instance: burst.instance,
          current: acceptedOrigin,
          token: navigationToken(),
          offset: target - burst.completed,
        };
        burst.active = { token: request.token, target, canceled: false };
        setPendingImage(undefined);
        setNavigationResult(undefined);
        setNavigationOrigin(acceptedOrigin || undefined);
        const command = request.offset > 0 ? 'next' : 'previous';
        setPending(command);
        showDestination(burst);
        result = await ui.action(command, undefined, request);
        burst.active = undefined;
        if (result?.code === 'navigation_superseded') continue;
        if (
          !result?.connected ||
          result.error ||
          result.code ||
          result.status?.uncertain ||
          result.status?.instance_id !== burst.instance ||
          !result.status.current
        )
          break;
        acceptedOrigin = result.status.current.id;
        burst.completed = target;
        burst.earliest = target - (result.status.history_cursor ?? 0);
        burst.desired = Math.max(burst.earliest, burst.desired);
        if (burst.desired === burst.completed)
          setNavigationResult({
            id: acceptedOrigin,
            imageID: result.status.current.image_id,
            instance: burst.instance,
          });
      }
    } finally {
      burstRef.current = undefined;
      pendingRef.current = false;
      setNavigationView(undefined);
      restoreFocus.current = { name: direction, origin: focus };
      setPending('');
    }
  }

  async function action(name: string) {
    if ((name === 'next' || name === 'previous') && status?.navigation_supported)
      return navigateRapidly(name);
    if (pendingRef.current) return;
    if (name === 'next' || name === 'previous' ? navigationBlocked : playbackBlocked) return;
    const focus = document.activeElement,
      page = ui.getState().page,
      expected =
        name === 'exclude' && accepted
          ? { ...accepted, instance_id: status?.instance_id }
          : undefined;
    pendingRef.current = true;
    setPendingImage(undefined);
    setNavigationResult(undefined);
    setNavigationOrigin(accepted?.id);
    setPending(name);
    try {
      const result = await ui.action(name, expected);
      if (
        (name === 'next' || name === 'previous') &&
        result?.connected &&
        !result.error &&
        !result.code &&
        result.status?.current
      ) {
        setNavigationResult({
          id: result.status.current.id,
          imageID: result.status.current.image_id,
          instance: result.status.instance_id || '',
        });
      }
    } finally {
      pendingRef.current = false;
      if (page === 'now') restoreFocus.current = { name, origin: focus };
      setPending('');
    }
  }
  async function open(target: 'page' | 'folder') {
    if (playbackBlocked || !metadata || !imageID) return;
    pendingRef.current = true;
    setPending('open');
    setOpenNotice('');
    try {
      const result = await ui.request('OpenCurrent', [imageID, target]);
      if (!result.opened)
        setOpenNotice(
          result.error ||
            'This image location is unavailable. Refresh to check the accepted image.',
        );
    } finally {
      pendingRef.current = false;
      setPending('');
    }
  }
  const code = ui.snapshot?.code || status?.last_error?.code,
    waiting = code === 'discovery_pending';
  const recover =
    ui.connected &&
    !status?.setup_required &&
    !status?.uncertain &&
    (waiting || ['selection_failed', 'no_images', 'rule_unavailable'].includes(code || ''));
  const uncertain = ui.connected && Boolean(status?.uncertain) && (
    <section className="recovery-panel" aria-labelledby="uncertain-title">
      <h2 id="uncertain-title">Wallpaper change unconfirmed</h2>
      <p>
        Check your desktop.{' '}
        {status?.setup_required
          ? 'Enable a source before choosing a wallpaper to confirm.'
          : `Choose ${accepted && (status?.history_cursor ?? 0) > 0 ? 'Next or Previous' : 'Next'} to confirm a wallpaper before resuming rotation.`}
      </p>
      {(!accepted || status?.setup_required) && (
        <FormActions>
          {status?.setup_required ? (
            <Button onClick={() => ui.navigate('providers')}>Enable a source</Button>
          ) : (
            <Button disabled={navigationBlocked} onClick={() => void action('next')}>
              Next wallpaper
            </Button>
          )}
        </FormActions>
      )}
    </section>
  );
  const warning = sourceAttention(status?.providers);
  const serviceWarnings = playbackWarnings(status?.warnings);
  const serviceWarning = ui.connected && serviceWarnings.length > 0 && (
    <div className="provider-summary" data-warning="true">
      <p>{serviceWarnings.join(' ')}</p>
      <Button
        type="button"
        onClick={() => {
          ui.navigate('settings');
          requestAnimationFrame(() => {
            if (ui.getState().page !== 'settings') return;
            const options = document.getElementById('service-options');
            if (options instanceof HTMLDetailsElement) {
              options.open = true;
              options.querySelector('summary')?.focus();
              options.scrollIntoView({ block: 'nearest' });
            }
          });
        }}
      >
        Service settings →
      </Button>
    </div>
  );
  const dimensions = metadata?.dimensions;
  const shownLabels = new Set<string>();
  const details = [
    dimensions ? `${dimensions.width} × ${dimensions.height}` : '',
    dimensions ? aspectRatio(dimensions.width, dimensions.height) : '',
    metadata?.attribution?.author || '',
    ...(metadata ? sourceLabels(metadata, ui.liveConfiguration || ui.configuration) : []),
    ui.snapshot?.current_error ? 'Image details temporarily unavailable' : '',
  ]
    .filter((label) => {
      const key = label.trim().toLowerCase();
      if (!key || shownLabels.has(key)) return false;
      shownLabels.add(key);
      return true;
    })
    .join(' · ');
  const rotation =
    pending === 'pause'
      ? 'Pausing…'
      : pending === 'resume'
        ? 'Resuming…'
        : pending === 'exclude'
          ? 'Excluding image…'
          : pending && pending !== 'open'
            ? 'Changing wallpaper…'
            : status?.setup_required
              ? 'Waiting for setup'
              : status?.uncertain
                ? 'Wallpaper change unconfirmed'
                : status?.busy
                  ? 'Changing wallpaper…'
                  : status?.paused
                    ? 'Rotation paused'
                    : 'Rotation on';
  const heading = (
    <div className="heading">
      <h1 id="now-title" tabIndex={-1}>
        Wallpaper
      </h1>
      {accepted && !ui.connected && (
        <Notice id="connection" className="connection" data-connected={false}>
          Disconnected · Showing last known state
        </Notice>
      )}
    </div>
  );
  if (!accepted)
    return (
      <>
        {heading}
        {serviceWarning}
        {uncertain}
        <section className="onboarding-empty" aria-labelledby="choose-wallpapers-title">
          <h2 id="choose-wallpapers-title">Choose your wallpapers</h2>
          <AddSources />
          <StartWallpapers />
          <p className="subtext">
            Image filters and picture fit are optional.{' '}
            <Button type="button" className="outline" onClick={openPictureSettings}>
              Picture settings
            </Button>
          </p>
        </section>
      </>
    );
  return (
    <>
      {heading}
      <StartWallpapers />
      {uncertain}
      {recover && (
        <section
          className="recovery-panel"
          id="wallpaper-recovery"
          aria-labelledby="wallpaper-recovery-title"
        >
          <h2 id="wallpaper-recovery-title">
            {waiting
              ? 'Checking your sources'
              : code === 'rule_unavailable'
                ? 'Review your image filters'
                : 'No wallpaper is ready'}
          </h2>
          <p id="wallpaper-recovery-detail">
            {waiting
              ? 'Panora is still checking for images. Check source progress, then choose Next when an image is ready.'
              : code === 'rule_unavailable'
                ? 'A source needs a known display shape. Edit its image filters in Sources to choose Any ratio or an explicit ratio, or reconnect a supported display.'
                : 'Panora could not choose an available image with the current sources and filters. Check source access or preview your image filters to see what needs adjusting.'}
            {' Your last accepted wallpaper is kept.'}
          </p>
          <p id="wallpaper-recovery-rules" className="subtext">
            Each folder and search has its own image filters. Edit the affected source to preview
            matches.
          </p>
          <FormActions>
            <Button
              id="recovery-sources"
              className="outline"
              onClick={() => ui.navigate('providers')}
            >
              Check sources
            </Button>
            <Button
              id="recovery-rules"
              className="outline"
              onClick={() => reviewImageRules(ui.navigate)}
            >
              Review image filters
            </Button>
          </FormActions>
        </section>
      )}
      <WallpaperStrip
        status={status!}
        active={ui.connected && ui.page === 'now'}
        previewReady={previewReady}
        settingsRevision={settingsRevision}
        thumbnails={thumbnails}
        allowHistoryReuse={!hasLiveAspectRule(config)}
        src={src}
        navigation={pending === 'next' || pending === 'previous' ? pending : undefined}
        targetOffset={navigationView?.offset}
        pendingImage={navigationView?.usesPending === false ? undefined : pendingImage}
        navigationOrigin={navigationOrigin}
        navigationResult={navigationResult}
        previous={() => void action('previous')}
        next={() => void action('next')}
        previousDisabled={
          (hasBurst ? !canQueue : navigationBlocked || Boolean(status?.setup_required)) ||
          previousStart
        }
        nextDisabled={hasBurst ? !canQueue : navigationBlocked || Boolean(status?.setup_required)}
        stripRef={stripRef}
        caption={
          <figcaption id="preview-caption" hidden={ui.connected}>
            {src
              ? 'Last known image · Reconnect to check the current state.'
              : 'No connection to Panora.'}
          </figcaption>
        }
      >
        {(animate) => (
          <div className="preview" id="preview-frame" aria-busy={previewLoading}>
            {src ? (
              <img
                id="live-preview"
                key={previewKey}
                src={src}
                alt={'Wallpaper: ' + title}
                onLoad={(event) => {
                  cacheDecoded(event.currentTarget);
                  animate(event);
                }}
                onError={markUnavailable}
              />
            ) : (
              <div id="placeholder">
                <p id="preview-message">
                  {previewLoading ? 'Loading image…' : 'Preview unavailable'}
                </p>
                {!previewLoading && (
                  <span id="preview-detail">
                    The preview could not be loaded. Check the desktop or try again.
                  </span>
                )}
                {ui.connected && !previewLoading && (
                  <Button className="outline" onClick={retry}>
                    Retry preview
                  </Button>
                )}
              </div>
            )}
            <ActionMenu className="preview-actions" id="image-actions" label="Image actions">
              {saveWallpaper.actions}
              {metadata?.attribution?.page_url && (
                <Button id="open-page" disabled={playbackBlocked} onClick={() => void open('page')}>
                  View on website ↗
                </Button>
              )}
              {metadata?.original_path && (
                <Button
                  id="open-folder"
                  disabled={playbackBlocked}
                  onClick={() => void open('folder')}
                >
                  Show in folder
                </Button>
              )}
              <Button
                id="exclude-current"
                disabled={playbackBlocked || status?.current_excluded}
                onClick={() => void action('exclude')}
              >
                {status?.current_excluded ? 'Image excluded' : 'Exclude image'}
              </Button>
              <Button onClick={openPictureSettings}>Picture settings</Button>
            </ActionMenu>
          </div>
        )}
      </WallpaperStrip>
      <section className="accepted" aria-labelledby="title">
        <h2 id="title">{title}</h2>
        <p id="source">{details}</p>
        {saveWallpaper.notice}
        <Notice id="open-notice" hidden={!openNotice}>
          {openNotice}
        </Notice>
        <p id="exclusion-state" className="subtext" hidden={!status?.current_excluded}>
          This image stays on the desktop and is skipped in future rotation. Original files are
          kept.
        </p>
      </section>
      <footer className="now-rotation" hidden={onboarding.blocked}>
        <Button
          id="pause"
          className="outline"
          disabled={playbackBlocked || status?.setup_required}
          onClick={() => void action(status?.paused ? 'resume' : 'pause')}
        >
          <span aria-hidden="true">{status?.paused ? '▶' : 'Ⅱ'}</span>
          {status?.paused ? 'Resume rotation' : 'Pause rotation'}
        </Button>
        <div aria-live="polite">
          <p
            id="rotation"
            hidden={ui.connected && rotation === 'Rotation on' && Boolean(status?.next_at)}
          >
            {rotation}
          </p>
          <p
            id="next-change"
            hidden={
              ui.connected && Boolean(status?.setup_required || status?.paused || status?.uncertain)
            }
          >
            {!ui.connected
              ? 'Reconnect to check the schedule.'
              : status?.next_at
                ? 'Next change at ' + clock(status.next_at)
                : 'The service will schedule the next change when an image is ready.'}
          </p>
        </div>
      </footer>
      {ui.connected && warning && (
        <div id="provider-summary" className="provider-summary" data-warning="true">
          <p id="provider-warning">{warning}</p>
          <Button
            onClick={() => {
              ui.navigate('providers');
              requestAnimationFrame(() => {
                const troubleshooting = document.getElementById('source-troubleshooting');
                if (troubleshooting instanceof HTMLDetailsElement) {
                  troubleshooting.open = true;
                  troubleshooting.querySelector('summary')?.focus();
                }
              });
            }}
          >
            Check sources →
          </Button>
        </div>
      )}
      {serviceWarning}
    </>
  );
}
