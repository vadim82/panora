import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState } from 'react';
import { Button, Field, Notice, TextInput } from '../components/ui';
import { ActionMenu } from '../components/ui/ActionMenu';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { usePanora } from '../client';
import { StartWallpapers, useOnboarding } from '../onboarding/Onboarding';
import { AddSourceMenu } from './AddSourceMenu';
import { OmarchyThemeEditor } from './OmarchyThemeEditor';
import { FolderEditor } from './FolderEditor';
import { SearchEditor } from './SearchEditor';
import { ProviderEditor } from './ProviderEditor';
import { SourceHealth } from './SourceHealth';
import type { SearchDraftStatus } from './WallhavenCatalog';
import { sourceFacts, sourceIdentity, sourceKey, sourceProvider } from './sourceFacts';
import type { Source } from './sourceFacts';
import type { EditorCallbacks } from './editorCallbacks';
import { canRestoreSourceFocus } from './useSourceEditorFocus';

const filters = ['All', 'In rotation', 'Off', 'Needs attention'] as const;
type Filter = (typeof filters)[number];
type View = {
  kind: Source['kind'];
  source?: Source;
  providerID?: string;
  picked?: { path: string; name?: string };
};
type Toggle = { key: string; revision: string; enabled: boolean };
type SaveFailure = Toggle & { message: string };

const rowID = (key: string) => `source-row-${key}`;
const editorID = (key: string) => `source-editor-${key}`;
const menuID = (key: string) => `source-actions-${key}`;
const filterID = (filter: Filter) => `source-filter-${filters.indexOf(filter)}`;

