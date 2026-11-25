import { Button, FormActions, Notice } from '../components/ui';
import { createContext, useContext, useEffect, useRef, useState } from 'react';
import type { PropsWithChildren } from 'react';
import { usePanora } from '../client';
import type { ClientState } from '../client';

const confirmedRejections = [
  'selection_failed',
  'discovery_pending',
  'theme_changed',
  'rule_unavailable',
  'no_images',
  'no_previous',
  'history_not_found',
  'history_unavailable',
  'history_excluded',
  'setup_required',
  'config_invalid',
  'backend_unavailable',
  'backend_ambiguous',
  'unsupported_presentation',
];
function hasSources(state: ClientState) {
  const config = state.liveConfiguration || state.configuration;
  return Boolean(
    config?.folders.some((source) => source.enabled) ||
    config?.searches.some((source) => source.enabled) ||
    config?.omarchy_theme?.enabled ||
    config?.inputs?.some((source) => source.enabled),
  );
}
function isFreshSetup(state: ClientState) {
  const config = state.liveConfiguration || state.configuration;
  const status = state.snapshot?.status;
  return Boolean(
    config &&
    !config.folders.length &&
    !config.searches.length &&
    !config.omarchy_theme &&
    !config.inputs?.length &&
    config.rules.min_width === 1 &&
    config.rules.min_height === 1 &&
    config.rules.ratio === 'any' &&
    status?.instance_id &&
    status.setup_required &&
    !status.current &&
    !status.providers?.some((provider) => !provider.enabled),
  );
}
function warming(state: ClientState) {
  const status = state.snapshot?.status;
  return Boolean(
    status?.refreshing ||
    status?.providers?.some(
      (provider) =>
        provider.enabled &&
        provider.inputs.some((input) => input.state === 'pending' || input.state === 'refreshing'),
    ),
  );
}
interface OnboardingState {
  sourceIntent: 'folder' | 'search' | null;
  openSource(kind: 'folder' | 'search'): void;
  consumeSourceIntent(): void;
  working: boolean;
  enrolling: boolean;
  enrollmentNotice: string;
  pending: boolean;
  needsResume: boolean;
  notice: string;
  blocked: boolean;
  start(): Promise<void>;
  chooseFirstFolder(): Promise<void>;
  reconcile(): Promise<void>;
}
const Context = createContext<OnboardingState | null>(null);
export function useOnboarding() {
  const value = useContext(Context);
  if (!value) throw new Error('First wallpaper controls need OnboardingProvider.');
  return value;
}
export function OnboardingProvider({ children }: PropsWithChildren) {
  const ui = usePanora();
  const [working, setWorking] = useState(false),
    [enrolling, setEnrolling] = useState(false),
    [enrollmentNotice, setEnrollmentNotice] = useState(''),
    [pending, setPending] = useState(false),
    [needsResume, setNeedsResume] = useState(false),
    [notice, setNotice] = useState(''),
    [sourceIntent, setSourceIntent] = useState<'folder' | 'search' | null>(null);
  const lock = useRef(false);
  const pageVisit = useRef(0);
  useEffect(() => {
    ++pageVisit.current;
  }, [ui.page]);
  useEffect(() => {
    if (ui.page !== 'providers') setSourceIntent(null);
  }, [ui.page]);
  async function reconcile() {
    if (lock.current || ui.getState().busy) return;
    const page = ui.getState().page,
      origin = document.activeElement;
    lock.current = true;
    setWorking(true);
    try {
      await ui.refresh();
      const fresh = ui.getState(),
        status = fresh.snapshot?.status;
      if (!fresh.connected || !status || status.busy || status.uncertain || fresh.uncertain) {
        setNotice(
          'The first wallpaper is still unconfirmed. Check the connection and try Check status again. Your sources are kept.',
        );
        return;
      }
      const accepted = Boolean(status.current?.id);
      setPending(false);
      setNeedsResume(accepted && Boolean(status.paused));
      setNotice(
        accepted
          ? status.paused
            ? 'The first wallpaper is confirmed. Resume rotation when you are ready.'
            : 'The first wallpaper is confirmed and rotation is on.'
          : 'No first wallpaper was accepted. Your sources are kept; choose Start wallpapers to try again.',
      );
    } finally {
      lock.current = false;
      setWorking(false);
      requestAnimationFrame(() => {
        if (
          ui.getState().page !== page ||
          ![document.body, document.documentElement, origin].includes(document.activeElement)
        )
          return;
        if (
          origin instanceof HTMLElement &&
          origin.isConnected &&
          !origin.closest('[hidden]') &&
          !(origin instanceof HTMLButtonElement && origin.disabled)
        )
          origin.focus();
        else document.getElementById(`${page}-title`)?.focus();
      });
    }
  }
  async function chooseFirstFolder() {
    const initial = ui.getState();
    const reviewed = initial.configuration;
    if (
      lock.current ||
      pending ||
      initial.busy ||
      initial.blocked ||
      !initial.connected ||
      initial.uncertain ||
      initial.page !== 'now' ||
      !reviewed ||
      !isFreshSetup(initial)
    )
      return;
    const visit = pageVisit.current,
      instance = initial.snapshot?.status?.instance_id;
    const sameVisit = () => {
      const latest = ui.getState();
      return (
        pageVisit.current === visit &&
        latest.page === 'now' &&
        latest.connected &&
        latest.snapshot?.status?.instance_id === instance
      );
    };
    const canSave = () => {
      const latest = ui.getState();
      return (
        sameVisit() &&
        !latest.blocked &&
        !latest.uncertain &&
        isFreshSetup(latest) &&
        (latest.liveConfiguration || latest.configuration)?.revision === reviewed.revision
      );
    };
    lock.current = true;
    setEnrolling(true);
    setEnrollmentNotice('');
    setNotice('');
    try {
      const folder = await ui.request('PickFolder', [], { shouldSend: canSave });
      if (!folder.path) {
        if (folder.error) setEnrollmentNotice(folder.error);
        return;
      }
      await ui.refresh();
      const saved = await ui.request(
        'AddFolderSettings',
        [{ path: folder.path, revision: reviewed.revision }],
        { mutation: true, shouldSend: canSave },
      );
      if (!saved.configuration) {
        setEnrollmentNotice(saved.error || 'Check saved settings before choosing a folder again.');
        return;
      }
      const confirmed = saved.configuration;
      const source = confirmed.folders.find((value) => value.path === folder.path && value.enabled);
      if (confirmed.revision === reviewed.revision || !source) {
        setEnrollmentNotice('This folder is already saved or off. Check Sources before starting.');
        return;
      }
      const sameSource = () => {
        const latest = ui.getState();
        const config = latest.liveConfiguration || latest.configuration;
        return (
          sameVisit() &&
          !latest.blocked &&
          !latest.uncertain &&
          config?.revision === confirmed.revision &&
          config.folders.some((value) => value.id === source.id && value.enabled)
        );
      };
      // Discovery is asynchronous. Poll only status/configuration, and allow a
      // slow source to finish without surprising the user with a later start.
      const until = performance.now() + 5000;
      do {
        await ui.refresh();
        if (!sameSource()) {
          setNotice('Your folder was saved. Check the current sources before starting.');
          return;
        }
        const status = ui.getState().snapshot?.status;
        if (status?.current || !status?.paused || status.uncertain) {
          setNotice('Your folder was saved. Check the current wallpaper before starting.');
          return;
        }
        const health = status.providers
          ?.find((provider) => provider.id === 'local' && provider.enabled)
          ?.inputs.find((input) => input.id === source.id);
        if (
          health?.state === 'ready' &&
          !status.busy &&
          !status.setup_required &&
          performance.now() < until
        ) {
          const mayDisplay = () =>
            sameSource() &&
            Boolean(ui.getState().snapshot?.status?.paused) &&
            !ui.getState().snapshot?.status?.current &&
            performance.now() < until;
          await startPlayback('now', document.activeElement, sameSource, mayDisplay);
          return;
        }
        if (health && !['pending', 'refreshing', 'ready'].includes(health.state)) {
          setNotice(
            'Your folder was saved. Check its access and image rules in Sources before starting.',
          );
          return;
        }
        if (performance.now() < until) await new Promise((resolve) => setTimeout(resolve, 200));
      } while (performance.now() < until && sameSource());
      setNotice('Your folder was saved. Choose Start wallpapers once its check is ready.');
    } finally {
      lock.current = false;
      setEnrolling(false);
      requestAnimationFrame(() => {
        if (sameVisit() && document.activeElement === document.body)
          (
            document.querySelector<HTMLElement>('#choose-first-folder:not(:disabled)') ||
            document.getElementById('reload-first-folder-settings') ||
            document.getElementById('now-title')
          )?.focus();
      });
    }
  }
  async function start() {
    const initial = ui.getState();
    if (
      lock.current ||
      pending ||
      initial.busy ||
      !initial.connected ||
      initial.blocked ||
      initial.uncertain
    )
      return;
    lock.current = true;
    setWorking(true);
    setNotice('');
    const origin = document.activeElement;
    try {
      await ui.refresh();
      await startPlayback(initial.page, origin);
    } finally {
      lock.current = false;
      setWorking(false);
    }
  }
  async function startPlayback(
    page: ClientState['page'],
    origin: Element | null,
    shouldSend?: () => boolean,
    mayDisplay = shouldSend,
  ) {
    let fresh = ui.getState(),
      status = fresh.snapshot?.status;
    if (
      !fresh.connected ||
      !status ||
      status.busy ||
      status.uncertain ||
      fresh.uncertain ||
      fresh.blocked
    ) {
      setNotice('Reconnect and check the current state before starting. Your sources are kept.');
      return;
    }
    if (!hasSources(fresh) || status.setup_required) {
      setNotice('Add or enable a folder or Wallhaven search before starting.');
      return;
    }
    if (!status.current) {
      if (warming(fresh)) {
        setNotice(
          'Panora is checking your sources. Your settings are saved; choose Start wallpapers once the check is ready.',
        );
        return;
      }
      setPending(true);
      const result = await ui.request('Action', ['next'], {
        mutation: true,
        settings: false,
        shouldSend: mayDisplay,
      });
      if (result.code === 'workflow_changed') {
        setPending(false);
        setNotice('Your folder was saved. Check the current sources before starting.');
        return;
      }
      if (
        !result.connected ||
        result.error ||
        !result.status?.current ||
        result.status.busy ||
        result.status.uncertain
      ) {
        const rejected =
          result.connected &&
          result.status &&
          !result.status.busy &&
          !result.status.uncertain &&
          confirmedRejections.includes(result.code || '');
        if (rejected) {
          setPending(false);
          setNotice(
            (result.error || 'No wallpaper is ready yet.') +
              ' Your sources are saved. Check their access and optional image filters in Sources, then try again.',
          );
        } else
          setNotice(
            'The first wallpaper may have been displayed. Choose Check status before trying again. Your sources are kept.',
          );
        return;
      }
      setPending(false);
    }
    fresh = ui.getState();
    status = fresh.snapshot?.status;
    if (!status?.current) {
      setNotice('The first wallpaper could not be confirmed. Check status before continuing.');
      return;
    }
    setNeedsResume(Boolean(status.paused));
    if (shouldSend && !shouldSend()) {
      setNotice('Check the current wallpaper before resuming rotation.');
      return;
    }
    if (status.paused) {
      setPending(true);
      const currentID = status.current.id;
      const resumed = await ui.request('Action', ['resume'], {
        mutation: true,
        settings: false,
        shouldSend: shouldSend
          ? () => shouldSend() && ui.getState().snapshot?.status?.current?.id === currentID
          : undefined,
      });
      if (resumed.code === 'workflow_changed') {
        setPending(false);
        setNotice('Check the current wallpaper before resuming rotation.');
        return;
      }
      if (
        !resumed.connected ||
        resumed.error ||
        !resumed.status?.current ||
        resumed.status.paused ||
        resumed.status.busy ||
        resumed.status.uncertain
      ) {
        setNotice(
          'The first wallpaper is accepted, but rotation could not be confirmed. Choose Check status before continuing.',
        );
        return;
      }
    }
    setPending(false);
    setNeedsResume(false);
    if (ui.getState().page === page) ui.navigate('now');
    requestAnimationFrame(() => {
      if (
        ui.getState().page === 'now' &&
        [document.body, document.documentElement, origin].includes(document.activeElement)
      )
        document.getElementById('now-title')?.focus();
    });
  }
  return (
    <Context.Provider
      value={{
        sourceIntent,
        openSource: (kind) => {
          setSourceIntent(kind);
          ui.navigate('providers');
        },
        consumeSourceIntent: () => setSourceIntent(null),
        working,
        enrolling,
        enrollmentNotice,
        pending,
        needsResume,
        notice,
        blocked: working || enrolling || pending || needsResume,
        start,
        chooseFirstFolder,
        reconcile,
      }}
    >
      {children}
    </Context.Provider>
  );
}
export function AddSources() {
  const ui = usePanora(),
    flow = useOnboarding();
  const disabled = ui.busy || ui.blocked || !ui.connected || !ui.configuration || flow.blocked;
  const fresh = isFreshSetup(ui);
  return (
    <>
      {fresh && <p>Choose a local folder to display its first wallpaper and start rotation.</p>}
      <FormActions>
        <Button
          id={fresh ? 'choose-first-folder' : undefined}
          type="button"
          className={fresh ? 'primary' : 'outline'}
          disabled={disabled}
          onClick={() => (fresh ? void flow.chooseFirstFolder() : flow.openSource('folder'))}
        >
          {fresh ? 'Choose folder and start' : 'Add a folder'}
        </Button>
      </FormActions>
      <p className="subtext">
        <Button type="button" disabled={disabled} onClick={() => flow.openSource('search')}>
          Set up Wallhaven in Sources
        </Button>
      </p>
      <Notice hidden={!flow.enrollmentNotice}>{flow.enrollmentNotice}</Notice>
      {ui.blocked && flow.enrollmentNotice && !ui.settingsError && (
        <Button
          type="button"
          className="outline"
          disabled={ui.busy}
          onClick={() => void ui.reload()}
        >
          Reload settings
        </Button>
      )}
    </>
  );
}
export function StartWallpapers() {
  const ui = usePanora(),
    flow = useOnboarding();
  const current = ui.snapshot?.status?.current;
  if (current && !flow.blocked) return null;
  if (!hasSources(ui) && !flow.pending && !flow.needsResume && !flow.working) return null;
  const disabled =
    flow.working ||
    flow.enrolling ||
    ui.busy ||
    !ui.connected ||
    ui.blocked ||
    ui.uncertain ||
    Boolean(ui.snapshot?.status?.busy || ui.snapshot?.status?.uncertain);
  return (
    <section
      className="onboarding-panel"
      aria-label="Start wallpapers"
      aria-busy={flow.working || flow.enrolling}
    >
      <h2>
        {flow.pending
          ? 'Check wallpaper status'
          : flow.needsResume
            ? 'Your first wallpaper is ready'
            : 'Ready to start wallpapers'}
      </h2>
      <p>
        {flow.pending
          ? 'An earlier request is unconfirmed. Check its result before sending another.'
          : flow.needsResume
            ? 'Resume automatic rotation without displaying another first image.'
            : 'Your sources are saved. Start displays the first image, then turns on automatic rotation.'}
      </p>
      <FormActions>
        <Button
          type="button"
          className="primary"
          disabled={disabled || flow.pending}
          onClick={() => void flow.start()}
        >
          {flow.working || flow.enrolling
            ? 'Please wait…'
            : flow.needsResume
              ? 'Resume rotation'
              : 'Start wallpapers'}
        </Button>
        {(flow.pending || flow.needsResume) && (
          <Button
            type="button"
            className="outline"
            disabled={flow.working || ui.busy}
            onClick={() => void flow.reconcile()}
          >
            Check status
          </Button>
        )}
      </FormActions>
      <Notice hidden={!flow.notice}>{flow.notice}</Notice>
    </section>
  );
}
