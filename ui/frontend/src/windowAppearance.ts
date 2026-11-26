import { bridge } from './client';
import type { WindowPalette } from './types';

export async function isMacDesktop() {
  const runtime = (
    window as Window & {
      runtime?: { Environment(): Promise<{ platform: string }> };
    }
  ).runtime;
  try {
    return (await runtime?.Environment())?.platform === 'darwin';
  } catch {
    return false;
  }
}

const hexColor = /^#[\da-f]{6}$/i;

function channels(color: string) {
  return [1, 3, 5].map((offset) => Number.parseInt(color.slice(offset, offset + 2), 16));
}

function blend(from: string, to: string, amount: number) {
  const target = channels(to);
  return (
    '#' +
    channels(from)
      .map((value, index) =>
        Math.round(value + (target[index] - value) * amount)
          .toString(16)
          .padStart(2, '0'),
      )
      .join('')
  );
}

function luminance(color: string) {
  const [red, green, blue] = channels(color).map((value) => {
    const channel = value / 255;
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
}

export function contrast(first: string, second: string) {
  const a = luminance(first);
  const b = luminance(second);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

export function windowColors(palette: WindowPalette | null) {
  if (!palette) return;
  const { background, foreground, accent, muted, selection, red, yellow } = palette;
  if (!hexColor.test(background) || !hexColor.test(foreground)) return;
  if (contrast(background, foreground) < 4.5) return;

  const dark = luminance(background) < luminance(foreground);
  const neutral = dark ? '#000000' : '#ffffff';
  const readable = (color: string, surface: string, minimum: number) =>
    hexColor.test(color) && contrast(color, surface) >= minimum;
  const surfaceTint = (color: string, amount: number) => {
    const tint = blend(background, color, amount);
    return readable(foreground, tint, 4.5) ? tint : blend(background, neutral, amount);
  };
  const panel = surfaceTint(foreground, 0.06);
  const surface = surfaceTint(foreground, 0.1);
  const hover =
    hexColor.test(selection) &&
    readable(foreground, selection, 4.5) &&
    contrast(selection, background) >= 1.1 &&
    contrast(selection, background) <= 1.7
      ? selection
      : surfaceTint(foreground, 0.14);
  const surfaces = [background, panel, surface, hover];
  const onSurfaces = (color: string, minimum: number) =>
    surfaces.every((fill) => readable(color, fill, minimum));
  const inkTint = (candidate: string, minimum: number) => {
    if (onSurfaces(candidate, minimum)) return candidate;
    if (hexColor.test(candidate)) {
      for (const amount of [0.25, 0.5, 0.75]) {
        const tint = blend(candidate, foreground, amount);
        if (onSurfaces(tint, minimum)) return tint;
      }
    }
    return foreground;
  };
  const themeInk = (candidate: string, fallback: string, minimum: number) =>
    onSurfaces(candidate, minimum) ? candidate : inkTint(fallback, minimum);
  const primary = themeInk(accent, dark ? '#c3d2a9' : '#64764f', 3);
  const labelInk = (fill: string) => {
    if (readable(background, fill, 4.5)) return background;
    return readable('#000000', fill, 4.5) ? '#000000' : '#ffffff';
  };
  const primaryInk = labelInk(primary);
  const buttonHover = blend(primary, foreground, 0.15);
  const danger = themeInk(red, dark ? '#f4aa9f' : '#8b3531', 4.5);
  const dangerInk = labelInk(danger);
  const dangerHover = blend(danger, foreground, 0.15);
  const warning = themeInk(yellow, dark ? '#d6a46c' : '#a36d39', 3);
  const warningTint = surfaceTint(warning, 0.08);
  const mutedInk = inkTint(hexColor.test(muted) ? muted : blend(foreground, background, 0.25), 4.5);

  return {
    'color-scheme': dark ? 'dark' : 'light',
    '--bg': background,
    '--panel': panel,
    '--surface': surface,
    '--ink': foreground,
    '--muted': readable(mutedInk, warningTint, 4.5) ? mutedInk : foreground,
    '--line': inkTint(blend(background, foreground, 0.45), 3),
    '--hover': hover,
    '--accent': primary,
    '--accent-ink': primaryInk,
    '--accent-hover':
      onSurfaces(buttonHover, 3) && readable(primaryInk, buttonHover, 4.5) ? buttonHover : primary,
    '--focus': inkTint(primary, 4.5),
    '--status-offline': warning,
    '--warning-line': warning,
    '--warning-bg': warningTint,
    '--warning-tint': warningTint,
    '--danger-ink': danger,
    '--danger-bg': danger,
    '--danger-hover': readable(dangerInk, dangerHover, 4.5) ? dangerHover : danger,
    '--danger-button-ink': dangerInk,
    '--dialog-backdrop': dark ? '#000000cc' : '#00000066',
    '--dialog-shadow': '#00000066',
    '--shadow': dark ? '#00000055' : '#00000024',
    '--matte': background,
    '--preview-glow': panel,
    '--preview-ink': foreground,
    '--preview-muted': mutedInk,
  };
}

export function followWindowAppearance() {
  const root = document.documentElement;
  let pending = false;
  let previous: ReturnType<typeof windowColors>;
  let previousPalette: string | undefined;

  const readPalette = async () => bridge('WindowPalette');
  const refresh = async () => {
    if (document.hidden || pending) return;
    pending = true;
    try {
      // The native bridge can disappear independently of the playback connection.
      const palette = await readPalette().catch(() => null);
      const signature = JSON.stringify(palette);
      if (signature === previousPalette) return;
      previousPalette = signature;
      const next = windowColors(palette);
      if (next) {
        root.dataset.windowPalette = 'omarchy';
        for (const [name, value] of Object.entries(next)) root.style.setProperty(name, value);
      } else if (previous) {
        delete root.dataset.windowPalette;
        for (const name of Object.keys(previous)) root.style.removeProperty(name);
      }
      previous = next;
    } finally {
      pending = false;
    }
  };

  document.addEventListener('visibilitychange', refresh);
  window.setInterval(refresh, 1000);
  void refresh();
}
