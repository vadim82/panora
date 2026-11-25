import { describe, expect, it } from 'vitest';
import { controlColors } from './windowAppearance';

const paper = { ink: '#fffefa', surfaces: ['#f8f7f2', '#eeefe7', '#fffefa'] };
const midnight = { ink: '#26331d', surfaces: ['#1c221e', '#242c24', '#2a332b'] };

describe('window accent contrast', () => {
  it('admits both Ethereal and Tokyo Night in Midnight', () => {
    expect(controlColors('#7d82d9', midnight.ink, midnight.surfaces, true)).toEqual({
      accent: '#7d82d9',
      ink: '#151914',
      hover: '#878cdc',
    });
    expect(controlColors('#7aa2f7', midnight.ink, midnight.surfaces, true)).toEqual({
      accent: '#7aa2f7',
      ink: '#26331d',
      hover: '#85a9f8',
    });
  });

  it('uses a distinct readable hover in Paper', () => {
    expect(controlColors('#0066cc', paper.ink, paper.surfaces, false)).toEqual({
      accent: '#0066cc',
      ink: '#fffefa',
      hover: '#005ebc',
    });
  });

  it('falls back when the color is absent, malformed or hard to see', () => {
    expect(controlColors('', midnight.ink, midnight.surfaces, true)).toBeUndefined();
    expect(controlColors('#nothex', midnight.ink, midnight.surfaces, true)).toBeUndefined();
    expect(controlColors('#242c24', midnight.ink, midnight.surfaces, true)).toBeUndefined();
    expect(controlColors('#7aa2f7', paper.ink, paper.surfaces, false)).toBeUndefined();
  });
});
