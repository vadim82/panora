import type { ProviderValues } from './providerManifest';

export const commonsID = 'wikimedia_commons';
export const maxCommonsApprovals = 36;
export const maxCommonsCategories = 8;

export type CommonsApproval = { page_id: number; revision: string; terms_digest: string };

export interface CommonsSearchRequest {
  query?: string;
  categories?: string[];
  min_width?: number;
  min_height?: number;
  exact_width?: number;
  exact_height?: number;
  offset?: number;
}

export interface CommonsSearchResult extends CommonsApproval {
  title: string;
  width: number;
  height: number;
  author: string;
  license_name: string;
  license_url: string;
  page_url: string;
  thumbnail?: string;
}

export interface CommonsSearchPage {
  results: CommonsSearchResult[];
  next_offset?: number;
}

export function commonsApprovals(values: ProviderValues): CommonsApproval[] {
  const approved = values.approved;
  return Array.isArray(approved)
    ? approved.filter((entry): entry is CommonsApproval =>
        Boolean(
          entry &&
          typeof entry === 'object' &&
          Number.isSafeInteger(entry.page_id) &&
          entry.page_id > 0 &&
          typeof entry.revision === 'string' &&
          typeof entry.terms_digest === 'string',
        ),
      )
    : [];
}

function commonsResolutionMode(values: ProviderValues) {
  return values.exact_width || values.exact_height ? 'exact' : 'min';
}

export function commonsResolutionChoice(values: ProviderValues) {
  const mode = commonsResolutionMode(values);
  const width = values[`${mode}_width`] || '';
  const height = values[`${mode}_height`] || '';
  return width || height ? `${mode}:${width}x${height}` : '';
}

export function commonsResolutionPatch(choice: string): ProviderValues {
  const [mode, size] = choice.split(':');
  const [width, height] = size ? size.split('x').map(Number) : [];
  return {
    min_width: mode === 'min' ? width : undefined,
    min_height: mode === 'min' ? height : undefined,
    exact_width: mode === 'exact' ? width : undefined,
    exact_height: mode === 'exact' ? height : undefined,
  };
}

export function commonsCategories(values: ProviderValues): string[] {
  if (Array.isArray(values.categories))
    return values.categories.filter((item): item is string => typeof item === 'string');
  return typeof values.category === 'string' && values.category.trim()
    ? [values.category.trim()]
    : [];
}

export function commonsCategoryError(value: string, selected: string[]) {
  if (selected.length >= maxCommonsCategories) return 'Choose up to eight categories.';
  if (new TextEncoder().encode(value).length > 128 || /["\\|:]/.test(value))
    return 'Use a Commons category name without a prefix or search syntax.';
}

export function commonsSearchRequest(values: ProviderValues): {
  request: CommonsSearchRequest;
  error?: string;
} {
  const query = typeof values.query === 'string' ? values.query.trim() : '';
  const categories = commonsCategories(values);
  const request: CommonsSearchRequest = {};
  if (query) request.query = query;
  if (categories.length) request.categories = categories;
  if (!query && !categories.length)
    return { request, error: 'Enter search terms or a Commons category.' };
  if (new TextEncoder().encode(query).length > 256 || /["\\|:]/.test(query))
    return { request, error: 'Use plain search terms without search syntax.' };
  const exact = commonsResolutionMode(values) === 'exact';
  const dimensions = exact
    ? (['exact_width', 'exact_height'] as const)
    : (['min_width', 'min_height'] as const);
  for (const key of dimensions) {
    const value = values[key];
    if (value === '' || value === undefined) continue;
    const dimension = Number(value);
    if (!Number.isSafeInteger(dimension) || dimension < 1 || dimension > 65535)
      return { request, error: 'Use whole pixel dimensions between 1 and 65,535.' };
    request[key] = dimension;
  }
  if (exact && (!request.exact_width || !request.exact_height))
    return { request, error: 'Enter both dimensions for an exact resolution.' };
  return { request };
}
