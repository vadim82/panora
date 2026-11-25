import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App, UIErrorBoundary } from './App';
import { ClientProvider } from './client';
import { followWindowAppearance, isMacDesktop } from './windowAppearance';
import { ImageSettingsProvider } from './settings/ImageSettings';
import '../style.css';
import '../searches.css';
import '../providers.css';
import '../history.css';
import '../forms.css';
import '../settings.css';
import '../macos.css';
import '../now.css';

// Fixture code is a development-only dynamic import and is absent from the app bundle.
const preview = new URLSearchParams(location.search);
const fixture = import.meta.env.DEV && preview.has('fixture');
const macFixture = fixture && preview.get('appearance') === 'mac';
if (fixture) {
  const { installFixture } = await import('./dev/fixture');
  installFixture();
}
if (macFixture || (!fixture && (await isMacDesktop())))
  document.documentElement.dataset.controls = 'macos';
else followWindowAppearance();
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <UIErrorBoundary>
      <ClientProvider>
        <ImageSettingsProvider>
          <>
            {fixture && (
              <aside className="development-banner">Development preview · Simulated data</aside>
            )}
            <App />
          </>
        </ImageSettingsProvider>
      </ClientProvider>
    </UIErrorBoundary>
  </StrictMode>,
);
