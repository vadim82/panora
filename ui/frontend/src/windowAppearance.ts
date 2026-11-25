import { bridge } from './client';

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

const colors = {
  accent: '--window-primary',
  ink: '--window-primary-ink',
  hover: '--window-primary-hover',
};

const hexColor = /^#[\da-f]{6}$/i;

function channels(color: string) {
  return [1, 3, 5].map((offset) => Number.parseInt(color.slice(offset, offset + 2), 16));
}

function luminance(color: string) {
  const [red, green, blue] = channels(color).map((value) => {
    const channel = value / 255;
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
}

function contrast(first: string, second: string) {
  const a = luminance(first);
  const b = luminance(second);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

export function controlColors(
  accent: string,
  buttonInk: string,
  surfaces: string[],
  dark: boolean,
) {
  if (!hexColor.test(accent) || !surfaces.every((surface) => contrast(accent, surface) >= 3))
    return;

  // Use the base button label when readable, otherwise the opposite neutral.
  const ink = [buttonInk, '#151914', '#fffefa'].find(
    (candidate) => contrast(accent, candidate) >= 4.5,
  );
  if (!ink) return;

  // A slightly lighter Midnight fill and darker Paper fill keep hover distinct.
  const mixed =
    '#' +
    channels(accent)
      .map((value) =>
        Math.round(value + ((dark ? 255 : 0) - value) * 0.08)
          .toString(16)
          .padStart(2, '0'),
      )
      .join('');
  const hover =
    contrast(mixed, ink) >= 4.5 && surfaces.every((surface) => contrast(mixed, surface) >= 3)
      ? mixed
      : accent;
  return { accent, ink, hover };
}

export function followWindowAppearance() {
  const root = document.documentElement;
  const appearance = window.matchMedia('(prefers-color-scheme: dark)');
  let accent = '';
  let pending = false;
  let previous: ReturnType<typeof controlColors>;

  const apply = () => {
    const styles = getComputedStyle(root);
    const token = (name: string) => styles.getPropertyValue(name).trim();
    const next = controlColors(
      accent,
      token('--accent-ink'),
      ['--bg', '--panel', '--surface'].map(token),
      appearance.matches,
    );
    if (
      previous?.accent === next?.accent &&
      previous?.hover === next?.hover &&
      previous?.ink === next?.ink
    )
      return;
    for (const key of ['accent', 'ink', 'hover'] as const) {
      if (next) root.style.setProperty(colors[key], next[key]);
      else root.style.removeProperty(colors[key]);
    }
    previous = next;
  };

  const refresh = async () => {
    if (document.hidden || pending) return;
    pending = true;
    try {
      const current = await bridge('WindowAccent');
      if (current !== accent) {
        accent = current;
        apply();
      }
    } catch {
      if (accent) {
        accent = '';
        apply();
      }
    } finally {
      pending = false;
    }
  };

  appearance.addEventListener('change', apply);
  document.addEventListener('visibilitychange', refresh);
  window.setInterval(refresh, 1000);
  void refresh();
}
