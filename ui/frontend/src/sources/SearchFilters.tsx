import { useRef, useState } from 'react';
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
  rulesOpen: boolean;
  onRulesOpen(open: boolean): void;
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
  sizeMode,
  minWidth,
  minHeight,
}: {
  kind: 'size' | 'ratio';
  sizeMode?: 'minimum' | 'exact';
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
          {kind === 'size' ? 'Width (px)' : 'Width'}
          <NumberInput
            id={`search-${kind}-width`}
            min={1}
            value={width}
            onChange={(e) => setWidth(e.target.value)}
          />
        </Field>
        <Field htmlFor={`search-${kind}-height`}>
          {kind === 'size' ? 'Height (px)' : 'Height'}
          <NumberInput
            id={`search-${kind}-height`}
            min={1}
            value={height}
            onChange={(e) => setHeight(e.target.value)}
          />
        </Field>
        <Button onClick={add}>
          {kind === 'ratio' ? 'Add ratio' : sizeMode === 'minimum' ? 'Apply minimum' : 'Add size'}
        </Button>
      </div>
      {error && <Notice>Enter a positive whole number for each dimension.</Notice>}
    </details>
  );
}

function FilterDisclosure({
  id,
  label,
  summary,
  open,
  onOpen,
  children,
}: {
  id: string;
  label: string;
  summary: string;
  open: boolean;
  onOpen(open: boolean): void;
  children: ReactNode;
}) {
  const trigger = useRef<HTMLButtonElement>(null);
  return (
    <div
      className="search-disclosure"
      onKeyDown={(event) => {
        if (event.key !== 'Escape' || event.defaultPrevented || !open) return;
        event.preventDefault();
        onOpen(false);
        trigger.current?.focus();
      }}
    >
      <Button
        id={id}
        ref={trigger}
        aria-expanded={open}
        aria-controls={`${id}-content`}
        aria-describedby={`${id}-summary`}
        className="search-disclosure-trigger"
        onClick={() => onOpen(!open)}
      >
        <span aria-hidden="true">{open ? '▾' : '▸'}</span>
        <strong>{label}</strong>
        <span id={`${id}-summary`} className="search-disclosure-summary" aria-hidden="true">
          {summary}
        </span>
      </Button>
      <div id={`${id}-content`} className="search-disclosure-content" hidden={!open}>
        {children}
      </div>
    </div>
  );
}