export function SourcesScreen() {
  const ui = usePanora();
  const onboarding = useOnboarding();
  const config = ui.configuration;
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<Filter>('All');
  const [view, setView] = useState<View | null>(null);
  const [searchDraft, setSearchDraft] = useState<SearchDraftStatus | null>(null);
  const [pending, setPending] = useState<Toggle | null>(null);
  const [retry, setRetry] = useState<SaveFailure | null>(null);
  const [removing, setRemoving] = useState<Source | null>(null);
  const [discard, setDiscard] = useState<{ proceed(): void } | null>(null);
  const [notice, setNotice] = useState('');
  const [picking, setPicking] = useState(false);
  const dirty = useRef(false);
  const operation = useRef(false);
  const focus = useRef<string[]>([]);
  const restoreDiscard = useRef<string[]>([]);

  const changed =
    config && ui.liveConfiguration && config.revision !== ui.liveConfiguration.revision;
  const lastConfirmed = !ui.connected || ui.blocked || Boolean(changed);
  const mutable = Boolean(config && !lastConfirmed && !ui.busy && !picking && !pending);
  const descriptions = config?.provider_editors || [];
  const sources: Source[] = [
    ...(config?.folders.map((input) => ({ kind: 'local' as const, input })) ?? []),
    ...(config?.searches.map((input) => ({ kind: 'wallhaven' as const, input })) ?? []),
    ...(config?.omarchy_theme ? [{ kind: 'omarchy' as const, input: config.omarchy_theme }] : []),
    ...(config?.inputs?.map((input) => {
      const provider = descriptions.find(({ id }) => id === input.provider_id);
      return {
        kind: 'external' as const,
        input,
        providerName: provider?.name || input.provider_id,
        editorError: provider?.editor
          ? undefined
          : provider?.editor_error || 'Provider editor unavailable',
      };
    }) ?? []),
  ];
  const opened = view?.source;
  const openedKey = opened && sourceKey(opened);
  const missing = opened && !sources.some((source) => sourceKey(source) === openedKey);
  // Keep the editor mounted if a settings reload removes its saved input.
  const rows = missing ? [...sources, opened] : sources;
  const identities = rows.map((source) =>
    sourceIdentity(source, ui.snapshot?.status?.omarchy_theme_name),
  );
  const labelCounts = new Map<string, number>();
  for (const { label } of identities) labelCounts.set(label, (labelCounts.get(label) || 0) + 1);
  const terms = query.trim().toLowerCase().split(/\s+/);
  const entries = rows.map((source, index) => {
    const identity = identities[index];
    return {
      source,
      key: sourceKey(source),
      ...identity,
      label:
        labelCounts.get(identity.label)! > 1
          ? `${identity.label} (ID ${source.input.id})`
          : identity.label,
      facts: sourceFacts(source, ui.snapshot, lastConfirmed),
    };
  });
  const matches = entries
    .slice(0, sources.length)
    .filter(({ source, facts, name, type, context }) => {
      const searchable = `${name} ${type} ${context}`.toLowerCase();
      return (
        terms.every((term) => searchable.includes(term)) &&
        (filter === 'All' ||
          (filter === 'In rotation' && source.input.enabled) ||
          (filter === 'Off' && !source.input.input_enabled) ||
          (filter === 'Needs attention' && facts.attention))
      );
    });
  // Searching or filtering keeps an open editor and its row available.
  const matchingRows = new Set(matches);
  const shown = entries.filter((entry) => entry.key === openedKey || matchingRows.has(entry));

  function restoreFocus(targets: string[]) {
    if (ui.getState().page !== 'providers') return false;
    return targets.some((id) => {
      const target = document.getElementById(id);
      if (!target || target.closest('[hidden]')) return false;
      target.focus();
      return document.activeElement === target;
    });
  }

  useLayoutEffect(() => {
    if (ui.page !== 'providers' || restoreFocus(focus.current)) focus.current = [];
  });

  function select(next: View | null, ...targets: string[]) {
    dirty.current = false;
    focus.current = targets;
    setView(next);
  }

  function leave(proceed: () => void) {
    if (operation.current || ui.getState().busy) return;
    if (view && dirty.current) setDiscard({ proceed });
    else proceed();
  }

  function back() {
    leave(() => select(null, openedKey ? rowID(openedKey) : 'source-add', filterID(filter)));
  }

  function openEditor(source: Source) {
    select(
      { kind: source.kind, source, providerID: sourceProvider(source) },
      source.kind === 'local'
        ? 'folder-editor-title'
        : source.kind === 'wallhaven'
          ? 'search-query'
          : source.kind === 'omarchy'
            ? 'theme-editor-title'
            : 'provider-editor-title',
    );
  }

  async function addOmarchyTheme() {
    if (!mutable || operation.current || !config || config.omarchy_theme) return;
    operation.current = true;
    const result = await ui.request('EditOmarchyTheme', ['add', { revision: config.revision }], {
      mutation: true,
    });
    const saved = result.configuration?.omarchy_theme;
    if (saved) {
      await ui.refresh();
      openEditor({ kind: 'omarchy', input: saved });
    } else {
      setNotice(result.error || 'The Omarchy theme source could not be saved.');
    }
    operation.current = false;
  }

  function addSource(kind: Source['kind'], providerID?: string) {
    if (kind === 'omarchy') {
      leave(() => void addOmarchyTheme());
      return;
    }
    const field =
      kind === 'local'
        ? 'folder-path'
        : kind === 'wallhaven'
          ? 'search-query'
          : 'provider-editor-title';
    if (view?.kind === kind && view.providerID === providerID && !view.source)
      restoreFocus([field]);
    else leave(() => select({ kind, providerID }, field));
  }

  async function chooseFolder(origin = document.activeElement) {
    if (!mutable || operation.current) return;
    operation.current = true;
    setPicking(true);
    setNotice('');
    const result = await ui.request('PickFolder');
    operation.current = false;
    if (result.path) {
      select(
        { kind: 'local', picked: { path: result.path, name: result.name } },
        ...(canRestoreSourceFocus(origin) ? ['folder-editor-title'] : []),
      );
    } else {
      if (result.error) setNotice(result.error);
      if (canRestoreSourceFocus(origin)) focus.current = ['source-add'];
    }
    setPicking(false);
  }

  const handleIntent = useEffectEvent(() => {
    const intent = onboarding.sourceIntent;
    const latest = ui.getState();
    if (!intent || !mutable || latest.busy || latest.page !== 'providers') return;
    onboarding.consumeSourceIntent();
    const kind = intent === 'folder' ? 'local' : 'wallhaven';
    if (view?.kind === kind) {
      focus.current = [kind === 'local' ? 'folder-editor-title' : 'search-query'];
      return;
    }
    leave(() => {
      if (intent === 'folder') void chooseFolder();
      else select({ kind }, 'search-query');
    });
  });
  useEffect(() => {
    if (onboarding.sourceIntent && ui.page === 'providers' && mutable) handleIntent();
  }, [onboarding.sourceIntent, ui.page, mutable]);

  async function reload(origin: Element) {
    if (ui.getState().busy) return;
    setRetry(null);
    const result = await ui.reload();
    requestAnimationFrame(() => {
      if (canRestoreSourceFocus(origin))
        restoreFocus(['source-reload', filterID(filter), 'providers-title']);
    });
    setNotice(
      result
        ? 'Saved sources reviewed. Any open draft is kept.'
        : 'Saved sources could not be loaded. Try Reload settings again.',
    );
  }

  async function toggle(source: Source, desired: boolean, origin: HTMLElement) {
    if (!mutable || operation.current || !config) return;
    operation.current = true;
    const change = { key: sourceKey(source), revision: config.revision, enabled: desired };
    setPending(change);
    setRetry(null);
    setNotice('');
    const patch = { revision: change.revision, id: source.input.id, enabled: desired };
    const result =
      source.kind === 'local'
        ? await ui.request('UpdateFolder', [patch], { mutation: true })
        : source.kind === 'omarchy'
          ? await ui.request('EditOmarchyTheme', ['update', patch], { mutation: true })
          : source.kind === 'wallhaven'
            ? await ui.request('UpdateSearch', [patch], { mutation: true })
            : await ui.request(
                'UpdateProviderInput',
                [{ ...patch, provider_id: source.input.provider_id }],
                { mutation: true },
              );
    if (result.configuration) {
      const saved =
        source.kind === 'omarchy'
          ? result.configuration.omarchy_theme
          : source.kind === 'external'
            ? result.configuration.inputs?.find(
                ({ id, provider_id }) =>
                  id === source.input.id && provider_id === sourceProvider(source),
              )
            : (source.kind === 'local'
                ? result.configuration.folders
                : result.configuration.searches
              ).find(({ id }) => id === source.input.id);
      setNotice(
        saved?.enabled
          ? 'Source is included in rotation.'
          : saved?.input_enabled
            ? 'Source is enabled. Its provider is disabled.'
            : 'Source is out of rotation. The displayed wallpaper stays in place.',
      );
      await ui.refresh();
    } else if (ui.getState().blocked || !ui.getState().connected) {
      setNotice(
        result.error ||
          'The change could not be confirmed. Reload settings to check the saved state.',
      );
    } else {
      const message = result.error || 'The change could not be saved.';
      setRetry({ ...change, message });
      setNotice(
        message + ' The saved rotation choice is unchanged. Use Retry in the source actions.',
      );
    }
    if (canRestoreSourceFocus(origin)) focus.current = [menuID(change.key), filterID(filter)];
    operation.current = false;
    setPending(null);
  }

  async function remove(source: Source, origin: Element | null) {
    if (!mutable || operation.current || !config) return;
    operation.current = true;
    const kind = source.kind;
    const patch = { revision: config.revision, id: source.input.id };
    const result =
      kind === 'local'
        ? await ui.request('RemoveFolder', [patch], { mutation: true })
        : kind === 'omarchy'
          ? await ui.request('EditOmarchyTheme', ['remove', patch], { mutation: true })
          : kind === 'wallhaven'
            ? await ui.request('RemoveSearch', [patch], { mutation: true })
            : await ui.request(
                'RemoveProviderInput',
                [{ ...patch, provider_id: source.input.provider_id }],
                { mutation: true },
              );
    const restore = canRestoreSourceFocus(origin);
    if (result.configuration) {
      await ui.refresh();
      if (view?.source && sourceKey(view.source) === sourceKey(source))
        select(null, ...(restore && canRestoreSourceFocus(origin) ? [filterID(filter)] : []));
      else if (restore && canRestoreSourceFocus(origin)) focus.current = [filterID(filter)];
      setNotice('Source removed from rotation. Originals and history are kept.');
    } else {
      if (restore) focus.current = [menuID(sourceKey(source)), filterID(filter)];
      setNotice(result.error || 'The source could not be removed.');
    }
    operation.current = false;
  }

  function reviewSource(kind: string, id: string) {
    const source = sources.find((item) => sourceProvider(item) === kind && item.input.id === id);
    if (!source) {
      setNotice('Reload settings to review this source.');
      return;
    }
    if (sourceKey(source) === openedKey) restoreFocus([rowID(openedKey)]);
    else leave(() => openEditor(source));
  }

  const editorCallbacks: EditorCallbacks = {
    onDirty(value) {
      dirty.current = value;
    },
    onCancel() {
      const source = view?.source;
      if (source && entries.some(({ key }) => key === sourceKey(source)))
        select(null, rowID(sourceKey(source)), filterID(filter));
      else select(null, 'source-add', filterID(filter));
    },
    onSaved(id, message, origin) {
      const restore = canRestoreSourceFocus(origin);
      if (id && view)
        select(
          null,
          ...(restore ? [rowID(`${view.providerID || view.kind}-${id}`), filterID(filter)] : []),
        );
      else select(null, ...(restore ? ['source-add'] : []));
      setNotice(message);
    },
  };

  const editor = view && config && (
    <section
      id={openedKey && editorID(openedKey)}
      className="source-detail"
      aria-label="Source editor"
      onKeyDown={(event) => {
        if (event.key === 'Escape' && !event.defaultPrevented) {
          event.preventDefault();
          back();
        }
      }}
    >
      {view.kind === 'local' ? (
        <FolderEditor
          key={view.source ? sourceKey(view.source) : 'add-local'}
          {...editorCallbacks}
          mutable={mutable}
          folder={view.source?.kind === 'local' ? view.source.input : undefined}
          picked={view.picked}
          onReview={(id, edited) => {
            const existing = sources.find((item) => item.kind === 'local' && item.input.id === id);
            if (!existing) return;
            const review = () => openEditor(existing);
            if (edited) leave(review);
            else review();
          }}
        />
      ) : view.source?.kind === 'omarchy' ? (
        <OmarchyThemeEditor
          key={sourceKey(view.source)}
          {...editorCallbacks}
          mutable={mutable}
          theme={view.source.input}
        />
      ) : view.kind === 'wallhaven' ? (
        <SearchEditor
          key={view.source ? sourceKey(view.source) : 'add-wallhaven'}
          {...editorCallbacks}
          mutable={mutable}
          search={view.source?.kind === 'wallhaven' ? view.source.input : undefined}
          onDraftStatus={setSearchDraft}
        />
      ) : (
        <ProviderEditor
          key={view.source ? sourceKey(view.source) : `add-${view.providerID}`}
          {...editorCallbacks}
          mutable={mutable}
          providerID={view.providerID!}
          description={descriptions.find(({ id }) => id === view.providerID)}
          input={view.source?.kind === 'external' ? view.source.input : undefined}
        />
      )}
    </section>
  );

  function closeActions(trigger: HTMLElement) {
    const menu = trigger.closest('details');
    const summary = menu?.querySelector('summary');
    if (menu) menu.open = false;
    summary?.focus();
    return summary ?? trigger;
  }

  return (
    <>
      <div className="heading sources-heading">
        <h1 id="providers-title" tabIndex={-1}>
          Sources
        </h1>
        <AddSourceMenu
          disabled={!mutable || ui.page !== 'providers'}
          onChoose={addSource}
          canAddOmarchy={Boolean(
            ui.liveConfiguration?.omarchy_theme_available && !config?.omarchy_theme,
          )}
          external={descriptions}
        />
      </div>
      <div className="sources-manager">
        <p className="sources-count subtext" role="status">
          {config
            ? `${lastConfirmed || pending ? 'Last confirmed · ' : ''}${matches.length}${matches.length !== sources.length ? ` of ${sources.length}` : ''} ${sources.length === 1 ? 'source' : 'sources'}${openedKey && !missing && shown.length > matches.length ? ' · Open editor shown' : ''}`
            : ui.connected
              ? 'Loading saved sources…'
              : 'Reconnect to load saved sources.'}
        </p>
        {!ui.settingsError && (ui.blocked || changed) && (
          <div className="source-review-notice">
            <p role="status">
              {ui.blocked
                ? 'Reload settings before making another change.'
                : 'Saved settings changed elsewhere. Reload to review them.'}
            </p>
            <Button
              id="source-reload"
              className="outline"
              disabled={ui.busy}
              onClick={(event) => void reload(event.currentTarget)}
            >
              Reload settings
            </Button>
          </div>
        )}
        <Notice className="source-operation-notice" hidden={!notice}>
          {notice}
        </Notice>
        <StartWallpapers />
        <div className="source-tools">
          <Field htmlFor="source-search">
            <span className="source-visually-hidden">Find a source or type</span>
            <TextInput
              id="source-search"
              value={query}
              placeholder="Find a source or type…"
              onChange={(event) => setQuery(event.target.value)}
            />
          </Field>
          <div className="source-filters" role="group" aria-label="Filter sources">
            {filters.map((item) => (
              <Button
                key={item}
                id={filterID(item)}
                aria-pressed={item === filter}
                onClick={() => setFilter(item)}
              >
                {item}
              </Button>
            ))}
          </div>
        </div>
        {view && !opened && editor}
        {!shown.length && config && (
          <p className="source-empty">
            {!sources.length
              ? 'No saved sources. Add a folder or a Wallhaven search.'
              : 'No sources match this search and filter.'}
          </p>
        )}
        {shown.length > 0 && (
          <ul className="source-list" aria-label="Saved sources">
            {shown.map(({ source, key, name, type, context, label, facts }) => {
              const active = openedKey === key;
              const removed = active && missing;
              const paused = !removed && !source.input.input_enabled;
              const saving = pending?.key === key;
              const failed =
                retry?.key === key && retry.revision === config?.revision && !lastConfirmed
                  ? retry
                  : null;
              const details = [
                removed && 'Removed from saved sources',
                facts.attention && facts.check,
                facts.origin,
                source.kind === 'omarchy' && source.input.enabled && !removed && 'In rotation',
                saving && 'Saving…',
              ]
                .filter(Boolean)
                .join(' · ');
              const description = [
                paused && `source-rotation-${key}`,
                details && `source-facts-${key}`,
                !removed && `source-image-rules-${key}`,
              ]
                .filter(Boolean)
                .join(' ');
              return (
                <li key={key} className="source-row">
                  <div className="source-summary" data-expanded={active} data-paused={paused}>
                    <Button
                      id={rowID(key)}
                      className="source-row-select"
                      aria-label={`Edit ${label}`}
                      aria-describedby={description || undefined}
                      aria-expanded={active}
                      aria-controls={active ? editorID(key) : undefined}
                      onClick={() => {
                        if (active) back();
                        else leave(() => openEditor(source));
                      }}
                    >
                      <span className="source-kind-mark" aria-hidden="true" data-kind={source.kind}>
                        {paused && <span className="source-pause-mark" />}
                      </span>
                      <span className="source-row-copy">
                        <span className="source-row-title">
                          <strong title={name}>{name}</strong>
                          {paused && <span id={`source-rotation-${key}`}>Out of rotation</span>}
                          {!removed && (
                            <span id={`source-image-rules-${key}`}>
                              {source.input.image_rules
                                ? 'Source-specific image rules'
                                : 'Shared image rules'}
                            </span>
                          )}
                        </span>
                        <span className="source-context" title={context}>
                          {type}
                          {context && ` · ${context}`}
                        </span>
                        {details && (
                          <span
                            id={`source-facts-${key}`}
                            className="source-row-facts"
                            data-attention={facts.attention}
                          >
                            {details}
                          </span>
                        )}
                      </span>
                      <span className="source-row-chevron" aria-hidden="true" />
                    </Button>
                    {!removed && (
                      <ActionMenu
                        id={menuID(key)}
                        label={`Actions for ${label}`}
                        className="source-row-actions"
                      >
                        <Button
                          disabled={!mutable}
                          onClick={(event) =>
                            void toggle(source, paused, closeActions(event.currentTarget))
                          }
                        >
                          {paused ? 'Include in rotation' : 'Take out of rotation'}
                        </Button>
                        {failed && (
                          <Button
                            disabled={!mutable}
                            aria-label={`Retry saving ${label}`}
                            onClick={(event) =>
                              void toggle(source, failed.enabled, closeActions(event.currentTarget))
                            }
                          >
                            Retry
                          </Button>
                        )}
                        <Button
                          className="source-remove-action"
                          disabled={!mutable}
                          onClick={(event) => {
                            closeActions(event.currentTarget);
                            setRemoving(source);
                          }}
                        >
                          Remove source…
                        </Button>
                      </ActionMenu>
                    )}
                  </div>
                  {active && editor}
                </li>
              );
            })}
          </ul>
        )}
        <SourceHealth onReview={reviewSource} searchDraft={searchDraft} />
      </div>
      <ConfirmDialog
        id="source-discard"
        open={Boolean(discard)}
        active={ui.page === 'providers'}
        title="Discard unsaved changes?"
        description="Keep editing to save your changes, or discard the draft to continue."
        confirmLabel="Discard changes"
        cancelLabel="Keep editing"
        onCancel={() => setDiscard(null)}
        onConfirm={() => {
          setDiscard(null);
          discard?.proceed();
          restoreDiscard.current = focus.current;
        }}
        onConfirmClose={() => {
          if (document.activeElement === document.body) restoreFocus(restoreDiscard.current);
          restoreDiscard.current = [];
        }}
      />
      <ConfirmDialog
        id="source-remove-confirm"
        open={Boolean(removing)}
        active={ui.page === 'providers'}
        title={removing ? `Remove “${sourceIdentity(removing).name}”?` : 'Remove source?'}
        description={
          removing && sourceKey(removing) === openedKey && dirty.current
            ? 'This source and its unsaved edits will be removed. Originals and history are kept.'
            : 'This source will stop supplying wallpapers. Originals and history are kept.'
        }
        confirmLabel={
          removing?.kind === 'wallhaven'
            ? 'Remove search'
            : removing?.kind === 'local'
              ? 'Remove folder'
              : 'Remove source'
        }
        cancelLabel={
          removing?.kind === 'wallhaven'
            ? 'Keep search'
            : removing?.kind === 'local'
              ? 'Keep folder'
              : 'Keep source'
        }
        returnFocusID={removing ? menuID(sourceKey(removing)) : undefined}
        disabled={!mutable}
        onCancel={() => setRemoving(null)}
        onConfirm={() => {
          const origin = document.activeElement;
          const target = removing && entries.find(({ key }) => key === sourceKey(removing));
          setRemoving(null);
          if (target) void remove(target.source, origin);
        }}
      />
    </>
  );
}
