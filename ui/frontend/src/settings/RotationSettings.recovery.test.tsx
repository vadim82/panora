import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, it } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { installBridge, snapshot } from '../test/fixtures';
import type { CacheStatus, Result, Snapshot } from '../types';
import { RotationSettings } from './RotationSettings';

const usage = (images: number): CacheStatus => ({
  saved: { max_images: 20, max_bytes: 104857600 },
  effective: { max_images: 20, max_bytes: 104857600 },
  usage: {
    state: 'known',
    images,
    bytes: images * 1048576,
    protected_images: 1,
    protected_bytes: 1048576,
  },
});

function Harness() {
  const client = usePanora();
  return (
    <>
      <button onClick={() => client.navigate('settings')}>Settings page</button>
      <button onClick={() => client.navigate('now')}>Now page</button>
      <button onClick={() => void client.refresh({ background: true })}>Read status</button>
      <section hidden={client.page !== 'settings'}>
        <RotationSettings />
      </section>
    </>
  );
}
async function mount() {
  const user = userEvent.setup();
  render(
    <ClientProvider>
      <Harness />
    </ClientProvider>,
  );
  await user.click(screen.getByRole('button', { name: 'Settings page' }));
  await user.click(screen.getByText('Storage usage'));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Refresh usage' })).toBeEnabled());
  return user;
}

it('clears old storage measurements on disconnect and automatically checks the reconnected service without losing drafts', async () => {
  const mock = installBridge();
  let reported: Snapshot = structuredClone(snapshot);
  mock.Status.mockImplementation(async () => structuredClone(reported));
  mock.CacheStatus.mockResolvedValue({ cache: usage(3) });
  const user = await mount();
  expect(screen.getByText(/3 managed images/)).toBeVisible();
  await user.click(screen.getByText('Storage limits'));
  fireEvent.change(screen.getByRole('spinbutton', { name: 'Maximum cached images' }), {
    target: { value: '35' },
  });
  reported = { connected: false, error: 'Disconnected' };
  await user.click(screen.getByRole('button', { name: 'Read status' }));
  await waitFor(() => expect(screen.queryByText(/3 managed images/)).not.toBeInTheDocument());
  expect(screen.getByText('Usage has not been checked.')).toBeVisible();
  expect(screen.getByRole('button', { name: 'Refresh usage' })).toBeDisabled();
  reported = structuredClone(snapshot);
  mock.CacheStatus.mockResolvedValue({ cache: usage(7) });
  await user.click(screen.getByRole('button', { name: 'Read status' }));
  expect(await screen.findByText(/7 managed images/)).toBeVisible();
  expect(mock.CacheStatus).toHaveBeenCalledTimes(2);
  expect(screen.getByRole('spinbutton', { name: 'Maximum cached images' })).toHaveValue(35);
  await user.click(screen.getByRole('button', { name: 'Read status' }));
  expect(mock.CacheStatus).toHaveBeenCalledTimes(2);
  expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
  expect(mock.Action).not.toHaveBeenCalled();
});

it('rechecks storage when a connected service instance changes and keeps the replacement failure distinct from old usage', async () => {
  const mock = installBridge();
  let reported: Snapshot = structuredClone(snapshot);
  mock.Status.mockImplementation(async () => structuredClone(reported));
  mock.CacheStatus.mockResolvedValue({ cache: usage(3) });
  const user = await mount();
  mock.CacheStatus.mockResolvedValue({ error: 'Replacement cache is unavailable.' });
  reported = { ...reported, status: { ...reported.status, instance_id: 'replacement-service' } };
  await user.click(screen.getByRole('button', { name: 'Read status' }));
  expect(await screen.findByText('Replacement cache is unavailable.')).toBeVisible();
  expect(screen.queryByText(/3 managed images/)).not.toBeInTheDocument();
  expect(screen.queryByText(/last successful measurement/)).not.toBeInTheDocument();
  expect(mock.CacheStatus).toHaveBeenCalledTimes(2);
});

it('discards a completed measurement when its response reveals a replacement service', async () => {
  const mock = installBridge();
  mock.CacheStatus.mockResolvedValue({ cache: usage(3) });
  const user = await mount();
  let finish: (result: Result) => void = () => {};
  mock.CacheStatus.mockResolvedValueOnce({
    cache: usage(90),
    connected: true,
    status: { ...snapshot.status, instance_id: 'replacement-service' },
  }).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await user.click(screen.getByRole('button', { name: 'Refresh usage' }));
  await waitFor(() => expect(mock.CacheStatus).toHaveBeenCalledTimes(3));
  expect(screen.queryByText(/90 managed images/)).not.toBeInTheDocument();
  expect(screen.queryByText(/3 managed images/)).not.toBeInTheDocument();
  expect(screen.getByText('Usage has not been checked.')).toBeVisible();
  expect(screen.getByRole('button', { name: 'Checking usage…' })).toHaveAttribute(
    'aria-busy',
    'true',
  );
  await act(async () => finish({ cache: usage(7) }));
  expect(await screen.findByText(/7 managed images/)).toBeVisible();
  expect(mock.Action).not.toHaveBeenCalled();
});

it('announces a pending usage check, sends one read and returns keyboard focus after failure', async () => {
  const mock = installBridge();
  mock.CacheStatus.mockResolvedValue({ cache: usage(3) });
  const user = await mount();
  let finish: (result: Result) => void = () => {};
  mock.CacheStatus.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const refresh = screen.getByRole('button', { name: 'Refresh usage' });
  refresh.focus();
  await user.keyboard('{Enter}');
  const pending = screen.getByRole('button', { name: 'Checking usage…' });
  expect(pending).toHaveAttribute('aria-busy', 'true');
  expect(pending).toBeDisabled();
  await user.keyboard('{Enter}');
  expect(mock.CacheStatus).toHaveBeenCalledTimes(2);
  await act(async () => finish({ error: 'Cache measurement failed.' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Refresh usage' })).toHaveFocus());
  expect(screen.getByText(/Cache measurement failed.*last successful measurement/)).toBeVisible();
  expect(screen.getByText(/3 managed images/)).toBeVisible();
  expect(mock.Action).not.toHaveBeenCalled();
});

it('does not take focus back from another page after a usage check completes', async () => {
  const mock = installBridge(),
    user = await mount();
  let finish: (result: Result) => void = () => {};
  mock.CacheStatus.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await user.click(screen.getByRole('button', { name: 'Refresh usage' }));
  const destination = screen.getByRole('button', { name: 'Now page' });
  await user.click(destination);
  await act(async () => finish({ cache: usage(7) }));
  await waitFor(() => expect(destination).toHaveFocus());
  expect(mock.Action).not.toHaveBeenCalled();
});
