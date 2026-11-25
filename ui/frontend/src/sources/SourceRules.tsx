import { useEffect, useRef, useState } from 'react';
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
import type { ImageRules, RulePreview } from '../types';

type Rules = Omit<ImageRules, 'max_depth'>;
type SourceRulesModel = { image_rules?: Rules | null; effective_rules?: Rules };
export interface SourceRuleDraft {
  rule_mode: 'inherit' | 'custom';
  exact_resolutions: string;
  min_width: string;
  min_height: string;
  ratio: string;
  ratio_presets: string;
}
const allImages: Rules = { min_width: 1, min_height: 1, ratio: 'any', ratio_presets: [] };
const presets = ['16:9', '16:10', '21:9', '32:9', '4:3', '3:2', '1:1', '9:16'];
const ruleKeys = [
  'rule_mode',
  'min_width',
  'min_height',
  'ratio',
  'ratio_presets',
  'exact_resolutions',
] as const;

// Mode and values form one block. Field-wise rebasing can turn a pending custom
// draft into inheritance and discard its hidden edited dimensions.
export function preserveSourceRuleEdits(
  draft: SourceRuleDraft,
  baseline: SourceRuleDraft,
  saved: SourceRuleDraft,
): SourceRuleDraft {
  const rules = ruleKeys.some((key) => draft[key] !== baseline[key]) ? draft : saved;
  return {
    rule_mode: rules.rule_mode,
    exact_resolutions: rules.exact_resolutions,
    min_width: rules.min_width,
    min_height: rules.min_height,
    ratio: rules.ratio,
    ratio_presets: rules.ratio_presets,
  };
}

export function sourceRuleDraft(
  source?: SourceRulesModel,
  inherited: Rules = allImages,
): SourceRuleDraft {
  const rules = source?.image_rules || source?.effective_rules || (source ? inherited : allImages);
  return {
    rule_mode: source && !source.image_rules ? 'inherit' : 'custom',
    exact_resolutions: (rules.exact_resolutions || []).join(','),
    min_width: String(rules.min_width),
    min_height: String(rules.min_height),
    ratio: rules.ratio,
    ratio_presets: (rules.ratio_presets || []).join(','),
  };
}
export function sourceRules(draft: SourceRuleDraft): Rules {
  return {
    ...(draft.exact_resolutions && { exact_resolutions: draft.exact_resolutions.split(',') }),
    min_width: Number(draft.min_width),
    min_height: Number(draft.min_height),
    ratio: draft.ratio,
    ratio_presets: draft.ratio_presets ? draft.ratio_presets.split(',') : [],
  };
}
export function sourceRulePatch(
  draft: SourceRuleDraft,
  baseline: SourceRuleDraft,
  existing: boolean,
) {
  const changed = ruleKeys.some((key) => draft[key] !== baseline[key]);
  if (existing && !changed) return {};
  return draft.rule_mode === 'inherit'
    ? { inherit_rules: true }
    : { image_rules: sourceRules(draft) };
}
export function sourceRuleSummary(source: SourceRulesModel, inherited: Rules): string {
  const rules = source.image_rules || source.effective_rules || inherited;
  const size = rules.exact_resolutions?.length
    ? rules.exact_resolutions.map((value) => value.replace('x', ' × ')).join(', ')
    : rules.min_width === 1 && rules.min_height === 1
      ? 'All sizes'
      : `At least ${rules.min_width} × ${rules.min_height} pixels`;
  const shape =
    rules.ratio === 'any'
      ? 'any shape'
      : rules.ratio === 'monitor'
        ? 'screen shape'
        : rules.ratio === 'monitor-similar'
          ? 'similar to screen shape'
          : rules.ratio === 'presets'
            ? (rules.ratio_presets || []).join(', ')
            : rules.ratio;
  return `${size} · ${shape}${source.image_rules ? '' : ' · inherited'}`;
}

export function sourceRuleError(draft: SourceRuleDraft, inherited: ImageRules) {
  const rules = draft.rule_mode === 'inherit' ? inherited : sourceRules(draft);
  for (const [field, value] of [
    ['min-width', rules.min_width],
    ['min-height', rules.min_height],
  ] as const) {
    if (!Number.isSafeInteger(value) || value < 1)
      return { field, message: 'Enter a positive whole pixel dimension.' };
  }
  if (rules.ratio === 'presets' && !rules.ratio_presets.length)
    return { field: 'ratio-presets', message: 'Choose at least one aspect ratio.' };
  if (
    !['any', 'monitor', 'monitor-similar', 'presets'].includes(rules.ratio) &&
    !/^[1-9][0-9]*:[1-9][0-9]*$/.test(rules.ratio)
  )
    return { field: 'ratio', message: 'Enter an aspect ratio such as 16:9.' };
  return null;
}

