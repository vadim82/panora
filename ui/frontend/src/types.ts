import type { EditorManifest, ProviderValues } from './sources/providerManifest';

export type Page = 'now' | 'providers' | 'history' | 'settings' | 'about';
export interface Failure {
  code: string;
  message: string;
  stage?: string;
}
export interface Entry {
  path?: string;
  id: string;
  image_id: number;
  source_id?: string;
  accepted_at?: string;
}
export type PreviewHistory = Pick<Entry, 'id' | 'image_id'> & { content_revision?: string };
export interface NavigationRequest {
  instance: string;
  current: string;
  token: string;
  offset: number;
}
export interface Status {
  omarchy_theme_name?: string;
  omarchy_theme_revision?: string;
  omarchy_theme_issue?: string;
  current?: Entry;
  current_preview_content?: string;
  setup_required?: boolean;
  current_excluded?: boolean;
  exclusion_undo_available?: boolean;
  paused?: boolean;
  busy?: boolean;
  uncertain?: boolean;
  interval?: string;
  next_at?: string;
  navigation_supported?: boolean;
  previous_history?: PreviewHistory;
  previous_beyond?: PreviewHistory;
  forward_beyond?: PreviewHistory;
  prepared_beyond?: PreviewHistory;
  forward_history?: PreviewHistory;
  prepared_next_image_id?: number;
  prepared_next_config_revision?: string;
  prepared_next_revision?: number;
  prepared_next_preview_content?: string;
  pending_image_id?: number;
  pending_preview_content?: string;
  history_preview_revision?: number;
  history_cursor?: number;
  history_length?: number;
  last_error?: Failure;
  warnings?: string[];
  refresh_warnings?: string[];
  refreshing?: boolean;
  providers?: ProviderHealth[];
  instance_id?: string;
  requested_backend?: string;
}
export interface Attribution {
  title?: string;
  author?: string;
  page_url?: string;
}
export interface HistoryEntry extends Entry {
  provider: string;
  image?: string;
  original_path?: string;
  dimensions?: { width: number; height: number };
  attribution?: Attribution;
  availability: string;
  reason?: string;
  current?: boolean;
}
export interface Exclusion {
  id: number;
  provider: string;
  image: string;
  title?: string;
  page_url?: string;
  excluded_at: string;
}
export interface HistoryPage {
  entries: HistoryEntry[];
  next_before?: string;
  current_id?: string;
}
export interface ExclusionPage {
  entries: Exclusion[];
  next_before?: number;
}
export interface Snapshot {
  connected: boolean;
  status?: Status;
  current?: HistoryEntry;
  current_error?: string;
  error?: string;
  code?: string;
}
export type RotationMode = 'balance_sources' | 'shuffle_all_images';
export interface Preferences {
  backend: string;
  fit: string;
  upscale: boolean;
  interval: string;
  rotation_mode: RotationMode;
  cache_max_images: number;
  cache_max_bytes: number;
}
export interface SourceImageRules {
  exact_resolutions?: string[];
  min_width: number;
  min_height: number;
  ratio: string;
  ratio_presets: string[];
}
export interface ImageRules extends SourceImageRules {
  max_depth: number;
  ratio_tolerance_pixels?: number;
}
export interface RuleResolution {
  state: string;
  ratio?: string;
  reason?: string;
}
export interface ConfiguredFolder {
  image_rules?: SourceImageRules | null;
  effective_rules?: SourceImageRules;
  rule_resolution?: RuleResolution;
  id: string;
  name: string;
  path: string;
  enabled: boolean;
  input_enabled: boolean;
  max_depth: number | null;
  effective_max_depth: number;
  refresh_interval: string;
}
export type WallhavenTagMatch = 'any' | 'all';

