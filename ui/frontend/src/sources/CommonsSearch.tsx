import { useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import { usePanora } from '../client';
import {
  Button,
  Field,
  FormActions,
  Notice,
  NumberInput,
  Select,
  TextInput,
} from '../components/ui';
import {
  commonsApprovals,
  commonsResolutionMode,
  commonsSearchRequest,
  maxCommonsApprovals,
} from './commonsSearch';
import type { CommonsSearchPage, CommonsSearchResult } from './commonsSearch';
import type { ProviderValues } from './providerManifest';

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
        <strong>{result.title}</strong>
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
        {page && (
          <a href={page} target="_blank" rel="noreferrer" onClick={openFile}>
            View file on Commons
          </a>
        )}
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
  values,
  disabled,
  onChange,
}: {
  values: ProviderValues;
  disabled: boolean;
  onChange(value: ProviderValues): void;
}) {
  const ui = usePanora();
  const approved = commonsApprovals(values);
  const mode = commonsResolutionMode(values);
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
  const prefix = mode === 'exact' ? 'exact' : 'min';
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
    <fieldset className="provider-field-group commons-search" disabled={disabled || working}>
      <legend>Search filters</legend>
      <div className="commons-search-fields commons-search-terms">
        <Field htmlFor="commons-query">
          Search terms
          <TextInput
            id="commons-query"
            value={text('query')}
            maxLength={240}
            onChange={(event) => change({ query: event.target.value })}
          />
        </Field>
        <Field htmlFor="commons-category" help="Use the name without the Category: prefix.">
          Category
          <TextInput
            id="commons-category"
            value={text('category')}
            maxLength={200}
            onChange={(event) => change({ category: event.target.value })}
          />
        </Field>
      </div>
      <div className="commons-search-fields">
        <Field htmlFor="commons-resolution">
          Search resolution
          <Select
            id="commons-resolution"
            value={mode}
            onChange={(event) =>
              change({
                resolution_mode: event.target.value,
                exact_width: undefined,
                exact_height: undefined,
                min_width: undefined,
                min_height: undefined,
              })
            }
          >
            <option value="minimum">Minimum size</option>
            <option value="exact">Exact resolution</option>
          </Select>
        </Field>
        <Field htmlFor="commons-format">
          File format
          <Select
            id="commons-format"
            value={text('format')}
            onChange={(event) => change({ format: event.target.value })}
          >
            <option value="">JPEG and PNG</option>
            <option value="jpeg">JPEG</option>
            <option value="png">PNG</option>
          </Select>
        </Field>
      </div>
      <div className="commons-search-fields">
        {(['width', 'height'] as const).map((dimension) => {
          const id = `commons-${dimension}`;
          const key = `${prefix}_${dimension}`;
          return (
            <Field htmlFor={id} key={id}>
              {mode === 'exact' ? 'Exact' : 'Minimum'} {dimension}
              <NumberInput
                id={id}
                value={text(key)}
                min={1}
                max={65535}
                onChange={(event) => change({ [key]: event.target.value })}
              />
            </Field>
          );
        })}
      </div>
      <p className="subtext">
        Search sends these filters to Wikimedia Commons. Review the images and reuse terms, then
        approve files before saving them to rotation.
      </p>
      <FormActions>
        <Button
          ref={searchButton}
          pending={working}
          disabled={disabled}
          onClick={() => void search()}
        >
          {working ? 'Searching Commons…' : 'Search Commons'}
        </Button>
        {approved.length > 0 && (
          <Button onClick={() => onChange({ approved: [] })}>Clear approvals</Button>
        )}
      </FormActions>
      {feedbackText && <Notice tone="error">{feedbackText}</Notice>}
      <p className="subtext" role="status">
        {approved.length} of {maxCommonsApprovals} files approved
      </p>
      {visibleResults && (
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
