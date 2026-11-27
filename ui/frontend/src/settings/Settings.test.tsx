import { useState } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { capabilities, configuration, installBridge, snapshot } from '../test/fixtures';
import type { BridgeMock } from '../test/fixtures';
import type { Configuration, Result, Snapshot } from '../types';
import { ImageSettings, ImageSettingsProvider } from './ImageSettings';
import { RotationSettings } from './RotationSettings';
import { ServiceSettings } from './ServiceSettings';

function savedSettings(mock: BridgeMock, initial: Configuration = structuredClone(configuration)) {
  const source = { config: initial };
  mock.Configuration.mockImplementation(async () => ({
    configuration: structuredClone(source.config),
  }));
  mock.UpdateConfiguration.mockImplementation(async (edit) => {
    const next = structuredClone(source.config);
    next.revision += '-saved';
    for (const key of [
      'backend',
      'fit',
      'upscale',
      'interval',
      'rotation_mode',
      'cache_max_images',
      'cache_max_bytes',
    ] as const)
      if (edit[key] !== undefined) {
        Object.assign(next.saved, { [key]: edit[key] });
        Object.assign(next.effective, { [key]: edit[key] });
      }
    for (const key of [
      'min_width',
      'min_height',
      'max_depth',
      'ratio',
      'ratio_presets',
      'ratio_tolerance_pixels',
    ] as const)
      if (edit[key] !== undefined) Object.assign(next.rules, { [key]: edit[key] });
    next.restart_required = [];
    source.config = next;
    return { configuration: structuredClone(next) };
  });
  return source;
}
function ServiceHarnessRotation() {
  const client = usePanora();
  useState(() => client.navigate('settings'));
  return <RotationSettings />;
}
function ImageHarness() {
  const client = usePanora();
  useState(() => client.navigate('settings'));
  const [mount, setMount] = useState(0);
  return (
    <>
      <button onClick={() => client.navigate('settings')}>Settings page</button>
      <button onClick={() => setMount((value) => value + 1)}>Remount form</button>
      <button onClick={() => void client.refresh({ background: true })}>Read status</button>
      <ImageSettings key={mount} />
    </>
  );
}
function renderImages() {
  return render(
    <ClientProvider>
      <ImageSettingsProvider>
        <ImageHarness />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
}
async function readyImages() {
  await waitFor(() => expect(screen.getByLabelText('Fit')).toBeEnabled());
  await waitFor(() =>
    expect(
      screen.getByRole('option', { name: 'Same shape as my screen', hidden: true }),
    ).toBeEnabled(),
  );
}

describe('image settings', () => {
  it('saves zero pixel tolerance without enabling shared filters', async () => {
    const user = userEvent.setup(),
      mock = installBridge(),
      config = structuredClone(configuration);
    Object.assign(config.rules, { min_width: 1, min_height: 1 });
    savedSettings(mock, config);
    renderImages();
    await readyImages();
    const tolerance = screen.getByLabelText('Aspect ratio tolerance (pixels)');
    expect(tolerance).toHaveValue(10);
    await user.clear(tolerance);
    await user.type(tolerance, '0');
    expect(screen.getByRole('button', { name: 'Use all images' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    await user.click(screen.getByRole('button', { name: 'Preview matches' }));
    await waitFor(() => expect(mock.RulePreview).toHaveBeenCalled());
    expect(mock.RulePreview.mock.calls[0][0].rules.ratio_tolerance_pixels).toBe(0);
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() =>
      expect(mock.UpdateConfiguration).toHaveBeenCalledWith({
        revision: 'reviewed',
        ratio_tolerance_pixels: 0,
      }),
    );
  });

  it('sets both minimum dimensions from a preset and shows inputs only for a custom size', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    savedSettings(mock);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    const minimum = screen.getByLabelText('Minimum resolution');
    await user.selectOptions(minimum, 'custom');
    expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(1920);
    expect(screen.getByLabelText('Minimum height (pixels)')).toHaveValue(1080);
    await user.selectOptions(minimum, '3840x2160');
    expect(screen.queryByLabelText('Minimum width (pixels)')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() =>
      expect(mock.UpdateConfiguration).toHaveBeenCalledWith({
        revision: 'reviewed',
        min_width: 3840,
        min_height: 2160,
      }),
    );
  });

  it('only removes existing filters after an explicit Use all images choice and saves neutral rules', async () => {
    const user = userEvent.setup(),
      mock = installBridge(),
      config = structuredClone(configuration);
    config.rules = {
      ...config.rules,
      min_width: 5120,
      min_height: 1440,
      ratio: 'presets',
      ratio_presets: ['32:9'],
    };
    const source = savedSettings(mock, config);
    renderImages();
    await readyImages();
    expect(screen.getByRole('button', { name: 'Filter images' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByLabelText('Minimum resolution')).toHaveValue('5120x1440');
    expect(screen.queryByLabelText('Minimum width (pixels)')).not.toBeInTheDocument();
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Use all images' }));
    expect(screen.getByRole('button', { name: 'Use all images' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByLabelText('Minimum resolution')).toHaveValue('1x1');
    expect(screen.queryByLabelText('Minimum width (pixels)')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1));
    expect(mock.UpdateConfiguration.mock.calls[0][0]).toEqual({
      revision: 'reviewed',
      min_width: 1,
      min_height: 1,
      ratio: 'any',
      ratio_presets: [],
    });
    expect(source.config.rules).toMatchObject({
      min_width: 1,
      min_height: 1,
      ratio: 'any',
      ratio_presets: [],
    });
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Use all images' })).toBeEnabled(),
    );
    await user.click(screen.getByRole('button', { name: 'Remount form' }));
    expect(screen.getByRole('button', { name: 'Use all images' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });
  it('restores the previous filtered draft across an All images choice and shared-form remount', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    savedSettings(mock);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
    fireEvent.change(screen.getByLabelText('Minimum width (pixels)'), {
      target: { value: '2560' },
    });
    await user.selectOptions(screen.getByLabelText('Aspect ratio'), 'custom');
    fireEvent.change(screen.getByLabelText('Custom width:height ratio'), {
      target: { value: '5:4' },
    });
    await user.click(screen.getByRole('button', { name: 'Use all images' }));
    await user.click(screen.getByRole('button', { name: 'Remount form' }));
    await user.click(screen.getByRole('button', { name: 'Filter images' }));
    expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(2560);
    expect(screen.getByLabelText('Custom width:height ratio')).toHaveValue('5:4');
    fireEvent.change(screen.getByLabelText('Minimum width (pixels)'), {
      target: { value: '3000' },
    });
    await user.click(screen.getByRole('button', { name: 'Filter images' }));
    expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(3000);
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
  });
  it('uses backing-pixel dimensions and screen shape together, while the aspect-only choice keeps dimensions', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    savedSettings(mock);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    await user.selectOptions(screen.getByLabelText('Aspect ratio'), 'monitor');
    expect(
      screen.getByRole('option', { name: 'Same shape as my screen', selected: true }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText('Minimum resolution')).toHaveValue('1920x1080');
    await user.click(screen.getByRole('button', { name: 'Match my screen' }));
    expect(screen.getByLabelText('Minimum resolution')).toHaveValue('3024x1964');
    expect(screen.queryByLabelText('Minimum width (pixels)')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Aspect ratio')).toHaveValue('monitor');
    expect(screen.getByText(/Match my screen selected: at least 3024 × 1964/)).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1));
    expect(mock.UpdateConfiguration.mock.calls[0][0]).toEqual({
      revision: 'reviewed',
      min_width: 3024,
      min_height: 1964,
      ratio: 'monitor',
      ratio_presets: [],
    });
  });
  it('refreshes capabilities on reconnect, service identity and effective backend changes', async () => {
    const user = userEvent.setup(),
      mock = installBridge(),
      source = savedSettings(mock);
    let reported: Snapshot = structuredClone(snapshot),
      caps = structuredClone(capabilities);
    mock.Status.mockImplementation(async () => structuredClone(reported));
    mock.Capabilities.mockImplementation(async () => ({ capabilities: structuredClone(caps) }));
    renderImages();
    await readyImages();
    expect(mock.Capabilities).toHaveBeenCalledTimes(1);
    reported = { connected: false, error: 'Disconnected' };
    await user.click(screen.getByRole('button', { name: 'Read status' }));
    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: 'Same shape as my screen', hidden: true }),
      ).toBeDisabled(),
    );
    reported = structuredClone(snapshot);
    reported.status!.instance_id = 'new-service';
    caps.geometry.displays[0].name = 'Replacement display';
    await user.click(screen.getByRole('button', { name: 'Read status' }));
    await readyImages();
    expect(mock.Capabilities).toHaveBeenCalledTimes(2);
    reported.status!.instance_id = 'another-service';
    caps.geometry = { state: 'unsupported', displays: [] };
    await user.click(screen.getByRole('button', { name: 'Read status' }));
    await waitFor(() => expect(mock.Capabilities).toHaveBeenCalledTimes(3));
    expect(
      screen.getByRole('option', { name: 'Same shape as my screen', hidden: true }),
    ).toBeDisabled();
    source.config.effective.backend = 'omarchy';
    caps = { ...structuredClone(capabilities), backend: 'omarchy' };
    await user.click(screen.getByRole('button', { name: 'Read status' }));
    await readyImages();
    expect(mock.Capabilities).toHaveBeenCalledTimes(4);
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
  });
  it.each(['service restart', 'backend change'] as const)(
    'clears completed shared-rule preview counts after %s and keeps edited fields',
    async (change) => {
      const user = userEvent.setup(),
        mock = installBridge(),
        source = savedSettings(mock);
      let reported: Snapshot = structuredClone(snapshot);
      mock.Status.mockImplementation(async () => structuredClone(reported));
      renderImages();
      await readyImages();
      await user.click(screen.getByText('Image filters'));
      await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
      fireEvent.change(screen.getByLabelText('Minimum width (pixels)'), {
        target: { value: '2560' },
      });
      await user.click(screen.getByRole('button', { name: 'Preview matches' }));
      expect(await screen.findByText(/1 of 1 inspected images match/)).toBeVisible();
      if (change === 'service restart')
        reported = { ...reported, status: { ...reported.status, instance_id: 'replacement' } };
      else
        source.config = {
          ...source.config,
          effective: { ...source.config.effective, backend: 'omarchy' },
        };
      await user.click(screen.getByRole('button', { name: 'Read status' }));
      await waitFor(() =>
        expect(screen.queryByText(/1 of 1 inspected images match/)).not.toBeInTheDocument(),
      );
      expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(2560);
      expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
      expect(mock.Action).not.toHaveBeenCalled();
    },
  );
  it('keeps a pending shared preset selection across a concurrent Any-ratio reload', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    const initial = structuredClone(configuration);
    initial.rules = { ...initial.rules, ratio: 'presets', ratio_presets: ['16:9'] };
    const source = savedSettings(mock, initial);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    await user.click(screen.getByLabelText('1:1'));
    source.config = {
      ...source.config,
      revision: 'external-any',
      rules: { ...source.config.rules, ratio: 'any', ratio_presets: [] },
    };
    await user.click(screen.getByRole('button', { name: 'Reload settings' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled());
    expect(screen.getByLabelText('Aspect ratio')).toHaveValue('presets');
    expect(screen.getByLabelText('16:9')).toBeChecked();
    expect(screen.getByLabelText('1:1')).toBeChecked();
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1));
    expect(mock.UpdateConfiguration.mock.calls[0][0]).toEqual({
      revision: 'external-any',
      ratio: 'presets',
      ratio_presets: ['16:9', '1:1'],
    });
    expect(mock.Action).not.toHaveBeenCalled();
  });
  it('retains a custom ratio while temporarily selecting another mode and remounting the form', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    savedSettings(mock);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    await user.selectOptions(screen.getByLabelText('Aspect ratio'), 'custom');
    fireEvent.change(screen.getByLabelText('Custom width:height ratio'), {
      target: { value: '5:4' },
    });
    await user.selectOptions(screen.getByLabelText('Aspect ratio'), 'any');
    await user.click(screen.getByRole('button', { name: 'Remount form' }));
    await user.selectOptions(screen.getByLabelText('Aspect ratio'), 'custom');
    expect(screen.getByLabelText('Custom width:height ratio')).toHaveValue('5:4');
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
  });
  it('keeps one reviewed draft across form remount and refresh, and saves paired presentation fields', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    savedSettings(mock);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
    fireEvent.change(screen.getByLabelText('Minimum width (pixels)'), {
      target: { value: '2560' },
    });
    await user.click(screen.getByRole('button', { name: 'Remount form' }));
    expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(2560);
    await user.click(screen.getByRole('button', { name: 'Settings page' }));
    await user.click(screen.getByRole('button', { name: 'Read status' }));
    expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(2560);
    await waitFor(() => expect(screen.getByLabelText('Fit')).toBeEnabled());
    await user.click(screen.getByLabelText('Enlarge smaller images'));
    await user.selectOptions(screen.getByLabelText('Fit'), 'crop');
    expect(screen.getByLabelText('Enlarge smaller images')).not.toBeChecked();
    expect(screen.getByLabelText('Enlarge smaller images')).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1));
    expect(mock.UpdateConfiguration.mock.calls[0][0]).toEqual({
      revision: 'reviewed',
      min_width: 2560,
      fit: 'crop',
      upscale: false,
    });
    expect(
      await screen.findByText('Changes saved. They apply with the next wallpaper.'),
    ).toBeVisible();
  });
  it('blocks conflicted saves until explicit reload while keeping edited fields and refreshing untouched fields', async () => {
    const user = userEvent.setup(),
      mock = installBridge(),
      source = savedSettings(mock);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
    fireEvent.change(screen.getByLabelText('Minimum width (pixels)'), {
      target: { value: '2560' },
    });
    mock.UpdateConfiguration.mockResolvedValueOnce({
      code: 'config_conflict',
      error: 'Settings changed elsewhere.',
    });
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled(),
    );
    source.config = {
      ...source.config,
      revision: 'external',
      rules: { ...source.config.rules, min_width: 2400, min_height: 1440 },
    };
    await user.click(screen.getByRole('button', { name: 'Reload settings' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled());
    expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(2560);
    expect(screen.getByLabelText('Minimum height (pixels)')).toHaveValue(1440);
    expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(2));
    expect(mock.UpdateConfiguration.mock.calls[1][0]).toEqual({
      revision: 'external',
      min_width: 2560,
    });
  });
  it('validates presets locally and explains zero, unknown and bounded rejection counts', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    savedSettings(mock);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    await user.selectOptions(screen.getByLabelText('Aspect ratio'), 'presets');
    await user.click(screen.getByRole('button', { name: 'Preview matches' }));
    expect(mock.RulePreview).not.toHaveBeenCalled();
    expect(screen.getByLabelText('16:9')).toHaveFocus();
    await user.click(screen.getByLabelText('21:9'));
    mock.RulePreview.mockResolvedValueOnce({
      preview: {
        matched: 0,
        inspected: 8,
        rejected: 8,
        unknown: 0,
        complete: true,
        limit: 200,
        exclusions_applied: true,
        reasons: { below_minimum: 5, ratio_mismatch: 3 },
      },
    });
    await user.click(screen.getByRole('button', { name: 'Preview matches' }));
    expect(await screen.findByText(/Try lowering the minimum/)).toBeVisible();
    expect(mock.RulePreview.mock.calls[0][0].rules.ratio_presets).toEqual(['21:9']);
    await user.click(screen.getByText('Check details'));
    expect(screen.getByText('Below the minimum dimensions: 5')).toBeVisible();
    mock.RulePreview.mockResolvedValueOnce({
      preview: {
        matched: 0,
        inspected: 8,
        rejected: 0,
        unknown: 8,
        complete: false,
        limit: 200,
        exclusions_applied: false,
        reasons: { unknown_remote: 8 },
      },
    });
    await user.click(screen.getByRole('button', { name: 'Preview matches' }));
    expect(await screen.findByText(/do not prove that your sources have no matches/)).toBeVisible();
    expect(screen.getByText(/This is a partial check/)).toBeVisible();
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
  });
  it('discards an obsolete preview and clears stale display capabilities after failed recheck', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    savedSettings(mock);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    let complete: (result: Result) => void = () => {};
    mock.RulePreview.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve;
        }),
    );
    await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
    await user.click(screen.getByRole('button', { name: 'Preview matches' }));
    fireEvent.change(screen.getByLabelText('Minimum width (pixels)'), {
      target: { value: '3000' },
    });
    await act(async () =>
      complete({
        preview: {
          matched: 999,
          inspected: 999,
          rejected: 0,
          unknown: 0,
          complete: true,
          limit: 200,
          exclusions_applied: true,
          reasons: {},
        },
      }),
    );
    expect(screen.queryByText(/999/)).not.toBeInTheDocument();
    mock.Capabilities.mockResolvedValue({ error: 'Display inspection failed.' });
    await user.click(screen.getByRole('button', { name: 'Reload settings' }));
    await waitFor(() =>
      expect(
        screen.getByRole('option', { name: 'Same shape as my screen', hidden: true }),
      ).toBeDisabled(),
    );
    expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(3000);
  });
  it('replaces temporary fit overrides without dropping an unrelated image-rule draft', async () => {
    const user = userEvent.setup(),
      mock = installBridge(),
      config = structuredClone(configuration);
    config.effective.fit = 'crop';
    config.effective.upscale = true;
    config.restart_required = ['fit', 'upscale'];
    savedSettings(mock, config);
    renderImages();
    await readyImages();
    await user.click(screen.getByText('Image filters'));
    await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
    fireEvent.change(screen.getByLabelText('Minimum width (pixels)'), {
      target: { value: '2800' },
    });
    await user.click(screen.getByRole('button', { name: 'Use saved fit now' }));
    await waitFor(() => expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1));
    expect(mock.UpdateConfiguration.mock.calls[0][0]).toEqual({
      revision: 'reviewed',
      fit: 'contain',
      upscale: false,
    });
    expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(2800);
  });
});

