import { useId, useState } from 'react';
import type { ReactNode } from 'react';
import { Button, Field, Notice, NumberInput, Select, TextInput } from '../components/ui';
import { fieldVisible } from './providerManifest';
import type {
  EditorField,
  EditorGroup,
  EditorOption,
  ProviderValue,
  ProviderValues,
} from './providerManifest';
import { SuggestionInput } from './SuggestionInput';
import type { Suggestions } from './SuggestionInput';

type Props = {
  field: EditorField;
  id?: string;
  value: ProviderValue | undefined;
  onChange(value: ProviderValue): void;
  error?: string;
  optionalLabel?: boolean;
  suggest?: Suggestions;
  showValues?: boolean;
  leadingControl?: ReactNode;
  addLabel?: string;
  validateValue?(value: string, selected: string[]): string | undefined;
  extraSwatches?: string[];
  swatchTools?: ReactNode;
};

export function ColorDots({ values }: { values: string[] }) {
  return (
    <span className="search-color-dots" aria-hidden="true">
      {values.map((value) => (
        <span key={value} style={{ backgroundColor: value }} />
      ))}
    </span>
  );
}
export const toggleValue = (values: string[], value: string) =>
  values.includes(value) ? values.filter((entry) => entry !== value) : [...values, value];

function Choices({
  label,
  options,
  selected,
  swatches,
  onToggle,
}: {
  label: string;
  options: EditorOption[];
  selected: string[];
  swatches?: boolean;
  onToggle(value: string, index: number): void;
}) {
  return (
    <div
      className={swatches ? 'search-picks search-swatches' : 'search-picks'}
      role="group"
      aria-label={label}
    >
      {options.map(({ value, label }, index) => (
        <Button
          key={value}
          aria-label={swatches ? label : undefined}
          title={swatches ? value : undefined}
          aria-pressed={selected.includes(value)}
          onClick={() => onToggle(value, index)}
        >
          {swatches ? <ColorDots values={[value]} /> : label}
        </Button>
      ))}
    </div>
  );
}

export function ProviderField({
  field,
  id,
  value,
  onChange,
  error,
  optionalLabel,
  suggest,
  showValues = true,
  leadingControl,
  addLabel,
  validateValue,
  extraSwatches = [],
  swatchTools,
}: Props) {
  const fallbackID = useId();
  const [notice, setNotice] = useState('');
  const [custom, setCustom] = useState(field.options?.[0]?.value || '#000000');
  const controlID = id || fallbackID;
  const label = (
    <>
      {field.label}
      {optionalLabel && (
        <>
          {' '}
          <span className="subtext">(optional)</span>
        </>
      )}
    </>
  );
  const text = typeof value === 'string' || typeof value === 'number' ? value : '';
  const values = Array.isArray(value) ? value : [];
  const options = field.options || [];
  const help = field.help && <p className="subtext">{field.help}</p>;

  function add(item: string) {
    if (values.some((entry) => entry.toLowerCase() === item.toLowerCase())) return true;
    const message =
      validateValue?.(item, values) || (values.length >= 32 ? 'Choose up to 32 values.' : '');
    setNotice(message);
    if (message) return false;
    onChange([...values, item]);
    return true;
  }

  switch (field.control) {
    case 'checkbox':
      return (
        <Field htmlFor={controlID} help={field.help} error={error}>
          {label}
          <input
            id={controlID}
            type="checkbox"
            checked={value === true}
            onChange={(event) => onChange(event.target.checked)}
          />
        </Field>
      );
    case 'bitmask': {
      const bits =
        typeof text === 'string' && text.length === options.length
          ? text
          : '0'.repeat(options.length);
      return (
        <>
          <Choices
            label={field.label}
            options={options}
            selected={options
              .filter((_, index) => bits[index] === '1')
              .map((option) => option.value)}
            onToggle={(_, index) =>
              onChange(
                bits.slice(0, index) + (bits[index] === '1' ? '0' : '1') + bits.slice(index + 1),
              )
            }
          />
          {help}
        </>
      );
    }
    case 'multivalue':
      return (
        <>
          {showValues && (
            <label className="provider-value-label" htmlFor={controlID}>
              {label}
            </label>
          )}
          <SuggestionInput
            id={controlID}
            label={field.label}
            describedBy={field.help ? `${controlID}-help` : undefined}
            placeholder={field.placeholder}
            addLabel={addLabel || 'Add value'}
            leadingControl={leadingControl}
            selected={values}
            suggest={suggest}
            onAdd={add}
          />
          {field.help && (
            <p className="subtext" id={`${controlID}-help`}>
              {field.help}
            </p>
          )}
          {showValues && values.length > 0 && (
            <div className="search-chip-row" aria-label={field.label}>
              {values.map((item) => (
                <span key={item} className="search-chip">
                  {item}{' '}
                  <Button
                    aria-label={`Remove ${item}`}
                    onClick={() => onChange(toggleValue(values, item))}
                  >
                    ×
                  </Button>
                </span>
              ))}
            </div>
          )}
          {(notice || error) && <Notice tone="error">{notice || error}</Notice>}
        </>
      );
    case 'swatches': {
      const labels = new Map(options.map((option) => [option.value, option.label]));
      const choices = [
        ...new Set([...labels.keys(), ...extraSwatches, ...(showValues ? values : [])]),
      ].filter((item) => /^#[0-9a-f]{6}$/i.test(item));
      return (
        <>
          <Choices
            label={field.label}
            options={choices.map((value) => ({
              value,
              label: labels.get(value) || `Color ${value}`,
            }))}
            selected={values}
            swatches
            onToggle={(item) => {
              if (values.includes(item)) {
                onChange(toggleValue(values, item));
                setNotice('');
              } else add(item);
            }}
          />
          <div className="search-color-tools">
            <input
              aria-label="Custom color"
              type="color"
              value={custom}
              onChange={(event) => setCustom(event.target.value)}
            />
            <Button onClick={() => add(custom)}>{addLabel || 'Add color'}</Button>
            {swatchTools}
          </div>
          {help}
          {(notice || error) && <Notice tone="error">{notice || error}</Notice>}
        </>
      );
    }
    case 'select':
      return (
        <Field htmlFor={controlID} help={field.help} error={error}>
          {label}
          <Select id={controlID} value={text} onChange={(event) => onChange(event.target.value)}>
            {text === '' && <option value="">Choose…</option>}
            {options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
        </Field>
      );
    case 'integer':
      return (
        <Field htmlFor={controlID} help={field.help} error={error}>
          {label}
          <NumberInput
            id={controlID}
            step={1}
            min={field.min}
            max={field.max}
            value={text}
            onChange={(event) => onChange(event.target.value)}
          />
        </Field>
      );
    case 'text':
      return (
        <Field htmlFor={controlID} help={field.help} error={error}>
          {label}
          <TextInput
            id={controlID}
            autoComplete="off"
            maxLength={2048}
            placeholder={field.placeholder}
            value={text}
            onChange={(event) => onChange(event.target.value)}
          />
        </Field>
      );
  }
}

export function ProviderFields({
  group,
  values,
  render,
}: {
  group: EditorGroup;
  values: ProviderValues;
  render(field: EditorField): ReactNode;
}) {
  const shown = group.fields.filter((field) => fieldVisible(field, values));
  if (!shown.length) return null;
  if (!group.label) return shown.map(render);
  return (
    <fieldset className="provider-field-group">
      <legend>{group.label}</legend>
      {shown.map(render)}
    </fieldset>
  );
}
