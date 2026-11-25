import type { PreviewResult } from './types';

export function previewImage(result: PreviewResult, imageID: number): string | null {
  const image = result.preview;
  return image?.image_id === imageID &&
    image.available &&
    image.mime_type === 'image/png' &&
    typeof image.data === 'string' &&
    image.data.length <= 2800000 &&
    /^[A-Za-z0-9+/]+={0,2}$/.test(image.data)
    ? 'data:image/png;base64,' + image.data
    : null;
}
