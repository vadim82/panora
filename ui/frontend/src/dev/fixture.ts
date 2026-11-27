import type {
  Bridge,
  Configuration,
  ConfiguredFolder,
  ConfiguredSearch,
  Entry,
  Exclusion,
  FolderEdit,
  HistoryEntry,
  SourceImageRules,
  Snapshot,
} from '../types';
import left from './canyon-left.png?url';
import center from './canyon-center.png?url';
import right from './canyon-right.png?url';
import portrait from './canyon-portrait.png?url';
import square from './sunset.png?url';

// Browser-only preview data. The production entry removes this module entirely.
export function installFixture(
  scenario = new URLSearchParams(location.search).get('fixture') || 'populated',
): Bridge {
  const firstRun = scenario === 'first-run';
  let sequence = 2;
  const config: Configuration = {
    save_folder: '',
    revision: 'preview-1',
    config_path: '/home/demo/.config/panora/config.toml',
    cache_path: '/home/demo/.cache/panora/playback',
    saved: {
      backend: 'macos',
      fit: 'contain',
      upscale: false,
      interval: '30m',
      rotation_mode: 'balance_sources',
      cache_max_images: 20,
      cache_max_bytes: 104857600,
    },
    effective: {
      backend: 'macos',
      fit: 'contain',
      upscale: false,
      interval: '30m',
      rotation_mode: 'balance_sources',
      cache_max_images: 20,
      cache_max_bytes: 104857600,
    },
    rules: { min_width: 1920, min_height: 1080, max_depth: 2, ratio: 'any', ratio_presets: [] },
    folders: [
      {
        id: 'demo-folder',
        name: 'Landscapes',
        path: '/Pictures/Wallpapers',
        enabled: true,
        input_enabled: true,
        max_depth: 2,
        effective_max_depth: 2,
        refresh_interval: '5m',
      },
    ],
    searches: [
      {
        id: 'demo-search',
        name: 'Quiet places',
        query: 'nature',
        enabled: true,
        input_enabled: true,
        categories: '100',
        sorting: 'relevance',
        order: 'desc',
        max_pages: 2,
        top_range: '1M',
        purity: '100',
        refresh_interval: '5m',
      },
    ],
    restart_required: [],
    rule_resolution: { state: 'resolved' },
  };
  const entries: HistoryEntry[] = [
    {
      id: 'preview-2',
      image_id: 2,
      provider: 'local',
      source_id: 'demo-folder',
      original_path: '/Pictures/Wallpapers/sunset.jpg',
      accepted_at: '2026-11-01T12:30:00Z',
      dimensions: { width: 640, height: 360 },
      availability: 'cached',
    },
    {
      id: 'preview-1',
      image_id: 1,
      provider: 'wallhaven',
      source_id: 'demo-search',
      accepted_at: '2026-11-01T12:00:00Z',
      dimensions: { width: 640, height: 360 },
      availability: 'cached',
      attribution: {
        title: 'Panora canyon artwork',
        author: 'Panora',
        page_url: 'https://wallhaven.cc/w/demo',
      },
    },
  ];
  const state: Snapshot = {
    connected: true,
    status: {
      instance_id: 'browser-preview',
      paused: true,
      setup_required: false,
      history_cursor: 1,
      history_length: 2,
      current: entries[0],
      current_excluded: false,
      exclusion_undo_available: false,
      providers: [],
    },
    current: entries[0],
  };
  if (scenario === 'sources') {
    config.folders.push({
      ...config.folders[0],
      id: 'travel-folder',
      name: 'Travel archive',
      path: '/Pictures/Trips/Long folder names and personal collections',
      enabled: false,
      input_enabled: false,
    });
    config.folders.push({
      ...config.folders[0],
      id: 'missing-folder',
      name: 'External drive',
      path: '/Volumes/Photos/Wallpapers',
    });
    config.searches.push({
      ...config.searches[0],
      id: 'night-search',
      name: 'Night skies',
      query: 'milky way',
      enabled: false,
      input_enabled: false,
    });
    state.status!.providers = [
      {
        id: 'local',
        enabled: true,
        state: 'ready',
        inputs: [
          { id: 'demo-folder', state: 'ready' },
          { id: 'travel-folder', state: 'disabled' },
          {
            id: 'missing-folder',
            state: 'failed',
            failure: { code: 'unavailable', message: 'External drive is unavailable.' },
          },
        ],
      },
      {
        id: 'wallhaven',
        enabled: true,
        state: 'ready',
        inputs: [
          {
            id: 'demo-search',
            state: 'ready',
            counts: { scope: 'metadata', inspected: 48, known_eligible: null, pages: 2 },
            last_success: '2026-11-01T12:30:00Z',
          },
          { id: 'night-search', state: 'disabled' },
        ],
      },
    ];
  }
  const exclusions: Exclusion[] = [];
  const savedWallpapers = new Set<string>();
  let lastExcluded: number | null = null;
  if (firstRun || scenario === 'empty') {
    if (firstRun)
      config.rules = {
        ...config.rules,
        min_width: 1,
        min_height: 1,
        ratio: 'any',
        ratio_presets: [],
      };
    if (firstRun) {
      config.folders = [];
      config.searches = [];
    }
    entries.length = 0;
    state.status!.current = undefined;
    state.current = undefined;
    state.status!.setup_required = firstRun;
    state.status!.history_length = 0;
  }
  if (scenario === 'source-error')
    state.status!.providers = [
      {
        id: 'wallhaven',
        enabled: true,
        state: 'failed',
        inputs: [
          {
            id: 'demo-search',
            state: 'failed',
            last_success: '2026-11-01T11:00:00Z',
            counts: {
              scope: 'committed_metadata_in_attempt',
              pages: 1,
              inspected: 24,
              known_eligible: null,
            },
            failure: { code: 'rate_limited', message: 'Waiting before the next check.' },
            retries: [
              {
                scope: 'discovery',
                attempts: 2,
                next_retry: '2026-11-01T13:00:00Z',
                failure: { code: 'rate_limited', message: 'Waiting before the next check.' },
              },
            ],
          },
        ],
      },
    ];
  const catalog = entries.map(({ provider, source_id, image_id }) => ({
    provider,
    source_id,
    image_id,
  }));
  const copy = <T>(value: T): T => structuredClone(value);
  const unrestricted = (): SourceImageRules => ({
    min_width: 1,
    min_height: 1,
    ratio: 'any',
    ratio_presets: [],
  });
  const sharedRules = (): SourceImageRules => {
    const { min_width, min_height, ratio, ratio_presets } = config.rules;
    return copy({ min_width, min_height, ratio, ratio_presets });
  };
  const sourceSettings = () => {
    for (const source of [...config.folders, ...config.searches]) {
      source.image_rules ??= null;
      source.effective_rules = copy(source.image_rules || sharedRules());
      source.rule_resolution = ['monitor', 'monitor-similar'].includes(source.effective_rules.ratio)
        ? { state: 'unavailable', reason: 'monitor_geometry_not_inspected' }
        : { state: 'resolved' };
    }
    for (const folder of config.folders)
      folder.effective_max_depth = folder.max_depth ?? config.rules.max_depth;
  };
  sourceSettings();
  const saved = () => {
    sourceSettings();
    config.revision = 'preview-' + ++sequence;
    return { configuration: copy(config) };
  };
  const currentSnapshot = () => {
    state.status!.setup_required = ![...config.folders, ...config.searches].some(
      (source) => source.enabled,
    );
    if (firstRun) {
      state.status!.instance_id = 'fixture-service';
      const folders = config.folders.filter((folder) => folder.enabled);
      state.status!.providers = folders.length
        ? [
            {
              id: 'local',
              enabled: true,
              state: 'ready',
              inputs: folders.map((folder) => ({ id: folder.id, state: 'ready' })),
            },
          ]
        : [];
    }
    const current = state.status!.current;
    const index = entries.findIndex((entry) => entry.id === current?.id);
    state.current = entries[index];
    state.status!.history_length = entries.length;
    state.status!.history_cursor = index < 0 ? -1 : entries.length - 1 - index;
    state.status!.previous_history = index < 0 ? undefined : entries[index + 1];
    state.status!.forward_history = index <= 0 ? undefined : entries[index - 1];
    state.status!.prepared_next_image_id =
      index > 0
        ? entries[index - 1].image_id
        : current
          ? current.image_id === 3
            ? 2
            : 3
          : undefined;
    state.status!.current_excluded = exclusions.some(
      (entry) => entry.image === String(current?.image_id),
    );
    state.status!.exclusion_undo_available = lastExcluded !== null;
    return copy(state);
  };
  const checkRevision = (revision: string) =>
    revision === config.revision
      ? null
      : { code: 'config_conflict', error: 'Settings changed. Reload and review your edits.' };
  const ruleEditError = (edit: { image_rules?: SourceImageRules; inherit_rules?: boolean }) =>
    edit.inherit_rules !== undefined && (!edit.inherit_rules || edit.image_rules !== undefined)
      ? { code: 'invalid_settings', error: 'Choose source rules or inheritance, not both.' }
      : null;
  const applyRules = (
    source: ConfiguredFolder | ConfiguredSearch,
    edit: { image_rules?: SourceImageRules; inherit_rules?: boolean },
  ) => {
    if (edit.inherit_rules) source.image_rules = null;
    else if (edit.image_rules) source.image_rules = copy(edit.image_rules);
  };
  const updateFolder = (edit: FolderEdit, adding: boolean) => {
    const conflict = checkRevision(edit.revision);
    if (conflict) return conflict;
    const rulesError = ruleEditError(edit);
    if (rulesError) return rulesError;
    let folder = config.folders.find((item) => item.id === edit.id);
    if (!folder) {
      if (!adding) return { code: 'folder_not_found', error: 'This folder is no longer saved.' };
      folder = {
        image_rules: edit.inherit_rules ? null : unrestricted(),
        id: 'folder-' + ++sequence,
        name: '',
        path: '',
        enabled: true,
        input_enabled: true,
        max_depth: null,
        effective_max_depth: config.rules.max_depth,
        refresh_interval: '5m',
      };
      config.folders.push(folder);
    }
    if (edit.path !== undefined) folder.path = edit.path;
    if (edit.name !== undefined) folder.name = edit.name;
    if (!folder.name) folder.name = folder.path.replace(/\/+$/, '').split('/').pop() || 'Pictures';
    if (edit.max_depth !== undefined)
      folder.max_depth = folder.effective_max_depth = edit.max_depth;
    if (edit.enabled !== undefined) folder.enabled = folder.input_enabled = edit.enabled;
    if (edit.refresh_interval !== undefined) folder.refresh_interval = edit.refresh_interval;
    applyRules(folder, edit);
    return saved();
  };
  const previewShape = new URLSearchParams(location.search).get('previewShape');
  const imageData = new Map<string, Promise<string>>();
  async function png(url: string) {
    if (!imageData.has(url))
      imageData.set(
        url,
        (async () => {
          const bytes = new Uint8Array(await (await fetch(url)).arrayBuffer());
          let text = '';
          for (let index = 0; index < bytes.length; index += 16384)
            text += String.fromCharCode(...bytes.subarray(index, index + 16384));
          return btoa(text);
        })(),
      );
    return imageData.get(url)!;
  }
  const client: Bridge = {
    BuildIdentity: async () => ({ development: true }),
    TakeAboutRequest: async () => false,
    Status: async () =>
      scenario === 'disconnected'
        ? {
            ...currentSnapshot(),
            connected: false,
            code: 'service_unavailable',
            error: 'Preview disconnected state.',
          }
        : currentSnapshot(),
    Configuration: async () => {
      sourceSettings();
      return { configuration: copy(config) };
    },
    Capabilities: async () => ({
      capabilities: {
        backend: 'macos',
        state: 'partial',
        backends: [
          { name: 'macos', ready: true },
          { name: 'omarchy', ready: false },
          { name: 'noctalia', ready: false },
        ],
        presentations: [
          { fit: 'auto', available: true, upscale: true },
          { fit: 'contain', available: true, upscale: true },
          { fit: 'crop', available: true, upscale: false },
        ],
        geometry: {
          state: 'known',
          displays: [{ name: 'Preview display', width: 3024, height: 1964, scale: 2 }],
        },
      },
    }),
    CacheStatus: async () => ({
      cache: {
        saved: { max_images: 20, max_bytes: 104857600 },
        effective: { max_images: 20, max_bytes: 104857600 },
        usage: {
          state: scenario === 'protected-cache' ? 'protected_over_budget' : 'known',
          images: 4,
          bytes: 120000000,
          protected_images: 2,
          protected_bytes: 110000000,
        },
      },
    }),
    CountSearchCandidates: async () => ({
      counts: {
        discovered: 480,
        eligible: 27,
        search_url: 'https://wallhaven.cc/search?q=nature&purity=100',
      },
    }),
    RulePreview: async (edit) => {
      const conflict = checkRevision(edit.revision);
      if (conflict) return conflict;
      if (Boolean(edit.provider_id) !== Boolean(edit.input_id))
        return {
          code: 'invalid_request',
          error: 'Choose both provider and source for a source preview.',
        };
      const scoped = Boolean(edit.provider_id);
      const inputs = [
        ...config.folders.map((source) => ({ provider: 'local', source })),
        ...config.searches.map((source) => ({ provider: 'wallhaven', source })),
      ].filter(
        ({ provider, source }) =>
          source.enabled &&
          (scoped
            ? provider === edit.provider_id && source.id === edit.input_id
            : source.image_rules == null),
      );
      // This browser fixture has metadata only. Never turn scripted sample rows
      // into verified eligible-image counts or run a second eligibility engine.
      const samples = catalog.filter((entry) =>
        inputs.some(
          ({ provider, source }) => entry.provider === provider && entry.source_id === source.id,
        ),
      );
      return {
        preview: {
          ...(scoped ? { provider_id: edit.provider_id, input_id: edit.input_id } : {}),
          scope: scoped ? 'source_catalog' : 'inherited_catalog',
          matched: 0,
          inspected: samples.length,
          rejected: 0,
          unknown: samples.length,
          complete: false,
          limit: 100,
          exclusions_applied: false,
          rule_resolution: ['monitor', 'monitor-similar'].includes(edit.rules.ratio)
            ? { state: 'resolved', ratio: '756:491' }
            : { state: 'resolved' },
          reason:
            scoped && !inputs.length ? 'source_not_enabled_or_enrolled' : 'fixture_metadata_only',
          reasons: {},
        },
      };
    },
    WindowPalette: async () => null,
    EditOmarchyTheme: async () => ({ error: 'Connect to a native Omarchy session.' }),
    OmarchyThemePreview: async () => ({}),
    ThemePalette: async () => ({ colors: ['#0066cc', '#66cccc', '#424153'] }),
    UpdateConfiguration: async (edit) => {
      const conflict = checkRevision(edit.revision);
      if (conflict) return conflict;
      if (edit.save_folder !== undefined) config.save_folder = edit.save_folder;
      for (const key of [
        'backend',
        'fit',
        'upscale',
        'interval',
        'rotation_mode',
        'cache_max_images',
        'cache_max_bytes',
      ] as const)
        if (edit[key] !== undefined) Object.assign(config.saved, { [key]: edit[key] });
      for (const key of [
        'min_width',
        'min_height',
        'max_depth',
        'ratio',
        'ratio_presets',
        'ratio_tolerance_pixels',
      ] as const)
        if (edit[key] !== undefined) Object.assign(config.rules, { [key]: edit[key] });
      config.effective = copy(config.saved);
      return saved();
    },
    PickFolder: async () => ({ name: 'Pictures', path: '/Demo/Pictures' }),
    PickSaveFolder: async () => ({ name: 'Favorites', path: '/Demo/Pictures/Favorites' }),
    SaveWallpaper: async (edit) => {
      const { current, instance_id } = state.status || {};
      if (
        instance_id !== edit.instance ||
        current?.id !== edit.history ||
        current.image_id !== edit.image_id
      )
        return {
          code: 'current_changed',
          error: 'The wallpaper changed. Review the current wallpaper and try again.',
        };
      const conflict = checkRevision(edit.revision);
      if (conflict) return conflict;
      const folder = edit.folder || config.save_folder;
      if (!folder) return { code: 'save_folder', error: 'Choose a folder for saved wallpapers.' };
      const key = JSON.stringify([folder, edit.image_id]);
      const already_saved = savedWallpapers.has(key);
      savedWallpapers.add(key);
      return { saved: true, already_saved, folder_name: folder.split('/').pop() };
    },
    AddFolderSettings: async (edit) => updateFolder(edit, true),
    UpdateFolder: async (edit) => updateFolder(edit, false),
    RemoveFolder: async (edit) => {
      const conflict = checkRevision(edit.revision);
      if (conflict) return conflict;
      config.folders = config.folders.filter((folder) => folder.id !== edit.id);
      return saved();
    },
    AddSearch: async (edit) => {
      const conflict = checkRevision(edit.revision);
      if (conflict) return conflict;
      const rulesError = ruleEditError(edit);
      if (rulesError) return rulesError;
      config.searches.push({
        image_rules: edit.inherit_rules ? null : unrestricted(),
        id: 'search-' + ++sequence,
        name: edit.name?.trim() || edit.query?.trim() || 'Wallhaven',
        custom_name: edit.name?.trim() || '',
        tags: edit.tags || [],
        tag_match: edit.tag_match ?? 'any',
        colors: edit.colors || [],
        query: edit.query || '',
        enabled: edit.enabled ?? true,
        input_enabled: edit.enabled ?? true,
        categories: edit.categories || '000',
        sorting: edit.sorting || 'relevance',
        order: edit.order || 'desc',
        max_pages: edit.max_pages || 2,
        top_range: edit.top_range || '1M',
        purity: '100',
        refresh_interval: edit.refresh_interval || '5m',
      });
      applyRules(config.searches.at(-1)!, edit);
      return saved();
    },
    UpdateSearch: async (edit) => {
      const conflict = checkRevision(edit.revision);
      if (conflict) return conflict;
      const rulesError = ruleEditError(edit);
      if (rulesError) return rulesError;
      const search = config.searches.find((item) => item.id === edit.id);
      if (search) {
        for (const key of [
          'tags',
          'tag_match',
          'colors',
          'query',
          'categories',
          'sorting',
          'order',
          'max_pages',
          'top_range',
          'refresh_interval',
        ] as const)
          if (edit[key] !== undefined) Object.assign(search, { [key]: edit[key] });
        if (edit.name !== undefined) search.custom_name = edit.name.trim();
        if (search.custom_name !== undefined)
          search.name = search.custom_name || search.query.trim() || 'Wallhaven';
        if (edit.enabled !== undefined) search.enabled = search.input_enabled = edit.enabled;
        applyRules(search, edit);
      } else return { code: 'search_not_found', error: 'This search is no longer saved.' };
      return saved();
    },
    AddProviderInput: async () => ({
      error: 'No external providers are installed in this preview.',
    }),
    UpdateProviderInput: async () => ({
      error: 'No external providers are installed in this preview.',
    }),
    RemoveProviderInput: async () => ({
      error: 'No external providers are installed in this preview.',
    }),
    SuggestProviderInput: async () => ({ values: [] }),
    RemoveSearch: async (edit) => {
      const conflict = checkRevision(edit.revision);
      if (conflict) return conflict;
      config.searches = config.searches.filter((search) => search.id !== edit.id);
      return saved();
    },
    Action: async (action) => {
      if (action === 'pause') state.status!.paused = true;
      if (action === 'resume') state.status!.paused = false;
      const index = entries.findIndex((entry) => entry.id === state.status!.current?.id);
      if (action === 'next' && index > 0) state.status!.current = entries[index - 1];
      if (action === 'next' && index <= 0) {
        const next: HistoryEntry = {
          ...{
            image_id: state.status!.current?.image_id === 3 ? 2 : 3,
            provider: 'local',
            original_path: '/Demo/Pictures/canyon.jpg',
            dimensions: { width: 640, height: 360 },
            availability: 'cached' as const,
          },
          id: 'accepted-' + ++sequence,
          accepted_at: '2026-11-01T13:00:00Z',
        };
        entries.unshift(next);
        state.status!.current = next;
      }
      if (action === 'previous' && index >= 0 && index + 1 < entries.length)
        state.status!.current = entries[index + 1];
      if (action === 'undo-exclude') {
        const index = exclusions.findIndex((entry) => entry.id === lastExcluded);
        if (index >= 0) exclusions.splice(index, 1);
        lastExcluded = null;
      }
      return currentSnapshot();
    },
    Navigate: async ({ current, instance, offset }) => {
      if (instance !== state.status?.instance_id || current !== (state.status?.current?.id ?? ''))
        return {
          ...currentSnapshot(),
          code: 'current_changed',
          error: 'The current image changed.',
        };
      const index = entries.findIndex((entry) => entry.id === current);
      const target = entries[index - offset];
      if (target) {
        state.status!.current = target;
        return currentSnapshot();
      }
      return offset > 0 ? client.Action('next') : currentSnapshot();
    },
    CancelNavigation: async () => false,
    ExcludeCurrent: async (instance, historyID, imageID) => {
      if (instance !== state.status!.instance_id) {
        return {
          ...currentSnapshot(),
          code: 'instance_changed',
          error: 'The Panora service changed.',
        };
      }
      const current = state.status!.current;
      const entry = entries.find((item) => item.id === historyID);
      if (!entry || current?.id !== historyID || current.image_id !== imageID) {
        return {
          ...currentSnapshot(),
          code: 'current_changed',
          error: 'The wallpaper changed before it could be excluded.',
        };
      }
      lastExcluded = ++sequence;
      exclusions.unshift({
        id: lastExcluded,
        image: String(entry.image_id),
        title: entry.attribution?.title || entry.original_path?.split('/').pop(),
        provider: entry.provider,
        excluded_at: '2026-11-01T13:00:00Z',
      });
      return currentSnapshot();
    },
    Preview: async (imageID) => ({
      preview: {
        image_id: imageID,
        available: true,
        mime_type: 'image/png',
        data: await png(
          imageID === 1
            ? left
            : imageID === 2
              ? previewShape === 'portrait'
                ? portrait
                : previewShape === 'square'
                  ? square
                  : center
              : right,
        ),
      },
    }),
    PreviewNeighbor: async (imageID, historyID) =>
      entries.some((entry) => entry.id === historyID && entry.image_id === imageID) &&
      !exclusions.some((entry) => entry.image === String(imageID))
        ? client.Preview(imageID)
        : { preview: { image_id: imageID, available: false, reason: 'history_unavailable' } },
    OpenCurrent: async () => ({
      error: 'Opening originals is disabled in the development preview.',
    }),
    OpenHistory: async () => ({
      error: 'Opening originals is disabled in the development preview.',
    }),
    History: async (before) => {
      const start = before ? entries.findIndex((entry) => entry.id === before) + 1 : 0;
      const page = entries.slice(start, start + 10).map((entry) => ({
        ...entry,
        availability: exclusions.some((mark) => mark.image === String(entry.image_id))
          ? 'unavailable'
          : 'cached',
        reason: exclusions.some((mark) => mark.image === String(entry.image_id)) ? 'excluded' : '',
      }));
      return {
        page: {
          entries: copy(page),
          next_before: entries.length > start + 10 ? page.at(-1)?.id : undefined,
        },
      };
    },
    ReplayHistory: async (id) => {
      state.status!.current = entries.find((entry) => entry.id === id) as Entry;
      return currentSnapshot();
    },
    Exclusions: async (before) => ({
      page: {
        entries: copy(exclusions.filter((entry) => !before || entry.id < before).slice(0, 10)),
      },
    }),
    RestoreExclusion: async (id) => {
      const index = exclusions.findIndex((entry) => entry.id === id);
      if (index >= 0) exclusions.splice(index, 1);
      if (lastExcluded === id) lastExcluded = null;
      return currentSnapshot();
    },
    ServiceState: async () => ({ service: { available: false } }),
    ServiceCommand: async () => ({
      service: { available: false },
      error: 'Service controls are disabled in the development preview.',
    }),
    OpenServiceLogs: async () => ({ error: 'Development preview has no service logs.' }),
    OpenConfiguration: async () => ({ error: 'The preview configuration is simulated.' }),
  };
  Object.defineProperty(window, 'go', { configurable: true, value: { main: { Client: client } } });
  return client;
}
