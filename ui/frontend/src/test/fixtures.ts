import { vi } from 'vitest';
import type { Mock } from 'vitest';
import type { Bridge, Capabilities, Configuration, Snapshot } from '../types';

export const configuration: Configuration = {
  revision: 'reviewed',
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
  folders: [
    {
      id: 'folder-1',
      name: 'Wallpapers',
      path: '/Pictures/Wallpapers',
      enabled: true,
      input_enabled: true,
      max_depth: 2,
      effective_max_depth: 2,
      refresh_interval: '5m',
    },
  ],
  searches: [],
  restart_required: [],
  rules: { min_width: 1920, min_height: 1080, max_depth: 2, ratio: 'any', ratio_presets: [] },
  rule_resolution: { state: 'resolved' },
};
export const snapshot: Snapshot = {
  connected: true,
  status: {
    instance_id: 'test-service',
    paused: true,
    busy: false,
    uncertain: false,
    setup_required: false,
    current: { id: 'accepted-1', image_id: 1 },
    history_cursor: 0,
    history_length: 1,
    providers: [],
  },
  current: {
    id: 'accepted-1',
    image_id: 1,
    provider: 'local',
    original_path: '/Pictures/Wallpapers/coast.jpg',
    dimensions: { width: 1920, height: 1080 },
    availability: 'cached',
    accepted_at: '2026-11-01T12:00:00Z',
  },
};
export const capabilities: Capabilities = {
  backend: 'macos',
  state: 'partial',
  backends: [
    { name: 'macos', ready: true },
    { name: 'omarchy', ready: false },
  ],
  presentations: [
    { fit: 'auto', available: true, upscale: true },
    { fit: 'contain', available: true, upscale: true },
    { fit: 'crop', available: true, upscale: false },
  ],
  geometry: {
    state: 'known',
    displays: [{ name: 'Review display', width: 3024, height: 1964, scale: 2 }],
  },
};
export type BridgeMock = { [K in keyof Bridge]: Mock<Bridge[K]> };
export function installBridge() {
  const mock: BridgeMock = {
    BuildIdentity: vi.fn(async () => ({ development: true })),
    TakeAboutRequest: vi.fn(async () => false),
    Status: vi.fn(async () => structuredClone(snapshot)),
    Configuration: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    Capabilities: vi.fn(async () => ({ capabilities: structuredClone(capabilities) })),
    CacheStatus: vi.fn(async () => ({
      cache: {
        saved: { max_images: 20, max_bytes: 104857600 },
        effective: { max_images: 20, max_bytes: 104857600 },
        usage: { state: 'known', images: 1, bytes: 100, protected_images: 1, protected_bytes: 100 },
      },
    })),
    WindowAccent: vi.fn(async () => ''),
    EditOmarchyTheme: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    OmarchyThemePreview: vi.fn(async () => ({})),
    ThemePalette: vi.fn(async () => ({ colors: ['#0066cc', '#66cccc', '#424153'] })),
    RulePreview: vi.fn(async () => ({
      preview: {
        matched: 1,
        inspected: 1,
        rejected: 0,
        unknown: 0,
        complete: true,
        limit: 100,
        exclusions_applied: true,
        reasons: {},
        rule_resolution: { state: 'resolved' },
      },
    })),
    UpdateConfiguration: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    PickFolder: vi.fn(async () => ({ canceled: true })),
    AddFolderSettings: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    UpdateFolder: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    RemoveFolder: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    AddSearch: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    UpdateSearch: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    AddProviderInput: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    UpdateProviderInput: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    RemoveProviderInput: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    SuggestProviderInput: vi.fn(async () => ({ values: [] })),
    RemoveSearch: vi.fn(async () => ({ configuration: structuredClone(configuration) })),
    Action: vi.fn(async () => structuredClone(snapshot)),
    Navigate: vi.fn(async () => structuredClone(snapshot)),
    CancelNavigation: vi.fn(async () => true),
    ExcludeCurrent: vi.fn(async () => structuredClone(snapshot)),
    Preview: vi.fn(async (imageID) => ({
      preview: {
        image_id: imageID,
        available: true,
        mime_type: 'image/png',
        data: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB',
      },
    })),
    PreviewNeighbor: vi.fn<Bridge['PreviewNeighbor']>(async (imageID) => mock.Preview(imageID)),
    OpenCurrent: vi.fn(async () => ({ opened: true })),
    History: vi.fn(async () => ({ page: { entries: [structuredClone(snapshot.current!)] } })),
    ReplayHistory: vi.fn(async () => structuredClone(snapshot)),
    Exclusions: vi.fn(async () => ({ page: { entries: [] } })),
    RestoreExclusion: vi.fn(async () => structuredClone(snapshot)),
    OpenHistory: vi.fn(async () => ({ opened: true })),
    ServiceState: vi.fn(async () => ({ service: { available: false } })),
    ServiceCommand: vi.fn(async () => ({ service: { available: false } })),
    OpenServiceLogs: vi.fn(async () => ({ opened: true })),
    OpenConfiguration: vi.fn(async () => ({ opened: true })),
  };
  Object.defineProperty(window, 'go', { configurable: true, value: { main: { Client: mock } } });
  return mock;
}
