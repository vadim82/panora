import type { ProviderValues } from './providerManifest';

export const commonsID = 'wikimedia_commons';
export const maxCommonsApprovals = 36;

export type CommonsApproval = { page_id: number; revision: string; terms_digest: string };

export interface CommonsSearchRequest {
  query?: string;
  category?: string;
  format?: 'jpeg' | 'png';
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

export function commonsResolutionMode(values: ProviderValues) {
  return values.resolution_mode === 'exact' || values.exact_width || values.exact_height
    ? 'exact'
    : 'minimum';
}

export function commonsSearchRequest(values: ProviderValues): {
  request: CommonsSearchRequest;
  error?: string;
} {
  const query = typeof values.query === 'string' ? values.query.trim() : '';
  const category = typeof values.category === 'string' ? values.category.trim() : '';
  const request: CommonsSearchRequest = {};
  if (query) request.query = query;
  if (category) request.category = category;
  if (values.format === 'jpeg' || values.format === 'png') request.format = values.format;
  if (!query && !category) return { request, error: 'Enter search terms or a Commons category.' };
  if (
    new TextEncoder().encode(query).length > 256 ||
    new TextEncoder().encode(category).length > 256 ||
    /["\\|:]/.test(query + category)
  )
    return {
      request,
      error: 'Use plain search terms and a category name without a prefix or search syntax.',
    };
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
