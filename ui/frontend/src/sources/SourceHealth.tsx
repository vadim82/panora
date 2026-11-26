import { Button, Notice, TextInput } from '../components/ui';
import { useRef, useState } from 'react';
import { copyText, usePanora } from '../client';
import { playbackWarnings } from '../now/playbackWarnings';
import type { Configuration, Failure, ProviderHealth, Retry } from '../types';
import { DraftSearchMatches, SavedSearchCount, useSearchCounts } from './WallhavenCatalog';
import type { SearchDraftStatus } from './WallhavenCatalog';

export const providerLabel = (id: string, config?: Configuration | null): string =>
  ({ local: 'Local folders', wallhaven: 'Wallhaven', omarchy: 'Omarchy theme' })[id] ||
  config?.provider_editors?.find((provider) => provider.id === id)?.name ||
  'Image provider';
const stateLabels: Record<string, string> = {
  disabled: 'Disabled',
  pending: 'Waiting for a check',
  refreshing: 'Checking images…',
  ready: 'Check complete',
  empty: 'No results',
  failed: 'Needs attention',
  retrying: 'Waiting to retry',
};
export function sourceAttention(providers: ProviderHealth[] = []): string {
  const affected = providers.filter(
    (provider) =>
      provider.enabled &&
      (provider.state === 'failed' ||
        provider.retries?.length ||
        provider.inputs?.some(
          (input) => input.state !== 'disabled' && (input.failure || input.retries?.length),
        )),
  );
  return affected.length
    ? affected.map((provider) => providerLabel(provider.id)).join(' and ') +
        ' need attention. Other eligible local or cached images can still rotate when available.'
    : '';
}
export function clock(value?: string): string {
  if (!value) return 'Time unavailable';
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? 'Time unavailable'
    : date.toLocaleString([], {
        month: 'short',
        day: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
      });
}
export function failureText(failure: Failure, provider: string): string {
  const local = provider === 'local';
  const messages: Record<string, string> = {
    unavailable: local
      ? 'Folder access is unavailable. Check that the folder is connected and Panora can read it.'
      : 'This source is unavailable. Check your connection; Panora will retry automatically.',
    rate_limited: 'This source has asked Panora to wait. The service will respect its retry time.',
    auth_required:
      'This source requires authentication. Credential editing is not available in Panora yet.',
    auth_rejected:
      'This source rejected authentication. Credential editing is not available in Panora yet.',
    invalid_config: 'The source settings need attention. Review the saved folder or search.',
    not_found: local
      ? 'The folder or an image could not be found. Check the saved location and folder access.'
      : 'The source could not find a search input or image.',
    timeout: 'The source took too long to respond. Panora will retry automatically.',
    canceled: 'The last check was interrupted. The next automatic check remains managed by Panora.',
    download_failed:
      'An original image could not be downloaded. Other eligible local or cached images can still be used when available.',
    invalid_content:
      'An image could not be used: its bytes are invalid or exceed supported image limits. Your original files are kept.',
    filter_mismatch:
      'An inspected image does not match the current image rules. Review the rules if too many images are skipped.',
    cache_failed:
      'Panora could not prepare the cached image. Check storage and available disk space.',
    resolve_failed:
      'The source could not locate an original image. Other eligible images can still be used when available.',
    unsupported: 'This source returned an unsupported operation or image format.',
    discovery_failed:
      'Panora could not finish checking this source. Review its settings and connection.',
  };
  return (
    messages[failure.code] ||
    failure.message ||
    'The last check failed. Panora will manage the next automatic check.'
  );
}
function Retries({ retries, provider }: { retries?: Retry[]; provider: string }) {
  return (
    <>
      {retries?.map((retry, index) => (
        <div className="source-retry" key={`${retry.scope}-${retry.failure.code}-${index}`}>
          <p>{failureText(retry.failure, provider)}</p>
          <p className="source-time">Retry at {clock(retry.next_retry)}</p>
          <details>
            <summary>Retry details</summary>
            <p>
              {retry.attempts || 0} attempts ·{' '}
              {retry.affected_images ? `${retry.affected_images} affected images · ` : ''}
              {retry.failure.stage || retry.scope || 'Source check'}
            </p>
          </details>
        </div>
      ))}
    </>
  );
}
export function reviewImageRules(navigate: (page: 'providers') => void): void {
  navigate('providers');
  requestAnimationFrame(() => {
    const field = document.getElementById('providers-title');
    field?.scrollIntoView?.({ block: 'nearest' });
    field?.focus();
  });
}
export function SourceHealth({
  onReview,
  searchDraft,
}: {
  onReview(provider: string, id: string): void;
  searchDraft?: SearchDraftStatus | null;
}) {
  const ui = usePanora(),
    status = ui.snapshot?.status,
    reported = status?.providers || [],
    configuration = ui.liveConfiguration || ui.configuration;
  const [open, setOpen] = useState(false);
  const [countRefresh, setCountRefresh] = useState(0);
  const searches = new Map(configuration?.searches.map((search) => [search.id, search]));
  const wallhaven = reported.find((provider) => provider.id === 'wallhaven');
  const reportedSearches = new Map(wallhaven?.inputs.map((input) => [input.id, input]));
  const unreported = wallhaven?.omitted_inputs
    ? []
    : [...searches.values()].filter((search) => !reportedSearches.has(search.id));
  const providers: ProviderHealth[] = unreported.length
    ? [
        ...reported.filter((provider) => provider.id !== 'wallhaven'),
        {
          id: 'wallhaven',
          enabled: unreported.some((search) => search.enabled),
          state: 'unreported',
          ...wallhaven,
          inputs: [
            ...(wallhaven?.inputs || []),
            ...unreported.map((search) => ({
              id: search.id,
              state: search.enabled ? 'unreported' : 'disabled',
            })),
          ],
        },
      ]
    : reported;
  const revision = configuration?.revision;
  const active = open && ui.page === 'providers';
  const connection =
    active && ui.connected && !ui.blocked && revision === ui.configuration?.revision
      ? JSON.stringify([status?.instance_id, configuration?.effective.backend])
      : null;
  const countedSearches = [...searches.values()]
    .filter(
      ({ id, enabled }) => enabled && (!wallhaven?.omitted_inputs || reportedSearches.has(id)),
    )
    .map(({ id }) => ({ id, lastSuccess: reportedSearches.get(id)?.last_success }));
  const counts = useSearchCounts(countedSearches, revision, connection, countRefresh);
  const activeDraft = active ? searchDraft : null;
  const draftInStatus =
    activeDraft?.id &&
    searches.has(activeDraft.id) &&
    (!wallhaven?.omitted_inputs || reportedSearches.has(activeDraft.id));
  const [checking, setChecking] = useState(false),
    [notice, setNotice] = useState('');
  const checkingRef = useRef(false),
    refreshRef = useRef<HTMLButtonElement>(null);
  const attention = sourceAttention(providers),
    warnings = [...playbackWarnings(status?.warnings), ...(status?.refresh_warnings || [])];
  const disabled = ui.busy || !ui.connected || ui.blocked;
  async function check() {
    if (checkingRef.current || ui.getState().busy) return;
    checkingRef.current = true;
    setChecking(true);
    setNotice('');
    try {
      const result = await ui.refresh();
      if (result?.connected) setCountRefresh((value) => value + 1);
      setNotice(
        result?.connected
          ? 'Status refreshed. Automatic source checks and retry times are unchanged.'
          : 'Panora could not be reached. Your saved sources are kept; reconnect or review service controls in Settings.',
      );
    } finally {
      checkingRef.current = false;
      setChecking(false);
      requestAnimationFrame(() => {
        if (
          ui.getState().page === 'providers' &&
          (document.activeElement === document.body ||
            document.activeElement === refreshRef.current)
        )
          refreshRef.current?.focus();
      });
    }
  }
  return (
    <details
      id="source-troubleshooting"
      className="source-health"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>Source troubleshooting</summary>
      <div className="source-health-heading">
        <h2>Source status</h2>
        <Button
          ref={refreshRef}
          id="provider-refresh"
          className="outline"
          disabled={ui.busy || checking}
          onClick={() => void check()}
        >
          {checking ? 'Refreshing status…' : ui.connected ? 'Refresh status' : 'Reconnect'}
        </Button>
      </div>
      <p id="provider-state" className="subtext">
        {!ui.connected
          ? 'Disconnected · These are the last reported values. Reconnect to check current source health.'
          : status?.setup_required
            ? 'Add a folder or Wallhaven search, or enable a saved source to begin.'
            : attention ||
              (status?.refreshing
                ? 'Checking for new images… Rotation and source checks run independently.'
                : 'Sources are checked automatically. Use Refresh status to read the latest result.')}
      </p>
      <Notice id="provider-read-notice" hidden={!notice}>
        {notice}
      </Notice>
      <ul id="provider-list" className="provider-list">
        {!providers.length && (
          <li>
            {status?.setup_required
              ? 'No included sources. Add a folder or Wallhaven search above to get started.'
              : 'No source details have been reported yet.'}
          </li>
        )}
        {providers.map((provider) => (
          <li
            className="source-health-card"
            data-attention={Boolean(provider.enabled && sourceAttention([provider]))}
            key={provider.id}
          >
            <div className="source-card-heading">
              <h3>{providerLabel(provider.id, configuration)}</h3>
              <span className="source-state">
                {!provider.enabled
                  ? 'Disabled'
                  : stateLabels[provider.state] || 'Status unavailable'}
              </span>
            </div>
            {!provider.enabled && (
              <p className="subtext">
                Saved inputs stay available, but this provider does not contribute new images.
              </p>
            )}
            {provider.enabled && !provider.inputs.length && !provider.omitted_inputs && (
              <p className="subtext">No inputs reported. Add a source above.</p>
            )}
            {provider.inputs.map((input) => {
              const folder =
                provider.id === 'local'
                  ? configuration?.folders.find((item) => item.id === input.id)
                  : undefined;
              const search = provider.id === 'wallhaven' ? searches.get(input.id) : undefined;
              const external = configuration?.inputs?.find(
                (item) => item.provider_id === provider.id && item.id === input.id,
              );
              const name =
                external?.name ||
                folder?.name ||
                folder?.path.replace(/\/+$/, '').split('/').pop() ||
                search?.name ||
                search?.query ||
                (provider.id === 'local'
                  ? 'Wallpaper folder'
                  : provider.id === 'wallhaven'
                    ? 'Saved search'
                    : 'Saved source');
              return (
                <section className="input-health" key={input.id}>
                  <h4>{name}</h4>
                  <p className="source-state">{stateLabels[input.state] || 'Status unavailable'}</p>
                  {folder?.path && <p className="source-path">{folder.path}</p>}
                  {input.counts ? (
                    <>
                      <p className="source-counts">
                        {input.counts.inspected} inspected in the latest check ·{' '}
                        {input.counts.known_eligible == null
                          ? 'Matching images not yet verified'
                          : `${input.counts.known_eligible} known eligible in this scope`}
                      </p>
                      <p className="subtext">
                        {input.counts.pages != null
                          ? `${input.counts.pages} ${input.counts.pages === 1 ? 'page' : 'pages'} · `
                          : ''}
                        Metadata from this check; original bytes may still need inspection.
                      </p>
                    </>
                  ) : (
                    <p className="subtext">Image counts have not been reported for this check.</p>
                  )}
                  {active && search?.enabled && (
                    <SavedSearchCount
                      result={counts?.get(search.id)?.result}
                      available={Boolean(connection)}
                    />
                  )}
                  {activeDraft && search && search.id === activeDraft.id && (
                    <DraftSearchMatches
                      draft={activeDraft}
                      connection={connection}
                      revision={revision}
                    />
                  )}
                  {input.state === 'empty' && !input.failure && (
                    <p>
                      The check succeeded with no results.{' '}
                      {provider.id === 'local'
                        ? 'Review this folder and its subfolder depth.'
                        : 'Try broader search terms.'}
                    </p>
                  )}
                  <p className="source-time">
                    {input.last_success
                      ? 'Last successful check at ' + clock(input.last_success)
                      : 'No successful check reported yet.'}
                  </p>
                  {input.next_refresh && input.state !== 'disabled' && provider.enabled && (
                    <p className="source-time">Next check at {clock(input.next_refresh)}</p>
                  )}
                  {input.failure && (
                    <p className="source-failure">{failureText(input.failure, provider.id)}</p>
                  )}
                  {provider.enabled && input.state !== 'disabled' && (
                    <Retries retries={input.retries} provider={provider.id} />
                  )}
                  {input.failure && (
                    <details>
                      <summary>Check details</summary>
                      <p>
                        {input.failure.stage || 'Source check'} · {input.failure.code || 'unknown'}
                      </p>
                      {input.last_attempt && <p>Last attempt at {clock(input.last_attempt)}</p>}
                    </details>
                  )}
                  <div className="source-health-actions">
                    {['local', 'wallhaven'].includes(provider.id) && (
                      <Button
                        className="outline"
                        data-health-action="review"
                        disabled={disabled}
                        onClick={() => onReview(provider.id, input.id)}
                      >
                        {provider.id === 'local' ? 'Review folder' : 'Review search'}
                      </Button>
                    )}
                    {search?.enabled && configuration && (
                      <WallhavenSearchURL
                        key={`${status?.instance_id}/${search.id}/${configuration.revision}`}
                        id={search.id}
                        revision={configuration.revision}
                        disabled={disabled}
                        catalogURL={counts?.get(search.id)?.result.counts?.search_url}
                      />
                    )}
                    {input.failure?.code === 'filter_mismatch' && (
                      <Button
                        className="outline"
                        disabled={disabled}
                        onClick={() => onReview(provider.id, input.id)}
                      >
                        Review image rules
                      </Button>
                    )}
                  </div>
                </section>
              );
            })}
            {Boolean(provider.omitted_inputs) && (
              <p className="subtext">
                {provider.omitted_inputs} additional inputs are outside this status response.
              </p>
            )}
            {provider.enabled && <Retries retries={provider.retries} provider={provider.id} />}
          </li>
        ))}
      </ul>
      {activeDraft && !draftInStatus && (
        <DraftSearchMatches draft={activeDraft} connection={connection} revision={revision} />
      )}
      <div id="provider-warnings">
        {warnings.length > 0 && (
          <details>
            <summary>Service notices</summary>
            {warnings.map((warning, index) => (
              <p key={`${warning}-${index}`}>{warning}</p>
            ))}
          </details>
        )}
      </div>
      <p className="subtext">
        Panora checks sources automatically, even while rotation is paused. Catalog counts use known
        image dimensions. Panora validates originals when selecting wallpapers.
      </p>
    </details>
  );
}