function RotationHarness() {
  const client = usePanora();
  return (
    <>
      <button onClick={() => client.navigate('settings')}>Settings page</button>
      <RotationSettings />
    </>
  );
}
describe('rotation and storage settings', () => {
  it('saves the selected rotation mode through the settings bridge and restores it on reopen', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    const saved = savedSettings(mock);
    const renderRotation = () =>
      render(
        <ClientProvider>
          <RotationHarness />
        </ClientProvider>,
      );
    const view = renderRotation();
    const mode = screen.getByRole('combobox', { name: 'Rotation mode' });
    await waitFor(() => expect(mode).toBeEnabled());
    expect(mode).toHaveDisplayValue('Balance sources');

    await user.selectOptions(mode, 'shuffle_all_images');
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await screen.findByText('Changes saved.');
    expect(mock.UpdateConfiguration).toHaveBeenCalledExactlyOnceWith({
      revision: 'reviewed',
      rotation_mode: 'shuffle_all_images',
    });
    expect(saved.config.effective.rotation_mode).toBe('shuffle_all_images');

    view.unmount();
    renderRotation();
    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: 'Rotation mode' })).toHaveDisplayValue(
        'Shuffle all images',
      ),
    );
  });

  it('applies a saved interval while paused and preserves a storage draft, then validates explicit budgets', async () => {
    const user = userEvent.setup(),
      mock = installBridge(),
      config = structuredClone(configuration);
    config.effective.interval = '1h';
    config.restart_required = ['interval'];
    savedSettings(mock, config);
    render(
      <ClientProvider>
        <RotationHarness />
      </ClientProvider>,
    );
    await waitFor(() => expect(screen.getByLabelText('Change wallpaper every')).toBeEnabled());
    await user.click(screen.getByText('Storage limits'));
    fireEvent.change(screen.getByLabelText('Maximum cached images'), { target: { value: '30' } });
    await user.click(screen.getByRole('button', { name: 'Use saved interval now' }));
    await waitFor(() => expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1));
    expect(mock.UpdateConfiguration.mock.calls[0][0]).toEqual({
      revision: 'reviewed',
      interval: '30m',
    });
    expect(screen.getByLabelText('Maximum cached images')).toHaveValue(30);
    expect(mock.Action).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText('Maximum cached images'), { target: { value: '0' } });
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1);
    fireEvent.change(screen.getByLabelText('Maximum cached images'), { target: { value: '30' } });
    await user.selectOptions(screen.getByLabelText('Change wallpaper every'), 'custom');
    fireEvent.change(screen.getByRole('textbox', { name: /^Custom interval/ }), {
      target: { value: '45m' },
    });
    await user.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(2));
    expect(mock.UpdateConfiguration.mock.calls[1][0]).toEqual({
      revision: 'reviewed-saved',
      interval: '45m',
      cache_max_images: 30,
    });
  });
  it('reports protected over-budget usage and unknown measurements without invented zeroes', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    savedSettings(mock);
    const cache = {
      saved: { max_images: 1, max_bytes: 1 },
      effective: { max_images: 1, max_bytes: 1 },
      usage: {
        state: 'protected_over_budget',
        images: 2,
        bytes: 104857600,
        protected_images: 2,
        protected_bytes: 104857600,
      },
    };
    mock.CacheStatus.mockResolvedValue({ cache });
    render(
      <ClientProvider>
        <ServiceHarnessRotation />
      </ClientProvider>,
    );
    await user.click(screen.getByText('Storage usage'));
    expect(await screen.findByText(/Protected images exceed the budget/)).toBeVisible();
    expect(screen.getByText(/2 protected images/)).toBeVisible();
    mock.CacheStatus.mockResolvedValue({
      cache: {
        ...cache,
        usage: undefined,
        usage_error: { code: 'unknown', message: 'Usage measurement failed.' },
      },
    });
    await user.click(screen.getByRole('button', { name: 'Refresh usage' }));
    expect(await screen.findByText(/Storage usage is unavailable/)).toBeVisible();
    expect(screen.queryByText(/0 managed images/)).not.toBeInTheDocument();
  });
});

