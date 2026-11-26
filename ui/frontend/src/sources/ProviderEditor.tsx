import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { bridge, usePanora } from '../client';
import { Button, Field, FormActions, Notice, TextInput } from '../components/ui';
import type { ConfiguredInput, ProviderEditorDescription, ProviderInputEdit } from '../types';
import type { EditorCallbacks } from './editorCallbacks';
import { CommonsSearch } from './CommonsSearch';
import { commonsApprovals, commonsID, commonsSearchRequest } from './commonsSearch';
import { ProviderField, ProviderFields } from './ProviderFields';
import { fieldError, fieldVisible, manifestDefaults, manifestFields } from './providerManifest';
import type { EditorManifest, ProviderValues } from './providerManifest';
import {
  preserveSourceRuleEdits,
  SourceRules,
  sourceRuleDraft,
  sourceRuleError,
  sourceRulePatch,
} from './SourceRules';

type Props = EditorCallbacks & {
  description?: ProviderEditorDescription;
  providerID: string;
  input?: ConfiguredInput;
  mutable: boolean;
};

const commonsFeaturedName = 'Featured landscapes';
const commonsSearchName = 'Wikimedia Commons search';
const commonsName = (collection: unknown) =>
  collection === 'search' ? commonsSearchName : commonsFeaturedName;
const same = (left: unknown, right: unknown) => JSON.stringify(left) === JSON.stringify(right);
const changedValues = (draft: ProviderValues, saved: ProviderValues) =>
  Object.keys({ ...saved, ...draft }).some((key) => !same(draft[key], saved[key]));

export function ProviderEditor(props: Props) {
  const manifest = props.description?.editor;
  if (!manifest || manifest.version !== 1) {
    return (
      <div className="settings-panel source-editor">
        <h3 id="provider-editor-title" tabIndex={-1}>
          {props.input?.name || props.description?.name || 'Provider settings'}
        </h3>
        <Notice tone="error">
          {props.description?.editor_error ||
            'Install a compatible version of this provider and restart the service to open its settings.'}
        </Notice>
        <Button onClick={props.onCancel}>Close</Button>
      </div>
    );
  }
  return <ManifestEditor {...props} manifest={manifest} />;
}

