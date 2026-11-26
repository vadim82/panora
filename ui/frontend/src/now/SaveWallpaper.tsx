import { useState } from 'react';
import { usePanora } from '../client';
import { restoreFormFocus } from '../settings/forms';
import { Button, FormActions, Notice } from '../components/ui';

interface Feedback {
  message: string;
  error?: boolean;
  chooseFolder?: boolean;
  saved?: boolean;
}

export function SaveWallpaper({ disabled }: { disabled: boolean }) {
  const ui = usePanora();
  const config = ui.liveConfiguration || ui.configuration;
  const { instance_id: instance, current } = ui.snapshot?.status || {};
  const [pending, setPending] = useState(false);
  const [feedback, setFeedback] = useState<Feedback>();
  if (config?.save_folder === undefined || !current || !instance) return null;

  async function save(pick: boolean) {
    if (!config || !instance || !current || pending) return;
    const revision = config.revision;
    const origin = document.activeElement;
    setPending(true);
    try {
      let folder: string | undefined;
      if (pick) {
        const chosen = await ui.request('PickSaveFolder');
        if (chosen.canceled) return;
        if (!chosen.path) {
          setFeedback({
            message: chosen.error || 'The save folder could not be opened.',
            error: true,
          });
          return;
        }
        folder = chosen.path;
      }
      setFeedback(undefined);
      const result = await ui.request(
        'SaveWallpaper',
        [{ instance, history: current.id, image_id: current.image_id, revision, folder }],
        { settings: false },
      );
      if (!result.saved) {
        setFeedback({
          message: result.error || 'The wallpaper could not be saved.',
          error: true,
          chooseFolder: result.code === 'save_folder',
        });
        if (
          result.code === 'current_changed' ||
          result.code === 'instance_changed' ||
          result.code === 'config_conflict'
        )
          await ui.refresh();
        return;
      }
      const message = result.already_saved
        ? 'Already saved'
        : `Saved to ${result.folder_name || 'the chosen folder'}`;
      setFeedback({ message, saved: true });
      if (folder) {
        const settings = await ui.request(
          'UpdateConfiguration',
          [{ revision, save_folder: folder }],
          {
            mutation: true,
            shouldSend: () => ui.getState().snapshot?.status?.instance_id === instance,
          },
        );
        if (!settings.configuration)
          setFeedback({
            message: message + '. The folder preference could not be confirmed.',
            saved: true,
          });
      }
    } finally {
      setPending(false);
      restoreFormFocus('save-wallpaper', ui, 'now', origin);
    }
  }

  async function openFolder() {
    const result = await ui.request('OpenSavedFolder', [], { settings: false });
    if (!result.opened)
      setFeedback({
        message: result.error || 'The saved folder could not be opened.',
        error: true,
        saved: true,
      });
  }

  return (
    <div className="wallpaper-save">
      <FormActions>
        <Button
          id="save-wallpaper"
          disabled={disabled}
          pending={pending}
          onClick={() => void save(!config.save_folder)}
        >
          Save to folder
        </Button>
        {feedback?.chooseFolder && (
          <Button disabled={disabled} pending={pending} onClick={() => void save(true)}>
            Choose another folder
          </Button>
        )}
        {feedback?.saved && (
          <Button disabled={ui.busy || pending} onClick={() => void openFolder()}>
            Open folder
          </Button>
        )}
      </FormActions>
      <Notice tone={feedback?.error ? 'error' : 'success'} hidden={!feedback?.message}>
        {feedback?.message}
      </Notice>
    </div>
  );
}
