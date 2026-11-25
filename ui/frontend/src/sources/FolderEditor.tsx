import { Button, Field, FormActions, Notice, NumberInput, TextInput } from '../components/ui';
import { useLayoutEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { usePanora } from '../client';
import type { ConfiguredFolder, FolderEdit, ImageRules } from '../types';
import { folderName } from './drafts';
import type { EditorCallbacks } from './editorCallbacks';
import { useSourceDraft } from './useSourceDraft';
import { useSourceEditorFocus } from './useSourceEditorFocus';
import {
  SourceRules,
  sourceRuleDraft,
  sourceRuleError,
  sourceRules,
  sourceRuleSummary,
  sourceRulePatch,
} from './SourceRules';
import type { SourceRuleDraft } from './SourceRules';

type Draft = SourceRuleDraft & { path: string; name: string; depth: string; enabled: boolean };
const values = (folder: ConfiguredFolder, inherited: ImageRules): Draft => ({
  ...sourceRuleDraft(folder, inherited),
  path: folder.path,
  name: folder.name === folderName(folder.path) ? '' : folder.name,
  depth: String(folder.effective_max_depth),
  enabled: folder.input_enabled,
});

type Props = EditorCallbacks & {
  folder?: ConfiguredFolder;
  picked?: { path: string; name?: string };
  mutable: boolean;
  onReview(id: string, edited: boolean): void;
};

export function FolderEditor({
  folder,
  picked,
  mutable,
  onDirty,
  onCancel,
  onSaved,
  onReview,
}: Props) {
  const ui = usePanora();
  const config = ui.configuration!;
  const id = folder?.id || undefined;
  const saved = id ? config.folders.find((input) => input.id === id) : undefined;
  const [editor, setEditor] = useSourceDraft(
    config.revision,
    id,
    () => {
      if (folder) return values(folder, config.rules);
      const path = picked?.path || '';
      const name = picked?.name || '';
      return {
        ...sourceRuleDraft(),
        path,
        name: name === folderName(path) ? '' : name,
        depth: String(config.rules.max_depth),
        enabled: true,
      };
    },
    saved,
    config.rules,
    values,
  );
  const [notice, setNotice] = useState(''),
    [working, setWorking] = useState(false),
    [options, setOptions] = useState(false),
    [validation, setValidation] = useState(0);
  const lock = useRef(false);
  const { focus, restoreAfter } = useSourceEditorFocus(ui.getState);
  const canSave = mutable && !working;
  const { draft, baseline, missing, revision } = editor;
  const pathError = !draft.path.startsWith('/')
    ? 'Enter an absolute folder path starting with /.'
    : undefined;
  const depth = Number(draft.depth);
  const depthError =
    draft.depth === '' || !Number.isInteger(depth) || depth < 0 || depth > 2147483647
      ? 'Use a whole subfolder depth from 0 to 2147483647.'
      : undefined;
  const edited = (Object.keys(draft) as (keyof Draft)[]).some(
    (key) => draft[key] !== baseline[key],
  );
  const dirty = missing || edited || Boolean(!id && picked?.path);
  const hasChanges = !id || dirty;
  const editingName = saved?.name || baseline.name || folderName(baseline.path);
  const existingFolder =
    !id || missing ? config.folders.find((folder) => folder.path === draft.path) : undefined;
  const alreadySaved = Boolean(existingFolder);
  useLayoutEffect(() => {
    onDirty(dirty);
  }, [dirty, onDirty]);
  function edit<K extends keyof Draft>(field: K, value: Draft[K]) {
    setEditor((current) => ({ ...current, draft: { ...current.draft, [field]: value } }));
  }
  async function pick() {
    if (lock.current || !canSave) return;
    const origin = document.activeElement;
    lock.current = true;
    setWorking(true);
    const result = await ui.request('PickFolder');
    lock.current = false;
    setWorking(false);
    if (result.path) {
      edit('path', result.path);
      setNotice('');
    } else if (result.error) setNotice(result.error);
    restoreAfter('change-folder', origin);
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSave || alreadySaved || !hasChanges) return;
    const origin = document.activeElement;
    setValidation((value) => value + 1);
    if (pathError || depthError) {
      if (!pathError) setOptions(true);
      setNotice('Use an absolute folder path and a valid subfolder depth. Your edits are kept.');
      focus.current = pathError ? 'folder-path' : 'folder-depth';
      return;
    }
    const ruleError = sourceRuleError(draft, config.rules);
    if (ruleError) {
      setOptions(true);
      setNotice(ruleError.message + ' Your edits are kept.');
      focus.current = `folder-filter-${draft.rule_mode === 'inherit' ? 'rule-mode' : ruleError.field}`;
      return;
    }
    const existing = id && !missing;
    const patch: FolderEdit = {
      revision,
      ...(existing ? { id } : {}),
    };
    if (!existing || draft.path !== baseline.path) patch.path = draft.path;
    if (!existing || draft.name !== baseline.name) patch.name = draft.name.trim();
    if (!existing || draft.depth !== baseline.depth) patch.max_depth = depth;
    if (!existing || draft.enabled !== baseline.enabled) patch.enabled = draft.enabled;
    Object.assign(patch, sourceRulePatch(draft, baseline, Boolean(existing)));
    if (lock.current) return;
    lock.current = true;
    setWorking(true);
    const result = existing
      ? await ui.request('UpdateFolder', [patch], { mutation: true })
      : await ui.request('AddFolderSettings', [patch], { mutation: true });
    if (!result.configuration) {
      setNotice((result.error || 'The save result is unknown.') + ' Your edits are kept.');
      restoreAfter('add-folder', origin);
    } else {
      const saved = result.configuration.folders.find((entry) =>
        existing ? entry.id === id : !config.folders.some((previous) => previous.id === entry.id),
      );
      const duplicate = !existing && !saved;
      await ui.refresh();
      onSaved(
        saved?.id,
        duplicate
          ? 'This folder is already saved. No new source was added.'
          : saved?.input_enabled === false
            ? 'Folder saved and turned off. Originals and history are kept.'
            : saved && !saved.enabled
              ? 'Folder saved. The local provider is disabled, so it will not rotate.'
              : 'Folder saved.',
        origin,
      );
    }
    lock.current = false;
    setWorking(false);
  }
  return (
    <div className="folder-test source-editor">
      <form
        id="folder-form"
        aria-labelledby="folder-editor-title"
        onSubmit={submit}
        onInvalidCapture={(event) => {
          event.preventDefault();
          setValidation((value) => value + 1);
          focus.current ||= (event.target as HTMLElement).id;
          if ((event.target as HTMLElement).closest('#folder-options')) setOptions(true);
        }}
      >
        <h3 id="folder-editor-title" tabIndex={-1}>
          {missing ? 'Save folder as new' : id ? `Edit folder: ${editingName}` : 'Add folder'}
        </h3>
        <p id="folder-draft" className="subtext">
          {dirty ? 'Unsaved changes' : ''}
        </p>
        <fieldset id="folder-fields" disabled={ui.busy || working}>
          <div className="folder-location">
            <Field htmlFor="folder-path" error={validation ? pathError : undefined}>
              Folder path
              <TextInput
                id="folder-path"
                maxLength={4096}
                value={draft.path}
                onChange={(event) => edit('path', event.target.value)}
              />
            </Field>
            <Button
              id="change-folder"
              className="outline"
              disabled={!canSave}
              onClick={() => void pick()}
            >
              Browse…
            </Button>
          </div>
          <label className="checkbox-field">
            <input
              id="folder-enabled"
              type="checkbox"
              checked={draft.enabled}
              onChange={(event) => edit('enabled', event.target.checked)}
            />{' '}
            Include in rotation
          </label>
          <details
            id="folder-options"
            open={options}
            onToggle={(event) => setOptions(event.currentTarget.open)}
          >
            <summary>
              Folder options
              {draft.rule_mode === 'custom' &&
                (draft.min_width !== '1' || draft.min_height !== '1' || draft.ratio !== 'any') && (
                  <span className="source-filter-summary">
                    {sourceRuleSummary({ image_rules: sourceRules(draft) }, config.rules)}
                  </span>
                )}
            </summary>
            <SourceRules
              prefix="folder-filter"
              validate={validation > 0}
              provider="local"
              inputID={missing ? null : (id ?? null)}
              revision={revision}
              draft={draft}
              inherited={{ ...config.rules, max_depth: depth }}
              disabled={ui.busy || working || ui.blocked}
              enrolled={Boolean(
                !missing &&
                saved?.enabled &&
                draft.enabled &&
                draft.path === baseline.path &&
                draft.depth === baseline.depth,
              )}
              onChange={(patch) =>
                setEditor((current) => ({ ...current, draft: { ...current.draft, ...patch } }))
              }
            />
            <div className="rules-grid">
              <Field htmlFor="folder-label">
                Name (optional)
                <TextInput
                  id="folder-label"

                  maxLength={256}
                  placeholder="Use folder name"
                  value={draft.name}
                  onChange={(event) => edit('name', event.target.value)}
                />
              </Field>
              <Field htmlFor="folder-depth" error={validation ? depthError : undefined}>
                Subfolder depth
                <NumberInput
                  id="folder-depth"

                  min="0"
                  max="2147483647"
                  step="1"
                  required
                  value={draft.depth}
                  onChange={(event) => edit('depth', event.target.value)}
                />
              </Field>
            </div>
            <p className="subtext">Subfolder depth 0 uses this folder only.</p>
          </details>
        </fieldset>
        {missing && (
          <p className="subtext">
            This folder was removed elsewhere. Your draft is kept; save it as a new folder or
            cancel.
          </p>
        )}
        {existingFolder && (
          <p className="subtext">
            This folder is already saved as “{existingFolder.name}”.{' '}
            {!existingFolder.input_enabled
              ? 'It is turned off. '
              : !existingFolder.enabled
                ? 'Its provider is disabled. '
                : ''}
            Review the saved folder to change its settings.
          </p>
        )}
        <Notice id="folder-notice" hidden={!notice}>
          {notice}
        </Notice>
        <FormActions className="source-editor-actions">
          {existingFolder && (
            <Button
              id="review-existing-folder"
              className="outline"
              type="button"
              disabled={ui.busy || working}
              onClick={() =>
                onReview(
                  existingFolder.id,
                  (Object.keys(draft) as (keyof Draft)[]).some(
                    (key) => key !== 'path' && draft[key] !== baseline[key],
                  ),
                )
              }
            >
              Review saved folder
            </Button>
          )}
          <Button
            id="add-folder"
            pending={working}
            className="primary"
            type="submit"
            disabled={!canSave || !draft.path || alreadySaved || !hasChanges}
          >
            {alreadySaved
              ? 'Already saved'
              : missing
                ? 'Save as new folder'
                : id
                  ? 'Save'
                  : 'Add source'}
          </Button>
          <Button id="cancel-folder" type="button" disabled={ui.busy || working} onClick={onCancel}>
            Cancel
          </Button>
        </FormActions>
      </form>
    </div>
  );
}