function ManifestEditor({
  description,
  providerID,
  input,
  manifest,
  mutable,
  onDirty,
  onCancel,
  onSaved,
}: Props & { manifest: EditorManifest }) {
  const ui = usePanora();
  const config = ui.configuration!;
  const defaults = useMemo(() => manifestDefaults(manifest), [manifest]);
  const isCommons = providerID === commonsID;
  const fields = useMemo(
    () => manifestFields(manifest).filter((field) => !isCommons || field.key === 'collection'),
    [isCommons, manifest],
  );
  const [editor, setEditor] = useState(() => {
    const saved = { ...defaults, ...input?.values };
    const name =
      input?.name || (isCommons ? commonsName(saved.collection) : description?.name) || '';
    const rules = sourceRuleDraft(input || { effective_rules: config.rules }, config.rules);
    return {
      revision: config.revision,
      saved,
      values: saved,
      savedName: name,
      name,
      rules,
      savedRules: rules,
      missing: false,
    };
  });
  const { values, rules, name } = editor;
  const customCommons = isCommons && values.collection === 'search';
  const approved = customCommons ? commonsApprovals(values) : [];
  const [enabled, setEnabled] = useState(true);
  const [suggestions, setSuggestions] = useState(false);
  const [showErrors, setShowErrors] = useState(false);
  const [notice, setNotice] = useState('');
  const [working, setWorking] = useState(false);
  const lock = useRef(false);
  const restore = useRef(false);
  const existing = input && !editor.missing;
  const dirtyValues = changedValues(values, editor.saved);
  const dirty =
    editor.missing ||
    dirtyValues ||
    name !== editor.savedName ||
    !same(rules, editor.savedRules) ||
    (!input && !enabled);

  useEffect(() => {
    setEditor((current) => {
      if (current.revision === config.revision) return current;
      if (!input) return { ...current, revision: config.revision };
      const saved = config.inputs?.find(
        ({ id, provider_id }) => id === input.id && provider_id === providerID,
      );
      if (!saved) return { ...current, revision: config.revision, missing: true };
      const baseline = { ...defaults, ...saved.values };
      const nextValues = { ...baseline };
      for (const key of Object.keys({ ...current.saved, ...current.values })) {
        if (!same(current.values[key], current.saved[key])) nextValues[key] = current.values[key];
      }
      const savedRules = sourceRuleDraft(saved, config.rules);
      return {
        ...current,
        saved: baseline,
        savedRules,
        savedName: saved.name,
        name: current.name === current.savedName ? saved.name : current.name,
        rules: preserveSourceRuleEdits(current.rules, current.savedRules, savedRules),
        values: nextValues,
        missing: false,
        revision: config.revision,
      };
    });
  }, [config, defaults, input, providerID]);
  useLayoutEffect(() => onDirty(dirty), [dirty, onDirty]);
  const lookup = useMemo(
    () =>
      Object.fromEntries(
        fields
          .filter((field) => field.suggest)
          .map(({ key }) => [
            key,
            async (prefix: string) => {
              const result = await bridge('SuggestProviderInput', [
                { provider_id: providerID, field: key, prefix, limit: 10 },
              ]);
              return result.values ?? [];
            },
          ]),
      ),
    [fields, providerID],
  );
  const previewSource = config.inputs?.find(
    ({ id, provider_id }) => id === input?.id && provider_id === providerID,
  );

  useLayoutEffect(() => {
    if (!working && restore.current) {
      restore.current = false;
      if (ui.getState().page === 'providers' && document.activeElement === document.body) {
        document.getElementById('provider-editor-save')?.focus();
      }
    }
  });

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!mutable || lock.current || (existing && !dirty)) return;
    const invalid = fields.find(
      (field) => fieldVisible(field, values) && fieldError(field, values[field.key]),
    );
    const ruleError = sourceRuleError(rules, config.rules);
    if (!name.trim() || invalid || ruleError) {
      setShowErrors(true);
      const target = !name.trim()
        ? 'provider-input-name'
        : invalid
          ? `provider-field-${invalid.key}`
          : `provider-rule-${rules.rule_mode === 'inherit' ? 'rule-mode' : ruleError?.field}`;
      requestAnimationFrame(() => document.getElementById(target)?.focus());
      return;
    }
    let cleaned = { ...values };
    if (customCommons) {
      const { request, error } = commonsSearchRequest(values);
      if (error || !approved.length) {
        setNotice(error || 'Search Commons and approve at least one file.');
        requestAnimationFrame(() => document.getElementById('commons-query')?.focus());
        return;
      }
      cleaned = { collection: 'search', ...request, approved };
    } else if (isCommons) cleaned = { collection: values.collection };
    for (const field of fields) {
      if (field.control === 'integer') {
        const number = cleaned[field.key];
        if (number === '' || number === undefined) delete cleaned[field.key];
        else cleaned[field.key] = Number(number);
      }
    }
    const origin = document.activeElement;
    const patch: ProviderInputEdit = {
      revision: editor.revision,
      provider_id: providerID,
      ...(!existing || name !== editor.savedName ? { name: name.trim() } : {}),
      ...(!existing || dirtyValues ? { values: cleaned } : {}),
      ...sourceRulePatch(rules, editor.savedRules, Boolean(existing)),
      ...(existing ? { id: input.id } : { enabled }),
    };
    lock.current = true;
    setWorking(true);
    setNotice('');
    try {
      const result = existing
        ? await ui.request('UpdateProviderInput', [patch], { mutation: true })
        : await ui.request('AddProviderInput', [patch], { mutation: true });
      if (!result.configuration) {
        setNotice(`${result.error || 'The source could not be saved.'} Your draft is kept.`);
        restore.current = [document.body, origin].includes(document.activeElement);
        return;
      }
      const saved = result.configuration.inputs?.find(
        ({ id, provider_id }) =>
          provider_id === providerID &&
          (existing
            ? id === input.id
            : !config.inputs?.some(
                (previous) => previous.provider_id === providerID && previous.id === id,
              )),
      );
      await ui.refresh();
      onSaved(
        saved?.id,
        saved?.input_enabled === false ? 'Source saved and out of rotation.' : 'Source saved.',
        origin,
      );
    } finally {
      lock.current = false;
      setWorking(false);
    }
  }
  const title = existing
    ? `${description?.name || 'Provider'} settings`
    : editor.missing
      ? 'Save removed source as new'
      : `Add ${description?.name || 'source'}`;

  return (
    <div className="settings-panel source-editor manifest-source-editor">
      <form
        aria-labelledby="provider-editor-title"
        noValidate
        onSubmit={(event) => void save(event)}
      >
        <h3 id="provider-editor-title" tabIndex={-1}>
          {title}
        </h3>
        <fieldset disabled={working || ui.busy}>
          <Field
            htmlFor="provider-input-name"
            error={showErrors && !name.trim() ? 'Enter a source name.' : undefined}
          >
            Name
            <TextInput
              id="provider-input-name"
              maxLength={256}
              value={name}
              onChange={(event) =>
                setEditor((current) => ({ ...current, name: event.target.value }))
              }
            />
          </Field>
          {Object.keys(lookup).length > 0 && (
            <label className="search-check">
              <input
                type="checkbox"
                checked={suggestions}
                onChange={(event) => setSuggestions(event.target.checked)}
              />
              Send matching input prefixes to this provider for suggestions
            </label>
          )}
          {manifest.groups.map((group) => (
            <ProviderFields
              key={group.id}
              group={
                isCommons
                  ? { ...group, fields: group.fields.filter((field) => field.key === 'collection') }
                  : group
              }
              values={values}
              render={(field) => (
                <ProviderField
                  key={field.key}
                  id={`provider-field-${field.key}`}
                  field={field}
                  value={values[field.key]}
                  error={showErrors ? fieldError(field, values[field.key]) : undefined}
                  suggest={
                    suggestions && mutable && ui.page === 'providers'
                      ? lookup[field.key]
                      : undefined
                  }
                  onChange={(value) =>
                    setEditor((current) => ({
                      ...current,
                      name:
                        isCommons &&
                        field.key === 'collection' &&
                        !input &&
                        [commonsFeaturedName, commonsSearchName].includes(current.name)
                          ? commonsName(value)
                          : current.name,
                      values: {
                        ...current.values,
                        [field.key]:
                          field.control === 'integer' && value !== '' ? Number(value) : value,
                      },
                    }))
                  }
                />
              )}
            />
          ))}
          {customCommons && (
            <CommonsSearch
              values={values}
              disabled={!mutable || working}
              onChange={(patch) =>
                setEditor((current) => ({
                  ...current,
                  values: { ...current.values, ...patch },
                }))
              }
            />
          )}
          <SourceRules
            prefix="provider-rule"
            provider={providerID}
            inputID={input?.id || null}
            revision={editor.revision}
            enrolled={Boolean(existing && previewSource?.enabled && !dirtyValues)}
            draft={rules}
            inherited={config.rules}
            disabled={!mutable || working}
            validate={showErrors}
            onChange={(patch) =>
              setEditor((current) => ({ ...current, rules: { ...current.rules, ...patch } }))
            }
          />
          {!existing && (
            <label className="search-check">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(event) => setEnabled(event.target.checked)}
              />
              Include in rotation
            </label>
          )}
        </fieldset>
        {editor.missing && (
          <p className="subtext">The saved source was removed. Your draft is kept.</p>
        )}
        {notice && <Notice tone="error">{notice}</Notice>}
        <FormActions>
          <Button onClick={onCancel} disabled={working || ui.busy}>
            Cancel
          </Button>
          <Button
            id="provider-editor-save"
            type="submit"
            variant="primary"
            pending={working}
            disabled={
              !mutable || Boolean(existing && !dirty) || (customCommons && !approved.length)
            }
          >
            {existing ? 'Save' : editor.missing ? 'Save as new source' : 'Add source'}
          </Button>
        </FormActions>
      </form>
    </div>
  );
}
