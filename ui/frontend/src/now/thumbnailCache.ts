import type { Configuration } from '../types';

const maxThumbnails = 6;

// Retain the loaded element so the webview can reuse its decoded backing pixels.
export class ThumbnailCache {
  constructor(readonly scope: string) {}

  private images = new Map<string, { src: string; image: HTMLImageElement }>();

  get(imageID: number | undefined, revision: string | undefined) {
    if (imageID && revision) return this.images.get(key(imageID, revision))?.src;
  }

  remember(imageID: number, revision: string | undefined, image: HTMLImageElement) {
    if (!revision) return;
    const identity = key(imageID, revision);
    this.images.delete(identity);
    this.images.set(identity, { src: image.src, image });
    if (this.images.size > maxThumbnails) {
      const oldest = this.images.keys().next();
      if (!oldest.done) this.images.delete(oldest.value);
    }
  }

  forget(imageID: number | undefined, revision: string | undefined) {
    if (imageID && revision) this.images.delete(key(imageID, revision));
  }
}

function key(imageID: number, revision: string) {
  return imageID + ':' + revision;
}

export function hasLiveAspectRule(config: Configuration | null | undefined) {
  if (!config) return true;
  const rules = [
    config.rules,
    config.omarchy_theme?.effective_rules,
    ...[...config.folders, ...config.searches, ...(config.inputs ?? [])].map(
      (input) => input.effective_rules ?? input.image_rules,
    ),
  ];
  return rules.some((rule) => rule?.ratio === 'monitor' || rule?.ratio === 'monitor-similar');
}
