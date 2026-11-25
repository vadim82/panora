import { useEffect, useRef, useState } from 'react';
import { Button } from '../components/ui';
import type { ProviderEditorDescription } from '../types';
import type { Source } from './sourceFacts';

export function AddSourceMenu({
  disabled,
  onChoose,
  canAddOmarchy = false,
  external = [],
}: {
  disabled: boolean;
  canAddOmarchy?: boolean;
  onChoose(kind: Source['kind'], providerID?: string): void;
  external?: ProviderEditorDescription[];
}) {
  const [open, setOpen] = useState(false);
  const menu = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (disabled) setOpen(false);
  }, [disabled]);

  useEffect(() => {
    if (!open || disabled) return;
    function dismiss(event: PointerEvent) {
      if (event.target instanceof Node && !menu.current?.contains(event.target)) setOpen(false);
    }
    document.addEventListener('pointerdown', dismiss, true);
    return () => document.removeEventListener('pointerdown', dismiss, true);
  }, [open, disabled]);

  function close() {
    setOpen(false);
    trigger.current?.focus();
  }

  function choose(kind: Source['kind'], providerID?: string) {
    close();
    onChoose(kind, providerID);
  }

  const expanded = open && !disabled;
  return (
    <div
      ref={menu}
      className="action-menu"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
      }}
      onKeyDown={(event) => {
        if (expanded && event.key === 'Escape') {
          event.preventDefault();
          close();
        }
      }}
    >
      <Button
        ref={trigger}
        id="source-add"
        variant="primary"
        disabled={disabled}
        aria-label="Add source options"
        aria-expanded={expanded}
        aria-controls="source-add-options"
        onClick={() => setOpen(!expanded)}
      >
        Add source <span className="source-add-caret" aria-hidden="true" />
      </Button>
      <div id="source-add-options" className="action-menu-items" hidden={!expanded}>
        <Button onClick={() => choose('local')}>Folder</Button>
        <Button onClick={() => choose('wallhaven')}>Wallhaven search</Button>
        {canAddOmarchy && <Button onClick={() => choose('omarchy')}>Omarchy theme</Button>}
        {external.map(({ id, name }) => (
          <Button key={id} onClick={() => choose('external', id)}>
            {name}
          </Button>
        ))}
      </div>
    </div>
  );
}
