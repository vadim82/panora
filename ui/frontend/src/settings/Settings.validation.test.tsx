import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, it } from 'vitest';
import { ClientProvider } from '../client';
import { installBridge } from '../test/fixtures';
import { ImageSettings, ImageSettingsProvider } from './ImageSettings';
import { RotationSettings } from './RotationSettings';

it('describes an empty preset selection on the focused checkbox and clears the error after correction', async () => {
  const api = installBridge(),
    user = userEvent.setup();
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <ImageSettings />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Preview matches' })).toBeEnabled(),
  );
  await user.click(screen.getByText('Image filters'));
  await user.selectOptions(screen.getByRole('combobox', { name: 'Aspect ratio' }), 'presets');
  await user.click(screen.getByText('Image filters'));
  const inspect = screen.getByRole('button', { name: 'Preview matches' });
  inspect.focus();
  await user.keyboard('{Enter}');
  const first = screen.getByRole('checkbox', { name: '16:9' });
  expect(first).toHaveFocus();
  expect(first).toHaveAttribute('aria-invalid', 'true');
  expect(first).toHaveAccessibleDescription('Choose at least one ratio. Your edits are kept.');
  expect(screen.getByRole('group', { name: 'Choose one or more ratios' })).toHaveAttribute(
    'aria-invalid',
    'true',
  );
  expect(api.RulePreview).not.toHaveBeenCalled();
  expect(api.UpdateConfiguration).not.toHaveBeenCalled();
  await user.keyboard(' ');
  expect(first).toBeChecked();
  expect(first).not.toHaveAttribute('aria-invalid');
  expect(first).not.toHaveAccessibleDescription();
  expect(
    screen.queryByText('Choose at least one ratio. Your edits are kept.'),
  ).not.toBeInTheDocument();
  await user.click(inspect);
  await waitFor(() => expect(api.RulePreview).toHaveBeenCalledOnce());
  expect(api.RulePreview.mock.calls[0][0].rules.ratio_presets).toEqual(['16:9']);
  expect(api.Action).not.toHaveBeenCalled();
});

it('reveals and describes an overflowing storage limit before saving, preserving its draft', async () => {
  const api = installBridge(),
    user = userEvent.setup();
  render(
    <ClientProvider>
      <RotationSettings />
    </ClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByRole('combobox', { name: 'Change wallpaper every' })).toBeEnabled(),
  );
  await user.click(screen.getByText('Storage limits'));
  const limit = screen.getByRole('spinbutton', { name: 'Storage limit (MiB)' });
  fireEvent.change(limit, { target: { value: '100000000000' } });
  await user.click(screen.getByText('Storage limits'));
  const save = screen.getByRole('button', { name: 'Save changes' });
  save.focus();
  await user.keyboard('{Enter}');
  await waitFor(() => expect(limit).toHaveFocus());
  expect(limit).toBeVisible();
  expect(limit).toHaveValue(100000000000);
  expect(limit).toHaveAttribute('aria-invalid', 'true');
  expect(limit).toHaveAccessibleDescription(
    'This storage limit is too large. Enter a smaller value.',
  );
  expect(api.UpdateConfiguration).not.toHaveBeenCalled();
  expect(api.Action).not.toHaveBeenCalled();
  fireEvent.change(limit, { target: { value: '200' } });
  expect(limit).not.toHaveAttribute('aria-invalid');
  expect(limit).not.toHaveAccessibleDescription();
  await user.click(save);
  await waitFor(() =>
    expect(api.UpdateConfiguration).toHaveBeenCalledWith({
      revision: 'reviewed',
      cache_max_bytes: 200 * 1048576,
    }),
  );
  expect(api.Action).not.toHaveBeenCalled();
});
