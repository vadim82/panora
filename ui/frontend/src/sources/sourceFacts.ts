import type {
  ConfiguredFolder,
  ConfiguredInput,
  ConfiguredSearch,
  ConfiguredOmarchyTheme,
  Snapshot,
} from '../types';

export type Source =
  | { kind: 'local'; input: ConfiguredFolder }
  | { kind: 'wallhaven'; input: ConfiguredSearch }
  | { kind: 'omarchy'; input: ConfiguredOmarchyTheme }
  | { kind: 'external'; input: ConfiguredInput; providerName: string; editorError?: string };

export const sourceProvider = (source: Source) =>
  source.kind === 'external' ? source.input.provider_id : source.kind;

export function sourceKey(source: Source) {
  return `${sourceProvider(source)}-${source.input.id}`;
}

export function sourceIdentity(source: Source, themeName?: string) {
  if (source.kind === 'omarchy') {
    return {
      name: 'Omarchy theme',
      label: 'Omarchy theme',
      type: 'Follows active theme',
      context: themeName ?? '',
    };
  }
  const context =
    source.kind === 'local'
      ? source.input.path
      : source.kind === 'wallhaven'
        ? source.input.query
        : '';
  const type =
    source.kind === 'local'
      ? 'Folder'
      : source.kind === 'wallhaven'
        ? 'Wallhaven search'
        : source.providerName;
  const name = source.input.name || context || type;
  return {
    name,
    context,
    type,
    label: `${type} ${name}${!context || name === context ? '' : ` (${context})`}`,
  };
}

export function sourceFacts(source: Source, snapshot: Snapshot | null, lastConfirmed: boolean) {
  const { input } = source;
  const kind = sourceProvider(source);
  const editError = source.kind === 'external' && source.editorError;
  const provider = snapshot?.status?.providers?.find((entry) => entry.id === kind);
  const health = provider?.inputs.find((entry) => entry.id === input.id);
  const retry = health?.retries?.[0] || provider?.retries?.[0];
  const failure = health?.failure || retry?.failure;
  const current =
    snapshot?.status?.current?.source_id === input.id && snapshot?.current?.provider === kind;
  const blocked = input.input_enabled && !input.enabled;
  const themeIssue =
    kind === 'omarchy' && input.enabled ? snapshot?.status?.omarchy_theme_issue : '';
  const attention =
    Boolean(themeIssue) ||
    blocked ||
    Boolean(editError) ||
    (input.enabled &&
      Boolean(failure || ['empty', 'failed', 'retrying'].includes(health?.state || '')));
  const check =
    themeIssue ||
    (editError
      ? 'Provider editor unavailable'
      : blocked
        ? 'Provider disabled'
        : kind === 'local' && ['unavailable', 'not_found'].includes(health?.failure?.code || '')
          ? 'Folder unavailable'
          : health?.state === 'empty'
            ? 'No images found'
            : retry || health?.state === 'retrying'
              ? 'Waiting to retry'
              : 'Check failed');

  return {
    attention,
    check,
    origin: current
      ? lastConfirmed
        ? 'Last wallpaper’s source'
        : 'Current wallpaper’s source'
      : '',
  };
}
