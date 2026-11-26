import { useRef, useState } from 'react';
import type { MouseEvent, ReactNode } from 'react';
import { bridge, usePanora } from '../client';
import { Button, Field, FormActions, Notice, Select, TextInput } from '../components/ui';
import {
  commonsApprovals,
  commonsCategories,
  commonsCategoryError,
  commonsID,
  commonsResolutionChoice,
  commonsResolutionPatch,
  commonsSearchRequest,
  maxCommonsApprovals,
} from './commonsSearch';
import type { CommonsSearchPage, CommonsSearchResult } from './commonsSearch';
import { ProviderField } from './ProviderFields';
import type { EditorField, ProviderValues } from './providerManifest';
import { labelResolution, wallpaperResolutions } from './wallpaperResolutions';

const categoryField: EditorField = {
  key: 'category',
  label: 'Categories',
  control: 'multivalue',
  help: 'Files must match every category. Typing loads suggestions from Wikimedia Commons.',
};

async function suggestCategories(prefix: string) {
  const result = await bridge('SuggestProviderInput', [
    { provider_id: commonsID, field: 'category', prefix, limit: 10 },
  ]);
  return result.values ?? [];
}

function externalPage(value: string, origin: string, prefix: string) {
  try {
    const url = new URL(value);
    if (url.origin === origin && !url.username && !url.password && url.pathname.startsWith(prefix))
      return url.href;
  } catch {
    return undefined;
  }
}

function openFile(event: MouseEvent<HTMLAnchorElement>) {
  const runtime = (window as Window & { runtime?: { BrowserOpenURL?(url: string): void } }).runtime;
  if (runtime?.BrowserOpenURL) {
    event.preventDefault();
    runtime.BrowserOpenURL(event.currentTarget.href);
  }
}

function ResultCard({
  result,
  approved,
  disabled,
  toggle,
}: {
  result: CommonsSearchResult;
  approved: boolean;
  disabled: boolean;
  toggle(): void;
}) {
  const page = externalPage(result.page_url, 'https://commons.wikimedia.org', '/wiki/File:');
  const license = externalPage(result.license_url, 'https://creativecommons.org', '/');
  const thumbnail = result.thumbnail;
  return (
    <li className="commons-result">
      {thumbnail &&
      thumbnail.length <= 80000 &&
      /^data:image\/(jpeg|png);base64,[A-Za-z0-9+/]+=*$/.test(thumbnail) ? (
        <img src={thumbnail} alt="" loading="lazy" />
      ) : (
        <p className="subtext commons-thumbnail-placeholder">Thumbnail unavailable</p>
      )}
      <div className="commons-result-details">
        <strong>
          {page ? (
            <a href={page} target="_blank" rel="noreferrer" onClick={openFile}>
              {result.title}
            </a>
          ) : (
            result.title
          )}
        </strong>
        <p className="subtext">
          {result.width} × {result.height} ·{' '}
          {license ? (
            <a href={license} target="_blank" rel="noreferrer" onClick={openFile}>
              {result.license_name}
            </a>
          ) : (
            result.license_name
          )}
        </p>
        <p className="subtext">{result.author}</p>
        <label className="search-check">
          <input
            type="checkbox"
            aria-label={`Approve ${result.title}`}
            checked={approved}
            disabled={disabled}
            onChange={toggle}
          />
          Approve for rotation
        </label>
      </div>
    </li>
  );
}

interface SearchResults extends CommonsSearchPage {
  scope: string;
  offset: number;
}