export interface ConfiguredSearch {
  custom_name?: string;
  tags?: string[];
  tag_match?: WallhavenTagMatch;
  colors?: string[];
  image_rules?: SourceImageRules | null;
  effective_rules?: SourceImageRules;
  rule_resolution?: RuleResolution;
  id: string;
  name: string;
  query: string;
  enabled: boolean;
  input_enabled: boolean;
  categories: string;
  sorting: string;
  order: string;
  top_range: string;
  max_pages: number;
  purity: string;
  refresh_interval: string;
}
export interface ConfiguredOmarchyTheme {
  id: string;
  enabled: boolean;
  input_enabled: boolean;
  image_rules: SourceImageRules | null;
  effective_rules: SourceImageRules;
}
export interface OmarchyThemeEdit {
  revision: string;
  id?: string;
  enabled?: boolean;
  image_rules?: SourceImageRules;
  inherit_rules?: boolean;
}
export interface OmarchyThemePreview {
  theme?: string;
  revision?: string;
  state: 'loading' | 'ready' | 'unavailable';
  warning?: string;
  total: number;
  eligible: number | null;
  page: number;
  palette: { label: string; color: string }[];
  stills: {
    id: number;
    name: string;
    width: number;
    height: number;
    eligible: boolean;
    jpeg: string;
  }[];
}
export interface ProviderEditorDescription {
  id: string;
  name: string;
  editor?: EditorManifest;
  editor_error?: string;
}
export interface ConfiguredInput {
  provider_id: string;
  id: string;
  name: string;
  enabled: boolean;
  input_enabled: boolean;
  values: ProviderValues;
  image_rules?: SourceImageRules | null;
  effective_rules?: SourceImageRules;
  rule_resolution?: RuleResolution;
}
export interface ProviderInputEdit {
  revision: string;
  provider_id: string;
  id?: string;
  name?: string;
  enabled?: boolean;
  values?: ProviderValues;
  image_rules?: SourceImageRules;
  inherit_rules?: boolean;
}
export interface Configuration {
  save_folder?: string;
  config_path?: string;
  cache_path?: string;
  omarchy_theme_available?: boolean;
  omarchy_theme?: ConfiguredOmarchyTheme;
  revision: string;
  saved: Preferences;
  effective: Preferences;
  rules: ImageRules;
  folders: ConfiguredFolder[];
  searches: ConfiguredSearch[];
  inputs?: ConfiguredInput[];
  provider_editors?: ProviderEditorDescription[];
  restart_required: string[];
  rule_resolution?: RuleResolution;
}
export interface Display {
  name: string;
  width: number;
  height: number;
  scale: number;
  transform?: number;
}
export interface Capabilities {
  backend?: string;
  state: string;
  requested_backend?: string;
  assignment_scope?: string;
  geometry: { state: string; displays: Display[]; failure?: Failure };
  backends: { name: string; ready: boolean }[];
  presentations: { fit: string; available: boolean; upscale: boolean; reason?: string }[];
}
export interface RulePreview {
  provider_id?: string;
  input_id?: string;
  scope?: string;
  reason?: string;
  matched: number;
  inspected: number;
  rejected: number;
  unknown: number;
  complete: boolean;
  limit: number;
  exclusions_applied: boolean;
  reasons: Record<string, number>;
  rule_resolution?: RuleResolution;
}
export interface Retry {
  scope: string;
  attempts: number;
  next_retry: string;
  failure: Failure;
  affected_images?: number;
}
export interface InputHealth {
  id: string;
  state: string;
  last_attempt?: string;
  last_success?: string;
  next_refresh?: string;
  counts?: { scope: string; pages?: number; inspected: number; known_eligible: number | null };
  failure?: Failure;
  retries?: Retry[];
}
export interface ProviderHealth {
  id: string;
  enabled: boolean;
  state: string;
  inputs: InputHealth[];
  retries?: Retry[];
  omitted_inputs?: number;
}
export interface CacheStatus {
  saved: { max_images: number; max_bytes: number };
  effective: { max_images: number; max_bytes: number };
  usage?: {
    state: string;
    images: number;
    bytes: number;
    protected_images: number;
    protected_bytes: number;
  };
  usage_error?: Failure;
  last_apply_error?: Failure;
}
export interface ServiceState {
  platform?: string;
  start_blocked?: boolean;
  remove_blocked?: boolean;
  log_command?: string;
  available?: boolean;
  running?: boolean;
  managed?: boolean;
  start_at_login?: boolean;
  login_state?: string;
  needs_restart?: boolean;
  log_directory?: string;
  error?: string;
}
export interface Result {
  error?: string;
  code?: string;
  configuration?: Configuration;
  status?: Status;
  connected?: boolean;
  current?: HistoryEntry;
  capabilities?: Capabilities;
  cache?: CacheStatus;
  preview?: RulePreview;
  opened?: boolean;
  service?: ServiceState;
  busy?: boolean;
}
export interface PreviewResult {
  preview?: {
    image_id: number;
    available: boolean;
    mime_type?: string;
    content_revision?: string;
    data?: string;
    reason?: string;
  };
  error?: string;
  code?: string;
}
export interface FolderResult {
  path?: string;
  name?: string;
  canceled?: boolean;
  error?: string;
  code?: string;
}
export interface HistoryResult {
  page?: HistoryPage;
  error?: string;
  code?: string;
}
export interface ExclusionsResult {
  page?: ExclusionPage;
  error?: string;
  code?: string;
}
export interface OpenResult {
  opened?: boolean;
  error?: string;
  code?: string;
}
export interface ServiceResult {
  service?: ServiceState;
  error?: string;
  busy?: boolean;
}
export interface FolderEdit {
  image_rules?: SourceImageRules;
  inherit_rules?: boolean;
  revision: string;
  id?: string;
  path?: string;
  name?: string;
  enabled?: boolean;
  max_depth?: number;
  refresh_interval?: string;
}
export interface SearchEdit {
  tags?: string[];
  tag_match?: WallhavenTagMatch;
  colors?: string[];
  image_rules?: SourceImageRules;
  inherit_rules?: boolean;
  revision: string;
  id?: string;
  name?: string;
  query?: string;
  enabled?: boolean;
  categories?: string;
  sorting?: string;
  order?: string;
  max_pages?: number;
  top_range?: string;
  refresh_interval?: string;
}
export type PreferenceEdit = Partial<Preferences & ImageRules> & {
  revision: string;
  save_folder?: string;
};
export interface SaveWallpaperEdit {
  instance: string;
  history: string;
  image_id: number;
  revision: string;
  folder?: string;
}
export interface SavedWallpaperResult {
  saved?: boolean;
  already_saved?: boolean;
  folder_name?: string;
  error?: string;
  code?: string;
}
export interface BuildIdentity {
  version?: string;
  arch_package?: string;
  revision?: string;
  development: boolean;
}
export interface WindowPalette {
  background: string;
  foreground: string;
  accent: string;
  muted: string;
  selection: string;
  red: string;
  yellow: string;
}

