// Rebase only untouched fields when the user explicitly reviews a newer revision.
export function preserveEdits<T extends object>(draft: T, baseline: T, saved: T): T {
  const next = { ...saved };
  for (const key of Object.keys(draft) as (keyof T)[]) {
    if (draft[key] !== baseline[key]) next[key] = draft[key];
  }
  return next;
}
export function folderName(path: string): string {
  return path.replace(/\/+$/, '').split('/').pop() || 'Wallpaper folder';
}
