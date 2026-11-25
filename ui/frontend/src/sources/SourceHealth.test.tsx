import { useState } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { configuration, installBridge, snapshot } from '../test/fixtures';
import type { ProviderHealth } from '../types';
import { SourceHealth } from './SourceHealth';

function Harness() {
  const ui = usePanora();
  const [editing, setEditing] = useState(false);
  return (
    <>
      <button onClick={() => ui.navigate('providers')}>Sources</button>
      <output data-testid="ready">
        {ui.configuration?.revision}:{String(ui.busy)}
      </output>
      <section hidden={ui.page !== 'providers'}>
        {editing && <p>Folder editor is open</p>}
        <SourceHealth
          onReview={(provider, id) => setEditing(provider === 'local' && id === 'folder-1')}
        />
      </section>
    </>
  );
}
async function mount() {
  render(
    <ClientProvider>
      <Harness />
    </ClientProvider>,
  );
  await waitFor(() => expect(screen.getByTestId('ready')).toHaveTextContent('reviewed:false'));
  await userEvent.click(screen.getByRole('button', { name: 'Sources' }));
  const troubleshooting = screen.getByText('Source troubleshooting', { selector: 'summary' });
  expect(troubleshooting.closest('details')).not.toHaveAttribute('open');
  await userEvent.click(troubleshooting);
  expect(screen.getByRole('heading', { name: 'Source status' })).toBeVisible();
  expect(screen.getByRole('button', { name: 'Refresh status' })).toBeVisible();
}
describe('Source health', () => {
  it('distinguishes empty success, unknown eligibility and rate-limited recovery without unsupported retry actions', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    const providers: ProviderHealth[] = [
      {
        id: 'local',
        enabled: true,
        state: 'empty',
        inputs: [
          {
            id: 'folder-1',
            state: 'empty',
            counts: { scope: 'latest-check', inspected: 0, known_eligible: null },
            last_success: '2026-11-01T12:00:00Z',
          },
        ],
      },
      {
        id: 'wallhaven',
        enabled: true,
        state: 'retrying',
        inputs: [
          {
            id: 'search-private-id',
            state: 'retrying',
            counts: { scope: 'metadata', inspected: 8, known_eligible: null },
            failure: { code: 'rate_limited', message: 'Private transport details' },
            retries: [
              {
                scope: 'provider',
                attempts: 2,
                next_retry: '2026-11-01T13:00:00Z',
                failure: { code: 'rate_limited', message: 'Please wait' },
              },
            ],
          },
        ],
      },
    ];
    mock.Status.mockResolvedValue({
      ...structuredClone(snapshot),
      status: { ...snapshot.status, providers },
    });
    mock.Configuration.mockResolvedValue({
      configuration: {
        ...structuredClone(configuration),
        searches: [
          {
            id: 'search-private-id',
            name: 'Quiet landscapes',
            query: 'nature',
            enabled: true,
            input_enabled: true,
            categories: '111',
            sorting: 'random',
            order: 'desc',
            top_range: '1M',
            max_pages: 1,
            purity: '100',
            refresh_interval: '10m',
          },
        ],
      },
    });
    await mount();
    expect(screen.getByText(/The check succeeded with no results/)).toBeVisible();
    expect(screen.getByRole('heading', { name: 'Quiet landscapes' })).toBeVisible();
    expect(screen.getByText(/8 inspected in the latest check/)).toBeVisible();
    expect(screen.getAllByText(/Matching images not yet verified/)).toHaveLength(2);
    expect(screen.getAllByText(/service will respect its retry time/).length).toBeGreaterThan(0);
    expect(screen.getByText(/Retry at/)).toBeVisible();
    expect(
      screen.queryByRole('button', { name: /retry now|cancel check/i }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Show Wallhaven URL' }));
    const url = await screen.findByRole('textbox', { name: 'Wallhaven search URL' });
    expect(url).toHaveValue('https://wallhaven.cc/search?q=nature&purity=100');
    expect(mock.CountSearchCandidates).toHaveBeenCalledWith('search-private-id', 'reviewed');
    await user.click(screen.getByRole('button', { name: 'Copy URL' }));
    expect(await screen.findByText('Wallhaven URL copied.')).toBeVisible();
    expect(await navigator.clipboard.readText()).toBe(
      'https://wallhaven.cc/search?q=nature&purity=100',
    );
    await user.click(screen.getByRole('button', { name: 'Review folder' }));
    await waitFor(() => expect(screen.getByText('Folder editor is open')).toBeVisible());
    await user.click(screen.getByRole('button', { name: 'Refresh status' }));
    expect(
      await screen.findByText(
        /Status refreshed. Automatic source checks and retry times are unchanged/,
      ),
    ).toBeVisible();
    expect(mock.Action).not.toHaveBeenCalled();
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
  });
  it('keeps disclosure state and useful failure categories across status updates and disconnects', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    const providers: ProviderHealth[] = [
      {
        id: 'local',
        enabled: true,
        state: 'failed',
        inputs: [
          {
            id: 'folder-1',
            state: 'failed',
            failure: { code: 'unavailable', message: 'inaccessible folder' },
            last_attempt: '2026-11-01T12:00:00Z',
          },
        ],
      },
      {
        id: 'wallhaven',
        enabled: true,
        state: 'failed',
        inputs: [
          {
            id: 'search',
            state: 'failed',
            failure: { code: 'auth_rejected', message: 'credential rejection' },
            retries: [
              {
                scope: 'image',
                attempts: 1,
                next_retry: '2026-11-01T13:00:00Z',
                failure: { code: 'invalid_content', message: 'bad bytes' },
              },
            ],
          },
        ],
      },
    ];
    mock.Status.mockResolvedValue({
      ...structuredClone(snapshot),
      status: { ...snapshot.status, providers },
    });
    await mount();
    expect(screen.getByText(/Folder access is unavailable/)).toBeVisible();
    expect(
      screen.getByText(/rejected authentication. Credential editing is not available/),
    ).toBeVisible();
    expect(screen.getByText(/its bytes are invalid/)).toBeVisible();
    const detail = screen.getAllByText('Check details')[0];
    await user.click(detail);
    expect(detail.closest('details')).toHaveAttribute('open');
    await user.click(screen.getByRole('button', { name: 'Refresh status' }));
    expect(detail.closest('details')).toHaveAttribute('open');
    mock.Status.mockResolvedValue({ connected: false, code: 'service_unavailable' });
    await user.click(screen.getByRole('button', { name: 'Refresh status' }));
    expect(await screen.findByText(/These are the last reported values/)).toBeVisible();
    expect(screen.getByRole('button', { name: 'Review folder' })).toBeDisabled();
    expect(screen.getByText(/Folder access is unavailable/)).toBeVisible();
    expect(mock.ServiceCommand).not.toHaveBeenCalled();
  });
});
