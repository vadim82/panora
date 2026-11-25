export function playbackWarnings(warnings: string[] = []): string[] {
  return warnings.filter(
    (warning) =>
      !warning.startsWith('Skipping ') &&
      !warning.includes('decoded image does not match current image rules') &&
      !warning.includes('image is excluded; restore it before playback'),
  );
}
