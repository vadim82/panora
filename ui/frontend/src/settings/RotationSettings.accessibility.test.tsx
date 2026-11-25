import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, it } from 'vitest';
import { ClientProvider } from '../client';
import { installBridge } from '../test/fixtures';
import { RotationSettings } from './RotationSettings';

it('names the custom interval separately from its instructions and keeps hidden help out of the visible form', async () => {
  const api = installBridge(),
    user = userEvent.setup();
  render(
    <ClientProvider>
      <RotationSettings />
    </ClientProvider>,
  );
  const preset = screen.getByRole('combobox', { name: 'Change wallpaper every' });
  await waitFor(() => expect(preset).toBeEnabled());
  expect(screen.getByText('For example, 30m for 30 minutes or 1h for one hour.')).not.toBeVisible();
  await user.selectOptions(preset, 'custom');
  const interval = screen.getByRole('textbox', { name: 'Custom interval' });
  expect(interval).toHaveAccessibleDescription(
    'For example, 30m for 30 minutes or 1h for one hour.',
  );
  await waitFor(() => expect(interval).toHaveFocus());
  expect(api.UpdateConfiguration).not.toHaveBeenCalled();
  expect(api.Action).not.toHaveBeenCalled();
});
