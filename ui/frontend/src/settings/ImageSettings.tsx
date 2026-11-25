import {
  Button,
  Field,
  FormActions,
  Notice,
  NumberInput,
  Select,
  TextInput,
} from '../components/ui';
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import type { PropsWithChildren, FormEvent } from 'react';
import { usePanora } from '../client';
import { useAcceptedPreview } from '../now/useAcceptedPreview';
import type {
  Capabilities,
  Configuration,
  PreferenceEdit,
  Preferences,
  RulePreview,
} from '../types';
import { fitLabel, restoreFormFocus, revealInvalid } from './forms';

const presets = ['16:9', '16:10', '21:9', '32:9', '4:3', '3:2', '1:1', '9:16'];
const minimumPresets = [
  '1x1',
  '1280x720',
  '1920x1080',
  '2560x1440',
  '3440x1440',
  '3840x2160',
  '5120x1440',
];
const preferenceLabels: Record<keyof Preferences, string> = {
  backend: 'Desktop integration',
  fit: 'Presentation',
  upscale: 'Enlarge smaller images',
  interval: 'Rotation interval',
  rotation_mode: 'Rotation mode',
  cache_max_images: 'Cached image limit',
  cache_max_bytes: 'Cache byte budget',
};
interface Draft {
  min_width: string;
  min_height: string;
  ratio: string;
  ratio_presets: string[];
  backend: string;
  fit: string;
  upscale: boolean;
}
interface Editor {
  baseline: Configuration;
  draft: Draft;
  filtersEnabled: boolean;
  customMinimum: boolean;
}
type RuleDraft = Pick<Draft, 'min_width' | 'min_height' | 'ratio' | 'ratio_presets'>;
const ruleKeys = ['min_width', 'min_height', 'ratio', 'ratio_presets'] as const;
function unrestricted(draft: Draft) {
  return (
    Number(draft.min_width) === 1 &&
    Number(draft.min_height) === 1 &&
    draft.ratio === 'any' &&
    draft.ratio_presets.length === 0
  );
}
function saved(config: Configuration): Draft {
  return {
    min_width: String(config.rules.min_width),
    min_height: String(config.rules.min_height),
    ratio: config.rules.ratio,
    ratio_presets: config.rules.ratio_presets || [],
    backend: config.saved.backend,
    fit: config.saved.fit,
    upscale: config.saved.upscale,
  };
}
function changed(editor: Editor): (keyof Draft)[] {
  const before = saved(editor.baseline);
  return (Object.keys(before) as (keyof Draft)[]).filter(
    (key) => JSON.stringify(editor.draft[key]) !== JSON.stringify(before[key]),
  );
}
function adopt(previous: Editor | null, config: Configuration, preserve = true): Editor {
  const draft = saved(config);
  if (previous && preserve) {
    const edited = changed(previous);
    for (const key of edited) Object.assign(draft, { [key]: previous.draft[key] });
    // When either ratio field changes, preserve the pair.
    if (edited.includes('ratio') || edited.includes('ratio_presets')) {
      draft.ratio = previous.draft.ratio;
      draft.ratio_presets = [...previous.draft.ratio_presets];
    }
    if (ruleKeys.some((key) => edited.includes(key)))
      return { ...previous, baseline: config, draft };
  }
  return { baseline: config, draft, filtersEnabled: !unrestricted(draft), customMinimum: false };
}
interface ImageContext {
  editor: Editor | null;
  hasUnsaved: boolean;
  capabilities: Capabilities | null;
  readCapabilities(): Promise<Capabilities | null>;
  backend: string;
  setBackend(value: string): void;
  setRatioMode(value: string): void;
  setMinimum(value: string): void;
  selectAllImages(): void;
  enableFilters(): void;
  matchScreen(width: number, height: number): void;
  update<K extends keyof Draft>(key: K, value: Draft[K]): void;
  adopt(config: Configuration, preserve?: boolean): void;
  notice: string;
  setNotice(text: string): void;
  preview: RulePreview | null;
  previewError: string | null;
  clearPreview(): void;
  inspect(): Promise<void>;
}
const ImageContext = createContext<ImageContext | null>(null);
export function useScreenCapabilities() {
  return useContext(ImageContext)?.capabilities;
}

