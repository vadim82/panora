export interface EditorCallbacks {
  onDirty(dirty: boolean): void;
  onCancel(): void;
  onSaved(id: string | undefined, notice: string, origin: Element | null): void;
}
