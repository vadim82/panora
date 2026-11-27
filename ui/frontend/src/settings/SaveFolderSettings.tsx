import { useState } from 'react';
import { usePanora } from '../client';
import { restoreFormFocus } from './forms';
import { Button, Notice } from '../components/ui';

export function SaveFolderSettings() {
  const ui = usePanora();
  const config = ui.liveConfiguration || ui.configuration;
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  if (config?.save_folder === undefined) return null;

  async function chooseFolder() {
    if (!config || pending) return;
    const origin = document.activeElement;
    const instance = ui.snapshot?.status?.instance_id;
    setPending(true);
    setError('');
    try {
      const folder = await ui.request('PickSaveFolder');
      if (folder.canceled) return;
      if (!folder.path) {
        setError(folder.error || 'The save folder could not be opened.');
        return;
      }
      const result = await ui.request(
        'UpdateConfiguration',
        [{ revision: config.revision, save_folder: folder.path }],
        {
          mutation: true,
          shouldSend: () => ui.getState().snapshot?.status?.instance_id === instance,
        },
      );
      if (!result.configuration) setError(result.error || 'The save folder could not be updated.');
    } finally {
      setPending(false);
      restoreFormFocus('choose-save-folder', ui, 'settings', origin);
    }
  }

  return (
    <section className="settings-panel" aria-labelledby="save-folder-heading">
      <h2 id="save-folder-heading">Save folder</h2>
      <p className="subtext settings-path" aria-live="polite">
        {config.save_folder || 'Choose where copies of saved wallpapers are kept.'}
      </p>
      <Button
        id="choose-save-folder"
        disabled={!ui.connected || ui.busy || ui.blocked}
        pending={pending}
        onClick={() => void chooseFolder()}
      >
        {config.save_folder ? 'Change folder' : 'Choose folder'}
      </Button>
      {error && <Notice tone="error">{error}</Notice>}
    </section>
  );
}