function ServiceHarness() {
  const client = usePanora();
  const [elsewhere, setElsewhere] = useState(false);
  return (
    <>
      <button
        onClick={() => {
          client.navigate('settings');
          setElsewhere(false);
        }}
      >
        Settings page
      </button>
      <button
        onClick={() => {
          client.navigate('now');
          setElsewhere(true);
        }}
      >
        Now page
      </button>
      <div hidden={elsewhere}>
        <ServiceSettings />
      </div>
    </>
  );
}
describe('installed service controls', () => {
  it('dismisses Stop and startup-removal confirmations safely and clears them when their page leaves', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    mock.ServiceState.mockResolvedValue({
      service: { platform: 'darwin', available: true, running: true, managed: true },
    });
    render(
      <ClientProvider>
        <ServiceHarness />
      </ClientProvider>,
    );
    await user.click(screen.getByRole('button', { name: 'Settings page' }));
    await user.click(screen.getByText('Background service'));
    const stop = screen.getByRole('button', { name: 'Stop service…' });
    await waitFor(() => expect(stop).toBeEnabled());
    await user.click(stop);
    expect(screen.getByRole('alertdialog', { name: 'Stop Panora service?' })).toBeVisible();
    await user.keyboard('{Escape}');
    await waitFor(() => expect(stop).toHaveFocus());
    await user.click(screen.getByRole('button', { name: 'Prepare to move or remove app…' }));
    expect(
      screen.getByRole('alertdialog', { name: 'Prepare to move or remove Panora?' }),
    ).toBeVisible();
    // Simulate an application-driven page change while the modal is open.
    fireEvent.click(screen.getByText('Now page'));
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    const destination = screen.getByRole('button', { name: 'Now page' });
    destination.focus();
    await user.click(screen.getByRole('button', { name: 'Settings page' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(mock.ServiceCommand).not.toHaveBeenCalled();
  });

  it('confirms Stop with Cancel by default and never starts a stopped service during status reads', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    let running = true;
    mock.ServiceState.mockImplementation(async () => ({
      service: {
        platform: 'linux',
        available: true,
        running,
        managed: true,
        start_at_login: false,
      },
    }));
    mock.ServiceCommand.mockImplementation(async (operation) => {
      if (operation === 'stop') running = false;
      if (operation === 'start') running = true;
      return {
        service: {
          platform: 'linux',
          available: true,
          running,
          managed: true,
          start_at_login: false,
        },
      };
    });
    render(
      <ClientProvider>
        <ServiceHarness />
      </ClientProvider>,
    );
    await user.click(screen.getByRole('button', { name: 'Settings page' }));
    await user.click(screen.getByText('Background service'));
    const stop = screen.getByRole('button', { name: 'Stop service…' });
    await waitFor(() => expect(stop).toBeEnabled());
    expect(screen.queryByRole('button', { name: 'Start service' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Restart service…' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Refresh service state' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /prepare to .*remove/i })).not.toBeInTheDocument();
    await user.click(stop);
    expect(screen.getByRole('alertdialog', { name: 'Stop Panora service?' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus();
    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(mock.ServiceCommand).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Stop service…' }));
    await user.click(screen.getByRole('button', { name: 'Stop service' }));
    await waitFor(() => expect(mock.ServiceCommand).toHaveBeenCalledWith('stop'));
    const start = await screen.findByRole('button', { name: 'Start service' });
    await waitFor(() => expect(start).toHaveFocus());
    expect(start).toBe(stop);
    expect(screen.queryByRole('button', { name: 'Stop service…' })).not.toBeInTheDocument();
    expect(mock.ServiceCommand).toHaveBeenCalledExactlyOnceWith('stop');
    expect(running).toBe(false);
    await user.click(start);
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Stop service…' })).toHaveFocus(),
    );
    expect(mock.ServiceCommand.mock.calls).toEqual([['stop'], ['start']]);
  });
  it('polls read-only while stopped and reflects an external start automatically', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    try {
      const user = userEvent.setup(),
        mock = installBridge();
      let running = false;
      mock.ServiceState.mockImplementation(async () => ({
        service: { available: true, running, managed: true, start_at_login: false },
      }));
      render(
        <ClientProvider>
          <ServiceHarness />
        </ClientProvider>,
      );
      await user.click(screen.getByRole('button', { name: 'Settings page' }));
      await user.click(screen.getByText('Background service'));
      expect(await screen.findByRole('button', { name: 'Start service' })).toBeEnabled();
      const reads = mock.ServiceState.mock.calls.length;
      await act(async () => vi.advanceTimersByTime(3000));
      expect(mock.ServiceState).toHaveBeenCalledTimes(reads + 1);
      expect(screen.getByRole('button', { name: 'Start service' })).toBeEnabled();
      expect(mock.ServiceCommand).not.toHaveBeenCalled();
      running = true;
      await act(async () => vi.advanceTimersByTime(3000));
      expect(screen.getByRole('button', { name: 'Stop service…' })).toBeEnabled();
      expect(mock.ServiceCommand).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it('keeps Linux startup controls and protects an external service without a removal action', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    let login = true;
    const state = () => ({
      platform: 'linux',
      available: true,
      running: true,
      managed: false,
      start_at_login: login,
      login_state: login ? 'enabled' : 'disabled',
      log_command: 'journalctl --user -u panora.service -b',
    });
    mock.ServiceState.mockImplementation(async () => ({ service: state() }));
    mock.ServiceCommand.mockImplementation(async (operation) => {
      if (operation === 'login_off') login = false;
      return { service: state() };
    });
    render(
      <ClientProvider>
        <ServiceHarness />
      </ClientProvider>,
    );
    await user.click(screen.getByRole('button', { name: 'Settings page' }));
    await user.click(screen.getByText('Background service'));
    const registration = screen.getByRole('checkbox', { name: 'Start Panora at login' });
    expect(await screen.findByText(/Connected to a separately started service/)).toBeVisible();
    expect(registration).toBeChecked();
    expect(screen.getByText(/Starts at your next graphical login/)).toBeVisible();
    expect(screen.getByText(state().log_command)).toBeVisible();
    expect(screen.queryByRole('button', { name: /prepare to .*remove/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Stop service…' })).toBeDisabled();

    await user.click(registration);
    await waitFor(() => expect(registration).not.toBeChecked());
    expect(mock.ServiceCommand).toHaveBeenCalledExactlyOnceWith('login_off');
    expect(screen.getByRole('button', { name: 'Stop service…' })).toBeDisabled();
    expect(screen.getByText(/Connected to a separately started service/)).toBeVisible();
  });

  it('only offers preparation after positively identifying the macOS service', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    const identity: { platform?: string } = {};
    const state = () => ({ ...identity, available: true, running: false, managed: true });
    mock.ServiceState.mockImplementation(async () => ({ service: state() }));
    mock.ServiceCommand.mockImplementation(async () => ({ service: state() }));
    render(
      <ClientProvider>
        <ServiceHarness />
      </ClientProvider>,
    );
    await user.click(screen.getByRole('button', { name: 'Settings page' }));
    await user.click(screen.getByText('Background service'));
    expect(await screen.findByRole('button', { name: 'Start service' })).toBeEnabled();
    expect(screen.queryByRole('button', { name: /prepare to .*remove/i })).not.toBeInTheDocument();

    identity.platform = 'darwin';
    await user.click(screen.getByRole('button', { name: 'Now page' }));
    await user.click(screen.getByRole('button', { name: 'Settings page' }));
    await user.click(await screen.findByRole('button', { name: 'Prepare to move or remove app…' }));
    expect(
      screen.getByRole('alertdialog', { name: 'Prepare to move or remove Panora?' }),
    ).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Stop and remove startup' }));
    expect(mock.ServiceCommand).toHaveBeenCalledExactlyOnceWith('remove_startup');
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('does not replay a lost lifecycle mutation or let its completion steal another page’s focus', async () => {
    const user = userEvent.setup(),
      mock = installBridge();
    mock.ServiceState.mockResolvedValue({
      service: { available: true, running: false, managed: true },
    });
    let fail: (error: Error) => void = () => {};
    mock.ServiceCommand.mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          fail = reject;
        }),
    );
    render(
      <ClientProvider>
        <ServiceHarness />
      </ClientProvider>,
    );
    await user.click(screen.getByRole('button', { name: 'Settings page' }));
    await user.click(screen.getByText('Background service'));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Start service' })).toBeEnabled(),
    );
    await user.click(screen.getByRole('button', { name: 'Start service' }));
    await waitFor(() => expect(mock.ServiceCommand).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole('button', { name: 'Now page' }));
    const focus = document.activeElement;
    await act(async () => fail(new Error('response lost')));
    expect(document.activeElement).toBe(focus);
    expect(mock.ServiceCommand).toHaveBeenCalledTimes(1);
  });
});
