import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { bridge } from '../client';
import { Button, Field, Notice, NumberInput, Select } from '../components/ui';
import { useScreenCapabilities, useScreenResolution } from '../settings/ImageSettings';
import { ColorDots, ProviderField, toggleValue as toggle } from './ProviderFields';
import { wallhavenField } from './wallhavenManifest';
import { wallhavenTags } from './wallhavenTags';
import type { Capabilities, ImageRules, RuleResolution, WallhavenTagMatch } from '../types';
import { sourceRules, sourceRuleSummary } from './SourceRules';
import type { SourceRuleDraft } from './SourceRules';

const categoryNames = wallhavenField('categories').options!.map(({ label }) => label);
const sizes = ['1920x1080', '2560x1440', '3440x1440', '3840x2160', '5120x1440'];
const screenRatios: Record<string, string> = {
  monitor: 'Same shape as my screen',
  'monitor-similar': 'Similar to my screen',
};
const isScreenRatio = (value: string) => Object.hasOwn(screenRatios, value);
const ratios = ['16:9', '16:10', '21:9', '32:9', '4:3', '3:2', '1:1', '9:16'];
const groups = {
  tags: 'Tags',
  categories: 'Categories',
  resolutions: 'Resolutions',
  ratios: 'Aspect ratios',
  colors: 'Colors',
  rules: 'Image rules',
} as const;

export type FilterGroup = keyof typeof groups;
export type SearchFilterDraft = SourceRuleDraft & {
  categories: string;
  tags: string;
  tag_match: WallhavenTagMatch;
  colors: string;
};
type Props = {
  draft: SearchFilterDraft;
  inherited: ImageRules;
  savedRatio: string;
  savedRuleResolution?: RuleResolution;
  onChange(patch: Partial<SearchFilterDraft>): void;
  group: FilterGroup | null;
  onGroup(group: FilterGroup | null): void;
  query: ReactNode;
  preview: ReactNode;
};

const split = (value: string, separator = ',') => (value ? value.split(separator) : []);
const labelSize = (value: string) => value.replace('x', ' × ');
const safePositive = (value: string) =>
  /^[1-9][0-9]*$/.test(value) && Number.isSafeInteger(Number(value));

function screenShapeWarning(capabilities?: Capabilities | null, saved?: RuleResolution) {
  const unavailable = 'Screen shape is unavailable. A single supported display is required.';
  const unsupported = 'Screen shape is unavailable for this display configuration.';
  const chooseBackend = 'Choose a desktop backend in Settings to determine screen shape.';
  const multipleDisplays = 'Multiple displays make screen shape ambiguous.';
  if (!capabilities) {
    if (saved?.reason === 'backend_ambiguous') return chooseBackend;
    if (saved?.reason === 'monitor_geometry_ambiguous') return multipleDisplays;
    if (saved?.state === 'unsupported') return unsupported;
    if (saved?.state === 'unavailable' && saved.reason !== 'monitor_geometry_not_inspected')
      return unavailable;
    return '';
  }
  const { state, geometry } = capabilities;
  if (state === 'ambiguous') return chooseBackend;
  if (state !== 'ready' && state !== 'partial') return unavailable;
  if (geometry.state === 'ambiguous' || geometry.displays.length > 1) return multipleDisplays;
  if (geometry.state === 'unsupported') return unsupported;
  if (geometry.state !== 'known' || geometry.displays.length !== 1) return unavailable;
  const { width, height, scale, transform } = geometry.displays[0];
  if (width < 1 || height < 1 || !Number.isFinite(scale) || scale <= 0 || (transform ?? 0) !== 0)
    return unsupported;
  return '';
}

