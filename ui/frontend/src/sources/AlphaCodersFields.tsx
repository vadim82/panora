import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { bridge } from '../client';
import { Button, Field, Notice, Select, TextInput } from '../components/ui';
import type { AlphaCheck, AlphaRequest, AlphaSample, AlphaTag } from '../types';
import type { ProviderValues } from './providerManifest';
import { SuggestionInput } from './SuggestionInput';

const count = new Intl.NumberFormat();
async function alpha(query: AlphaRequest) {
  const result = await bridge('AlphaCoders', [query]);
  if (result.error) throw new Error(result.error);
  return result;
}

export function AlphaCodersFields({
  values,
  onChange,
  onConnection,
}: {
  values: ProviderValues;
  onChange(values: ProviderValues): void;
  onConnection(connected: boolean): void;
}) {
  const [connected, setConnected] = useState(false);
  const [ready, setReady] = useState(false);
  const [manage, setManage] = useState(false);
  const [working, setWorking] = useState(false);
  const [error, setError] = useState('');
  const [suggestError, setSuggestError] = useState('');
  const [more, setMore] = useState(false);
  const [mode, setMode] = useState(values.keyword ? 'keyword' : 'tags');
  const keyInput = useRef<HTMLInputElement>(null);
  const tagsInput = useRef<{ key: string; tags: AlphaTag[] }[]>([]);
  const valuesTags = values.tags;
  const tags = Array.isArray(valuesTags)
    ? valuesTags.filter((item): item is AlphaTag => typeof item === 'object')
    : [];
  useEffect(() => {
    let current = true;
    void alpha({ action: 'status' })
      .then(({ connected }) => {
        if (!current) return;
        setConnected(connected);
        setManage(!connected);
        onConnection(connected);
      })
      .catch(() => {
        if (!current) return;
        setManage(true);
        setError('Could not read the Alpha Coders connection.');
      })
      .finally(() => {
        if (current) setReady(true);
      });
    return () => {
      current = false;
    };
  }, [onConnection]);

  async function connection(remove = false) {
    if (working) return;
    const key = keyInput.current?.value.trim() || '';
    if (!remove && !key) {
      setError('Enter your personal API key.');
      keyInput.current?.focus();
      return;
    }
    if (keyInput.current) keyInput.current.value = '';
    setWorking(true);
    setError('');
    try {
      const result = await alpha(remove ? { action: 'remove' } : { action: 'connect', key });
      setConnected(result.connected);
      setManage(!result.connected);
      onConnection(result.connected);
      if (result.connected)
        requestAnimationFrame(() => document.getElementById('alpha-tag')?.focus());
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Alpha Coders could not complete the request.');
    } finally {
      setWorking(false);
    }
  }
  const suggest = useCallback(async (prefix: string) => {
    setSuggestError('');
    try {
      const result = await alpha({ action: 'tags', prefix });
      const tags = result.tags || [];
      const key = prefix.toLocaleLowerCase();
      tagsInput.current = [
        ...tagsInput.current.filter((row) => row.key !== key).slice(-12),
        { key, tags },
      ];
      return tags.map(({ name }) => name);
    } catch (err) {
      setSuggestError(err instanceof Error ? err.message : 'Tag suggestions are unavailable.');
      return [];
    }
  }, []);
  const selected = tags.map(({ name }) => name);
  function option(name: string) {
    return tagsInput.current.flatMap(({ tags }) => tags).find((tag) => tag.name === name);
  }

  return (
    <>
      {!ready && <p className="subtext">Checking the Alpha Coders connection…</p>}
      {ready && connected && !manage && (
        <div className="alpha-connection">
          <span className="subtext">Alpha Coders key connected</span>
          <Button onClick={() => setManage(true)}>Manage key</Button>
        </div>
      )}
      {ready && manage && (
        <div className="alpha-connection-form">
          <Field htmlFor="alpha-key">
            Personal API key
            <input
              id="alpha-key"
              className="ui-input"
              type="password"
              ref={keyInput}
              autoComplete="off"
              spellCheck={false}
              maxLength={256}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault();
                  void connection();
                }
              }}
            />
          </Field>
          <p className="subtext">
            Connecting allows Panora to send tag prefixes and searches to Alpha Coders.
          </p>
          <div className="source-card-actions">
            <Button pending={working} onClick={() => void connection()}>
              {connected ? 'Replace key' : 'Connect'}
            </Button>
            {connected && (
              <>
                <Button disabled={working} onClick={() => void connection(true)}>
                  Remove key
                </Button>
                <Button disabled={working} onClick={() => setManage(false)}>
                  Done
                </Button>
              </>
            )}
            <Button
              onClick={() => {
                const native = (
                  window as Window & { runtime?: { BrowserOpenURL?(url: string): void } }
                ).runtime;
                const location = 'https://api.alphacoders.com/api/instructions';
                if (native?.BrowserOpenURL) native.BrowserOpenURL(location);
                else window.open(location, '_blank', 'noopener,noreferrer');
              }}
            >
              Get a key
            </Button>
          </div>
        </div>
      )}
      {error && <Notice tone="error">{error}</Notice>}
      {connected && (
        <>
          {mode === 'tags' ? (
            <div className="alpha-tags">
              <span>Tags</span>
              {tags.length > 0 && (
                <div className="alpha-tag-list" aria-label="Selected tags">
                  {tags.map((tag) => (
                    <Button
                      key={tag.id}
                      aria-label={`Remove ${tag.name}`}
                      onClick={() =>
                        onChange({ ...values, tags: tags.filter(({ id }) => id !== tag.id) })
                      }
                    >
                      {tag.name} ×
                    </Button>
                  ))}
                </div>
              )}
              <SuggestionInput
                id="alpha-tag"
                label="Add an Alpha Coders tag"
                placeholder="Add a tag…"
                addLabel="Add tag"
                approvedOnly
                selected={selected}
                suggest={tags.length < 5 ? suggest : undefined}
                renderOption={(name) => (
                  <span className="alpha-tag-option">
                    <span>{name}</span>
                    <span className="subtext">
                      {count.format(option(name)?.wallpaper_count || 0)} total
                    </span>
                  </span>
                )}
                onAdd={(name) => {
                  const tag = option(name);
                  if (!tag || tags.length >= 5 || tags.some(({ id }) => id === tag.id))
                    return false;
                  onChange({ ...values, tags: [...tags, { id: tag.id, name: tag.name }] });
                  return true;
                }}
              />
              <p className="subtext">
                {tags.length >= 5
                  ? 'Remove a tag to add another.'
                  : 'Matches any selected tag. Suggestions show totals before filters.'}
              </p>
              {suggestError && <Notice tone="error">{suggestError}</Notice>}
            </div>
          ) : (
            <Field htmlFor="alpha-keyword">
              Keyword
              <TextInput
                id="alpha-keyword"
                maxLength={128}
                value={String(values.keyword || '')}
                onChange={(event) => onChange({ ...values, keyword: event.target.value })}
              />
            </Field>
          )}
          <Button aria-expanded={more} onClick={() => setMore(!more)}>
            More options
          </Button>
          {more && (
            <div className="rules-grid">
              <Field htmlFor="alpha-mode">
                Search by
                <Select
                  id="alpha-mode"
                  value={mode}
                  onChange={(event) => {
                    const keyword = event.target.value === 'keyword';
                    setMode(event.target.value);
                    onChange({
                      ...values,
                      ...(keyword ? { tags: undefined, keyword: '' } : { keyword: undefined }),
                    });
                  }}
                >
                  <option value="tags">Tags</option>
                  <option value="keyword">Keyword</option>
                </Select>
              </Field>
              <Field htmlFor="alpha-sort">
                Sort
                <Select
                  id="alpha-sort"
                  value={String(values.sort || 'newest')}
                  onChange={(event) => onChange({ ...values, sort: event.target.value })}
                >
                  <option value="newest">Newest</option>
                  <option value="highest_rated">Highest rated</option>
                </Select>
              </Field>
            </div>
          )}
        </>
      )}
    </>
  );
}