export function SearchFilters({
  draft,
  inherited,
  savedRatio,
  savedRuleResolution,
  onChange,
  rulesOpen,
  onRulesOpen,
}: Props) {
  const [geometryNotice, setGeometryNotice] = useState('');
  const [paletteNotice, setPaletteNotice] = useState('');
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [colorsOpen, setColorsOpen] = useState(false);
  const colorTrigger = useRef<HTMLButtonElement>(null);
  const [theme, setTheme] = useState<string[]>([]);
  const [loadingTheme, setLoadingTheme] = useState(false);
  const [sizeMode, setSizeMode] = useState<'minimum' | 'exact' | null>(null);
  const screenResolution = useScreenResolution();
  const capabilities = useScreenCapabilities();
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
        setGeometryNotice('Choose up to 12 exact resolutions.');
        return;
      }
      setResolutions(
        remove ? toggle(resolutions, value) : [...new Set([...resolutions, value])],
        mode,
      );
    } else setResolutions(remove && resolutions.includes(value) ? [] : [value], mode);
    setGeometryNotice('');
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
      setGeometryNotice('Choose up to 12 aspect ratios.');
      return;
    }
    const values = screenRatio ? [] : selectedRatios.filter((ratio) => !isScreenRatio(ratio));
    setRatios(
      remove && selectedRatios.includes(value)
        ? values.filter((ratio) => ratio !== value)
        : [...new Set([...values, value])],
    );
    setGeometryNotice('');
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
    const unavailable = 'No Omarchy theme palette is available. Choose colors manually.';
    try {
      const result = await bridge('ThemePalette');
      const colors = [
        ...new Set(
          (result.colors || [])
            .filter((color) => /^#[0-9a-f]{6}$/i.test(color))
            .map((color) => color.toLowerCase()),
        ),
      ].slice(0, 6);
      if (colors.length) {
        setTheme(colors);
        onChange({ colors: colors.join(',') });
        setPaletteNotice('');
      } else setPaletteNotice(result.error || unavailable);
    } catch {
      setPaletteNotice(unavailable);
    } finally {
      setLoadingTheme(false);
    }
  }

  const resolved = effective.ratio === savedRatio ? savedRuleResolution : undefined;
  const shapeWarning =
    effective.ratio === 'monitor-similar' ? screenShapeWarning(capabilities, resolved) : '';
  const categories = categoryNames.filter((_, index) => draft.categories[index] === '1');
  const filterSummary =
    [
      tagList.length && `${tagList.length} ${tagList.length === 1 ? 'tag' : 'tags'}`,
      categories.join(', '),
      selectedColors.length &&
        `${selectedColors.length} ${selectedColors.length === 1 ? 'color' : 'colors'}`,
    ]
      .filter(Boolean)
      .join(' · ') || 'No filters';
  const ruleSummary = sourceRuleSummary(shared ? {} : { image_rules: effective }, inherited)
    .replace(/ · inherited$/, '')
    .replace(/ pixels · /, ' · ');

  return (
    <>
      <FilterDisclosure
        id="search-filters"
        label="Filters"
        summary={filterSummary}
        open={filtersOpen}
        onOpen={setFiltersOpen}
      >
        <div className="search-tags">
          <label htmlFor="search-tag-name">Tags</label>
          <ProviderField
            id="search-tag-name"
            field={wallhavenField('tags')}
            value={tagList}
            addLabel="Add tag"
            leadingControl={
              <Select
                aria-label="Tag matching"
                value={draft.tag_match}
                onChange={(event) =>
                  onChange({ tag_match: event.target.value === 'all' ? 'all' : 'any' })
                }
              >
                <option value="any">Any (OR)</option>
                <option value="all">All (AND)</option>
              </Select>
            }
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
                      document.getElementById('search-tag-name')?.focus();
                    }}
                  >
                    {tag} <span aria-hidden="true">×</span>
                  </Button>
                </span>
              ))}
            </div>
          )}
          <a
            href="https://wallhaven.cc/tags"
            target="_blank"
            rel="noreferrer"
            onMouseDown={(event) => {
              // Keep the suggestions from moving the link before the click.
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
        <fieldset className="search-categories">
          <legend>Categories</legend>
          {categoryNames.map((name, index) => (
            <label key={name} className="search-check">
              <input
                type="checkbox"
                checked={draft.categories[index] === '1'}
                onChange={() => setCategory(index)}
              />
              {name}
            </label>
          ))}
        </fieldset>
        <div
          className="search-colors"
          onKeyDown={(event) => {
            if (event.key !== 'Escape' || !colorsOpen || event.defaultPrevented) return;
            event.preventDefault();
            setColorsOpen(false);
            colorTrigger.current?.focus();
          }}
        >
          <div className="search-control-row">
            <strong>Colors</strong>
            <span className="subtext">{selectedColors.length} of 6 selected</span>
          </div>
          {selectedColors.length > 0 && (
            <div className="search-picks" role="group" aria-label="Selected colors">
              {selectedColors.map((color) => (
                <Button
                  key={color}
                  aria-label={`Remove color ${color}`}
                  title={color}
                  onClick={() => {
                    onChange({ colors: toggle(selectedColors, color).join(',') });
                    colorTrigger.current?.focus();
                  }}
                >
                  <ColorDots values={[color]} /> <span aria-hidden="true">×</span>
                </Button>
              ))}
            </div>
          )}
          <Button
            ref={colorTrigger}
            aria-expanded={colorsOpen}
            aria-controls="search-color-picker"
            className="search-color-trigger"
            onClick={() => setColorsOpen(!colorsOpen)}
          >
            <span aria-hidden="true">{colorsOpen ? '▾' : '▸'}</span> Add color
          </Button>
          <div id="search-color-picker" className="search-color-picker" hidden={!colorsOpen}>
            <ProviderField
              field={{ ...wallhavenField('colors'), label: 'Wallhaven swatches' }}
              value={selectedColors}
              onChange={(values) => onChange({ colors: (values as string[]).join(',') })}
              showValues={false}
              addLabel="Add custom color"
              extraSwatches={theme}
              validateValue={(_, selected) =>
                selected.length >= 6 ? 'Choose up to 6 colors.' : undefined
              }
              swatchTools={
                <Button pending={loadingTheme} onClick={() => void importTheme()}>
                  Use theme palette
                </Button>
              }
            />
            {paletteNotice && <Notice>{paletteNotice}</Notice>}
          </div>
        </div>
      </FilterDisclosure>
      <FilterDisclosure
        id="search-image-rules"
        label="Image rules"
        summary={shared ? `Shared · ${ruleSummary}` : ruleSummary}
        open={rulesOpen}
        onOpen={onRulesOpen}
      >
        <label className="search-control-row" htmlFor="search-filter-rule-mode">
          Rules
          <Select
            id="search-filter-rule-mode"
            value={draft.rule_mode}
            onChange={(event) =>
              onChange({ rule_mode: event.target.value === 'inherit' ? 'inherit' : 'custom' })
            }
          >
            <option value="inherit">Use shared rules</option>
            <option value="custom">Set for this source</option>
          </Select>
        </label>
        {shared && <p className="subtext">From Settings · {ruleSummary}</p>}
        <div hidden={shared} className="search-geometry">
          <section aria-labelledby="search-resolutions-title">
            <div className="search-control-row">
              <h4 id="search-resolutions-title">Resolutions</h4>
              <Select
                aria-label="Resolution match"
                value={mode}
                onChange={(event) => {
                  const next = event.target.value === 'exact' ? 'exact' : 'minimum';
                  setSizeMode(next);
                  setResolutions(resolutions.slice(0, 1), next);
                }}
              >
                <option value="minimum">Minimum</option>
                <option value="exact">Exact</option>
              </Select>
            </div>
            <div className="search-picks" role="group" aria-label="Resolution presets">
              {resolutionPresets.map((size) => (
                <Button
                  key={size}
                  aria-pressed={resolutions.includes(size)}
                  onClick={() => addResolution(size, true)}
                >
                  {size === screenResolution ? `My screen · ${labelSize(size)}` : labelSize(size)}
                </Button>
              ))}
            </div>
            <DimensionsInput
              key={mode}
              kind="size"
              sizeMode={mode}
              minWidth={mode === 'minimum' ? draft.min_width : undefined}
              minHeight={mode === 'minimum' ? draft.min_height : undefined}
              onAdd={(width, height) => addResolution(width + 'x' + height)}
            />
          </section>
          <section aria-labelledby="search-ratios-title">
            <h4 id="search-ratios-title">Aspect ratios</h4>
            <p className="subtext">Match any selected ratio, or choose your screen shape.</p>
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
                ...new Set([...ratios, ...selectedRatios.filter((value) => !isScreenRatio(value))]),
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
          </section>
          {geometryNotice && <Notice>{geometryNotice}</Notice>}
        </div>
      </FilterDisclosure>
      {shapeWarning && <Notice>{shapeWarning}</Notice>}
    </>
  );
}