function DimensionsInput({
  kind,
  onAdd,
  minWidth,
  minHeight,
}: {
  kind: 'size' | 'ratio';
  onAdd(width: string, height: string): void;
  minWidth?: string;
  minHeight?: string;
}) {
  const [width, setWidth] = useState(minWidth || '');
  const [height, setHeight] = useState(minHeight || '');
  const [error, setError] = useState(false);
  function add() {
    const valid = safePositive(width) && safePositive(height);
    setError(!valid);
    if (valid) onAdd(width, height);
  }
  return (
    <details className="search-custom">
      <summary>{kind === 'size' ? 'Custom size' : 'Custom ratio'}</summary>
      <div
        className="search-dimensions"
        onKeyDown={(event) => {
          if (event.key === 'Enter' && event.target instanceof HTMLInputElement) {
            event.preventDefault();
            add();
          }
        }}
      >
        <Field htmlFor={`search-${kind}-width`}>
          Width
          <NumberInput
            id={`search-${kind}-width`}
            min={1}
            value={width}
            onChange={(e) => setWidth(e.target.value)}
          />
        </Field>
        <Field htmlFor={`search-${kind}-height`}>
          Height
          <NumberInput
            id={`search-${kind}-height`}
            min={1}
            value={height}
            onChange={(e) => setHeight(e.target.value)}
          />
        </Field>
        <Button onClick={add}>{minWidth ? 'Apply' : 'Add'}</Button>
      </div>
      {error && <Notice>Enter a positive whole number for each dimension.</Notice>}
    </details>
  );
}

