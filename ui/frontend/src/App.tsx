import { Component, useLayoutEffect, useRef } from 'react';
import type { FocusEvent, ReactNode } from 'react';
import panoraIcon from '../../../assets/branding/icons/2026-11-02/masters/canyon-passage.svg?url';
import { bridge, usePanora } from './client';
import { NowScreen } from './now/NowScreen';
import { SourcesScreen } from './sources/SourcesScreen';
import { HistoryScreen } from './history/HistoryScreen';
import { SettingsScreen } from './settings/SettingsScreen';
import { AboutScreen } from './settings/AboutScreen';
import { OnboardingProvider } from './onboarding/Onboarding';
import type { Page } from './types';

const destinations = [
  ['now', NowScreen, 'Now'],
  ['providers', SourcesScreen, 'Sources'],
  ['history', HistoryScreen, 'History'],
  ['settings', SettingsScreen, null],
  ['about', AboutScreen, null],
] as const;
const mainDestinations = destinations.filter(([, , label]) => label);
function readableError(code?: string, message?: string) {
  const messages: Record<string, string> = {
    service_unavailable: 'Panora is not responding. Open Settings for help, then reconnect.',
    selection_failed: 'No wallpaper is ready with the current sources and image filters.',
    discovery_pending: 'Panora is still checking for matching images.',
    rule_unavailable:
      'Your screen’s aspect ratio is unavailable. Review image filters or reconnect a supported display.',
  };
  return (code && messages[code]) || message || '';
}
export function App() {
  return (
    <OnboardingProvider>
      <AppContent />
    </OnboardingProvider>
  );
}
function AppContent() {
  const ui = usePanora();
  const { navigate } = ui;
  const header = useRef<HTMLElement>(null);
  const previousPage = useRef<Page>(ui.page);
  useLayoutEffect(() => {
    const openSettings = () => navigate('settings');
    const openAbout = () => navigate('about');
    const launchAbout = async () => {
      try {
        if (await bridge('TakeAboutRequest')) openAbout();
      } catch {
        // A browser preview may have no native launch bridge.
      }
    };
    window.addEventListener('panora:open-settings', openSettings);
    window.addEventListener('panora:open-about', openAbout);
    window.addEventListener('panora:launch-about', launchAbout);
    void launchAbout();
    return () => {
      window.removeEventListener('panora:open-settings', openSettings);
      window.removeEventListener('panora:open-about', openAbout);
      window.removeEventListener('panora:launch-about', launchAbout);
    };
  }, [navigate]);
  useLayoutEffect(() => {
    if (previousPage.current === ui.page) return;
    previousPage.current = ui.page;
    document.getElementById(`${ui.page}-title`)?.focus();
    window.scrollTo(0, 0);
  }, [ui.page]);
  function revealFocus(event: FocusEvent<HTMLElement>) {
    const target = event.target;
    if (!event.currentTarget.contains(target) || header.current?.contains(target)) return;
    requestAnimationFrame(() => {
      if (!header.current || document.activeElement !== target) return;
      const headerBottom = header.current.getBoundingClientRect().bottom;
      const bounds = target.getBoundingClientRect();
      if (bounds.top < headerBottom && bounds.bottom > 0) {
        window.scrollBy(0, bounds.top - headerBottom - 6);
      }
    });
  }
  async function recover() {
    const page = ui.getState().page,
      origin = document.activeElement;
    await (ui.settingsError ? ui.reload() : ui.refresh());
    requestAnimationFrame(() => {
      if (
        ui.getState().page !== page ||
        ![document.body, document.documentElement, origin].includes(document.activeElement)
      )
        return;
      const trigger = document.getElementById('refresh');
      if (trigger instanceof HTMLButtonElement && !trigger.hidden && !trigger.disabled)
        trigger.focus();
      else document.getElementById(`${page}-title`)?.focus();
    });
  }
  const status = ui.snapshot?.status;
  const error =
    readableError(ui.snapshot?.code, ui.snapshot?.error) ||
    ui.settingsError ||
    readableError(status?.last_error?.code, status?.last_error?.message);
  const showRefresh = !ui.connected || ui.uncertain || ui.snapshot?.error || ui.settingsError;
  return (
    <main onFocusCapture={revealFocus}>
      <header ref={header} className="app-header">
        <span className="brand">
          <img src={panoraIcon} alt="" width={24} height={24} />
          Panora
        </span>
        <nav aria-label="Main navigation">
          {mainDestinations.map(([page, , label]) => (
            <button
              key={page}
              data-page={page}
              aria-current={ui.page === page ? 'page' : undefined}
              onClick={() => ui.navigate(page)}
            >
              <span>{label}</span>
            </button>
          ))}
        </nav>
        <div className="header-actions">
          <button
            id="refresh"
            className="outline"
            hidden={!showRefresh || ui.page === 'about'}
            disabled={ui.busy}
            onClick={() => void recover()}
          >
            {ui.settingsError ? 'Reload settings' : ui.connected ? 'Refresh' : 'Reconnect'}
          </button>
          <button
            className="settings-gear"
            aria-label="Settings"
            aria-current={ui.page === 'settings' ? 'page' : undefined}
            title="Settings"
            onClick={() => ui.navigate('settings')}
          >
            <span aria-hidden="true">⚙︎</span>
          </button>
        </div>
      </header>
      <p
        id="notice"
        className="notice global-notice"
        role="alert"
        hidden={!error || ui.page === 'about'}
      >
        {error}
      </p>
      {destinations.map(([page, Screen]) => (
        <section
          key={page}
          id={`page-${page}`}
          className="page"
          aria-labelledby={`${page}-title`}
          hidden={ui.page !== page}
        >
          {(page !== 'about' || ui.page === 'about') && <Screen />}
        </section>
      ))}
    </main>
  );
}
export class UIErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    if (this.state.failed)
      return (
        <main>
          <div className="heading">
            <h1>Panora could not show this screen</h1>
          </div>
          <p className="notice" role="alert">
            Your service and saved settings are separate from this window. Reopen the window to try
            again.
          </p>
          <button className="outline" onClick={() => window.location.reload()}>
            Reload window
          </button>
        </main>
      );
    return this.props.children;
  }
}