interface Props {
  prefix: string;
  provider: string;
  scopeRevision?: string;
  compact?: boolean;
  inputID: string | null;
  revision: string;
  enrolled: boolean;
  draft: SourceRuleDraft;
  inherited: ImageRules;
  disabled: boolean;
  validate?: boolean;
  onChange(patch: Partial<SourceRuleDraft>): void;
}

export function SourceRules({
  prefix,
  provider,
  scopeRevision,
  inputID,
  revision,
  enrolled,
  draft,
  inherited,
  disabled,
  validate = false,
  compact = false,
  onChange,
}: Props) {
  const ui = usePanora();
  const themeRules = provider === 'omarchy';
  const [editMinimum, setEditMinimum] = useState(false);
  const [preview, setPreview] = useState<RulePreview | null>(null);
  const [notice, setNotice] = useState('');
  const [attempted, setAttempted] = useState(false);
  const [working, setWorking] = useState<'preview' | 'screen' | null>(null);
  const token = useRef(0);
  const lock = useRef(false);
  const scope = JSON.stringify([
    provider,
    scopeRevision,
    inputID,
    revision,
    enrolled,
    draft,
    ui.connected,
    ui.page,
    ui.snapshot?.status?.instance_id || null,
    (ui.liveConfiguration || ui.configuration)?.effective.backend || null,
  ]);
  const latestScope = useRef(scope);
  latestScope.current = scope;
  useEffect(() => {
    token.current++;
    setPreview(null);
    setNotice('');
  }, [scope]);
  useEffect(
    () => () => {
      token.current++;
    },
    [],
  );
  const effective = draft.rule_mode === 'inherit' ? inherited : sourceRules(draft);
  const mode = ['any', 'monitor', 'monitor-similar', 'presets'].includes(draft.ratio)
    ? draft.ratio
    : 'custom';
  const minimumMode =
    draft.exact_resolutions || editMinimum
      ? 'custom'
      : effective.min_width === 1 && effective.min_height === 1
        ? 'none'
        : effective.min_width === inherited.min_width &&
            effective.min_height === inherited.min_height
          ? 'shared'
          : 'custom';
  const locked = disabled || Boolean(working) || ui.busy || !ui.connected || ui.blocked;
  function restoreFocus(origin: HTMLButtonElement, operationScope: string) {
    if (operationScope !== latestScope.current) return;
    requestAnimationFrame(() => {
      if (
        ui.getState().page !== 'providers' ||
        !origin.isConnected ||
        origin.matches(':disabled') ||
        origin.closest('[hidden]') ||
        ![document.body, document.documentElement, origin].some(
          (element) => element === document.activeElement,
        )
      )
        return;
      origin.focus();
    });
  }
  const error = validate || attempted ? sourceRuleError(draft, inherited) : null;
  function valid(origin: HTMLButtonElement) {
    setAttempted(true);
    const error = sourceRuleError(draft, inherited);
    if (!error) return true;
    setNotice(error.message + ' Your edits are kept.');
    const requestScope = latestScope.current;
    requestAnimationFrame(() => {
      if (
        requestScope !== latestScope.current ||
        ui.getState().page !== 'providers' ||
        !origin.isConnected ||
        ![document.body, document.documentElement, origin].some(
          (element) => element === document.activeElement,
        )
      )
        return;
      const target = document.getElementById(
        `${prefix}-${draft.rule_mode === 'inherit' ? 'rule-mode' : error.field}`,
      );
      if (target && !target.closest('[hidden]') && !target.matches(':disabled')) target.focus();
    });
    return false;
  }
  async function inspect(origin: HTMLButtonElement) {
    if (locked || lock.current || !enrolled || !inputID || !valid(origin)) return;
    const requestScope = latestScope.current;
    const requestToken = ++token.current;
    lock.current = true;
    setWorking('preview');
    setNotice('');
    try {
      const result = await ui.request('RulePreview', [
        {
          revision,
          provider_id: provider,
          input_id: inputID,
          rules: { ...effective, max_depth: inherited.max_depth },
        },
      ]);
      if (requestToken !== token.current || requestScope !== latestScope.current) return;
      setPreview(result.preview || null);
      setNotice(result.error || '');
    } finally {
      lock.current = false;
      setWorking(null);
      restoreFocus(origin, requestScope);
    }
  }
  async function matchScreen(origin: HTMLButtonElement) {
    if (locked || lock.current) return;
    const requestScope = latestScope.current;
    const requestToken = ++token.current;
    lock.current = true;
    setWorking('screen');
    try {
      const result = await ui.request('Capabilities');
      if (requestToken !== token.current || requestScope !== latestScope.current) return;
      const geometry = result.capabilities?.geometry;
      const display =
        geometry?.state === 'known' && geometry.displays.length === 1 ? geometry.displays[0] : null;
      if (!display || display.width < 1 || display.height < 1) {
        setNotice(
          result.error ||
            'Matching your screen needs one display with a known size. Your filters are kept.',
        );
        return;
      }
      onChange({
        rule_mode: 'custom',
        exact_resolutions: '',
        min_width: String(display.width),
        min_height: String(display.height),
        ratio: 'monitor',
        ratio_presets: '',
      });
    } finally {
      lock.current = false;
      setWorking(null);
      restoreFocus(origin, requestScope);
    }
  }
  const dimensionFields = (
    <>
      <Field
        htmlFor={`${prefix}-min-width`}
        error={error?.field === 'min-width' ? error.message : undefined}
      >
        {themeRules ? 'Minimum width (px)' : 'Minimum width (pixels)'}
        <NumberInput
          id={`${prefix}-min-width`}
          min={1}
          max={Number.MAX_SAFE_INTEGER}
          step={1}
          required
          value={draft.min_width}
          onChange={(event) => onChange({ min_width: event.target.value, exact_resolutions: '' })}
        />
      </Field>
      <Field
        htmlFor={`${prefix}-min-height`}
        error={error?.field === 'min-height' ? error.message : undefined}
      >
        {themeRules ? 'Minimum height (px)' : 'Minimum height (pixels)'}
        <NumberInput
          id={`${prefix}-min-height`}
          min={1}
          max={Number.MAX_SAFE_INTEGER}
          step={1}
          required
          value={draft.min_height}
          onChange={(event) => onChange({ min_height: event.target.value, exact_resolutions: '' })}
        />
      </Field>
    </>
  );
  return (
    <fieldset
      className="source-rules"
      disabled={locked}
      onInvalidCapture={() => setAttempted(true)}
    >
      {!compact && (
        <>
          {!themeRules && <legend>Image rules</legend>}
          {!themeRules && (
            <FormActions>
              <Button
                variant="secondary"
                aria-pressed={
                  draft.rule_mode === 'custom' &&
                  effective.min_width === 1 &&
                  effective.min_height === 1 &&
                  effective.ratio === 'any'
                }
                onClick={() => onChange({ ...sourceRuleDraft(), rule_mode: 'custom' })}
              >
                Use all images
              </Button>
              <Button
                variant="secondary"
                pending={working === 'screen'}
                onClick={(event) => void matchScreen(event.currentTarget)}
              >
                {working === 'screen' ? 'Reading display…' : 'Match my screen'}
              </Button>
            </FormActions>
          )}
          {themeRules ? (
            <div className="theme-rule-mode" role="radiogroup" aria-label="Image rule settings">
              {(['inherit', 'custom'] as const).map((value) => (
                <label key={value}>
                  <input
                    id={draft.rule_mode === value ? `${prefix}-rule-mode` : undefined}
                    type="radio"
                    name={`${prefix}-rule-mode`}
                    checked={draft.rule_mode === value}
                    onChange={() => onChange({ rule_mode: value })}
                  />
                  {value === 'inherit' ? 'Use shared rules' : 'Customize for this source'}
                </label>
              ))}
            </div>
          ) : (
            <Field
              htmlFor={`${prefix}-rule-mode`}
              error={draft.rule_mode === 'inherit' ? error?.message : undefined}
            >
              Filter settings
              <Select
                id={`${prefix}-rule-mode`}
                value={draft.rule_mode}
                onChange={(event) =>
                  onChange({ rule_mode: event.target.value as SourceRuleDraft['rule_mode'] })
                }
              >
                <option value="custom">Customize for this source</option>
                <option value="inherit">Use shared rules</option>
              </Select>
            </Field>
          )}
          {draft.rule_mode === 'inherit' ? (
            !themeRules && (
              <p className="subtext">
                Keeps the shared rules from Settings. Other sources with their own filters are
                unaffected.
              </p>
            )
          ) : (
            <>
              <div className="rules-grid">
                {themeRules && (
                  <Field htmlFor={`${prefix}-minimum`}>
                    Minimum resolution
                    <Select
                      id={`${prefix}-minimum`}
                      value={minimumMode}
                      onChange={(event) => {
                        const value = event.target.value;
                        setEditMinimum(value === 'custom');
                        if (value !== 'custom') {
                          onChange({
                            min_width: value === 'none' ? '1' : String(inherited.min_width),
                            min_height: value === 'none' ? '1' : String(inherited.min_height),
                            exact_resolutions: '',
                          });
                        }
                      }}
                    >
                      <option value="none">No minimum</option>
                      {(inherited.min_width !== 1 || inherited.min_height !== 1) && (
                        <option value="shared">
                          {inherited.min_width} × {inherited.min_height}
                        </option>
                      )}
                      <option value="custom">Custom dimensions</option>
                    </Select>
                  </Field>
                )}
                {!themeRules && <>{dimensionFields}</>}
                <Field htmlFor={`${prefix}-ratio-mode`}>
                  Aspect ratio
                  <Select
                    id={`${prefix}-ratio-mode`}
                    value={mode}
                    onChange={(event) =>
                      onChange({
                        ratio: event.target.value === 'custom' ? '16:9' : event.target.value,
                        ratio_presets: event.target.value === 'presets' ? '16:9' : '',
                      })
                    }
                  >
                    <option value="any">{themeRules ? 'Any shape' : 'Any ratio'}</option>
                    <option value="monitor">Same shape as my screen</option>
                    {mode === 'monitor-similar' && (
                      <option value="monitor-similar">Similar to my screen</option>
                    )}
                    <option value="presets">Selected ratios</option>
                    <option value="custom">Custom ratio</option>
                  </Select>
                </Field>
              </div>
              {themeRules && minimumMode === 'custom' && (
                <div className="rules-grid">{dimensionFields}</div>
              )}
              {!themeRules &&
                (effective.min_width !== 1 ||
                  effective.min_height !== 1 ||
                  draft.exact_resolutions) && (
                  <Button
                    variant="secondary"
                    onClick={() =>
                      onChange({ min_width: '1', min_height: '1', exact_resolutions: '' })
                    }
                  >
                    Clear minimum resolution
                  </Button>
                )}
              {mode === 'custom' && (
                <Field
                  htmlFor={`${prefix}-ratio`}
                  error={error?.field === 'ratio' ? error.message : undefined}
                >
                  Custom width:height ratio
                  <TextInput
                    id={`${prefix}-ratio`}
                    required
                    maxLength={128}
                    pattern="[1-9][0-9]*:[1-9][0-9]*"
                    value={draft.ratio}
                    onChange={(event) => onChange({ ratio: event.target.value })}
                  />
                </Field>
              )}
              {mode === 'presets' && (
                <fieldset
                  id={`${prefix}-ratio-presets`}
                  tabIndex={-1}
                  className="preset-options"
                  aria-invalid={error?.field === 'ratio-presets' || undefined}
                  aria-describedby={
                    error?.field === 'ratio-presets' ? `${prefix}-ratio-presets-error` : undefined
                  }
                >
                  <legend>Choose one or more ratios</legend>
                  {presets.map((ratio) => (
                    <label key={ratio}>
                      <input
                        type="checkbox"
                        checked={effective.ratio_presets.includes(ratio)}
                        onChange={(event) =>
                          onChange({
                            ratio_presets: presets
                              .filter((value) =>
                                value === ratio
                                  ? event.target.checked
                                  : effective.ratio_presets.includes(value),
                              )
                              .join(','),
                          })
                        }
                      />{' '}
                      {ratio}
                    </label>
                  ))}
                  {error?.field === 'ratio-presets' && (
                    <p id={`${prefix}-ratio-presets-error`} className="ui-field-error">
                      {error.message}
                    </p>
                  )}
                </fieldset>
              )}
            </>
          )}
        </>
      )}
      {enrolled && inputID ? (
        <Button
          variant="secondary"
          pending={working === 'preview'}
          disabled={locked}
          onClick={(event) => void inspect(event.currentTarget)}
        >
          {working === 'preview' ? 'Checking…' : 'Preview source matches'}
        </Button>
      ) : (
        <p className="subtext">
          Save and enable this source before checking its known images. Saving does not change your
          wallpaper.
        </p>
      )}
      {notice && <Notice>{notice}</Notice>}
      {preview?.reason === 'source_not_enabled_or_enrolled' ? (
        <Notice>Save and enable this source before checking its known images.</Notice>
      ) : preview && preview.rule_resolution && preview.rule_resolution.state !== 'resolved' ? (
        <Notice>
          The source's screen shape could not be resolved. Choose an explicit ratio or reconnect a
          supported display, then preview again.
        </Notice>
      ) : preview && preview.inspected === 0 ? (
        <Notice>
          No known images were inspected yet. Wait for this source's checks; this does not mean it
          has no matching images.
        </Notice>
      ) : (
        preview && (
          <Notice>
            {preview.matched} matching images from {preview.inspected} inspected for this source.{' '}
            {preview.rejected} rejected. {preview.unknown} unknown. Up to {preview.limit} known
            images checked.
            {!preview.complete && ' This is a partial catalog check.'}
            {' No new images were fetched and your wallpaper is unchanged.'}
          </Notice>
        )
      )}
    </fieldset>
  );
}