export function SearchFilters({
  draft,
  inherited,
  savedRatio,
  savedRuleResolution,
  onChange,
  group,
  onGroup,
  query,
  preview,
}: Props) {
  const [notice, setNotice] = useState('');
  const [theme, setTheme] = useState<string[]>([]);
  const [loadingTheme, setLoadingTheme] = useState(false);
  const [sizeMode, setSizeMode] = useState<'minimum' | 'exact' | null>(null);
  const screenResolution = useScreenResolution();
  const capabilities = useScreenCapabilities();
  const wrapper = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLElement>(null);
  const origin = useRef<HTMLButtonElement | null>(null);
  const shared = draft.rule_mode === 'inherit';
  const effective = shared ? inherited : sourceRules(draft);
  const tagList = split(draft.tags, '\n');
  const selectedColors = split(draft.colors);
  const exact = effective.exact_resolutions?.length ? effective.exact_resolutions : undefined;
  const resolutionMode = exact ? 'exact' : 'minimum';
  const mode = sizeMode ?? resolutionMode;
  const minimum =
    effective.min_width !== 1 || effective.min_height !== 1
      ? `${effective.min_width}x${effective.min_height}`
      : '';
  const resolutions = exact ?? (minimum ? [minimum] : []);
  const resolutionPresets = [
    ...new Set([...(screenResolution ? [screenResolution] : []), ...sizes, ...resolutions]),
  ];
  const selectedRatios =
    effective.ratio === 'presets'
      ? effective.ratio_presets
      : effective.ratio === 'any'
        ? []
        : [effective.ratio];
  const geometryGroup = group === 'resolutions' || group === 'ratios';

  function open(next: FilterGroup, button?: HTMLButtonElement) {
    if (button) origin.current = button;
    setNotice('');
    if (next === 'resolutions') setSizeMode(null);
    onGroup(next);
  }
  const focusTagInput = () =>
    panel.current?.querySelector<HTMLInputElement>('#search-tag-name')?.focus();
  useEffect(() => {
    if (group === 'tags') focusTagInput();
  }, [group]);

  useEffect(() => {
    if (!group) return;
    const dismiss = (event: MouseEvent) => {
      const target = event.target;
      if (
        target instanceof Element &&
        !wrapper.current?.contains(target) &&
        !target.closest('.search-chip-row')
      )
        onGroup(null);
    };
    function escape(event: KeyboardEvent) {
      if (
        event.key !== 'Escape' ||
        !wrapper.current?.parentElement?.contains(document.activeElement) ||
        document.activeElement?.matches('[role="combobox"][aria-expanded="true"]')
      )
        return;
      event.preventDefault();
      event.stopPropagation();
      onGroup(null);
      (origin.current?.isConnected ? origin.current : trigger.current)?.focus();
    }
    document.addEventListener('click', dismiss);
    document.addEventListener('keydown', escape, true);
    return () => {
      document.removeEventListener('click', dismiss);
      document.removeEventListener('keydown', escape, true);
    };
  }, [group, onGroup]);

  function setResolutions(values: string[], match: 'minimum' | 'exact') {
    const [width, height] = (match === 'minimum' ? values[0] || '1x1' : '1x1').split('x');
    onChange({
      min_width: width,
      min_height: height,
      exact_resolutions: match === 'exact' ? values.join(',') : '',
    });
  }
  function addResolution(value: string, remove = false) {
    if (mode === 'exact') {
      if (resolutions.length === 12 && !resolutions.includes(value)) {
        setNotice('Choose up to 12 exact resolutions.');
        return;
      }
      setResolutions(
        remove ? toggle(resolutions, value) : [...new Set([...resolutions, value])],
        mode,
      );
    } else setResolutions(remove && resolutions.includes(value) ? [] : [value], mode);
    setNotice('');
  }
  function setRatios(values: string[]) {
    const screenRatio = values.find(isScreenRatio);
    onChange({
      ratio: screenRatio ?? (values.length ? 'presets' : 'any'),
      ratio_presets: screenRatio ? '' : values.join(','),
    });
  }
  function addRatio(value: string, remove = false) {
    const screenRatio = isScreenRatio(value);
    if (selectedRatios.length === 12 && !selectedRatios.includes(value) && !screenRatio) {
      setNotice('Choose up to 12 aspect ratios.');
      return;
    }
    const values = screenRatio ? [] : selectedRatios.filter((ratio) => !isScreenRatio(ratio));
    setRatios(
      remove && selectedRatios.includes(value)
        ? values.filter((ratio) => ratio !== value)
        : [...new Set([...values, value])],
    );
    setNotice('');
  }
  function removeTag(tag: string) {
    onChange({ tags: tagList.filter((value) => value !== tag).join('\n') });
  }
  function setCategory(index: number) {
    onChange({
      categories: draft.categories
        .split('')
        .map((value, i) => (i === index ? (value === '1' ? '0' : '1') : value))
        .join(''),
    });
  }
  async function importTheme() {
    if (loadingTheme) return;
    setLoadingTheme(true);
    try {
      const result = await bridge('ThemePalette');
      if (result.colors?.length) {
        setTheme(result.colors);
        onChange({ colors: result.colors.join(',') });
        setNotice('');
      } else
        setNotice(result.error || 'No Omarchy theme palette is available. Choose colors manually.');
    } catch {
      setNotice('No Omarchy theme palette is available. Choose colors manually.');
    } finally {
      setLoadingTheme(false);
    }
  }
  function chip(
    group: FilterGroup,
    label: string,
    remove?: () => void,
    content: ReactNode = label,
  ) {
    return (
      <span className="search-chip" key={`${group}:${label}`}>
        <Button
          aria-label={`Edit filter ${label}`}
          onClick={(e) => {
            open(group, e.currentTarget);
            if (group !== 'tags') requestAnimationFrame(() => panel.current?.focus());
          }}
        >
          {content}
        </Button>
        {remove && (
          <Button
            aria-label={`Remove filter ${label}`}
            onClick={() => {
              remove();
              trigger.current?.focus();
            }}
          >
            ×
          </Button>
        )}
      </span>
    );
  }
  const resolved = effective.ratio === savedRatio ? savedRuleResolution : undefined;
  const shapeWarning =
    effective.ratio === 'monitor-similar' ? screenShapeWarning(capabilities, resolved) : '';
  const tagLabel = `Tags · ${draft.tag_match === 'any' ? 'Any' : 'All'}`;
  const hasOtherFilters =
    draft.categories.includes('1') ||
    resolutions.length > 0 ||
    selectedRatios.length > 0 ||
    selectedColors.length > 0 ||
    shared;
  const screenName = `Screen shape${resolved?.ratio ? ` · ${resolved.ratio}` : ''}`;
  return (
    <>
      <div className="search-query-wrap" ref={wrapper}>
        {query}
        <Button
          id="search-filters"
          ref={trigger}
          aria-expanded={Boolean(group)}
          aria-controls="search-filter-panel"
          onClick={(e) => (group ? onGroup(null) : open('tags', e.currentTarget))}
        >
          Filters <span aria-hidden="true">⌄</span>
        </Button>
        <section
          className={group === 'tags' ? 'search-picker search-picker-tags' : 'search-picker'}
          id="search-filter-panel"
          aria-label="Wallpaper filters"
          tabIndex={-1}
          hidden={!group}
          ref={panel}
        >
          <div className="search-filter-rail" role="group" aria-label="Filter groups">
            {Object.entries(groups).map(([key, title]) => (
              <Button
                key={key}
                aria-pressed={group === key}
                onClick={() => open(key as FilterGroup)}
              >
                {title}
              </Button>
            ))}
          </div>
          <div className="search-filter-content">
            {group && <h4>{group === 'tags' ? 'Wallpaper tags' : groups[group]}</h4>}
            {group === 'rules' && (
              <Select
                id="search-filter-rule-mode"
                aria-label="Image rules"
                value={draft.rule_mode}
                onChange={(e) =>
                  onChange({ rule_mode: e.target.value === 'inherit' ? 'inherit' : 'custom' })
                }
              >
                <option value="inherit">Use shared image rules</option>
                <option value="custom">Set for this source</option>
              </Select>
            )}
            {shared && (group === 'rules' || geometryGroup) && (
              <>
                <p className="subtext">
                  From Settings · {sourceRuleSummary({}, inherited).replace(/ · inherited$/, '')}
                </p>
                {geometryGroup && <Button onClick={() => open('rules')}>Change image rules</Button>}
              </>
            )}
            {group === 'rules' && (
              <>
                {!shared && (
                  <div className="search-picks">
                    <Button onClick={() => open('resolutions')}>Resolutions</Button>
                    <Button onClick={() => open('ratios')}>Aspect ratios</Button>
                  </div>
                )}
                {preview}
              </>
            )}
            <div hidden={group !== 'tags'} className="search-tags">
              <div className="search-tag-match">
                <span id="search-tag-match-label">Match</span>
                <div
                  className="search-tag-segmented"
                  role="group"
                  aria-labelledby="search-tag-match-label"
                >
                  <Button
                    aria-pressed={draft.tag_match === 'any'}
                    onClick={() => onChange({ tag_match: 'any' })}
                  >
                    Any <span>OR</span>
                  </Button>
                  <Button
                    aria-pressed={draft.tag_match === 'all'}
                    onClick={() => onChange({ tag_match: 'all' })}
                  >
                    All <span>AND</span>
                  </Button>
                </div>
                <p className="subtext" aria-live="polite">
                  {draft.tag_match === 'any'
                    ? 'Wallpapers can match at least one selected tag.'
                    : 'Wallpapers must match every selected tag.'}
                </p>
              </div>
              {tagList.length > 0 && (
                <div
                  className="search-chip-row search-tag-selected"
                  role="group"
                  aria-label="Selected tags"
                >
                  {tagList.map((tag) => (
                    <span className="search-chip" key={tag}>
                      <Button
                        aria-label={`Remove tag ${tag}`}
                        onClick={() => {
                          removeTag(tag);
                          focusTagInput();
                        }}
                      >
                        {tag} <span aria-hidden="true">×</span>
                      </Button>
                    </span>
                  ))}
                </div>
              )}
              <ProviderField
                id="search-tag-name"
                field={wallhavenField('tags')}
                value={tagList}
                addLabel="Add tag"
                suggest={wallhavenTags}
                showValues={false}
                onChange={(values) => onChange({ tags: (values as string[]).join('\n') })}
                validateValue={(value, selected) =>
                  selected.length >= 16 ||
                  new TextEncoder().encode(value).length > 128 ||
                  /^[+\-@]/.test(value) ||
                  /[:"\\\t\r\n\0]/.test(value)
                    ? 'Use up to 16 plain tag names, each at most 128 bytes.'
                    : undefined
                }
              />
              <a
                href="https://wallhaven.cc/tags"
                target="_blank"
                rel="noreferrer"
                onMouseDown={(event) => {
                  // Keep the list from collapsing and moving the link before click.
                  if (event.button === 0) event.preventDefault();
                }}
                onClick={(event) => {
                  const runtime = (
                    window as Window & { runtime?: { BrowserOpenURL?(url: string): void } }
                  ).runtime;
                  if (runtime?.BrowserOpenURL) {
                    event.preventDefault();
                    runtime.BrowserOpenURL(event.currentTarget.href);
                  }
                }}
              >
                Browse Wallhaven tags
              </a>
            </div>
            {group === 'categories' && (
              <ProviderField
                field={wallhavenField('categories')}
                value={draft.categories}
                onChange={(value) => onChange({ categories: String(value) })}
              />
            )}
            {group === 'resolutions' && !shared && (
              <>
                <Select
                  aria-label="Resolution match"
                  value={mode}
                  onChange={(e) => {
                    const next = e.target.value === 'exact' ? 'exact' : 'minimum';
                    setSizeMode(next);
                    setResolutions(resolutions.slice(0, 1), next);
                  }}
                >
                  <option value="minimum">Minimum</option>
                  <option value="exact">Exact</option>
                </Select>
                <div className="search-picks" role="group" aria-label="Resolution presets">
                  {resolutionPresets.map((size) => (
                    <Button
                      key={size}
                      aria-pressed={resolutions.includes(size)}
                      onClick={() => addResolution(size, true)}
                    >
                      {size === screenResolution
                        ? `My screen · ${labelSize(size)}`
                        : labelSize(size)}
                    </Button>
                  ))}
                </div>
                <DimensionsInput
                  key={mode}
                  kind="size"
                  minWidth={mode === 'minimum' ? draft.min_width : undefined}
                  minHeight={mode === 'minimum' ? draft.min_height : undefined}
                  onAdd={(width, height) => addResolution(width + 'x' + height)}
                />
              </>
            )}
            {group === 'ratios' && !shared && (
              <>
                <div className="search-picks" role="group" aria-label="Aspect ratio presets">
                  {Object.entries(screenRatios).map(([value, label]) => (
                    <Button
                      key={value}
                      aria-pressed={selectedRatios.includes(value)}
                      onClick={() => addRatio(value, true)}
                    >
                      {label}
                    </Button>
                  ))}
                  {[
                    ...new Set([
                      ...ratios,
                      ...selectedRatios.filter((value) => !isScreenRatio(value)),
                    ]),
                  ].map((ratio) => (
                    <Button
                      key={ratio}
                      aria-pressed={selectedRatios.includes(ratio)}
                      onClick={() => addRatio(ratio, true)}
                    >
                      {ratio}
                    </Button>
                  ))}
                </div>
                <DimensionsInput
                  kind="ratio"
                  onAdd={(width, height) => addRatio(width + ':' + height)}
                />
              </>
            )}
            <div hidden={group !== 'colors'}>
              <ProviderField
                field={wallhavenField('colors')}
                value={selectedColors}
                onChange={(values) => onChange({ colors: (values as string[]).join(',') })}
                extraSwatches={theme}
                validateValue={(_, selected) =>
                  selected.length >= 6 ? 'Choose up to 6 colors.' : undefined
                }
                swatchTools={
                  <Button pending={loadingTheme} onClick={() => void importTheme()}>
                    {theme.length > 0 && <ColorDots values={theme.slice(0, 3)} />}Use theme palette
                  </Button>
                }
              />
            </div>
            {notice && <Notice>{notice}</Notice>}
          </div>
        </section>
      </div>
      {shapeWarning && <Notice>{shapeWarning}</Notice>}
      <div className="search-chip-row search-chip-groups" role="group" aria-label="Active filters">
        {group !== 'tags' && tagList.length > 0 && (
          <div className="search-chip-group" role="group" aria-label={tagLabel}>
            <span className="search-chip-label">{tagLabel}</span>
            <div className="search-chip-items">
              {tagList.map((value) => chip('tags', value, () => removeTag(value)))}
            </div>
          </div>
        )}
        {hasOtherFilters && (
          <div className="search-chip-group" role="group" aria-label="Other filters">
            <span className="search-chip-label">Other filters</span>
            <div className="search-chip-items">
              {categoryNames.map(
                (value, i) =>
                  draft.categories[i] === '1' && chip('categories', value, () => setCategory(i)),
              )}
              {resolutions.map((value) =>
                chip(
                  shared ? 'rules' : 'resolutions',
                  (exact ? '' : 'At least ') + labelSize(value),
                  shared
                    ? undefined
                    : () =>
                        setResolutions(
                          resolutions.filter((entry) => entry !== value),
                          resolutionMode,
                        ),
                ),
              )}
              {selectedRatios.map((value) =>
                chip(
                  shared ? 'rules' : 'ratios',
                  value === 'monitor'
                    ? screenName
                    : isScreenRatio(value)
                      ? screenRatios[value]
                      : value,
                  shared ? undefined : () => setRatios(toggle(selectedRatios, value)),
                ),
              )}
              {selectedColors.length > 0 &&
                chip(
                  'colors',
                  selectedColors.length === 1 ? '1 color' : `${selectedColors.length} colors`,
                  () => onChange({ colors: '' }),
                  <>
                    <ColorDots values={selectedColors.slice(0, 3)} />
                    {selectedColors.length} {selectedColors.length === 1 ? 'color' : 'colors'}
                  </>,
                )}
              {shared && chip('rules', 'Shared image rules')}
            </div>
          </div>
        )}
      </div>
    </>
  );
}
