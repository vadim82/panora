import { useEffect, useState } from 'react';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { capabilities, configuration, installBridge, snapshot } from '../test/fixtures';
import { SourceRules, sourceRuleDraft } from './SourceRules';
import type { Result } from '../types';

function Harness({
  enrolled = true,
  inherited = false,
}: {
  enrolled?: boolean;
  inherited?: boolean;
}) {
  const ui = usePanora(),
    { navigate } = ui;
  const [draft, setDraft] = useState(() =>
    sourceRuleDraft(inherited ? {} : undefined, configuration.rules),
  );
  useEffect(() => navigate('providers'), [navigate]);
  return (
    <>
      <button onClick={() => ui.navigate('settings')}>Leave Sources</button>
      <button onClick={() => void ui.refresh({ background: true })}>Refresh source status</button>
      <section hidden={ui.page !== 'providers'}>
        <SourceRules
          prefix="source"
          provider="local"
          inputID={enrolled ? 'folder-1' : null}
          revision="reviewed"
          enrolled={enrolled}
          draft={draft}
          inherited={configuration.rules}
          disabled={false}
          onChange={(patch) => setDraft((previous) => ({ ...previous, ...patch }))}
        />
      </section>
    </>
  );
}
async function ready(props: { enrolled?: boolean; inherited?: boolean } = {}) {
  render(
    <ClientProvider>
      <Harness {...props} />
    </ClientProvider>,
  );
  await waitFor(() => expect(screen.getByRole('button', { name: 'Use all images' })).toBeEnabled());
}

