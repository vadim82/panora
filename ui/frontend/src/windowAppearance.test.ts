import { describe, expect, it, vi } from 'vitest';
import { contrast, followWindowAppearance, windowColors } from './windowAppearance';

const ethereal = {
  background: '#060b1e',
  foreground: '#ffcead',
  accent: '#7d82d9',
  muted: '#6d7db6',
  selection: '#252e56',
  red: '#ed5b5a',
  yellow: '#e9bb4f',
};

const flexoki = {
  background: '#fffcf0',
  foreground: '#100f0f',
  accent: '#205ea6',
  muted: '#b7b5ac',
  selection: '#cecdc3',
  red: '#d14d41',
  yellow: '#d0a215',
};

describe('Omarchy window colors', () => {
  it.each([
    ['dark', ethereal],
    ['light', flexoki],
  ] as const)('keeps %s theme colors and checks the derived roles', (scheme, palette) => {
    const colors = windowColors(palette)!;
    expect(colors['color-scheme']).toBe(scheme);
    expect(colors['--bg']).toBe(palette.background);
    expect(colors['--ink']).toBe(palette.foreground);
    expect(colors['--accent']).toBe(palette.accent);
    expect(new Set([colors['--bg'], colors['--panel'], colors['--surface']]).size).toBe(3);
    for (const surface of [
      colors['--bg'],
      colors['--panel'],
      colors['--surface'],
      colors['--hover'],
    ]) {
      for (const token of ['--ink', '--muted', '--danger-ink'] as const)
        expect(contrast(colors[token], surface)).toBeGreaterThanOrEqual(4.5);
      for (const token of ['--line', '--accent', '--focus', '--status-offline'] as const)
        expect(contrast(colors[token], surface)).toBeGreaterThanOrEqual(3);
    }
    for (const token of ['--accent', '--accent-hover'] as const)
      expect(contrast(colors[token], colors['--accent-ink'])).toBeGreaterThanOrEqual(4.5);
    for (const token of ['--danger-bg', '--danger-hover'] as const)
      expect(contrast(colors[token], colors['--danger-button-ink'])).toBeGreaterThanOrEqual(4.5);
  });

  it('follows the built-in scheme when the theme is missing, malformed or unreadable', () => {
    expect(windowColors(null)).toBeUndefined();
    expect(windowColors({ ...ethereal, foreground: 'bogus' })).toBeUndefined();
    expect(windowColors({ ...ethereal, foreground: '#232735' })).toBeUndefined();
  });

  it('uses readable fallback roles for partial palettes', () => {
    const colors = windowColors({
      ...ethereal,
      accent: '#060b1e',
      red: '',
      muted: '#nothex',
      selection: '#070c20',
    })!;
    expect(colors['--accent']).toBe('#c3d2a9');
    expect(colors['--danger-bg']).toBe('#f4aa9f');
    expect(contrast(colors['--muted'], colors['--hover'])).toBeGreaterThanOrEqual(4.5);
  });

  it('updates and clears the window styles without replacing the focused edit', async () => {
    vi.useFakeTimers();
    const root = document.documentElement;
    const input = document.createElement('input');
    input.value = 'unsaved search';
    document.body.append(input);
    input.focus();
    const windowPalette = vi.fn().mockResolvedValue(ethereal);
    vi.stubGlobal('go', { main: { Client: { WindowPalette: windowPalette } } });
    try {
      followWindowAppearance();
      await vi.advanceTimersByTimeAsync(0);
      expect(root.style.getPropertyValue('--bg')).toBe(ethereal.background);
      windowPalette.mockResolvedValue(flexoki);
      await vi.advanceTimersByTimeAsync(1000);
      expect(root.style.getPropertyValue('--bg')).toBe(flexoki.background);
      expect(document.activeElement).toBe(input);
      expect(input.value).toBe('unsaved search');
      windowPalette.mockRejectedValue(new Error('unavailable'));
      await vi.advanceTimersByTimeAsync(1000);
      expect(root.style.getPropertyValue('--bg')).toBe('');
      expect(root.dataset.windowPalette).toBeUndefined();
    } finally {
      input.remove();
      vi.clearAllTimers();
      vi.useRealTimers();
      vi.unstubAllGlobals();
    }
  });
});