export function useScreenResolution(): string | null {
  const geometry = useScreenCapabilities()?.geometry;
  if (geometry?.state !== 'known' || geometry.displays.length !== 1) return null;
  const { width, height } = geometry.displays[0];
  return `${width}x${height}`;
}

function useImageSettings(): ImageContext {
  const value = useContext(ImageContext);
  if (!value) throw new Error('Image settings need ImageSettingsProvider.');
  return value;
}
export function ImageSettingsProvider({ children }: PropsWithChildren) {
  const client = usePanora(),
    { request } = client;
  const [editor, setEditor] = useState<Editor | null>(() =>
    client.configuration ? adopt(null, client.configuration) : null,
  );
  const capabilityScope = client.connected
    ? JSON.stringify([
        client.snapshot?.status?.instance_id || null,
        (client.liveConfiguration || client.configuration)?.effective.backend || null,
      ])
    : null;
  const [capabilitySnapshot, setCapabilitySnapshot] = useState<{
    scope: string | null;
    value: Capabilities | null;
  } | null>(null);
  const capabilities =
    capabilityScope && capabilitySnapshot?.scope === capabilityScope
      ? capabilitySnapshot.value
      : null;
  const [notice, setNotice] = useState('');
  const [preview, setPreview] = useState<RulePreview | null>(null),
    [previewError, setPreviewError] = useState<string | null>(null);
  const editorRef = useRef(editor);
  editorRef.current = editor;
  const scopeRef = useRef(capabilityScope);
  scopeRef.current = capabilityScope;
  const customRatio = useRef('16:9');
  const previousFilters = useRef<RuleDraft | null>(null);
  const capabilityRead = useRef<Promise<Capabilities | null> | null>(null),
    capabilityEpoch = useRef(0),
    automaticallyChecked = useRef(false),
    previewToken = useRef(0);
  useEffect(() => {
    if (client.configuration) {
      const config = client.configuration;
      setEditor((previous) => (previous?.baseline === config ? previous : adopt(previous, config)));
      previewToken.current++;
      setPreview(null);
      setPreviewError(null);
    }
  }, [client.configuration]);
  useEffect(() => {
    capabilityEpoch.current++;
    previewToken.current++;
    setPreview(null);
    setPreviewError(null);
    automaticallyChecked.current = false;
    capabilityRead.current = null;
  }, [capabilityScope]);
  const readCapabilities = useCallback(() => {
    if (capabilityRead.current) return capabilityRead.current;
    const epoch = capabilityEpoch.current,
      scope = scopeRef.current;
    const pending = request('Capabilities')
      .then((result) => {
        if (epoch !== capabilityEpoch.current || scope !== scopeRef.current) return null;
        if (result.code === 'busy') {
          // Release the rejected request before the idle effect retries it.
          if (capabilityRead.current === pending) capabilityRead.current = null;
          automaticallyChecked.current = false;
          return null;
        }
        const caps = result.capabilities || null;
        setCapabilitySnapshot({ scope, value: caps });
        return caps;
      })
      .finally(() => {
        if (capabilityRead.current === pending) capabilityRead.current = null;
      });
    capabilityRead.current = pending;
    return pending;
  }, [request]);
  useEffect(() => {
    if (client.connected && client.configuration && !client.busy && !automaticallyChecked.current) {
      automaticallyChecked.current = true;
      void readCapabilities();
    }
  }, [client.connected, client.configuration, client.busy, capabilityScope, readCapabilities]);
  const clearPreview = () => {
    previewToken.current++;
    setPreview(null);
    setPreviewError(null);
  };
  function update<K extends keyof Draft>(key: K, value: Draft[K]) {
    if (
      key === 'ratio' &&
      typeof value === 'string' &&
      !['any', 'monitor', 'monitor-similar', 'presets'].includes(value)
    )
      customRatio.current = value;
    clearPreview();
    setEditor((previous) =>
      previous
        ? {
            ...previous,
            draft: { ...previous.draft, [key]: value },
            filtersEnabled: ruleKeys.some((rule) => rule === key) ? true : previous.filtersEnabled,
          }
        : previous,
    );
  }
  async function inspect() {
    const current = editorRef.current;
    if (!current) return;
    const token = ++previewToken.current,
      draftKey = JSON.stringify(current.draft),
      scope = scopeRef.current,
      { draft, baseline } = current;
    setPreview(null);
    setPreviewError(null);
    const result = await client.request('RulePreview', [
      {
        revision: baseline.revision,
        rules: {
          min_width: Number(draft.min_width),
          min_height: Number(draft.min_height),
          max_depth: baseline.rules.max_depth,
          ratio: draft.ratio,
          ratio_presets: draft.ratio === 'presets' ? draft.ratio_presets : [],
        },
      },
    ]);
    if (
      token !== previewToken.current ||
      scope !== scopeRef.current ||
      editorRef.current?.baseline.revision !== baseline.revision ||
      JSON.stringify(editorRef.current?.draft) !== draftKey
    )
      return;
    setPreview(result.preview || null);
    setPreviewError(
      result.preview
        ? null
        : result.error || 'Matches could not be checked. No settings were changed.',
    );
  }
  const value: ImageContext = {
    editor,
    hasUnsaved: Boolean(editor && changed(editor).length),
    capabilities,
    readCapabilities,
    backend: editor?.draft.backend || 'auto',
    setBackend: (value) => update('backend', value),
    setRatioMode: (mode) => {
      const ratio = editorRef.current?.draft.ratio;
      if (ratio && !['any', 'monitor', 'monitor-similar', 'presets'].includes(ratio))
        customRatio.current = ratio;
      update('ratio', mode === 'custom' ? customRatio.current : mode);
    },
    setMinimum: (value) => {
      const [width, height] = value.split('x');
      clearPreview();
      setEditor(
        (previous) =>
          previous && {
            ...previous,
            customMinimum: value === 'custom',
            draft: height
              ? { ...previous.draft, min_width: width, min_height: height }
              : previous.draft,
          },
      );
    },
    selectAllImages: () => {
      const current = editorRef.current;
      if (current?.filtersEnabled)
        previousFilters.current = {
          min_width: current.draft.min_width,
          min_height: current.draft.min_height,
          ratio: current.draft.ratio,
          ratio_presets: [...current.draft.ratio_presets],
        };
      clearPreview();
      setEditor((previous) =>
        previous
          ? {
              ...previous,
              filtersEnabled: false,
              customMinimum: false,
              draft: {
                ...previous.draft,
                min_width: '1',
                min_height: '1',
                ratio: 'any',
                ratio_presets: [],
              },
            }
          : previous,
      );
    },
    enableFilters: () => {
      clearPreview();
      setEditor((previous) =>
        previous
          ? {
              ...previous,
              filtersEnabled: true,
              draft: previous.filtersEnabled
                ? previous.draft
                : { ...previous.draft, ...previousFilters.current },
            }
          : previous,
      );
    },
    matchScreen: (width, height) => {
      clearPreview();
      setEditor((previous) =>
        previous
          ? {
              ...previous,
              filtersEnabled: true,
              customMinimum: false,
              draft: {
                ...previous.draft,
                min_width: String(width),
                min_height: String(height),
                ratio: 'monitor',
                ratio_presets: [],
              },
            }
          : previous,
      );
      if (!client.getState().blocked)
        setNotice(
          `Match my screen selected: at least ${width} × ${height} pixels and the same aspect ratio. Save changes to use these filters.`,
        );
    },
    update,
    adopt: (config, preserve = true) => {
      if (!preserve)
        customRatio.current = ['any', 'monitor', 'monitor-similar', 'presets'].includes(
          config.rules.ratio,
        )
          ? '16:9'
          : config.rules.ratio;
      setEditor((previous) => adopt(previous, config, preserve));
    },
    notice,
    setNotice,
    preview,
    previewError,
    clearPreview,
    inspect,
  };
  return <ImageContext.Provider value={value}>{children}</ImageContext.Provider>;
}

