import { useEffect } from 'react';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, it } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { OnboardingProvider } from '../onboarding/Onboarding';
import { configuration, installBridge, snapshot } from '../test/fixtures';
import type { Configuration } from '../types';
import { SourcesScreen } from './SourcesScreen';

function Sources() {
  const { navigate } = usePanora();
  useEffect(() => navigate('providers'), [navigate]);
  return (
    <OnboardingProvider>
      <SourcesScreen />
    </OnboardingProvider>
  );
}

it.each([10, 100])(
  'uses the shared editor and pages a %i-still gallery from the keyboard',
  async (total) => {
    const user = userEvent.setup();
    const api = installBridge();
    let saved: Configuration = {
      ...structuredClone(configuration),
      omarchy_theme_available: true,
      rules: { min_width: 5120, min_height: 1440, max_depth: 2, ratio: '32:9', ratio_presets: [] },
    };
    api.Status.mockImplementation(async () => ({
      ...snapshot,
      status: { ...snapshot.status, omarchy_theme_name: 'Ethereal' },
    }));
    api.Configuration.mockImplementation(async () => ({ configuration: structuredClone(saved) }));
    api.EditOmarchyTheme.mockImplementation(async (action, edit) => {
      if (action === 'add') {
        saved = {
          ...saved,
          revision: 'theme-added',
          omarchy_theme: {
            enabled: true,
            input_enabled: true,
            id: 'theme-source',
            image_rules: null,
            effective_rules: saved.rules,
          },
        };
      } else if (action === 'update' && saved.omarchy_theme && edit.image_rules) {
        saved = {
          ...saved,
          revision: 'theme-edited',
          omarchy_theme: {
            ...saved.omarchy_theme,
            image_rules: edit.image_rules,
            effective_rules: edit.image_rules,
          },
        };
      }
      return { configuration: structuredClone(saved) };
    });
    api.OmarchyThemePreview.mockImplementation(async (page, limit) => {
      const first = (page - 1) * limit;
      const permissive = saved.omarchy_theme?.image_rules?.ratio === 'any';
      return {
        theme: {
          theme: 'Ethereal',
          revision: saved.revision,
          state: 'ready',
          total,
          eligible: permissive ? total : 0,
          page,
          palette: [{ label: 'Accent', color: '#7e83dc' }],
          stills: Array.from({ length: Math.min(limit, total - first) }, (_, index) => ({
            id: first + index + 1,
            name: `Still ${first + index + 1}`,
            width: 2560,
            height: 1440,
            eligible: permissive,
            jpeg: 'AQID',
          })),
        },
      };
    });
    render(
      <ClientProvider>
        <Sources />
      </ClientProvider>,
    );
    const add = await screen.findByRole('button', { name: 'Add source options' });
    await waitFor(() => expect(add).toBeEnabled());
    add.focus();
    await user.keyboard('{Enter}');
    await user.tab();
    await user.tab();
    await user.tab();
    expect(screen.getByRole('button', { name: 'Omarchy theme' })).toHaveFocus();
    await user.keyboard('{Enter}');
    expect(await screen.findByText(`${total} stills · 0 eligible`)).toBeVisible();
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Ethereal' })).toHaveFocus());
    const gallery = screen.getByRole('region', { name: 'Wallpapers' });
    expect(within(gallery).getAllByRole('img')).toHaveLength(4);
    const openGallery = within(gallery).getByRole('button', { name: `View all ${total}` });
    openGallery.focus();
    await user.keyboard('{Enter}');
    await waitFor(() =>
      expect(within(gallery).getAllByRole('img')).toHaveLength(Math.min(12, total)),
    );
    if (total === 10) {
      expect(within(gallery).queryByRole('button', { name: 'Next' })).not.toBeInTheDocument();
    } else {
      expect(within(gallery).getByText('Showing 1–12 of 100')).toBeVisible();
      const next = within(gallery).getByRole('button', { name: 'Next' });
      next.focus();
      await user.keyboard('{Enter}');
      expect(await within(gallery).findByText('Showing 13–24 of 100')).toBeVisible();
      expect(next).toHaveFocus();
    }
    await user.click(screen.getByText('Image rules', { exact: true }));
    expect(screen.getByRole('radio', { name: 'Use shared rules' })).toBeChecked();
    await user.click(screen.getByRole('radio', { name: 'Customize for this source' }));
    await user.selectOptions(screen.getByRole('combobox', { name: 'Minimum resolution' }), 'none');
    await user.selectOptions(screen.getByRole('combobox', { name: 'Aspect ratio' }), 'any');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(api.EditOmarchyTheme).toHaveBeenCalledWith(
        'update',
        expect.objectContaining({
          id: 'theme-source',
          revision: 'theme-added',
          image_rules: { min_width: 1, min_height: 1, ratio: 'any', ratio_presets: [] },
        }),
      ),
    );
    const row = screen.getByRole('button', { name: 'Edit Omarchy theme' });
    await waitFor(() => expect(row).toHaveFocus());
    await user.keyboard('{Enter}');
    expect(await screen.findByText(`${total} stills · ${total} eligible`)).toBeVisible();
    expect(api.Action).not.toHaveBeenCalled();
  },
);