describe('source image filters', () => {
  it.each(['moved focus', 'another page'] as const)(
    'does not take focus after deferred validation when the user chose %s',
    async (destination) => {
      const bridge = installBridge(),
        user = userEvent.setup();
      await ready();
      const width = screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' });
      await user.clear(width);
      await user.type(width, '0');
      const callbacks: FrameRequestCallback[] = [];
      const animationFrame = vi
        .spyOn(window, 'requestAnimationFrame')
        .mockImplementation((callback) => {
          callbacks.push(callback);
          return callbacks.length;
        });
      try {
        await user.click(screen.getByRole('button', { name: 'Preview source matches' }));
        expect(callbacks).toHaveLength(1);
        const target = screen.getByRole('button', {
          name: destination === 'moved focus' ? 'Refresh source status' : 'Leave Sources',
        });
        if (destination === 'moved focus') target.focus();
        else await user.click(target);
        act(() => callbacks.forEach((callback) => callback(performance.now())));
        expect(target).toHaveFocus();
        expect(bridge.RulePreview).not.toHaveBeenCalled();
        expect(bridge.Action).not.toHaveBeenCalled();
      } finally {
        animationFrame.mockRestore();
      }
    },
  );

  it.each(['Preview source matches', 'Match my screen'])(
    'returns keyboard focus after %s finishes',
    async (action) => {
      const bridge = installBridge(),
        user = userEvent.setup();
      let finish: (value: Result) => void = () => {};
      const pending = new Promise<Result>((resolve) => {
        finish = resolve;
      });
      if (action === 'Preview source matches') bridge.RulePreview.mockReturnValueOnce(pending);
      else bridge.Capabilities.mockReturnValueOnce(pending);
      await ready();
      const trigger = screen.getByRole('button', { name: action });
      await user.click(trigger);
      expect(trigger).toHaveAccessibleName(
        action === 'Preview source matches' ? 'Checking…' : 'Reading display…',
      );
      expect(trigger).toHaveAttribute('aria-busy', 'true');
      if (action === 'Match my screen') {
        expect(screen.getByRole('button', { name: 'Preview source matches' })).toBeDisabled();
        expect(bridge.RulePreview).not.toHaveBeenCalled();
      }
      trigger.blur();
      finish(
        action === 'Preview source matches'
          ? {
              preview: {
                matched: 1,
                inspected: 1,
                rejected: 0,
                unknown: 0,
                complete: true,
                limit: 100,
                exclusions_applied: true,
                reasons: {},
                rule_resolution: { state: 'resolved' },
              },
            }
          : { capabilities: structuredClone(capabilities) },
      );
      await waitFor(() => expect(trigger).toBeEnabled());
      await waitFor(() => expect(trigger).toHaveFocus());
      expect(bridge.Action).not.toHaveBeenCalled();
      expect(bridge.UpdateFolder).not.toHaveBeenCalled();
    },
  );

  it('does not steal focus moved elsewhere during a source preview', async () => {
    const bridge = installBridge(),
      user = userEvent.setup();
    let finish: (value: Result) => void = () => {};
    bridge.RulePreview.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve;
      }),
    );
    await ready();
    await user.click(screen.getByRole('button', { name: 'Preview source matches' }));
    const destination = screen.getByRole('button', { name: 'Refresh source status' });
    destination.focus();
    finish({ error: 'The source check is temporarily unavailable.' });
    await screen.findByText('The source check is temporarily unavailable.');
    expect(destination).toHaveFocus();
    expect(bridge.Action).not.toHaveBeenCalled();
  });

  it('invalidates completed counts after service restart while preserving the draft', async () => {
    const bridge = installBridge(),
      user = userEvent.setup();
    let instance = 'instance-1';
    bridge.Status.mockImplementation(async () => ({
      ...structuredClone(snapshot),
      status: { ...snapshot.status, instance_id: instance },
    }));
    await ready();
    const width = screen.getByLabelText('Minimum width (pixels)');
    await user.clear(width);
    await user.type(width, '2560');
    await user.click(screen.getByRole('button', { name: 'Preview source matches' }));
    expect(
      await screen.findByText(/1 matching images from 1 inspected for this source/),
    ).toBeVisible();
    instance = 'instance-2';
    await user.click(screen.getByRole('button', { name: 'Refresh source status' }));
    await waitFor(() =>
      expect(
        screen.queryByText(/1 matching images from 1 inspected for this source/),
      ).not.toBeInTheDocument(),
    );
    expect(width).toHaveValue(2560);
    expect(bridge.UpdateFolder).not.toHaveBeenCalled();
  });

  it('starts new sources unrestricted and requires saving before a catalog preview', async () => {
    const bridge = installBridge();
    await ready({ enrolled: false });
    expect(screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' })).toHaveValue(1);
    expect(screen.getByRole('spinbutton', { name: 'Minimum height (pixels)' })).toHaveValue(1);
    expect(
      screen.queryByRole('button', { name: 'Preview source matches' }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/Save and enable this source/)).toBeVisible();
    expect(bridge.RulePreview).not.toHaveBeenCalled();
    expect(bridge.UpdateConfiguration).not.toHaveBeenCalled();
  });

  it('previews only the enrolled source and does not save or apply draft filters', async () => {
    const bridge = installBridge(),
      user = userEvent.setup();
    await ready();
    const width = screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' });
    await user.clear(width);
    await user.type(width, '2560');
    await user.click(screen.getByRole('button', { name: 'Preview source matches' }));
    await waitFor(() => expect(bridge.RulePreview).toHaveBeenCalledTimes(1));
    expect(bridge.RulePreview.mock.calls[0][0]).toMatchObject({
      revision: 'reviewed',
      provider_id: 'local',
      input_id: 'folder-1',
      rules: { min_width: 2560, min_height: 1, ratio: 'any', ratio_presets: [] },
    });
    expect(
      await screen.findByText(/1 matching images from 1 inspected for this source/),
    ).toBeVisible();
    expect(bridge.UpdateConfiguration).not.toHaveBeenCalled();
    expect(bridge.UpdateFolder).not.toHaveBeenCalled();
    expect(bridge.Action).not.toHaveBeenCalled();
  });

  it('discards a pending preview after leaving Sources', async () => {
    const bridge = installBridge(),
      user = userEvent.setup();
    let finish: (value: Awaited<ReturnType<typeof bridge.RulePreview>>) => void = () => {};
    bridge.RulePreview.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    await ready();
    await user.click(screen.getByRole('button', { name: 'Preview source matches' }));
    await waitFor(() => expect(bridge.RulePreview).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole('button', { name: 'Leave Sources' }));
    finish({
      preview: {
        matched: 99,
        inspected: 99,
        rejected: 0,
        unknown: 0,
        complete: true,
        limit: 100,
        exclusions_applied: true,
        reasons: {},
      },
    });
    await waitFor(() => expect(screen.queryByText(/99 matching images/)).not.toBeInTheDocument());
  });

  it('keeps inherited rules until an explicit source-only choice and matches screen dimensions without saving', async () => {
    const bridge = installBridge(),
      user = userEvent.setup();
    await ready({ inherited: true });
    expect(screen.getByRole('combobox', { name: 'Filter settings' })).toHaveValue('inherit');
    await user.click(screen.getByRole('button', { name: 'Match my screen' }));
    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: 'Filter settings' })).toHaveValue('custom'),
    );
    expect(screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' })).toHaveValue(3024);
    expect(screen.getByRole('spinbutton', { name: 'Minimum height (pixels)' })).toHaveValue(1964);
    expect(screen.getByRole('combobox', { name: 'Aspect ratio' })).toHaveValue('monitor');
    expect(bridge.UpdateConfiguration).not.toHaveBeenCalled();
    expect(bridge.UpdateFolder).not.toHaveBeenCalled();
    expect(bridge.Action).not.toHaveBeenCalled();
  });
});