export function ImageSettings() {
  const client = usePanora(),
    image = useImageSettings();
  const { editor, capabilities, preview, previewError } = image;
  const formRef = useRef<HTMLFormElement>(null),
    filtersRef = useRef<HTMLDetailsElement>(null);
  const [presetsValidated, setPresetsValidated] = useState(false);
  const draft = editor?.draft || {
    min_width: '',
    min_height: '',
    ratio: 'any',
    ratio_presets: [],
    backend: 'auto',
    fit: 'contain',
    upscale: false,
  };
  const keys = editor ? changed(editor) : [],
    filtersEnabled = editor?.filtersEnabled ?? true,
    mode = ['any', 'monitor', 'monitor-similar', 'presets'].includes(draft.ratio)
      ? draft.ratio
      : 'custom';
  const presetError = presetsValidated && mode === 'presets' && !draft.ratio_presets.length;

  const current = client.liveConfiguration || client.configuration;
  const geometry = capabilities?.geometry,
    display =
      geometry?.state === 'known' && geometry.displays.length === 1 ? geometry.displays[0] : null;
  const displayMinimum = display ? `${display.width}x${display.height}` : null;
  const minimumOptions =
    displayMinimum && !minimumPresets.includes(displayMinimum)
      ? [...minimumPresets, displayMinimum]
      : minimumPresets;
  const minimum = `${draft.min_width}x${draft.min_height}`;
  const minimumMode =
    editor?.customMinimum || !minimumOptions.includes(minimum) ? 'custom' : minimum;
  const framing = useAcceptedPreview(client.page === 'settings' && Boolean(display));
  const dimensions = framing.metadata?.dimensions;
  const scale =
    dimensions && display && dimensions.width > 0 && dimensions.height > 0
      ? Math.min(1, display.width / dimensions.width, display.height / dimensions.height)
      : null;
  const nativeThemeFill =
    draft.fit === 'auto' &&
    !draft.upscale &&
    capabilities?.backend === 'omarchy' &&
    framing.metadata?.provider === 'omarchy';
  const fillDisplay = draft.fit === 'crop' || nativeThemeFill;
  const fitWithoutUpscaling =
    !fillDisplay && !draft.upscale && scale !== null && dimensions && display;
  const presentation = capabilities?.presentations.find((item) => item.fit === draft.fit);
  const resolution =
    preview?.rule_resolution ||
    (current && ['monitor', 'monitor-similar'].includes(current.rules.ratio)
      ? current.rule_resolution
      : null);
  const presentationOverride =
    current &&
    (current.saved.fit !== current.effective.fit ||
      current.saved.upscale !== current.effective.upscale);
  const differences = current
    ? (Object.keys(preferenceLabels) as (keyof Preferences)[]).filter(
        (key) =>
          current.restart_required.includes(key) && current.saved[key] !== current.effective[key],
      )
    : [];
  function valid() {
    if (!formRef.current?.reportValidity()) return false;
    if (draft.ratio === 'presets' && !draft.ratio_presets.length) {
      if (filtersRef.current) filtersRef.current.open = true;
      setPresetsValidated(true);
      restoreFormFocus('ratio-preset-0', client, client.getState().page, document.activeElement);
      return false;
    }
    return true;
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!editor || !keys.length || client.busy || client.blocked || !client.connected || !valid())
      return;
    const page = client.getState().page,
      origin = document.activeElement;
    const edit: PreferenceEdit = { revision: editor.baseline.revision };
    for (const key of keys) {
      if (key === 'min_width' || key === 'min_height') edit[key] = Number(draft[key]);
      else if (key !== 'ratio_presets') Object.assign(edit, { [key]: draft[key] });
    }
    if (keys.includes('fit') || keys.includes('upscale')) {
      edit.fit = draft.fit;
      edit.upscale = draft.upscale;
    }
    if (keys.includes('ratio') || keys.includes('ratio_presets'))
      edit.ratio_presets = draft.ratio === 'presets' ? draft.ratio_presets : [];
    if (!filtersEnabled && keys.some((key) => ruleKeys.some((rule) => rule === key)))
      Object.assign(edit, { min_width: 1, min_height: 1, ratio: 'any', ratio_presets: [] });
    const result = await client.request('UpdateConfiguration', [edit], { mutation: true });
    if (result.configuration) {
      image.adopt(result.configuration, false);
      image.clearPreview();
      image.setNotice('Changes saved. They apply with the next wallpaper.');
      await client.refresh();
      await image.readCapabilities();
      restoreFormFocus('fit', client, page, origin);
    } else
      image.setNotice(
        (result.error || 'The save result is unknown.') +
          (client.getState().blocked
            ? ' Reload settings before trying again. Your edits are kept.'
            : ' Your edits are kept.'),
      );
  }
  async function resetPresentation() {
    if (!editor) return;
    const page = client.getState().page,
      origin = document.activeElement;
    const result = await client.request(
      'UpdateConfiguration',
      [
        {
          revision: editor.baseline.revision,
          fit: editor.baseline.saved.fit,
          upscale: editor.baseline.saved.upscale,
        },
      ],
      { mutation: true },
    );
    if (result.configuration) {
      image.adopt(result.configuration);
      image.setNotice('Saved presentation is now active. Other pending image-rule edits are kept.');
      await client.refresh();
      await image.readCapabilities();
      restoreFormFocus('fit', client, page, origin);
    } else
      image.setNotice(
        (result.error || 'The saved presentation could not be applied.') +
          (client.getState().blocked ? ' Reload settings before trying again.' : ''),
      );
  }
  async function reload() {
    image.clearPreview();
    if (!(await client.reload())) {
      image.setNotice(
        'Settings could not be reloaded. Try Reload settings again. Your pending edits are kept.',
      );
      return;
    }
    await image.readCapabilities();
    image.setNotice(
      'Settings loaded. Your pending edits are kept; review saved values before saving.',
    );
  }
  return (
    <section id="image-settings" className="settings-panel" aria-labelledby="image-settings-title">
      <h2 id="image-settings-title" tabIndex={-1}>
        Picture fit
      </h2>
      <form id="rules-form" ref={formRef} onSubmit={save} onInvalid={revealInvalid}>
        <fieldset id="rules-fields" disabled={client.busy || !editor}>
          <div className="presentation-controls">
            <div className="rules-grid presentation-grid">
              <Field htmlFor="fit">
                Fit{' '}
                <Select
                  id="fit"
                  value={draft.fit}
                  onChange={(event) => {
                    image.update('fit', event.target.value);
                    if (
                      !capabilities?.presentations.find((item) => item.fit === event.target.value)
                        ?.upscale
                    )
                      image.update('upscale', false);
                  }}
                >
                  {['auto', 'contain', 'crop'].map((fit) => (
                    <option
                      key={fit}
                      value={fit}
                      disabled={
                        !capabilities?.presentations.some(
                          (item) => item.fit === fit && item.available,
                        )
                      }
                    >
                      {fitLabel(fit)}
                    </option>
                  ))}
                </Select>
              </Field>
              <label className="checkbox-field">
                <input
                  id="upscale"
                  type="checkbox"
                  checked={draft.upscale}
                  disabled={!presentation?.available || !presentation.upscale}
                  title={
                    presentation?.available && !presentation.upscale
                      ? 'Enlarging images is unavailable for this fit option.'
                      : ''
                  }
                  onChange={(event) => image.update('upscale', event.target.checked)}
                />{' '}
                Enlarge smaller images
              </label>
            </div>
            <p className="subtext">Changes apply with the next wallpaper.</p>
          </div>
          {client.connected && display && framing.src && presentation?.available && (
            <figure className="desktop-framing">
              <div
                className="desktop-framing-image"
                style={{
                  aspectRatio: `${display.width} / ${display.height}`,
                  maxWidth: Math.min(480, Math.round((250 * display.width) / display.height)),
                }}
              >
                <img
                  src={framing.src}
                  alt="Estimated wallpaper placement"
                  onError={framing.markUnavailable}
                  style={{
                    objectFit: fillDisplay ? 'cover' : 'contain',
                    width: fitWithoutUpscaling
                      ? `${(dimensions.width * scale * 100) / display.width}%`
                      : '100%',
                    height: fitWithoutUpscaling
                      ? `${(dimensions.height * scale * 100) / display.height}%`
                      : '100%',
                  }}
                />
              </div>
              <figcaption>Estimated desktop framing on {display.name}</figcaption>
            </figure>
          )}

          {capabilities && !presentation?.available && (
            <p id="picture-fit-status" className="subtext">
              {presentation?.reason ||
                'This fit option is unavailable. Check Desktop integration below.'}
            </p>
          )}
          {presentationOverride && current && (
            <>
              <p id="presentation-effective" className="subtext">
                Saved: {fitLabel(current.saved.fit)} / upscaling{' '}
                {current.saved.upscale ? 'on' : 'off'} · Active: {fitLabel(current.effective.fit)} /
                upscaling {current.effective.upscale ? 'on' : 'off'}. Use saved fit to replace the
                temporary settings.
              </p>
              <Button
                id="use-saved-presentation"
                type="button"
                className="outline"
                disabled={
                  client.busy ||
                  client.blocked ||
                  !client.connected ||
                  keys.some((key) => key === 'fit' || key === 'upscale')
                }
                onClick={() => void resetPresentation()}
              >
                Use saved fit now
              </Button>
            </>
          )}
          <div className="settings-subsection">
            <h2 id="image-selection-label">Shared image filters</h2>
            <p id="image-selection-help" className="subtext">
              For sources that use shared rules.
            </p>
            <FormActions
              className="form-actions image-selection"
              role="group"
              aria-labelledby="image-selection-label"
            >
              <Button
                type="button"
                className={'selection-choice ' + (filtersEnabled ? 'outline' : 'primary')}
                aria-pressed={!filtersEnabled}
                onClick={() => {
                  if (!filtersEnabled) return;
                  image.selectAllImages();
                  if (!client.blocked)
                    image.setNotice(
                      'All sizes and shapes selected. Save changes to remove image filters.',
                    );
                }}
              >
                Use all images
              </Button>
              <Button
                type="button"
                className={'selection-choice ' + (filtersEnabled ? 'primary' : 'outline')}
                aria-pressed={filtersEnabled}
                onClick={() => {
                  image.enableFilters();
                  if (!client.blocked) image.setNotice('');
                  if (filtersRef.current) filtersRef.current.open = true;
                }}
              >
                Filter images
              </Button>
              <Button
                id="match-screen"
                type="button"
                className="outline"
                aria-describedby="match-screen-help"
                disabled={!display}
                onClick={() => {
                  if (display) {
                    image.matchScreen(display.width, display.height);
                    if (filtersRef.current) filtersRef.current.open = true;
                  }
                }}
              >
                Match my screen
              </Button>
            </FormActions>
            <p id="match-screen-help" className="subtext">
              {display
                ? `Match my screen uses ${display.width} × ${display.height} and your screen’s aspect ratio.`
                : 'Match my screen needs one display with a known size.'}
            </p>
            <details id="image-filters" ref={filtersRef} hidden={!filtersEnabled}>
              <summary>Image filters</summary>
              <div id="image-filter-presets" className="rules-grid filter-grid">
                <Field htmlFor="minimum-resolution">
                  Minimum resolution
                  <Select
                    id="minimum-resolution"
                    value={minimumMode}
                    onChange={(event) => image.setMinimum(event.target.value)}
                  >
                    {minimumOptions.map((size) => (
                      <option key={size} value={size}>
                        {size === '1x1' ? 'No minimum' : size.replace('x', ' × ')}
                      </option>
                    ))}
                    <option value="custom">Custom size…</option>
                  </Select>
                </Field>
                <Field htmlFor="ratio-mode">
                  Aspect ratio{' '}
                  <Select
                    id="ratio-mode"
                    value={mode}
                    onChange={(event) => image.setRatioMode(event.target.value)}
                  >
                    <option value="any">Any ratio</option>
                    <option value="monitor" disabled={!display}>
                      Same shape as my screen
                    </option>
                    {mode === 'monitor-similar' && (
                      <option value="monitor-similar">Similar to my screen</option>
                    )}
                    <option value="presets">Selected ratios</option>
                    <option value="custom">Custom ratio</option>
                  </Select>
                </Field>
              </div>
              {minimumMode === 'custom' && (
                <div className="rules-grid filter-grid">
                  <Field htmlFor="min-width">
                    Minimum width (pixels){' '}
                    <NumberInput
                      id="min-width"

                      min="1"
                      max={Number.MAX_SAFE_INTEGER}
                      step="1"
                      required
                      value={draft.min_width}
                      onChange={(event) => image.update('min_width', event.target.value)}
                    />
                  </Field>
                  <Field htmlFor="min-height">
                    Minimum height (pixels){' '}
                    <NumberInput
                      id="min-height"

                      min="1"
                      max={Number.MAX_SAFE_INTEGER}
                      step="1"
                      required
                      value={draft.min_height}
                      onChange={(event) => image.update('min_height', event.target.value)}
                    />
                  </Field>
                </div>
              )}
              {mode === 'custom' && (
                <div id="custom-ratio" className="custom-rule">
                  <Field htmlFor="ratio">
                    Custom width:height ratio{' '}
                    <TextInput
                      id="ratio"

                      maxLength={128}
                      pattern="[1-9][0-9]*:[1-9][0-9]*"
                      placeholder="16:9"
                      required
                      value={draft.ratio}
                      onChange={(event) => image.update('ratio', event.target.value)}
                    />
                  </Field>
                </div>
              )}
              {mode === 'presets' && (
                <>
                  {presetError && (
                    <Notice id="ratio-presets-error" tone="error">
                      Choose at least one ratio. Your edits are kept.
                    </Notice>
                  )}
                  <fieldset
                    id="ratio-presets"
                    className="preset-options"
                    aria-invalid={presetError || undefined}
                    aria-describedby={presetError ? 'ratio-presets-error' : undefined}
                  >
                    <legend>Choose one or more ratios</legend>
                    {presets.map((ratio, index) => (
                      <label key={ratio}>
                        <input
                          id={`ratio-preset-${index}`}
                          type="checkbox"
                          aria-invalid={presetError || undefined}
                          aria-describedby={presetError ? 'ratio-presets-error' : undefined}
                          value={ratio}
                          data-preset
                          checked={draft.ratio_presets.includes(ratio)}
                          onChange={(event) =>
                            image.update(
                              'ratio_presets',
                              event.target.checked
                                ? presets.filter(
                                    (value) =>
                                      value === ratio || draft.ratio_presets.includes(value),
                                  )
                                : draft.ratio_presets.filter((value) => value !== ratio),
                            )
                          }
                        />{' '}
                        {ratio}
                      </label>
                    ))}
                  </fieldset>
                </>
              )}
              {['monitor', 'monitor-similar'].includes(mode) && (
                <p id="ratio-resolution" className="subtext">
                  {resolution?.state === 'resolved' && resolution.ratio
                    ? 'Service-resolved monitor ratio: ' + resolution.ratio
                    : display
                      ? 'Preview matches to resolve this draft from the reported display.'
                      : 'This shape filter needs one unambiguous display. Existing screen rules stay saved when geometry is unavailable.'}{' '}
                </p>
              )}
            </details>
            <Button
              id="inspect-rules"
              type="button"
              className="outline"
              disabled={client.busy || !client.connected || !editor}
              onClick={() => {
                if (valid()) void image.inspect();
              }}
            >
              Preview matches
            </Button>
          </div>

          <details id="desktop-options">
            <summary>Desktop integration</summary>
            <div className="rules-grid">
              <Field htmlFor="backend">
                Integration{' '}
                <Select
                  id="backend"
                  value={draft.backend}
                  onChange={(event) => image.setBackend(event.target.value)}
                >
                  {[
                    ['auto', 'Automatic'],
                    ['macos', 'macOS'],
                    ['omarchy', 'Omarchy'],
                    ['noctalia', 'Noctalia'],
                  ].map(([name, label]) => (
                    <option
                      key={name}
                      value={name}
                      disabled={
                        name !== 'auto' &&
                        !capabilities?.backends.some((item) => item.name === name && item.ready)
                      }
                    >
                      {label}
                    </option>
                  ))}
                </Select>
              </Field>
            </div>
            <p id="desktop-readiness" className="subtext">
              {capabilities
                ? capabilities.backend
                  ? 'Desktop: ' +
                    capabilities.backend +
                    ' · ' +
                    (['ready', 'partial'].includes(capabilities.state)
                      ? 'Available'
                      : 'Needs attention')
                  : 'Choose an available desktop integration.'
                : 'Desktop capabilities have not been checked.'}
            </p>
            <p id="display-geometry" className="subtext">
              {display
                ? `${display.name} · ${display.width} × ${display.height} backing pixels · Shared across supported screens.`
                : geometry?.failure?.message ||
                  'Display size is unavailable. Independent wallpapers for each display are not supported.'}
            </p>
            <p id="presentation-support" className="subtext">
              {!capabilities
                ? 'Presentation support is unavailable until the service responds.'
                : presentation?.reason ||
                  (!presentation?.upscale
                    ? 'Upscaling is unavailable for this presentation.'
                    : 'Smaller images are enlarged only when you choose it.')}
            </p>
          </details>
        </fieldset>
        {keys.length > 0 && (
          <p id="rules-draft" className="subtext">
            Unsaved changes.
          </p>
        )}
        <FormActions
          id="image-settings-actions"

          hidden={Boolean(editor) && !keys.length && !client.blocked}
        >
          <Button
            id="save-rules"
            type="submit"
            className="primary"
            disabled={client.busy || client.blocked || !client.connected || !editor || !keys.length}
          >
            Save changes
          </Button>
          <Button
            id="cancel-rules"
            type="button"
            disabled={client.busy || !keys.length}
            onClick={() => {
              if (editor) image.adopt(editor.baseline, false);
              image.clearPreview();
              image.setNotice('Unsaved image settings canceled.');
              restoreFormFocus('fit', client, client.page, document.activeElement);
            }}
          >
            Cancel edits
          </Button>
          <Button
            id="reload-image-settings"
            type="button"
            className="outline"
            disabled={client.busy}
            onClick={() => void reload()}
          >
            Reload settings
          </Button>
        </FormActions>
      </form>
      <Notice id="rules-notice" hidden={!image.notice}>
        {image.notice}
      </Notice>
      {(preview || previewError) && <MatchPreview preview={preview} error={previewError} />}
      {differences.length > 0 && current && (
        <details id="preference-differences">
          <summary>Settings in use</summary>
          <p id="restart-notice">These settings differ from your saved preferences.</p>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Setting</th>
                  <th>Saved</th>
                  <th>In use</th>
                </tr>
              </thead>
              <tbody id="startup-settings">
                {differences.map((key) => (
                  <tr key={key}>
                    <td>{preferenceLabels[key]}</td>
                    <td>
                      {typeof current.saved[key] === 'boolean'
                        ? current.saved[key]
                          ? 'On'
                          : 'Off'
                        : String(current.saved[key])}
                    </td>
                    <td>
                      {typeof current.effective[key] === 'boolean'
                        ? current.effective[key]
                          ? 'On'
                          : 'Off'
                        : String(current.effective[key])}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}
    </section>
  );
}
function MatchPreview({ preview, error }: { preview: RulePreview | null; error: string | null }) {
  const reasons: Record<string, string> = {
    below_minimum: 'Below the minimum dimensions',
    ratio_mismatch: 'Different aspect ratio',
    unavailable_bytes: 'Image files could not be inspected',
    inspection_budget: 'Outside this check’s time or file-size limits',
    outside_scope: 'Outside the enabled inputs',
  };
  return (
    <div id="rule-preview" className="usage-card" role="status">
      {!preview ? (
        error
      ) : (
        <>
          <p>
            {preview.matched} of {preview.inspected} inspected images match. {preview.unknown} not
            yet verified.{!preview.complete && ' This is a partial check; more images may remain.'}
          </p>
          {!preview.matched && (
            <p>
              {preview.rule_resolution && preview.rule_resolution.state !== 'resolved'
                ? 'The monitor ratio could not be resolved. Review wallpaper support in Settings or choose Any ratio, then preview again.'
                : preview.inspected === 0
                  ? 'No images were inspected. Add or enable a source and wait for its image check, then preview again.'
                  : preview.unknown
                    ? 'No matches verified yet. Some images could not be checked; these results do not prove that your sources have no matches.'
                    : 'No inspected images match these filters. Try lowering the minimum dimensions or choosing Any ratio, then preview again.'}
            </p>
          )}
          <details>
            <summary>Check details</summary>
            <p>
              {preview.rejected} rejected. Enabled inputs only, up to {preview.limit} records per
              check. {preview.reasons.unknown_remote || 0} remote images need bytes before matching
              can be verified. Local files are checked by image headers; cached images use validated
              bytes. Online discovery continues.{' '}
              {preview.exclusions_applied
                ? 'Exclusions are included in this count.'
                : 'Exclusions are not included in this count.'}{' '}
              Images are checked again before display.
            </p>
            <ul>
              {Object.entries(reasons)
                .filter(([key]) => preview.reasons[key])
                .map(([key, label]) => (
                  <li key={key}>
                    {label}: {preview.reasons[key]}
                  </li>
                ))}
            </ul>
          </details>
        </>
      )}
    </div>
  );
}
