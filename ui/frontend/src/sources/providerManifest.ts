import type { AlphaTag } from '../types';

export type ProviderValue = string | number | boolean | string[] | AlphaTag[];
export type ProviderValues = Record<string, ProviderValue | undefined>;
export type EditorOption = { value: string; label: string };

export interface EditorField {
  key: string;
  label: string;
  control: 'text' | 'integer' | 'select' | 'checkbox' | 'multivalue' | 'swatches' | 'bitmask';
  help?: string;
  placeholder?: string;
  default?: ProviderValue;
  options?: EditorOption[];
  visible_when?: { key: string; equals: string | number | boolean };
  suggest?: 'suggest';
  required?: boolean;
  min?: number;
  max?: number;
}

export interface EditorGroup {
  id: string;
  label?: string;
  fields: EditorField[];
}

export interface EditorManifest {
  version: 1;
  groups: EditorGroup[];
}

export const manifestFields = (manifest: EditorManifest) =>
  manifest.groups.flatMap((group) => group.fields);

export function manifestDefaults(manifest: EditorManifest): ProviderValues {
  return Object.fromEntries(
    manifestFields(manifest).flatMap((field) =>
      field.default === undefined ? [] : [[field.key, field.default]],
    ),
  );
}

export function fieldVisible(field: EditorField, values: ProviderValues) {
  return !field.visible_when || values[field.visible_when.key] === field.visible_when.equals;
}

export function fieldError(field: EditorField, value: ProviderValue | undefined) {
  if (field.control === 'integer' && value !== undefined && value !== '') {
    const number = Number(value);
    if (
      !Number.isSafeInteger(number) ||
      number < (field.min ?? -Infinity) ||
      number > (field.max ?? Infinity)
    )
      return 'Enter a whole number within the allowed range.';
  }
  if (
    field.required &&
    (value === undefined ||
      (typeof value === 'string' && !value.trim()) ||
      (Array.isArray(value) && !value.length))
  )
    return 'Enter a value.';
}