export function AlphaCodersResults({
  query,
  disabled,
}: {
  query: AlphaRequest;
  disabled: boolean;
}) {
  const [check, setCheck] = useState<AlphaCheck | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [limit, setLimit] = useState(3);
  const fingerprint = JSON.stringify(query);
  const latest = useRef(fingerprint);
  latest.current = fingerprint;
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const [checked, setChecked] = useState('');
  const current = check && checked === fingerprint ? check : null;
  const samples = useMemo(() => current?.samples.slice(0, limit) || [], [current, limit]);

  async function inspect() {
    if (pending || disabled) return;
    setPending(true);
    setError('');
    setCheck(null);
    setLimit(3);
    setChecked(fingerprint);
    try {
      const result = await alpha(query);
      if (alive.current && latest.current === fingerprint) setCheck(result.check || null);
    } catch (err) {
      if (alive.current && latest.current === fingerprint)
        setError(err instanceof Error ? err.message : 'Could not check results.');
    } finally {
      if (alive.current) setPending(false);
    }
  }

  const pages = current?.scopes.reduce((sum, row) => sum + row.pages, 0) || 0;
  const partial = current?.scopes.some((row) => row.more || row.error);
  const failure = current?.scopes.find((row) => row.error)?.error;

  return (
    <div className="alpha-results">
      <Button
        variant="secondary"
        pending={pending}
        disabled={disabled}
        onClick={() => void inspect()}
      >
        Check results
      </Button>
      {error && checked === fingerprint && <Notice tone="error">{error}</Notice>}
      {check && !current && <p className="subtext">Search changed. Check results again.</p>}
      {current && !pages && failure && <Notice tone="error">{failure}</Notice>}
      {current && pages > 0 && (
        <>
          <p role="status">
            <strong>
              {count.format(current.count)} {current.count === 1 ? 'candidate' : 'candidates'} found
            </strong>
          </p>
          <p className="subtext">
            {pages} {pages === 1 ? 'page' : 'pages'} checked{partial ? ' · Partial results' : ''}
          </p>
          {failure && <Notice tone="error">{failure}</Notice>}
          {Array.isArray(query.settings?.tags) && query.settings.tags.length > 0 && (
            <ul
              className="alpha-result-counts"
              aria-label="Candidates by tag. The combined count excludes duplicates."
            >
              {current.scopes
                .filter((row) => row.pages > 0)
                .map((row) => (
                  <li key={row.name}>
                    {row.name} · {count.format(row.count)}
                    {row.more ? '+' : ''}
                  </li>
                ))}
            </ul>
          )}
          {samples.length > 0 && (
            <div className="alpha-sample-list">
              {samples.map((sample) => (
                <Thumbnail key={sample.thumbnail} sample={sample} />
              ))}
            </div>
          )}
          {current.samples.length > 3 && (
            <>
              <p className="subtext">
                Showing {samples.length} of {current.samples.length} previews
              </p>
              {samples.length < current.samples.length && (
                <Button onClick={() => setLimit((previous) => previous + 6)}>
                  Show more previews
                </Button>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
}

function Thumbnail({ sample }: { sample: AlphaSample }) {
  const [url, setURL] = useState('');
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    let current = true;
    void alpha({ action: 'thumbnail', thumbnail: sample.thumbnail })
      .then(({ thumbnail }) => {
        if (current) {
          setURL(thumbnail || '');
          setUnavailable(!thumbnail);
        }
      })
      .catch(() => {
        if (current) setUnavailable(true);
      });
    return () => {
      current = false;
    };
  }, [sample.thumbnail]);
  const title = sample.title || 'Alpha Coders wallpaper';
  const open = async (page: string) => {
    try {
      const result = await bridge('OpenAlphaCodersPage', [page]);
      if (!result.opened) setUnavailable(true);
    } catch {
      setUnavailable(true);
    }
  };
  return (
    <div>
      {url ? (
        <img src={url} alt="" />
      ) : (
        <p className="subtext">{unavailable ? 'Preview unavailable' : 'Loading preview…'}</p>
      )}
      {sample.page ? (
        <Button title={title} onClick={() => void open(sample.page!)}>
          {title}
        </Button>
      ) : (
        <p>{title}</p>
      )}
      <p className="subtext">
        {sample.width} × {sample.height}
      </p>
    </div>
  );
}