export interface Bridge {
  BuildIdentity(): Promise<BuildIdentity>;
  TakeAboutRequest(): Promise<boolean>;
  Status(): Promise<Snapshot>;
  Configuration(): Promise<Result>;
  OpenConfiguration(): Promise<OpenResult>;
  Capabilities(): Promise<Result>;
  CacheStatus(): Promise<Result>;
  WindowPalette(): Promise<WindowPalette | null>;
  EditOmarchyTheme(action: 'add' | 'update' | 'remove', edit: OmarchyThemeEdit): Promise<Result>;
  OmarchyThemePreview(
    page: number,
    limit: number,
  ): Promise<{ theme?: OmarchyThemePreview; error?: string }>;
  ThemePalette(): Promise<{ colors?: string[]; error?: string }>;
  RulePreview(edit: {
    revision: string;
    rules: ImageRules;
    provider_id?: string;
    input_id?: string;
  }): Promise<Result>;
  UpdateConfiguration(edit: PreferenceEdit): Promise<Result>;
  PickFolder(): Promise<FolderResult>;
  PickSaveFolder(): Promise<FolderResult>;
  SaveWallpaper(edit: SaveWallpaperEdit): Promise<SavedWallpaperResult>;
  AddFolderSettings(edit: FolderEdit): Promise<Result>;
  UpdateFolder(edit: FolderEdit): Promise<Result>;
  RemoveFolder(edit: { revision: string; id: string }): Promise<Result>;
  CountSearchCandidates(
    id: string,
    revision: string,
  ): Promise<{
    counts?: { discovered: number; eligible: number | null; search_url?: string };
    error?: string;
  }>;
  AddSearch(edit: SearchEdit): Promise<Result>;
  UpdateSearch(edit: SearchEdit): Promise<Result>;
  RemoveSearch(edit: { revision: string; id: string }): Promise<Result>;
  AddProviderInput(edit: ProviderInputEdit): Promise<Result>;
  UpdateProviderInput(edit: ProviderInputEdit): Promise<Result>;
  RemoveProviderInput(edit: ProviderInputEdit): Promise<Result>;
  SuggestProviderInput(query: {
    provider_id: string;
    field: string;
    prefix: string;
    limit: number;
  }): Promise<{ values?: string[]; error?: string }>;
  Action(action: string): Promise<Snapshot>;
  Navigate(request: NavigationRequest): Promise<Snapshot>;
  CancelNavigation(instance: string, token: string): Promise<boolean>;
  ExcludeCurrent(instance: string, historyID: string, imageID: number): Promise<Snapshot>;
  Preview(imageID: number): Promise<PreviewResult>;
  PreviewNeighbor(imageID: number, historyID: string): Promise<PreviewResult>;
  OpenCurrent(imageID: number, target: 'page' | 'folder'): Promise<OpenResult>;
  History(before: string): Promise<HistoryResult>;
  ReplayHistory(id: string): Promise<Snapshot>;
  Exclusions(before: number): Promise<ExclusionsResult>;
  RestoreExclusion(id: number): Promise<Snapshot>;
  OpenHistory(id: string, imageID: number, target: 'page' | 'folder'): Promise<OpenResult>;
  ServiceState(): Promise<ServiceResult>;
  ServiceCommand(operation: string): Promise<ServiceResult>;
  OpenServiceLogs(): Promise<OpenResult>;
}
export interface RequestOptions {
  mutation?: boolean;
  settings?: boolean;
  shouldSend?: () => boolean;
}
export type Request = <K extends keyof Bridge>(
  method: K,
  args?: Parameters<Bridge[K]>,
  options?: RequestOptions,
) => ReturnType<Bridge[K]>;
