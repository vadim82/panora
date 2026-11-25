import { Button, Field, FormActions, Notice, TextInput } from '../components/ui';
import { useLayoutEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { usePanora } from '../client';
import type { ConfiguredSearch, SearchEdit, ImageRules } from '../types';
import type { EditorCallbacks } from './editorCallbacks';
import { useSourceDraft } from './useSourceDraft';
import { useSourceEditorFocus } from './useSourceEditorFocus';
import { SourceRules, sourceRuleDraft, sourceRuleError, sourceRulePatch } from './SourceRules';
import { SearchFilters } from './SearchFilters';
import { ProviderField, ProviderFields } from './ProviderFields';
import { manifestDefaults } from './providerManifest';
import type { EditorField } from './providerManifest';
import { wallhavenField, wallhavenGroup, wallhavenManifest } from './wallhavenManifest';
import type { FilterGroup, SearchFilterDraft } from './SearchFilters';

type Draft = SearchFilterDraft & {
  name: string;
  query: string;
  enabled: boolean;
  sorting: string;
  order: string;
  max_pages: string;
  top_range: string;
  refresh_interval: string;
};
const providerDefaults = manifestDefaults(wallhavenManifest);
const defaults: Draft = {
  ...sourceRuleDraft(),
  name: '',
  enabled: true,
  query: String(providerDefaults.query),
  categories: String(providerDefaults.categories),
  tags: (providerDefaults.tags as string[]).join('\n'),
  tag_match: 'any',
  colors: (providerDefaults.colors as string[]).join(','),
  sorting: String(providerDefaults.sorting),
  order: String(providerDefaults.order),
  max_pages: String(providerDefaults.max_pages),
  top_range: String(providerDefaults.top_range),
  refresh_interval: String(providerDefaults.refresh_interval),
};
const values = (search: ConfiguredSearch, inherited: ImageRules): Draft => ({
  ...sourceRuleDraft(search, inherited),
  name: search.custom_name ?? search.name ?? '',
  tags: (search.tags || []).join('\n'),
  tag_match: search.tag_match ?? 'all',
  colors: (search.colors || []).map((color) => color.toLowerCase()).join(','),
  query: search.query,
  enabled: search.input_enabled,
  categories: search.categories,
  sorting: search.sorting,
  order: search.order,
  max_pages: String(search.max_pages),
  top_range: search.top_range,
  refresh_interval: search.refresh_interval,
});

type Props = EditorCallbacks & { search?: ConfiguredSearch; mutable: boolean };

export function SearchEditor({ search, mutable, onDirty, onCancel, onSaved }: Props) {
  const ui = usePanora();
  const config = ui.configuration!;
  const id = search?.id || undefined;
  const saved = id ? config.searches.find((input) => input.id === id) : undefined;
  const [editor, setEditor] = useSourceDraft(
    config.revision,
    id,
    () => {
      if (search) return values(search, config.rules);
      return defaults;
    },
    saved,
    config.rules,
    values,
  );
  const [notice, setNotice] = useState(''),
    [working, setWorking] = useState(false),
    [options, setOptions] = useState(false),
    [panel, setPanel] = useState<FilterGroup | null>(null),
    [renaming, setRenaming] = useState(false),
    [validation, setValidation] = useState(0);
  const lock = useRef(false);
  const { focus, restoreAfter } = useSourceEditorFocus(ui.getState);
  const canSave = mutable && !working;
  const { draft, baseline, missing, revision } = editor;
  const existing = id && !missing;
  const pages = Number(draft.max_pages);
  const pagesError =
    !Number.isInteger(pages) || pages < 1 || pages > 100
      ? 'Use a whole number from 1 to 100 pages per refresh.'
      : undefined;
  const refreshError = !draft.refresh_interval.trim()
    ? 'Enter a refresh interval such as 5m.'
    : undefined;
  const changed = (Object.keys(draft) as (keyof Draft)[]).some(
    (key) => draft[key] !== baseline[key],
  );
  const dirty = changed || missing;
  useLayoutEffect(() => {
    onDirty(dirty);
  }, [dirty, onDirty]);
  function edit<K extends keyof Draft>(field: K, value: Draft[K]) {
    setEditor((current) => ({ ...current, draft: { ...current.draft, [field]: value } }));
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSave) return;
    const origin = document.activeElement;
    setValidation((value) => value + 1);
    if (pagesError || refreshError) {
      setOptions(true);
      focus.current = pagesError ? 'search-pages' : 'search-refresh';
      setNotice('Use 1–100 pages per refresh and enter a refresh interval. Your draft is kept.');
      return;
    }
    const ruleError = sourceRuleError(draft, config.rules);
    if (ruleError) {
      setPanel('rules');
      setNotice(ruleError.message + ' Your draft is kept.');
      focus.current = 'search-filter-rule-mode';
      return;
    }
    const edit: SearchEdit = { revision, ...(existing ? { id } : {}) };
    if (!existing || draft.name !== baseline.name) edit.name = draft.name;
    if (!existing || draft.query !== baseline.query) edit.query = draft.query;
    if (!existing || draft.enabled !== baseline.enabled) edit.enabled = draft.enabled;
    if (!existing || draft.categories !== baseline.categories) edit.categories = draft.categories;
    if (!existing || draft.tags !== baseline.tags)
      edit.tags = draft.tags ? draft.tags.split('\n') : [];
    if (!existing || draft.tag_match !== baseline.tag_match) edit.tag_match = draft.tag_match;
    if (!existing || draft.colors !== baseline.colors)
      edit.colors = draft.colors ? draft.colors.split(',') : [];
    if (!existing || draft.sorting !== baseline.sorting) edit.sorting = draft.sorting;
    if (!existing || draft.order !== baseline.order) edit.order = draft.order;
    if (!existing || draft.max_pages !== baseline.max_pages) edit.max_pages = pages;
    if (!existing || draft.top_range !== baseline.top_range) edit.top_range = draft.top_range;
    if (!existing || draft.refresh_interval !== baseline.refresh_interval)
      edit.refresh_interval = draft.refresh_interval;
    Object.assign(edit, sourceRulePatch(draft, baseline, Boolean(existing)));
    if (lock.current) return;
    lock.current = true;
    setWorking(true);
    const result = existing
      ? await ui.request('UpdateSearch', [edit], { mutation: true })
      : await ui.request('AddSearch', [edit], { mutation: true });
    if (!result.configuration) {
      setNotice((result.error || 'The search could not be saved.') + ' Your draft is kept.');
      restoreAfter('search-save', origin);
    } else {
      const saved = result.configuration.searches.find((entry) =>
        existing ? entry.id === id : !config.searches.some((previous) => previous.id === entry.id),
      );
      await ui.refresh();
      onSaved(
        saved?.id,
        saved?.input_enabled === false
          ? 'Search saved and turned off. Originals and history are kept.'
          : saved && !saved.enabled
            ? 'Search saved. The Wallhaven provider is disabled; this search will not rotate.'
            : 'Search saved.',
        origin,
      );
    }
    lock.current = false;
    setWorking(false);
  }
  const firstSearch = config.searches.length === 0 && !id;
  const patchRules = (patch: Partial<SearchFilterDraft>) =>
    setEditor((current) => ({ ...current, draft: { ...current.draft, ...patch } }));
  return (
    <div className="settings-panel source-editor search-editor">
      <form id="search-form" aria-labelledby="search-editor-title" onSubmit={submit} noValidate>
        <header className="search-editor-head">
          <h3 id="search-editor-title" tabIndex={-1}>
            {existing
              ? 'Search settings'
              : missing
                ? 'Save removed search as new'
                : 'Add Wallhaven search'}
          </h3>
          <Button
            className="search-rename"
            aria-expanded={renaming}
            aria-controls="search-name-field"
            onClick={(event) => {
              setRenaming(!renaming);
              if (!renaming) focus.current = 'search-name';
              else event.currentTarget.focus();
            }}
          >
            Rename
          </Button>
        </header>
        <fieldset id="search-fields" disabled={ui.busy || working}>
          <div id="search-name-field" hidden={!renaming}>
            <Field htmlFor="search-name">
              Name <span className="subtext">(optional)</span>
              <TextInput
                id="search-name"
                maxLength={256}
                autoComplete="off"
                placeholder="Use search terms or Wallhaven"
                value={draft.name}
                onChange={(event) => edit('name', event.target.value)}
              />
            </Field>
          </div>
          {firstSearch && (
            <p className="subtext">
              Including this search enables network requests to Wallhaven for safe-for-work results.
            </p>
          )}
          <SearchFilters
            draft={draft}
            inherited={config.rules}
            savedRuleResolution={search?.rule_resolution}
            savedRatio={(search?.effective_rules ?? search?.image_rules ?? config.rules).ratio}
            onChange={patchRules}
            group={panel}
            onGroup={setPanel}
            query={
              <ProviderField
                id="search-query"
                field={wallhavenField('query')}
                value={draft.query}
                optionalLabel
                onChange={(value) => edit('query', String(value))}
              />
            }
            preview={
              existing &&
              search?.enabled &&
              ![
                'query',
                'categories',
                'tags',
                'tag_match',
                'colors',
                'sorting',
                'order',
                'max_pages',
                'top_range',
              ].some((key) => draft[key as keyof Draft] !== baseline[key as keyof Draft]) ? (
                <SourceRules
                  compact
                  prefix="search-filter"
                  provider="wallhaven"
                  inputID={id}
                  revision={revision}
                  draft={draft}
                  inherited={config.rules}
                  disabled={ui.busy || working || ui.blocked}
                  enrolled
                  onChange={patchRules}
                />
              ) : null
            }
          />
          {!existing && (
            <label className="search-check">
              <input
                id="search-enabled"
                type="checkbox"
                checked={draft.enabled}
                onChange={(event) => edit('enabled', event.target.checked)}
              />
              Include in rotation
            </label>
          )}
        </fieldset>
        {missing && (
          <p className="subtext">
            This search was removed elsewhere. Your draft is kept; save it as a new search or
            cancel.
          </p>
        )}
        <Notice id="search-notice" hidden={!notice}>
          {notice}
        </Notice>
        <footer className="search-editor-footer">
          <div className="search-sort">
            <fieldset disabled={ui.busy || working} className="search-sort-field">
              <ProviderField
                id="search-sorting"
                field={wallhavenField('sorting')}
                value={draft.sorting}
                onChange={(value) => edit('sorting', String(value))}
              />
            </fieldset>
            <Button
              className="search-advanced-trigger"
              disabled={ui.busy || working}
              aria-expanded={options}
              aria-controls="search-advanced"
              onClick={() => setOptions(!options)}
            >
              More settings
            </Button>
          </div>
          <FormActions className="source-editor-actions search-editor-actions">
            <Button
              id="search-cancel"
              type="button"
              disabled={ui.busy || working}
              onClick={onCancel}
            >
              Cancel
            </Button>
            <Button
              id="search-save"
              pending={working}
              className="primary"
              type="submit"
              disabled={!canSave || Boolean(existing && !changed)}
            >
              {firstSearch
                ? draft.enabled
                  ? 'Enable Wallhaven and add source'
                  : 'Add disabled source'
                : missing
                  ? 'Save as new search'
                  : id
                    ? 'Save'
                    : 'Add source'}
            </Button>
          </FormActions>
        </footer>
        <fieldset
          id="search-advanced"
          className="search-advanced rules-grid"
          disabled={ui.busy || working}
          hidden={!options}
        >
          <ProviderFields
            group={wallhavenGroup('advanced')}
            values={{ ...draft }}
            render={(field: EditorField) => {
              const key = field.key as 'order' | 'top_range' | 'max_pages' | 'refresh_interval';
              const ids = {
                order: 'search-order',
                top_range: 'search-toprange',
                max_pages: 'search-pages',
                refresh_interval: 'search-refresh',
              };
              return (
                <ProviderField
                  key={key}
                  id={ids[key]}
                  field={field}
                  value={draft[key]}
                  error={
                    !validation
                      ? undefined
                      : key === 'max_pages'
                        ? pagesError
                        : key === 'refresh_interval'
                          ? refreshError
                          : undefined
                  }
                  onChange={(value) => edit(key, String(value))}
                />
              );
            }}
          />
        </fieldset>
      </form>
    </div>
  );
}
