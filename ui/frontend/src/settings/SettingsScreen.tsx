import { ImageSettings } from './ImageSettings';
import { RotationSettings } from './RotationSettings';
import { ServiceSettings } from './ServiceSettings';
import { usePanora } from '../client';
import { SaveFolderSettings } from './SaveFolderSettings';

export function SettingsScreen() {
  const { navigate } = usePanora();
  return (
    <>
      <div className="heading">
        <h1 id="settings-title" tabIndex={-1}>
          Settings
        </h1>
        <button type="button" className="outline" onClick={() => navigate('about')}>
          About Panora
        </button>
      </div>
      <ServiceSettings />
      <ImageSettings />
      <RotationSettings />
      <SaveFolderSettings />
    </>
  );
}