export function CommonsSearch({
  advanced,
  values,
  disabled,
  onChange,
}: {
  advanced: ReactNode;
  values: ProviderValues;
  disabled: boolean;
  onChange(value: ProviderValues): void;
}) {
  const ui = usePanora();
  const approved = commonsApprovals(values);
  const custom = values.collection === 'search';
  const resolution = commonsResolutionChoice(values);
  const [mode, size = ''] = resolution.split(':');
  const [width, height] = size.split('x');
  const savedSize = size && !wallpaperResolutions.includes(size);
  const { request, error } = commonsSearchRequest(values);
  const instance = ui.snapshot?.status?.instance_id;
  const scope = JSON.stringify([instance, request]);
  const [results, setResults] = useState<SearchResults>();
  const [feedback, setFeedback] = useState<{ scope: string; text: string }>();
  const [working, setWorking] = useState(false);
  const lock = useRef(false);
  const searchButton = useRef<HTMLButtonElement>(null);
  const visibleResults = results?.scope === scope ? results : undefined;
  const feedbackText = feedback?.scope === scope ? feedback.text : '';

  const text = (key: string) => {
    const value = values[key];
    return typeof value === 'string' || typeof value === 'number' ? value : '';
  };

  function change(patch: ProviderValues) {
    onChange({ ...patch, approved: undefined });
    setResults(undefined);
    setFeedback(undefined);
  }

  async function search(offset = 0) {
    if (lock.current || disabled) return;
    if (error) {
      setFeedback({ scope, text: error });
      return;
    }
    const origin = document.activeElement;
    lock.current = true;
    setWorking(true);
    setFeedback(undefined);
    if (!offset) setResults(undefined);
    try {
      const result = await ui.request(
        'SearchCommons',
        [{ ...request, ...(offset ? { offset } : {}) }],
        {
          shouldSend: () =>
            ui.getState().page === 'providers' &&
            ui.getState().snapshot?.status?.instance_id === instance,
        },
      );
      if (result.commons_search) setResults({ scope, offset, ...result.commons_search });
      else setFeedback({ scope, text: result.error || 'The Commons search could not be loaded.' });
    } finally {
      lock.current = false;
      setWorking(false);
      requestAnimationFrame(() => {
        if (
          ui.getState().page === 'providers' &&
          [origin, document.body].includes(document.activeElement)
        )
          searchButton.current?.focus();
      });
    }
  }

  return (
    <fieldset
      className="commons-search"
      aria-label="Wikimedia Commons"
      disabled={disabled || working}
    >
      {custom ? (
        <>
          <div className="commons-search-fields">
            <Field htmlFor="commons-query">
              Search terms
              <TextInput
                id="commons-query"
                value={text('query')}
                maxLength={240}
                onChange={(event) => change({ query: event.target.value })}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
                    event.preventDefault();
                    void search();
                  }
                }}
              />
            </Field>
            <Field htmlFor="commons-resolution">
              Resolution
              <Select
                id="commons-resolution"
                value={resolution}
                onChange={(event) => change(commonsResolutionPatch(event.target.value))}
              >
                <option value="">Any size</option>
                {(['min', 'exact'] as const).map((match) => (
                  <optgroup key={match} label={match === 'min' ? 'At least' : 'Exact'}>
                    {wallpaperResolutions.map((preset) => (
                      <option key={preset} value={`${match}:${preset}`}>
                        {match === 'min' ? '≥ ' : ''}
                        {labelResolution(preset)}
                      </option>
                    ))}
                    {savedSize && match === mode && (
                      <option value={resolution}>
                        {match === 'min' ? '≥ ' : ''}
                        {width && height
                          ? labelResolution(size)
                          : width
                            ? `${width} px wide`
                            : `${height} px tall`}
                      </option>
                    )}
                  </optgroup>
                ))}
              </Select>
            </Field>
          </div>
          <FormActions>
            <Button
              ref={searchButton}
              pending={working}
              disabled={disabled}
              onClick={() => void search()}
            >
              {working ? 'Searching Commons…' : 'Search Commons'}
            </Button>
          </FormActions>
          {feedbackText && <Notice tone="error">{feedbackText}</Notice>}
        </>
      ) : (
        <p className="subtext">
          Use the reviewed Featured landscapes collection from Wikimedia Commons.
        </p>
      )}
      <details className="commons-options">
        <summary>More options</summary>
        {custom && (
          <ProviderField
            id="commons-category"
            field={categoryField}
            value={commonsCategories(values)}
            suggest={disabled ? undefined : suggestCategories}
            addLabel="Add category"
            validateValue={commonsCategoryError}
            onChange={(categories) => change({ category: undefined, categories })}
          />
        )}
        {advanced}
      </details>
      {custom && (visibleResults || approved.length > 0) && (
        <div className="commons-approval-status">
          <p className="subtext" role="status">
            {approved.length > 0
              ? `${approved.length} of ${maxCommonsApprovals} files approved`
              : 'Approve the files you want to include in rotation.'}
          </p>
          {approved.length > 0 && (
            <Button onClick={() => onChange({ approved: [] })}>Clear approvals</Button>
          )}
        </div>
      )}
      {custom && visibleResults && (
        <>
          {visibleResults.results.length > 0 ? (
            <ul className="commons-results" aria-label="Commons search results">
              {visibleResults.results.map((result) => {
                const selected = approved.some(
                  ({ page_id, revision, terms_digest }) =>
                    page_id === result.page_id &&
                    revision === result.revision &&
                    terms_digest === result.terms_digest,
                );
                return (
                  <ResultCard
                    key={result.page_id}
                    result={result}
                    approved={selected}
                    disabled={
                      !selected &&
                      approved.length >= maxCommonsApprovals &&
                      !approved.some(({ page_id }) => page_id === result.page_id)
                    }
                    toggle={() => {
                      const next = approved.filter(({ page_id }) => page_id !== result.page_id);
                      if (!selected)
                        next.push({
                          page_id: result.page_id,
                          revision: result.revision,
                          terms_digest: result.terms_digest,
                        });
                      onChange({ approved: next });
                    }}
                  />
                );
              })}
            </ul>
          ) : (
            <Notice>No files on this page matched the search and reuse requirements.</Notice>
          )}
          <FormActions>
            {visibleResults.offset > 0 && (
              <Button onClick={() => void search()}>First results</Button>
            )}
            {visibleResults.next_offset !== undefined && (
              <Button onClick={() => void search(visibleResults.next_offset)}>Next results</Button>
            )}
          </FormActions>
        </>
      )}
    </fieldset>
  );
}