function WallhavenSearchURL({
  id,
  revision,
  disabled,
  catalogURL,
}: {
  id: string;
  revision: string;
  disabled: boolean;
  catalogURL?: string;
}) {
  const ui = usePanora();
  const [url, setURL] = useState('');
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(false);
  const field = useRef<HTMLInputElement>(null);

  async function show() {
    if (!url && catalogURL) {
      setURL(catalogURL);
      return;
    }
    setLoading(true);
    setNotice('');
    const result = await ui.request('CountSearchCandidates', [id, revision]);
    const value = result.counts?.search_url;
    if (value) setURL(value);
    else setNotice(result.error || 'The Wallhaven URL is unavailable.');
    setLoading(false);
  }

  async function copy() {
    try {
      await copyText(url);
      setNotice('Wallhaven URL copied.');
    } catch {
      field.current?.focus();
      field.current?.select();
      setNotice('Copy the selected URL.');
    }
  }

  return (
    <>
      <Button className="outline" disabled={disabled} pending={loading} onClick={() => void show()}>
        {loading ? 'Loading search URL…' : url ? 'Refresh Wallhaven URL' : 'Show Wallhaven URL'}
      </Button>
      {url && (
        <div className="source-search-url">
          <div className="source-search-url-field">
            <TextInput
              aria-label="Wallhaven search URL"
              onFocus={(event) => event.currentTarget.select()}
              ref={field}
              value={url}
              readOnly
            />
            <Button className="outline" onClick={() => void copy()}>
              Copy URL
            </Button>
          </div>
          <p className="subtext">Panora applies local eligibility checks after discovery.</p>
        </div>
      )}
      {notice && (
        <p className="source-search-url" role="status">
          {notice}
        </p>
      )}
    </>
  );
}
